package unibox

import "fmt"

type Agent struct {
	ID, Command, Instructions, CoAuthor string
	Args                                []string
}

var Agents = []Agent{
	{ID: "claude", Command: "claude", Instructions: ".claude/CLAUDE.md", CoAuthor: "Claude <noreply@anthropic.com>", Args: []string{"--dangerously-skip-permissions"}},
	{ID: "codex", Command: "codex", Instructions: ".codex/AGENTS.md", CoAuthor: "Codex <noreply@openai.com>", Args: []string{"--dangerously-bypass-approvals-and-sandbox"}},
	{ID: "copilot", Command: "copilot", Instructions: ".copilot/copilot-instructions.md", CoAuthor: "Copilot <noreply@github.com>", Args: []string{"--allow-all"}},
	{ID: "opencode", Command: "opencode", Instructions: ".config/opencode/AGENTS.md", CoAuthor: "opencode <noreply@opencode.ai>"},
}

func FindAgent(id string) (Agent, error) {
	for _, a := range Agents {
		if a.ID == id {
			return a, nil
		}
	}
	return Agent{}, fmt.Errorf("unknown agent %q; choose one listed by unibox profiles", id)
}
