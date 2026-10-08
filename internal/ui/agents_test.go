package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
)

// fakeAgents puts stand-ins for the named agents, and for herdr when given,
// where this app looks for programs. Each agent writes down where it ran.
func fakeAgents(t *testing.T, herdr string, ids ...string) (string, func(*App)) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "ran")
	bins := map[string]string{}
	for _, id := range ids {
		ag, _ := agents.ByID(id)
		bin := filepath.Join(dir, ag.Command)
		must(t, os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s %s\\n' \""+id+"\" \"$PWD\" >> '"+log+"'\n"), 0o755))
		bins[ag.Command] = bin
	}
	if herdr != "" {
		bins["herdr"] = herdr
	}
	return log, func(a *App) {
		a.findExecutable = func(name string) (string, error) {
			if bin, ok := bins[name]; ok {
				return bin, nil
			}
			switch name {
			case "zoxide", "yazi", "herdr", "claude", "codex", "copilot", "opencode", "agy":
				return "", exec.ErrNotFound
			}
			return exec.LookPath(name)
		}
	}
}

func pickPlaceNamed(t *testing.T, a *App, sc tcell.SimulationScreen, agent, place string) {
	t.Helper()
	ag, _ := agents.ByID(agent)
	waitFor(t, a, sc, "Open in "+ag.Name+" · where")
	items := onLoop(a, func() []pickItem { return a.agentPlaces(ag) })
	at := -1
	for i, it := range items {
		if it.Label == place {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("%q not offered: %v", place, items)
	}
	waitFor(t, a, sc, "NORMAL")
	// The cursor starts on the place used last; k goes back to the top.
	typeRunes(sc, strings.Repeat("k", len(items)))
	if at > 0 {
		typeRunes(sc, strings.Repeat("j", at))
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

// TestAnAgentOpensWhereItIsAsked: Open in Claude Code… asks where, among
// the places this unagit has, and opens the directory there - the agent
// itself, not an editor - remembering the place for the next time.
func TestAnAgentOpensWhereItIsAsked(t *testing.T) {
	t.Parallel()
	tool, prepareMux := fakeMux(t)
	_, prepareAgents := fakeAgents(t, "", "claude", "codex")
	a, sc, _ := newTestAppSrv(t, prepareMux, prepareAgents)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")

	pickMuxAction(t, a, sc, "Open in Claude Code")
	waitFor(t, a, sc, "Open in Claude Code · where")
	for _, want := range []string{"This Terminal", "Zellij Tab", "Zellij Split Right", "Zellij Split Below"} {
		waitFor(t, a, sc, want)
	}
	if strings.Contains(a.screenText(sc), "herdr · Unagit Agents") {
		t.Fatal("herdr offered without herdr")
	}
	pickPlaceNamed(t, a, sc, "claude", "Zellij Split Right")
	waitEditorState(t, a, func() bool {
		return strings.Contains(a.transient, "opened Claude Code in Zellij: "+p.path)
	})
	var split []string
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-pane" {
			split = call.Args
		}
	}
	if len(split) == 0 || split[5] != "right" || split[7] != p.clone || !strings.HasSuffix(split[len(split)-1], "/claude") {
		t.Fatalf("split: %v", split)
	}
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].Editor != "claude" || rows[0].Mux != mux.Zellij {
		t.Fatalf("recorded: %+v", rows)
	}
	if got := onLoop(a, func() string { return a.cfg.Integrations.AgentPlace }); got != "zellij-right" {
		t.Fatalf("remembered place %q", got)
	}
	// The next agent starts on the place used last: Enter alone goes there.
	tool.SetPanes(t, nil)
	changeOnLoop(a, a.clearSaid)
	pickMuxAction(t, a, sc, "Open in Codex")
	waitFor(t, a, sc, "Open in Codex · where")
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitEditorState(t, a, func() bool {
		return strings.Contains(a.transient, "opened Codex in Zellij: "+p.path)
	})
}

// TestAnAgentStartsInAHerdrWorkspaceFromOutsideHerdr: from a plain
// terminal herdr offers a workspace of its own, and herdr itself starts
// the agent there so it knows what the agent is doing.
func TestAnAgentStartsInAHerdrWorkspaceFromOutsideHerdr(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	_, prepareAgents := fakeAgents(t, h.Binary, "claude")
	a, sc, _ := newTestAppSrv(t, prepareAgents, func(a *App) { a.findHerdr = func() *mux.Client { return h.HerdrClient(false) } })
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")

	pickMuxAction(t, a, sc, "Open in Claude Code")
	waitFor(t, a, sc, "herdr · Unagit Agents")
	if strings.Contains(a.screenText(sc), "herdr Tab") {
		t.Fatal("a herdr tab offered from outside herdr")
	}
	pickPlaceNamed(t, a, sc, "claude", "herdr · Unagit Agents")
	waitEditorState(t, a, func() bool {
		return strings.Contains(a.transient, "started Claude Code in herdr: "+p.path)
	})
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	var created, started, renamed []string
	for _, call := range calls {
		switch strings.Join(call.Args[:2], " ") {
		case "workspace create":
			created = call.Args
		case "agent start":
			started = call.Args
		case "tab rename":
			renamed = call.Args
		}
	}
	// The tab is named after what it opened.
	if !reflect.DeepEqual(renamed, []string{"tab", "rename", "wN:t1", "gateway"}) {
		t.Fatalf("tab: %v", renamed)
	}
	if !reflect.DeepEqual(created, []string{"workspace", "create", "--cwd", p.clone, "--label", "Unagit Agents", "--focus"}) {
		t.Fatalf("workspace: %v", created)
	}
	if len(started) != 7 || !strings.HasPrefix(started[2], "gateway-") || started[4] != "claude" || started[6] != "wN:p1" {
		t.Fatalf("agent start: %v", started)
	}
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].Editor != "claude" || rows[0].Mux != mux.Herdr || rows[0].Pane != "wN:p1" {
		t.Fatalf("recorded: %+v", rows)
	}
}

