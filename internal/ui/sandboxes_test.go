package ui

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

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
	typeRunes(sc, "5")
	waitFor(t, a, sc, "billing-5e6f7a8b")
}

// selectSandboxRow puts the focus on the panel of a key - the tasks, the
// cards, the envs - and its cursor on the row.
func selectSandboxRow(t *testing.T, a *App, key string) {
	t.Helper()
	found := onLoop(a, func() bool {
		v := a.sandboxes
		switch key[:2] {
		case "t:":
			for i, r := range v.tasks {
				if r.key() == key {
					v.focus(v.pane.table)
					v.pane.table.Select(i+1, 0)
					return true
				}
			}
		case "s:":
			for i, r := range v.boxes {
				if r.key() == key {
					v.cards.at = i
					v.focus(v.cards)
					return true
				}
			}
		case "e:":
			for i, r := range v.envRows {
				if r.key() == key {
					v.focus(v.envs)
					v.envs.Select(i+1, 0)
					return true
				}
			}
		}
		return false
	})
	if !found {
		t.Fatalf("no row %s", key)
	}
	waitEditorState(t, a, func() bool {
		r, ok := a.sandboxes.chosen()
		return ok && r.key() == key
	})
}

// waitDetail waits for the detail to say something, on screen or below
// what it shows.
func waitDetail(t *testing.T, a *App, text string) {
	t.Helper()
	waitEditorState(t, a, func() bool { return strings.Contains(a.sandboxesPane.detail.GetText(true), text) })
}

// waitCalled waits for the stand-in to have been run with a command line.
func waitCalled(t *testing.T, a *App, fake *uniboxtest.Fake, line string) {
	t.Helper()
	waitEditorState(t, a, func() bool { return fake.Called(line) })
}

// TestTheSandboxesTabShowsItsPanels: the sandboxes as cards with their
// state and tasks, the environment beside them, the tasks of every
// sandbox, the envs and the log; the detail follows the panel with the
// focus and says what unibox says of what it has chosen. The context is
// on the tab itself.
func TestTheSandboxesTabShowsItsPanels(t *testing.T) {
	t.Parallel()
	_, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "[5]")
	// Wide enough for the detail to stand beside the tasks by itself.
	resizeApp(a, sc, 180, 44)
	openSandboxes(t, a, sc)
	for _, want := range []string{"Sandboxes [B]", "Environment [E]", "Tasks [t]", "Envs [N]", "Log [L]",
		"web · context", "Claude Code, Copi", "running", "2 tasks",
		"rate", "billing-5e6f7a8b", "working", "feat/rate", "billing, gateway", "fix/round",
		"api-box", "1 value missing", "default env web", "2 tokens kept by sbx"} {
		waitFor(t, a, sc, want)
	}
	text := a.screenText(sc)
	if lineOf(text, "Claude Code, Copi") > lineOf(text, "rate") || lineOf(text, "rate") > lineOf(text, "1 value missing") {
		t.Fatalf("not cards, then tasks, then envs:\n%s", text)
	}
	if bar := strings.Split(text, "\n")[0]; !strings.Contains(bar, "Sandboxes [5]  "+glyphDot+"web") {
		t.Fatalf("the context is not on the tab: %q", bar)
	}
	// An env's detail is read from unibox: the token sbx lacks, its
	// repositories, the value it asks for that is missing.
	selectSandboxRow(t, a, "e:web")
	waitDetail(t, a, "artifactory")
	waitDetail(t, a, "adrs")
	selectSandboxRow(t, a, "e:api")
	waitDetail(t, a, "jira_user")
	waitDetail(t, a, "not made yet")
	// l moves to the log as it does from every panel.
	typeRunes(sc, "l")
	waitEditorState(t, a, func() bool { return a.tv.GetFocus() == a.sandboxes.log })
	selectSandboxRow(t, a, "s:web")
	waitFor(t, a, sc, "http://127.0.0.1:3000")
	waitDetail(t, a, "jitpack.io")
	waitDetail(t, a, "repo.example.com")
	selectSandboxRow(t, a, "t:gateway-1a2b3c4d")
	waitDetail(t, a, "mounted from /src/gateway")
	selectSandboxRow(t, a, "t:billing-5e6f7a8b")
	waitFor(t, a, sc, "https://gl.test/acme/billing.git")
	typeRunes(sc, "E")
	waitDetail(t, a, "off, not chosen")
	waitDetail(t, a, "anthropic")

	// The filter narrows the tasks.
	typeRunes(sc, "t/fix/round")
	waitGone(t, a, sc, "feat/rate")
	waitFor(t, a, sc, "billing-5e6f7a8b")
}

