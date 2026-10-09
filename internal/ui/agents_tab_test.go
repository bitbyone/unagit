package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
	"github.com/tobola/unagit/internal/session"
)

// agentsFixture is an app with a herdr and a Ghostty stand-in and two
// agents unagit started in herdr - one at work, one in the gateway's clone
// waiting for an answer - beside one herdr runs that unagit did not start.
// It returns the pane of the waiting one.
func agentsFixture(t *testing.T) (*App, tcell.SimulationScreen, muxtest.Stand, muxtest.Stand, *realProject, string) {
	t.Helper()
	h, g := muxtest.NewHerdr(t), muxtest.NewGhostty(t)
	_, prepareAgents := fakeAgents(t, h.Binary, "claude")
	a, sc, _ := newTestAppSrv(t, prepareAgents, func(a *App) {
		a.findHerdr = func() *mux.Client { return h.HerdrClient(false) }
		a.findGhostty = func() *mux.Client { return g.GhosttyClient(false) }
	})
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	c := h.HerdrClient(false)
	other := t.TempDir()
	var panes []string
	for i, r := range []session.Record{
		{Dir: p.clone, Project: p.path, Editor: "claude", Mode: session.ModeRepository},
		{Dir: other, Project: "acme/billing", Editor: "codex", Mode: session.ModeRepository},
		{Dir: t.TempDir()},
	} {
		pane, err := c.OpenShell(mux.Window, r.Dir, "x")
		must(t, err)
		panes = append(panes, pane)
		if i == 2 {
			break // herdr's own, which unagit did not start
		}
		r.Pane, r.Mux, r.MuxSession, r.MuxLauncher = pane, mux.Herdr, c.Session, c.Binary
		_, err = a.sessions.Add(r)
		must(t, err)
	}
	h.SetAgents(t, []map[string]any{
		{"agent": "codex", "agent_status": "working", "cwd": other, "pane_id": panes[1], "tab_id": "wN:t9", "workspace_id": "wN",
			"terminal_title_stripped": "Rewrite the parser"},
		{"agent": "claude", "agent_status": "blocked", "cwd": p.clone, "pane_id": panes[0], "tab_id": "wN:t1", "workspace_id": "wN",
			"terminal_title_stripped": "Fix the login"},
		{"agent": "claude", "agent_status": "idle", "cwd": "/elsewhere", "pane_id": panes[2], "tab_id": "wN:t9", "workspace_id": "wN",
			"terminal_title_stripped": "Somebody else's"},
	})
	return a, sc, h, g, p, panes[0]
}

// TestTheAgentsTabShowsWhatEachIsDoing: the agent waiting for an answer
// comes first and is counted on the tab, each says what it works on, and
// Enter goes to it - in herdr, and to herdr's Ghostty terminal.
func TestTheAgentsTabShowsWhatEachIsDoing(t *testing.T) {
	t.Parallel()
	a, sc, h, g, p, pane := agentsFixture(t)
	typeRunes(sc, "4")
	waitFor(t, a, sc, "Fix the login")
	waitFor(t, a, sc, "1 need you")
	waitTrue(t, "the tab does not count the waiting agent", func() bool { return tabBadge(a, sc, " 1 ") })
	lines := strings.Split(a.screenText(sc), "\n")
	login, parser := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "Claude Code") && strings.Contains(line, "waits") {
			login = i
			for _, want := range []string{"waits", "Claude Code", "acme/gateway"} {
				if !strings.Contains(line, want) {
					t.Fatalf("%q missing from %q", want, line)
				}
			}
		}
		if strings.Contains(line, "Codex") && strings.Contains(line, "working") {
			parser = i
			if !strings.Contains(line, "working") || !strings.Contains(line, "Codex") {
				t.Fatalf("working agent: %q", line)
			}
		}
	}
	if strings.Contains(a.screenText(sc), "Somebody else's") {
		t.Fatal("an agent unagit did not start is listed")
	}
	if login < 0 || parser < 0 || login > parser {
		t.Fatalf("the waiting agent is not first:\n%s", a.screenText(sc))
	}
	// Where it runs is in its detail, beside the list.
	waitFor(t, a, sc, "herdr · Unagit Agents")
	assertLegible(t, a, sc, "Activity with agents")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "went to Claude Code · "+p.path) })
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	focused := false
	for _, call := range calls {
		focused = focused || strings.Join(call.Args, " ") == "agent focus "+pane
	}
	if !focused {
		t.Fatal("herdr was not asked to focus the agent")
	}
	var shown []muxtest.GhosttyCall
	g.Calls(t, &shown)
	if len(shown) == 0 || shown[len(shown)-1].Argv[0] != ": Unagit Agents" {
		t.Fatalf("herdr's terminal was not brought forward: %+v", shown)
	}
}

