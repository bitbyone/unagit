package unibox

import "fmt"

type Agent struct {
	ID, Command, Instructions, CoAuthor string
	// Config is the agent's JSON configuration in the guest home, the file a
	// profile's agent_config seed is merged into; empty when the agent keeps
	// no JSON configuration unibox knows how to merge.
	Config string
	Args   []string
	// Setup runs in the guest before every session of the agent, for what
	// depends on the sandbox's credentials rather than on the profile.
	Setup string
	// Env is set for the agent's process.
	Env []string
	// Trust is merged into Config before every session, naming the
	// directories the session works in, so the agent does not stop to ask
	// whether to trust what unibox prepared.
	Trust func(dirs []string) map[string]any
}

// claudeTrust is what sbx's own claude kit seeds: each directory's trust
// dialog answered, and the bypass mode unibox starts claude in accepted.
func claudeTrust(dirs []string) map[string]any {
	projects := map[string]any{}
	for _, d := range dirs {
		projects[d] = map[string]any{"hasTrustDialogAccepted": true}
	}
	return map[string]any{"bypassPermissionsModeAccepted": true, "projects": projects}
}

var Agents = []Agent{
	{ID: "claude", Command: "claude", Instructions: ".claude/CLAUDE.md", CoAuthor: "Claude <noreply@anthropic.com>", Config: ".claude.json", Args: []string{"--dangerously-skip-permissions"}, Trust: claudeTrust},
	{ID: "codex", Command: "codex", Instructions: ".codex/AGENTS.md", CoAuthor: "Codex <noreply@openai.com>", Args: []string{"--dangerously-bypass-approvals-and-sandbox"}, Setup: codexSetup},
	{ID: "copilot", Command: "copilot", Instructions: ".copilot/copilot-instructions.md", CoAuthor: "Copilot <noreply@github.com>", Config: ".copilot/config.json", Args: []string{"--allow-all"}, Env: []string{"COPILOT_ALLOW_ALL=true"}},
	{ID: "opencode", Command: "opencode", Instructions: ".config/opencode/AGENTS.md", CoAuthor: "opencode <noreply@opencode.ai>", Config: ".config/opencode/opencode.json"},
}

func FindAgent(id string) (Agent, error) {
	for _, a := range Agents {
		if a.ID == id {
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("unknown agent %q; choose one listed by unibox profiles", id)
}