// TestWithoutUniboxTheSandboxesTabSaysSo: the tab is on the bar, grey,
// and going there says in the middle of it that unibox is not there, and
// Enter sets it up; Settings is S, at the bar's right end.
func TestWithoutUniboxTheSandboxesTabSaysSo(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "Sandboxes [5]")
	waitFor(t, a, sc, "Settings [S]")
	bar := strings.Split(a.screenText(sc), "\n")[0]
	if !strings.HasSuffix(strings.TrimRight(bar, " "), "Settings [S]") || strings.Index(bar, "Sandboxes [5]") > strings.Index(bar, "Settings [S]") {
		t.Fatalf("Settings is not at the right end: %q", bar)
	}
	typeRunes(sc, "5")
	waitFor(t, a, sc, "Sandboxes are not available")
	waitFor(t, a, sc, "unibox is not installed")
	text := a.screenText(sc)
	if line := lineOf(text, "Sandboxes are not available"); line < 6 {
		t.Fatalf("the word is not in the middle of the tab, at line %d:\n%s", line, text)
	}
	// Enter sets unibox up, from installing it.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Set up unibox")
	waitFor(t, a, sc, "Install unibox")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Install unibox")
	typeRunes(sc, "1")
	waitEditorState(t, a, func() bool { return a.currentTab() == pageProjects })
	typeRunes(sc, "S")
	waitEditorState(t, a, func() bool { return a.currentTab() == pageSettings })
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
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "opened Claude Code in Zellij: web/rate") })
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
	if len(rows) != 1 || rows[0].Editor != "claude" || rows[0].Sandbox != "web" || rows[0].Task != "gateway-1a2b3c4d" {
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

// TestATaskResumesWithAnotherAgentHere: a picks among the sandbox's
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
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "Claude Code ended in web/billing-5e6f7a8b") })
}

// TestASandboxIsStoppedAndARefusalIsAWarning: x stops the sandbox with
// unibox and says what unibox said; d asks, and unibox refusing to lose
// work not pushed is a warning naming what to do.
func TestASandboxIsStoppedAndARefusalIsAWarning(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"sandbox stop web --json":     uniboxtest.OK("stopped web"),
		"sandbox rm web --yes --json": uniboxtest.Failure("unpushed_work", "task rate has commits not pushed - push them first", 4),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "s:web")
	typeRunes(sc, "x")
	waitCalled(t, a, fake, "sandbox stop web --json")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "stopped web") })

	selectSandboxRow(t, a, "s:web")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Remove sandbox web")
	waitFor(t, a, sc, "its tasks go with it")
	keys := buttonKeys([]string{"Cancel", "Remove", "Force Remove"})
	typeRunes(sc, string(keys[1]))
	waitCalled(t, a, fake, "sandbox rm web --yes --json")
	waitFor(t, a, sc, "push them first")
	// The log has both, and how each went.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "push them first")
	waitFor(t, a, sc, "Stopping web")
	waitFor(t, a, sc, "Removing web")
	got := onLoop(a, func() []string {
		var levels []string
		for _, e := range a.ubxLog {
			levels = append(levels, e.title+": "+e.level)
		}
		return levels
	})
	if !slices.Equal(got, []string{"Stopping web: done", "Removing web: refused"}) {
		t.Fatalf("log: %v", got)
	}
}

