// Package agents knows the coding agents a directory can be opened in: what
// each is called, the command that starts it, and the kind herdr knows it by.
package agents

// Agent is one coding agent.
type Agent struct {
	// ID names it in the configuration.
	ID string
	// Name is how it is called on screen, in "Open in <Name>…".
	Name string
	// Command starts it, looked up on PATH.
	Command string
	// HerdrKind is the kind herdr starts and recognises it as.
	HerdrKind string
}

// All are the agents unagit can open, in the order they are offered.
var All = []Agent{
	{ID: "claude", Name: "Claude Code", Command: "claude", HerdrKind: "claude"},
	{ID: "codex", Name: "Codex", Command: "codex", HerdrKind: "codex"},
	{ID: "copilot", Name: "Copilot CLI", Command: "copilot", HerdrKind: "copilot"},
	{ID: "opencode", Name: "opencode", Command: "opencode", HerdrKind: "opencode"},
	{ID: "agy", Name: "Antigravity", Command: "agy", HerdrKind: "agy"},
}

// ByID finds an agent by its id.
func ByID(id string) (Agent, bool) {
	for _, a := range All {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}
