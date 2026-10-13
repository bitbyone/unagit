package unibox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/tobola/unagit/internal/unibox/api"
)

// sbx signs an agent in by what it stores for the agent's service: an OAuth
// login (only ChatGPT's can be stored locally), a key, or a command it runs
// on the host. Claude's OAuth is not stored: claude signs in inside the
// sandbox and the proxy keeps the token on the host.
var sbxAuthMethods = map[string][]string{
	"claude":  {AuthOAuth, AuthAPIKey, AuthCommand, AuthToken},
	"codex":   {AuthOAuth, AuthAPIKey, AuthCommand},
	"copilot": {AuthAPIKey, AuthCommand},
}

// sbxTokens are the agents that sign in by a long-lived token in an
// environment variable. sbx keeps the token as a custom secret: the sandbox
// sees a placeholder of the token's shape in the variable, and the proxy
// puts the token in its place on requests to the hosts. This is how claude
// keeps a subscription login beside codex's, since sbx wires one OAuth
// login per sandbox - and it outlives a recreated sandbox.
var sbxTokens = map[string]struct {
	Env, Placeholder string
	Hosts            []string
}{
	"claude": {"CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-{rand}", []string{"api.anthropic.com"}},
}

// storedLogins says, for each service with something stored for the
// sandbox, globally or for it alone, whether it is an OAuth login or a
// secret. sbx shows an OAuth login, masked, as a phrase about OAuth rather
// than a key; nothing else of the value is read or kept.
func storedLogins(ctx context.Context, sandbox string) (map[string]string, error) {
	out := map[string]string{}
	data, err := commandOutput(hostCommand(ctx, "sbx", "secret", "ls", "--json"))
	if err != nil {
		return out, err
	}
	var list struct {
		Secrets []struct{ Scope, Type, Name, Secret string }
		Custom  []map[string]any `json:"custom_secrets"`
	}
	if err = json.Unmarshal(data, &list); err != nil {
		return out, fmt.Errorf("sbx secret ls answered in a form unibox does not know; upgrade unibox")
	}
	// A custom secret is recorded by the variable it fills; only its
	// description is read, never a value.
	for _, c := range list.Custom {
		scope := fmt.Sprint(c["scope"])
		if scope != "global" && scope != sandbox && scope != "<nil>" && scope != "" {
			continue
		}
		for _, v := range c {
			if name, ok := v.(string); ok {
				out["env:"+name] = AuthToken
			}
		}
	}
	for _, sec := range list.Secrets {
		if sec.Type != "service" || (sec.Scope != "global" && sec.Scope != sandbox) {
			continue
		}
		if strings.Contains(strings.ToLower(sec.Secret), "oauth") {
			out[sec.Name] = AuthOAuth
		} else if out[sec.Name] == "" {
			out[sec.Name] = "secret"
		}
	}
	return out, nil
}

func (r *SBX) AuthNeeds(ctx context.Context, sandbox string, p Profile) []api.Need {
	needs := []api.Need{}
	oauth := []string{}
	for _, id := range p.Agents {
		if a, ok := p.Auth[id]; ok && a.Method == AuthOAuth {
			oauth = append(oauth, id)
		}
	}
	if len(oauth) > 1 && len(p.Agents) > 1 {
		needs = append(needs, api.Need{Code: "auth_conflict", Message: fmt.Sprintf("sbx signs one agent of a sandbox in by OAuth, and profile %s asks it for %s; give the others api-key or a command", p.Name, strings.Join(oauth, " and "))})
	}
	if len(p.Auth) == 0 {
		return needs
	}
	stored, err := storedLogins(ctx, sandbox)
	if err != nil {
		return append(needs, api.Need{Code: "runtime_unavailable", Message: err.Error()})
	}
	for _, id := range p.Agents {
		a, ok := p.Auth[id]
		if !ok {
			continue
		}
		if !slices.Contains(sbxAuthMethods[id], a.Method) {
			needs = append(needs, api.Need{Code: "auth_unsupported", Agent: id, Message: fmt.Sprintf("sbx cannot sign %s in by %s; choose %s", id, a.Method, strings.Join(sbxAuthMethods[id], ", "))})
			continue
		}
		service := sbxKitBindings[id].Service
		have := stored[service]
		missing := ""
		switch {
		case a.Method == AuthOAuth && id == "claude":
			// Nothing to store; claude asks once inside the sandbox.
		case a.Method == AuthOAuth && have != AuthOAuth:
			missing = "no " + service + " OAuth login is stored"
		case a.Method == AuthAPIKey && have != "secret":
			missing = "no " + service + " key is stored"
		case a.Method == AuthCommand && have == "":
			missing = "no " + service + " token command is stored"
		case a.Method == AuthToken && stored["env:"+sbxTokens[id].Env] != AuthToken:
			missing = "no token for " + sbxTokens[id].Env + " is stored"
		}
		if missing != "" {
			needs = append(needs, api.Need{Code: "login_required", Agent: id, Message: fmt.Sprintf("%s signs in by %s, but %s; %s", id, a.Method, missing, sbxLoginHint(id, service, a))})
		}
	}
	return needs
}

func sbxLoginArgs(agent, service string, a AgentAuth) []string {
	switch a.Method {
	case AuthToken:
		t := sbxTokens[agent]
		args := []string{"secret", "set-custom", "--env", t.Env, "--placeholder", t.Placeholder}
		for _, h := range t.Hosts {
			args = append(args, "--host", h)
		}
		return append(args, "--command", a.Command)
	case AuthOAuth:
		return []string{"secret", "set", service, "--oauth"}
	case AuthCommand:
		return []string{"secret", "set", service, "--command", a.Command}
	}
	return []string{"secret", "set", service}
}

func sbxLoginHint(agent, service string, a AgentAuth) string {
	args := sbxLoginArgs(agent, service, a)
	if a.Method == AuthCommand || a.Method == AuthToken {
		args[len(args)-1] = Quote(a.Command)
	}
	return "unibox run asks to store it, or run sbx " + strings.Join(args, " ")
}

// Login runs sbx's own question on the user's terminal: the browser for an
// OAuth login, a hidden prompt for a key. The value goes from the user to
// sbx's store and never through unibox.
func (r *SBX) Login(ctx context.Context, agent string, a AgentAuth, in io.Reader, out, errout io.Writer) error {
	b, ok := sbxKitBindings[agent]
	if !ok {
		return fmt.Errorf("sbx has no login for agent %s", agent)
	}
	cmd := hostCommand(ctx, "sbx", sbxLoginArgs(agent, b.Service, a)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sbx did not store the %s login: %w", b.Service, err)
	}
	return nil
}