// TestAnEnvsTokensAreStoredInTheTerminal: K on an env lists the tokens its
// sandboxes use and whether sbx has each; Enter has unibox ask for one in
// this terminal, d has sbx forget one after asking.
func TestAnEnvsTokensAreStoredInTheTerminal(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"secret rm anthropic -e web --scope global --json": uniboxtest.OK("removed anthropic"),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "e:web")
	typeRunes(sc, "K")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "artifactory")
	waitFor(t, a, sc, "missing")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitCalled(t, a, fake, "secret set artifactory -e web")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "storing the token of artifactory - done") })

	selectSandboxRow(t, a, "e:web")
	typeRunes(sc, "K")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "k")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "anthropic?")
	typeRunes(sc, string(buttonKeys([]string{"Cancel", "Remove"})[1]))
	waitCalled(t, a, fake, "secret rm anthropic -e web --scope global --json")
}

// TestAMissingValueIsSetForTheEnv: V on an env lists what it asks of you;
// Enter on a missing one sets it, for that env unless chosen otherwise.
func TestAMissingValueIsSetForTheEnv(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	resizeApp(a, sc, 140, 40)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "e:api")
	typeRunes(sc, "V")
	waitFor(t, a, sc, "Values of api")
	for _, want := range []string{"jira_user", "missing", "your Jira login"} {
		waitFor(t, a, sc, want)
	}
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Set jira_user")
	setField(t, a, frontForm(a), 0, "dev")
	pressButton(t, a, sc, frontForm(a), "Set")
	waitCalled(t, a, fake, "var set jira_user dev -e api --json")
}

// TestUniboxSettingsSetOnlyWhatChanged: U is unibox's configuration as a
// form, and saving it sets what was changed and nothing else.
func TestUniboxSettingsSetOnlyWhatChanged(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	typeRunes(sc, "U")
	waitFor(t, a, sc, "unibox settings")
	waitFor(t, a, sc, "Default env")
	waitFor(t, a, sc, "Stop a sandbox when idle")
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel("Stop a sandbox when idle").(*tview.Checkbox).SetChecked(true)
		return true
	})
	pressButton(t, a, sc, form, "Save")
	waitCalled(t, a, fake, "config set stop_idle true --json")
	for _, call := range fake.Calls() {
		if strings.HasPrefix(call, "config set") && call != "config set stop_idle true --json" {
			t.Fatalf("set what did not change: %v", fake.Calls())
		}
	}
}

// TestANewTaskIsCheckedThenRun: n on a sandbox asks what to work on and
// with which agent, asks unibox what stands in the way, offers the push
// it names, and runs unibox run with it - a clone mounted.
func TestANewTaskIsCheckedThenRun(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"task check * --json": uniboxtest.JSON(`{"api_version":2,"ready":false,"needs":[{"code":"branch_not_pushed","message":"the forge has no branch main - push it first","repository":"gateway"},{"code":"env_changed","message":"env web changed since sandbox web was created","warning":true}],"repositories":[]}`),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "s:web")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New task in web · what to work on")
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway · which agent")
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway with Claude Code · where")
	pickPlace(t, a, sc, "claude", "This Terminal")
	waitFor(t, a, sc, "push it first")
	waitFor(t, a, sc, "env web changed")
	// unibox run refuses a branch the forge lacks: no start without the push.
	if strings.Contains(a.screenText(sc), "Start Anyway") {
		t.Fatal("a start unibox would refuse is offered")
	}
	keys := buttonKeys([]string{"Cancel", "Push and Start"})
	typeRunes(sc, string(keys[1]))
	run := "run --repo " + p.clone + " -s web --push --agent claude"
	waitCalled(t, a, fake, run)
	if !fake.Called("task check --repo " + p.clone + " -s web --json") {
		t.Fatalf("not checked first: %v", fake.Calls())
	}
}

