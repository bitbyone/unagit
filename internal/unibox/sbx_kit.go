package unibox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The sbx translations of the agents' credential needs, condensed from the
// official example mixins in github.com/docker/sandbox-kit-spec. A sandbox
// of several agents is created from a generated v3 workload kit carrying
// these, since v2 built-in kinds cannot be combined.
var sbxKitHosts = map[string][]string{
	"claude":  {"api.anthropic.com:443", "platform.claude.com:443", "claude.com:443"},
	"codex":   {"api.openai.com:443", "openai.com:443", "auth.openai.com:443", "chatgpt.com:443"},
	"copilot": {"api.github.com:443", "github.com:443", "api.githubcopilot.com:443", "copilot.github.com:443"},
}

var sbxKitCredentials = map[string]string{
	"claude": `  - type: com.docker.sandbox/credential@1
    optional: true
    description: Anthropic API access (API key or claude.ai OAuth)
    config:
      service: anthropic
      phase: runtime
      apiKey:
        name: ANTHROPIC_API_KEY
        inject:
          - {domain: api.anthropic.com, header: x-api-key, format: "%s"}
      oauth:
        tokenEndpoint: {host: platform.claude.com, path: /v1/oauth/token}
        sentinels:
          accessToken: sk-ant-oat01-proxy-managed
          refreshToken: sk-ant-ort01-proxy-managed
        credentialFile:
          path: ~/.claude/.credentials.json
          structure:
            claudeAiOauth:
              accessToken: "{{.AccessToken}}"
              refreshToken: "{{.RefreshToken}}"
              expiresAt: "{{.ExpiresAt}}"
              scopes: "{{.Scopes}}"
            primaryApiKey: "{{.PrimaryApiKey}}"
`,
	"codex": `  - type: com.docker.sandbox/credential@1
    optional: true
    description: OpenAI API access (API key or ChatGPT OAuth)
    config:
      service: openai
      phase: runtime
      apiKey:
        name: OPENAI_API_KEY
        proxyManaged: true
        inject:
          - {domain: api.openai.com, header: Authorization, format: "Bearer %s"}
      oauth:
        tokenEndpoint: {host: auth.openai.com, path: /oauth/token}
        resourceHosts:
          - chatgpt.com
        sentinels:
          accessToken: oai-oat01-proxy-managed
          refreshToken: oai-ort01-proxy-managed
`,
	"copilot": `  - type: com.docker.sandbox/credential@1
    optional: true
    description: GitHub access for copilot and gh
    config:
      service: github
      phase: runtime
      apiKey:
        name: GH_TOKEN
        proxyManaged: true
        inject:
          - {domain: api.github.com, header: Authorization, format: "Bearer %s"}
          - {domain: api.githubcopilot.com, header: Authorization, format: "Bearer %s"}
          - {domain: copilot.github.com, header: Authorization, format: "Bearer %s"}
`,
}

// sbxKitBindings are the approvals the declarations above need. sbx asks
// for them only when a third-party kit is run from a terminal, and unibox
// creates without one, so an unapproved credential was quietly withheld and
// the agent asked for a login. A single agent's built-in kit needs no
// approval; these give the generated kit exactly what the built-in ones get:
// each service with the domains its declaration names, nothing wider.
type sbxBinding struct {
	Service       string
	APIKey, OAuth []string
}

var sbxKitBindings = map[string]sbxBinding{
	"claude":  {"anthropic", []string{"api.anthropic.com"}, []string{"platform.claude.com"}},
	"codex":   {"openai", []string{"api.openai.com"}, []string{"auth.openai.com", "chatgpt.com"}},
	"copilot": {"github", []string{"api.github.com", "api.githubcopilot.com", "copilot.github.com"}, nil},
}

