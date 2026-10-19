package ui

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

// fakeUnibox puts a unibox stand-in where this app looks for programs,
// answering with the overview's fixtures and whatever more is given.
func fakeUnibox(t *testing.T, more map[string]uniboxtest.Answer) (*uniboxtest.Fake, func(*App)) {
	t.Helper()
	answers := uniboxtest.Overview()
	for k, v := range more {
		answers[k] = v
	}
	fake := uniboxtest.Install(t, answers)
	return fake, func(a *App) {
		a.findExecutable = func(name string) (string, error) {
			if name == "unibox" {
				return fake.Path, nil
			}
			switch name {
			case "zoxide", "yazi", "herdr", "claude", "codex", "copilot", "opencode", "agy":
				return "", exec.ErrNotFound
			}
			return exec.LookPath(name)
		}
	}
}

// openSandboxes goes to the Sandboxes tab and waits for what unibox said.
func openSandboxes(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "6")
	waitFor(t, a, sc, "billing-5e6f7a8b")
}

// selectSandboxRow puts the cursor on the row of a key.
func selectSandboxRow(t *testing.T, a *App, key string) {
	t.Helper()
	found := onLoop(a, func() bool {
		p := a.sandboxesPane
		for row := 1; row < p.table.GetRowCount(); row++ {
			if i, ok := p.table.GetCell(row, 0).GetReference().(int); ok && a.sandboxRows[i].key() == key {
				p.table.Select(row, 0)
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("no row %s", key)
	}
	waitEditorState(t, a, func() bool {
		r, ok := a.selectedSandboxRow()
		return ok && r.key() == key
	})
}

// waitCalled waits for the stand-in to have been run with a command line.
func waitCalled(t *testing.T, a *App, fake *uniboxtest.Fake, line string) {
	t.Helper()
	waitEditorState(t, a, func() bool { return fake.Called(line) })
}

// TestTheSandboxesTabShowsProfilesSandboxesAndTasks: one tree, each
// profile with its sandboxes and each sandbox with its tasks, and the
// detail beside it with what unibox says of the row.
func TestTheSandboxesTabShowsProfilesSandboxesAndTasks(t *testing.T) {
	t.Parallel()
	_, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "[6]")
	// Wide enough for the detail to stand beside the tree by itself.
	resizeApp(a, sc, 180, 40)
	openSandboxes(t, a, sc)
	for _, want := range []string{"web-1", "web-2", "rate", "billing-5e6f7a8b", "broken", "does not load",
		"msb outdated", "working", "running", "stopped", "8GiB", "feat/rate", "billing, gateway", "fix/round"} {
		waitFor(t, a, sc, want)
	}
	text := a.screenText(sc)
	if lineOf(text, "web-1") > lineOf(text, "rate") || lineOf(text, "rate") > lineOf(text, "web-2") {
		t.Fatalf("not a tree:\n%s", text)
	}
	// The profile's detail is read from unibox: its services, whose token
	// is stored and whose is not.
	selectSandboxRow(t, a, "p:web")
	waitFor(t, a, sc, "token missing")
	waitFor(t, a, sc, "jitpack.io")
	selectSandboxRow(t, a, "s:web-1")
	waitFor(t, a, sc, "http://127.0.0.1:3000")
	waitFor(t, a, sc, "update-skills")
	selectSandboxRow(t, a, "t:billing-5e6f7a8b")
	waitFor(t, a, sc, "https://gl.test/acme/billing.git")

	// The filter keeps what a task is under.
	typeRunes(sc, "/fix/round")
	waitGone(t, a, sc, "web-2")
	waitFor(t, a, sc, "web-1")
	waitFor(t, a, sc, "billing-5e6f7a8b")
}

// TestWithoutUniboxThereIsNoSandboxesTab: a machine without unibox has no
// tab that would only say it is missing, and 6 does nothing.
func TestWithoutUniboxThereIsNoSandboxesTab(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "Settings [5]")
	if strings.Contains(a.screenText(sc), "[6]") {
		t.Fatal("a Sandboxes tab without unibox")
	}
	typeRunes(sc, "6")
	if got := onLoop(a, a.currentTab); got != pageProjects {
		t.Fatalf("6 went to %s", got)
	}
}

// TestATaskResumesInAZellijSplit: o asks where, the places of the
// multiplexer among them, and runs unibox resume there, written down with
// its sandbox and task so Activity lists the agent.
func TestATaskResumesInAZellijSplit(t *testing.T) {
	t.Parallel()
	tool, prepareMux := fakeMux(t)
	fake, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepareMux, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "t:gateway-1a2b3c4d")
	typeRunes(sc, "o")
	waitFor(t, a, sc, "Resume rate with Claude Code · where")
	for _, want := range []string{"This Terminal", "Zellij Tab", "Zellij Split Right"} {
		waitFor(t, a, sc, want)
	}
	pickPlace(t, a, sc, "claude", "Zellij Split Right")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "opened Claude Code in Zellij: web-1/rate") })
	var split []string
	for _, call := range tool.Calls(t) {
		if slices.Contains(call.Args, "new-pane") {
			split = call.Args
		}
	}
	if !slices.Contains(split, fake.Path) || strings.Join(split[len(split)-2:], " ") != "resume gateway-1a2b3c4d" {
		t.Fatalf("split: %v", split)
	}
	rows := a.sessions.List()
	if len(rows) != 1 || rows[0].Editor != "claude" || rows[0].Sandbox != "web-1" || rows[0].Task != "gateway-1a2b3c4d" {
		t.Fatalf("recorded: %+v", rows)
	}
}