// TestTheSandboxesTabFitsEverySize: the panels and the detail stay inside
// their frames, every heading on screen, legible in the default theme.
func TestTheSandboxesTabFitsEverySize(t *testing.T) {
	t.Parallel()
	_, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	selectSandboxRow(t, a, "t:gateway-1a2b3c4d")
	for _, size := range [][2]int{{80, 24}, {120, 34}, {200, 50}} {
		resizeApp(a, sc, size[0], size[1])
		waitFor(t, a, sc, "TASK")
		waitFor(t, a, sc, "rate")
		waitFor(t, a, sc, "web")
		text := a.screenText(sc)
		for _, line := range strings.Split(text, "\n") {
			if cells(line) > size[0] {
				t.Fatalf("%dx%d: a line wider than the terminal: %q", size[0], size[1], line)
			}
		}
		assertLegible(t, a, sc, "sandboxes")
	}
}

// TestTheSandboxFormsFitTheirFrames: unibox's settings and a new value, at
// several sizes - every field and button inside the frame, its border whole
// on every row, every label on screen, legible.
func TestTheSandboxFormsFitTheirFrames(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{120, 34}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			_, prepare := fakeUnibox(t, nil)
			a, sc, _ := newTestAppSrv(t, prepare)
			resizeApp(a, sc, size.w, size.h)
			openSandboxes(t, a, sc)
			typeRunes(sc, "U")
			waitFor(t, a, sc, "unibox settings")
			assertFormFits(t, a, sc, frontForm(a), "Default env", "Stop a sandbox when idle", "Paste images from the clipboard", "Save", "Cancel")
			assertLegible(t, a, sc, "unibox settings")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitGone(t, a, sc, "unibox settings")

			selectSandboxRow(t, a, "e:api")
			typeRunes(sc, "V")
			waitFor(t, a, sc, "jira_user")
			assertLegible(t, a, sc, "your values")
			waitFor(t, a, sc, "NORMAL")
			typeRunes(sc, "a")
			waitFor(t, a, sc, "New value")
			assertFormFits(t, a, sc, frontForm(a), "Name", "Value", "For", "this env", "Set", "Cancel")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitGone(t, a, sc, "New value")

			selectSandboxRow(t, a, "s:web")
			typeRunes(sc, "M")
			waitFor(t, a, sc, "Resources of web")
			assertFormFits(t, a, sc, frontForm(a), "CPUs", "Memory", "Set", "Cancel")
			assertLegible(t, a, sc, "the resources")
			assertLegible(t, a, sc, "a new value")
		})
	}
}

// assertFormFits checks a form in its frame: its fields and buttons inside
// it, its right border on every row - a field too wide is drawn over it,
// whatever rectangle tview gave the field - and each of wants on screen.
func assertFormFits(t *testing.T, a *App, sc tcell.SimulationScreen, form *tview.Form, wants ...string) {
	t.Helper()
	type measured struct {
		inner, frame rect
		items        map[string]rect
	}
	m := onLoop(a, func() measured {
		var out measured
		x, y, w, h := form.GetInnerRect()
		out.inner = rect{x, y, w, h}
		x, y, w, h = form.GetRect()
		out.frame = rect{x, y, w, h}
		out.items = map[string]rect{}
		for i := 0; i < form.GetFormItemCount(); i++ {
			item := form.GetFormItem(i)
			x, y, w, h := item.GetRect()
			out.items[item.GetLabel()] = rect{x, y, w, h}
		}
		for i := 0; i < form.GetButtonCount(); i++ {
			b := form.GetButton(i)
			x, y, w, h := b.GetRect()
			out.items["button "+b.GetLabel()] = rect{x, y, w, h}
		}
		return out
	})
	text := a.screenText(sc)
	for label, r := range m.items {
		if !r.within(m.inner) {
			t.Errorf("%q is drawn at %v, outside the frame %v\n%s", label, r, m.inner, text)
		}
	}
	for y := m.frame.y + 1; y < m.frame.y+m.frame.h-1; y++ {
		if r, _ := cellAt(a, sc, m.frame.x+m.frame.w-1, y); r != '│' {
			t.Errorf("row %d: the frame's right border is missing (%q):\n%s", y, r, text)
			break
		}
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not on screen:\n%s", want, text)
		}
	}
}