// TestAnAgentIsClosedOnlyAfterAsking: x asks, and closing closes its pane.
func TestAnAgentIsClosedOnlyAfterAsking(t *testing.T) {
	t.Parallel()
	a, sc, h, _, _, pane := agentsFixture(t)
	typeRunes(sc, "4")
	waitFor(t, a, sc, "Fix the login")
	typeRunes(sc, "x")
	waitFor(t, a, sc, "Close Claude Code in")
	typeRunes(sc, "y")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "closed Claude Code") })
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	found := false
	for _, call := range calls {
		found = found || strings.Join(call.Args, " ") == "pane close "+pane
	}
	if !found {
		t.Fatalf("pane not closed: %v", calls)
	}
}

// TestRunningAgentsFromAnyScreen: Alt-A lists the agents over whatever is
// in front, and Enter goes to one.
func TestRunningAgentsFromAnyScreen(t *testing.T) {
	t.Parallel()
	a, sc, h, _, _, pane := agentsFixture(t)
	changeOnLoop(a, a.agentsNowAsk)
	waitEditorState(t, a, func() bool { return len(a.agentRows) == 2 })
	sc.InjectKey(tcell.KeyRune, 'a', tcell.ModAlt)
	waitFor(t, a, sc, "Running agents")
	waitFor(t, a, sc, "Rewrite the parser")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "went to Claude Code") })
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	found := false
	for _, call := range calls {
		found = found || strings.Join(call.Args, " ") == "agent focus "+pane
	}
	if !found {
		t.Fatal("the first agent - the waiting one - was not gone to")
	}
}

// TestAgentsElsewhereAreListedWithoutHerdr: what unagit started in Zellij
// is listed with where it runs, though nothing says what it is doing.
func TestAgentsElsewhereAreListedWithoutHerdr(t *testing.T) {
	t.Parallel()
	tool, prepareMux := fakeMux(t)
	a, sc, _ := newTestAppSrv(t, prepareMux)
	waitFor(t, a, sc, "acme/gateway")
	tool.SetPanes(t, []muxtest.Pane{{ID: 12}})
	_, err := a.sessions.Add(session.Record{Dir: t.TempDir(), Project: "acme/billing", Editor: "codex", Mode: session.ModeRepository,
		Pane: "terminal_12", Mux: mux.Zellij, MuxSession: "test-session", MuxLauncher: tool.Binary})
	must(t, err)
	typeRunes(sc, "4")
	waitFor(t, a, sc, "herdr is off")
	waitFor(t, a, sc, "acme/billing")
	text := a.screenText(sc)
	if !strings.Contains(text, "Codex") || !strings.Contains(text, "Zellij") {
		t.Fatalf("agent in Zellij:\n%s", text)
	}
}

// TestTheAgentsTabFits: the columns and the tab bar at several sizes.
func TestTheAgentsTabFits(t *testing.T) {
	t.Parallel()
	a, sc, _, _, _, _ := agentsFixture(t)
	typeRunes(sc, "4")
	waitFor(t, a, sc, "Fix the login")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "Settings")
		text := a.screenText(sc)
		t.Logf("Activity at %dx%d:\n%s", size.w, size.h, text)
		lines := strings.Split(text, "\n")
		if !strings.HasSuffix(strings.TrimSpace(lines[0]), "Settings") {
			t.Fatalf("the tab bar is cut at %d:\n%s", size.w, lines[0])
		}
		for _, line := range lines {
			if strings.Contains(line, "Fix the login") && !strings.HasSuffix(strings.TrimRight(line, " "), "│") {
				t.Fatalf("row over its frame: %q", line)
			}
		}
		assertLegible(t, a, sc, "Activity with agents")
	}
}