// pickPlace chooses a place in the picker that asks where an agent goes.
func pickPlace(t *testing.T, a *App, sc tcell.SimulationScreen, agent, place string) {
	t.Helper()
	ag, _ := agents.ByID(agent)
	items := onLoop(a, func() []pickItem { return a.orderPlaces(placeOfAgent, a.agentPlaces(ag)) })
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
	typeRunes(sc, strings.Repeat("k", len(items)))
	if at > 0 {
		typeRunes(sc, strings.Repeat("j", at))
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

// TestATaskResumesWithAnotherAgentHere: a picks among the profile's
// agents, and in this terminal unibox resume has it until it ends.
func TestATaskResumesWithAnotherAgentHere(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "t:billing-5e6f7a8b")
	typeRunes(sc, "a")
	waitFor(t, a, sc, "Resume billing-5e6f7a8b with")
	waitFor(t, a, sc, "Copilot CLI")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "kk")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Resume billing-5e6f7a8b with Claude Code · where")
	pickPlace(t, a, sc, "claude", "This Terminal")
	waitCalled(t, a, fake, "resume billing-5e6f7a8b --agent claude")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "Claude Code ended in web-1/billing-5e6f7a8b") })
}

// TestASandboxIsStoppedAndARefusalIsAWarning: x stops the sandbox with
// unibox and says what unibox said; d asks, and unibox refusing to lose
// work not pushed is a warning naming what to do.
func TestASandboxIsStoppedAndARefusalIsAWarning(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"sandbox stop web-1 --json":     uniboxtest.OK("stopped web-1"),
		"sandbox rm web-1 --yes --json": uniboxtest.Failure("unpushed_work", "task rate has commits not pushed - push them first", 4),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "s:web-1")
	typeRunes(sc, "x")
	waitCalled(t, a, fake, "sandbox stop web-1 --json")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "stopped web-1") })

	selectSandboxRow(t, a, "s:web-1")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Remove sandbox web-1")
	waitFor(t, a, sc, "its tasks go with it")
	typeRunes(sc, "y")
	waitCalled(t, a, fake, "sandbox rm web-1 --yes --json")
	waitFor(t, a, sc, "push them first")
}

// TestANewTaskIsCheckedThenRun: n on a sandbox asks what to work on and
// with which agent, asks unibox what stands in the way, offers the push
// it names, and runs unibox run with it.
func TestANewTaskIsCheckedThenRun(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"task check * --json": uniboxtest.JSON(`{"api_version":1,"ready":false,"needs":[{"code":"branch_not_pushed","message":"the forge has no branch main - push it first","repository":"gateway"},{"code":"profile_changed","message":"the profile changed since the build","warning":true}],"repositories":[]}`),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "s:web-1")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New task · what to work on")
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway · which agent")
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway with Claude Code · where")
	pickPlace(t, a, sc, "claude", "This Terminal")
	waitFor(t, a, sc, "the forge has no branch main")
	waitFor(t, a, sc, "the profile changed since the build")
	keys := buttonKeys([]string{"Cancel", "Push and Start", "Start Anyway"})
	typeRunes(sc, string(keys[1]))
	run := "run " + p.clone + " -s web-1 -p web --agent claude --push"
	waitCalled(t, a, fake, run)
	if !fake.Called("task check " + p.clone + " -s web-1 -p web --agent claude --json") {
		t.Fatalf("not checked first: %v", fake.Calls())
	}
}

// TestTheSandboxesTabFitsEverySize: the tree and its detail stay inside
// their frames, every heading on screen, legible in the default theme.
func TestTheSandboxesTabFitsEverySize(t *testing.T) {
	t.Parallel()
	_, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "t:gateway-1a2b3c4d")
	for _, size := range [][2]int{{80, 24}, {120, 34}, {200, 50}} {
		resizeApp(a, sc, size[0], size[1])
		waitFor(t, a, sc, "NAME")
		waitFor(t, a, sc, "rate")
		text := a.screenText(sc)
		for _, line := range strings.Split(text, "\n") {
			if cells(line) > size[0] {
				t.Fatalf("%dx%d: a line wider than the terminal: %q", size[0], size[1], line)
			}
		}
		assertLegible(t, a, sc, "sandboxes")
	}
}