// TestASandboxWithAnAgentAsksTheSandboxMostUsedFirst: Sandbox with
// Claude Code… on a repository asks which sandbox only when more than one
// runs it; Sandbox with Codex… does, the context's first, then the most
// used, an env's sandbox not made yet among them - and runs the agent
// there, the clone mounted.
func TestASandboxWithAnAgentAsksTheSandboxMostUsedFirst(t *testing.T) {
	t.Parallel()
	answers := map[string]uniboxtest.Answer{
		"task check * --json": uniboxtest.JSON(`{"api_version":2,"ready":true,"needs":[],"repositories":[]}`),
	}
	fake, prepare := fakeUnibox(t, answers)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	// Only web runs Claude Code: no question of which sandbox.
	pickMuxAction(t, a, sc, "Sandbox with Claude Code…")
	waitFor(t, a, sc, "acme/gateway with Claude Code in web · where")
	pickPlace(t, a, sc, "claude", "This Terminal")
	waitCalled(t, a, fake, "run --repo "+p.clone+" -s web --agent claude")

	// Codex is api's, whose sandbox is made on the way.
	pickMuxAction(t, a, sc, "Sandbox with Codex…")
	waitFor(t, a, sc, "acme/gateway with Codex in api-box · where")
	pickPlace(t, a, sc, "codex", "This Terminal")
	waitCalled(t, a, fake, "run --repo "+p.clone+" -s api-box -e api --agent codex")
}

// TestTheContextsSandboxIsOfferedWithoutAsking: the context's sandbox has
// actions of its own, which ask only where; a worktree goes in cloned.
func TestTheContextsSandboxIsOfferedWithoutAsking(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"task check * --json": uniboxtest.JSON(`{"api_version":2,"ready":true,"needs":[],"repositories":[]}`),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	gateway, billing := t.TempDir(), t.TempDir()
	waitFor(t, a, sc, "acme/gateway")
	openSandboxes(t, a, sc)
	acts := onLoop(a, func() []string {
		var names []string
		for _, act := range a.openInSandboxActions(func() sandboxTarget {
			return sandboxTarget{label: "feat", dirs: []string{gateway, billing}, clone: true}
		}, nil) {
			names = append(names, act.name)
		}
		return names
	})
	for _, want := range []string{"Open in Sandbox…", "Sandbox with Claude Code…", "Sandbox with Claude Code in web…", "Sandbox with Copilot CLI in web…", "Sandbox with Codex…"} {
		if !slices.Contains(acts, want) {
			t.Fatalf("%q is not offered: %v", want, acts)
		}
	}
	if slices.Contains(acts, "Sandbox with Codex in web…") {
		t.Fatalf("an agent the context's sandbox lacks is offered in it: %v", acts)
	}
	changeOnLoop(a, func() {
		for _, act := range a.openInSandboxActions(func() sandboxTarget {
			return sandboxTarget{label: "feat", dirs: []string{gateway, billing}, clone: true}
		}, nil) {
			if act.name == "Sandbox with Copilot CLI in web…" {
				act.run()
			}
		}
	})
	waitFor(t, a, sc, "feat with Copilot CLI in web · where")
	pickPlace(t, a, sc, "copilot", "This Terminal")
	waitCalled(t, a, fake, "run --repo "+gateway+" --repo "+billing+" -s web --clone --agent copilot")
}