// writeBindings adds the agents' approvals to sbx's credentials.yaml. It
// only adds - a domain the user approved stays - and holds no secret.
func writeBindings(file string, agents []string, log io.Writer) error {
	if file == "" {
		return nil
	}
	doc := map[string]any{}
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("sbx credential bindings in %s are not valid YAML; repair the file: %w", file, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	bindings, _ := doc["bindings"].(map[string]any)
	if bindings == nil {
		bindings = map[string]any{}
	}
	added := []string{}
	for _, id := range agents {
		want, ok := sbxKitBindings[id]
		if !ok {
			continue
		}
		service, _ := bindings[want.Service].(map[string]any)
		if service == nil {
			service = map[string]any{}
		}
		for mechanism, domains := range map[string][]string{"apiKey": want.APIKey, "oauth": want.OAuth} {
			if len(domains) == 0 {
				continue
			}
			entry, _ := service[mechanism].(map[string]any)
			if entry == nil {
				entry = map[string]any{}
			}
			have := []any{}
			if list, ok := entry["domains"].([]any); ok {
				have = list
			}
			for _, d := range domains {
				if !slices.Contains(have, any(d)) {
					have = append(have, d)
					added = append(added, want.Service+" "+mechanism+" "+d)
				}
			}
			entry["domains"] = have
			service[mechanism] = entry
		}
		bindings[want.Service] = service
	}
	if len(added) == 0 {
		return nil
	}
	doc["bindings"] = bindings
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err = enc.Encode(doc); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	if err = AtomicWrite(file, out.Bytes(), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(log, "approved sbx credentials for the profile's agents: %s\n", strings.Join(added, ", "))
	return nil
}

// sbx keeps one OAuth policy per sandbox and takes it from the first
// credential that declares OAuth, whether or not a login for it is stored.
// Listed in the profile's order, Claude's came first, no Anthropic login
// was stored, and the stored ChatGPT login was never wired - codex asked to
// sign in. So the agents whose service has a stored OAuth login go first.
func credentialOrder(agents []string, oauth map[string]bool) []string {
	out := slices.Clone(agents)
	slices.SortStableFunc(out, func(a, b string) int {
		x, y := oauth[sbxKitBindings[a].Service], oauth[sbxKitBindings[b].Service]
		switch {
		case x && !y:
			return -1
		case y && !x:
			return 1
		}
		return 0
	})
	return out
}

// oauthLogins names the services with an OAuth login stored for the
// sandbox, globally or for it alone. sbx shows such a login, masked, as a
// phrase about OAuth rather than a key; nothing else of the value is read
// or kept. When sbx cannot say, the profile's order stands.
func oauthLogins(ctx context.Context, sandbox string) map[string]bool {
	out := map[string]bool{}
	data, err := commandOutput(hostCommand(ctx, "sbx", "secret", "ls", "--json"))
	if err != nil {
		return out
	}
	var list struct {
		Secrets []struct{ Scope, Type, Name, Secret string }
	}
	if json.Unmarshal(data, &list) != nil {
		return out
	}
	for _, sec := range list.Secrets {
		if sec.Type == "service" && (sec.Scope == "global" || sec.Scope == sandbox) && strings.Contains(strings.ToLower(sec.Secret), "oauth") {
			out[sec.Name] = true
		}
	}
	return out
}

// writeKit lays the generated workload kit for a sandbox into the adapter's
// cache, where it stays for inspection: regenerated on every create, never
// edited. The descriptor carries the agents' credential declarations; the
// Dockerfile is one FROM of the profile's built image.
func (r *SBX) writeKit(s Spec, oauth map[string]bool) (string, error) {
	dir := filepath.Join(r.Cache, "kits", s.Name)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	hosts := map[string]bool{}
	credentials := []string{}
	for _, id := range credentialOrder(s.Agents, oauth) {
		for _, h := range sbxKitHosts[id] {
			hosts[h] = true
		}
		if c, ok := sbxKitCredentials[id]; ok {
			credentials = append(credentials, c)
		}
	}
	allow := []string{}
	for h := range hosts {
		allow = append(allow, h)
	}
	sort.Strings(allow)
	var b strings.Builder
	b.WriteString("# syntax=docker/sandbox-kit:3\n# Generated by unibox from the profile; do not edit - it is rewritten\n# on every create. Inspect it with unibox inspect.\nschemaVersion: \"3\"\ndisplayName: unibox " + s.Name + "\ndescription: unibox sandbox workload with the profile's agents\nkind: workload\n\ncapabilities:\n")
	if len(allow) > 0 {
		b.WriteString("  - type: com.docker.sandbox/network-policy@1\n    config:\n      runtime:\n        allow:\n")
		for _, h := range allow {
			b.WriteString("          - " + h + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(credentials, "\n"))
	if err := AtomicWrite(filepath.Join(dir, s.Name+".yaml"), []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	if err := AtomicWrite(filepath.Join(dir, s.Name+".dockerfile"), []byte("FROM "+s.Image+"\n"), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

// KitDir says where the generated kit of a runtime sandbox lives, for
// inspection; empty when the sandbox is not kit-made.
func (r *SBX) KitDir(name string) string {
	dir := filepath.Join(r.Cache, "kits", name)
	if _, err := os.Stat(filepath.Join(dir, name+".yaml")); err != nil {
		return ""
	}
	return dir
}