// TestAnAgentRunsInThisTerminal: the first place is unagit's own terminal,
// which the agent has until it ends, as a terminal editor does.
func TestAnAgentRunsInThisTerminal(t *testing.T) {
	t.Parallel()
	log, prepareAgents := fakeAgents(t, "", "opencode")
	a, sc, _ := newTestAppSrv(t, prepareAgents)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	pickMuxAction(t, a, sc, "Open in opencode")
	pickPlaceNamed(t, a, sc, "opencode", "This Terminal")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "opencode ended in") })
	ran, _ := os.ReadFile(log)
	real, _ := filepath.EvalSymlinks(p.clone)
	if got := strings.TrimSpace(string(ran)); got != "opencode "+p.clone && got != "opencode "+real {
		t.Fatalf("ran: %q", got)
	}
}

// TestAgentsAreIntegrationsOfTheirOwn: each agent has its card, and one
// turned off is not offered.
func TestAgentsAreIntegrationsOfTheirOwn(t *testing.T) {
	t.Parallel()
	_, prepareAgents := fakeAgents(t, "", "claude", "codex")
	a, sc, _ := newTestAppSrv(t, prepareAgents)
	waitFor(t, a, sc, "acme/gateway")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Open in Claude Code")
	text := a.screenText(sc)
	if strings.Contains(text, "Open in Copilot CLI") {
		t.Fatal("an agent that is not installed is offered")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Open in Claude Code")

	openSection(t, a, sc, sectionIntegrations)
	changeOnLoop(a, func() {
		v := a.settings.integrations
		for i, card := range v.cards {
			if card.name == "Claude Code" {
				v.current = i
			}
		}
		a.tv.SetFocus(v)
		v.paintFocus(true)
	})
	waitFor(t, a, sc, "Open in Claude Code… starts it")
	typeRunes(sc, "e")
	waitEditorState(t, a, func() bool {
		on, set := a.cfg.Integrations.Agents["claude"]
		return set && !on
	})
	saved, err := config.LoadFrom(a.cfg.Dir())
	must(t, err)
	if on, set := saved.Integrations.Agents["claude"]; !set || on {
		t.Fatalf("saved: %v", saved.Integrations.Agents)
	}
	typeRunes(sc, "1")
	waitFor(t, a, sc, "acme/gateway")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Open in Codex")
	if strings.Contains(a.screenText(sc), "Open in Claude Code") {
		t.Fatal("an agent turned off is offered")
	}
}

// TestThePlacePickerFits: the places, with their explanation, at several
// terminal sizes.
func TestThePlacePickerFits(t *testing.T) {
	t.Parallel()
	_, prepareMux := fakeMux(t)
	g := muxtest.NewGhostty(t)
	h := muxtest.NewHerdr(t)
	_, prepareAgents := fakeAgents(t, h.Binary, "claude")
	a, sc, _ := newTestAppSrv(t, prepareMux, prepareAgents, func(a *App) {
		a.findHerdr = func() *mux.Client { return h.HerdrClient(false) }
		a.findGhostty = func() *mux.Client { return g.GhosttyClient(false) }
	})
	waitFor(t, a, sc, "acme/gateway")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		pickMuxAction(t, a, sc, "Open in Claude Code")
		waitFor(t, a, sc, "Open in Claude Code · where")
		for _, want := range []string{"This Terminal", "Zellij Split Below", "herdr · Unagit Agents", "Ghostty Window", "Ghostty Tab", "Suspend unagit"} {
			waitFor(t, a, sc, want)
		}
		text := a.screenText(sc)
		t.Logf("Place picker at %dx%d:\n%s", size.w, size.h, text)
		frame := onLoop(a, func() rect {
			_, prim := a.pages.GetFrontPage()
			x, y, w, h := prim.(*modalBox).content.GetRect()
			return rect{x, y, w, h}
		})
		for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
			if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
				t.Fatalf("border drawn over on row %d:\n%s", y, text)
			}
		}
		assertLegible(t, a, sc, "place picker")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "Open in Claude Code · where")
	}
}
