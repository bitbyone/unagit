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
// cards, the profiles - and its cursor on the row.
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
		case "p:":
			for i, r := range v.profileRows {
				if r.key() == key {
					v.focus(v.profiles)
					v.profiles.Select(i+1, 0)
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
// state and resources, the environment beside them, the tasks of every
// sandbox, the profiles and the log; the detail follows the panel with the
// focus and says what unibox says of what it has chosen.
func TestTheSandboxesTabShowsItsPanels(t *testing.T) {
	t.Parallel()
	_, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "[5]")
	// Wide enough for the detail to stand beside the tasks by itself.
	resizeApp(a, sc, 180, 44)
	openSandboxes(t, a, sc)
	for _, want := range []string{"Sandboxes [B]", "Environment [E]", "Tasks [t]", "Profiles [P]", "Log [L]",
		"web-1 · context", "web-2", "web · sbx", "running", "no tasks", "stopped", "8GiB", "4 cpu", "2 tasks",
		"rate", "billing-5e6f7a8b", "working", "feat/rate", "billing, gateway", "fix/round",
		"broken", "does not load", "/envs", "web-1 · web", "3 secrets · 2 values"} {
		waitFor(t, a, sc, want)
	}
	text := a.screenText(sc)
	if lineOf(text, "web · sbx") > lineOf(text, "rate") || lineOf(text, "rate") > lineOf(text, "does not load") {
		t.Fatalf("not cards, then tasks, then profiles:\n%s", text)
	}
	// The profile's detail is read from unibox: its services, whose token
	// is stored and whose is not, and the value it asks for that is
	// missing.
	// The row says the default runtime's image; msb's, outdated, is
	// the detail's alone, or it reads as the profile's own.
	text = a.screenText(sc)
	if i := lineOf(text, "Claude Code, Copilot"); i < 0 || !strings.Contains(strings.Split(text, "\n")[i], "built") || strings.Contains(strings.Split(text, "\n")[i], "msb") {
		t.Fatalf("the row of web is not its sbx image:\n%s", text)
	}
	selectSandboxRow(t, a, "p:web")
	waitFor(t, a, sc, "msb outdated")
	waitFor(t, a, sc, "token missing")
	waitFor(t, a, sc, "jitpack.io")
	waitDetail(t, a, "jira_user")
	// l moves to the log as it does from every panel; signing the
	// agents in is A, and once took l away from moving.
	typeRunes(sc, "l")
	waitEditorState(t, a, func() bool { return a.tv.GetFocus() == a.sandboxes.log })
	selectSandboxRow(t, a, "s:web-1")
	waitFor(t, a, sc, "http://127.0.0.1:3000")
	waitDetail(t, a, "update-skills")
	selectSandboxRow(t, a, "t:billing-5e6f7a8b")
	waitFor(t, a, sc, "https://gl.test/acme/billing.git")
	typeRunes(sc, "E")
	waitFor(t, a, sc, "login keychain")
	waitFor(t, a, sc, "keeps running when idle")

	// The filter narrows the tasks.
	typeRunes(sc, "t/fix/round")
	waitGone(t, a, sc, "feat/rate")
	waitFor(t, a, sc, "billing-5e6f7a8b")
}

// TestWithoutUniboxTheSandboxesTabSaysSo: the tab is on the bar, grey,
// and going there says in the middle of it that unibox is not there and
// where it is turned on; Settings is S, at the bar's right end.
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
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitEditorState(t, a, func() bool { return a.currentTab() == pageSettings && a.settings.current == sectionIntegrations })
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
	// The log has both, and how each went.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "push them first")
	waitFor(t, a, sc, "Stopping web-1")
	waitFor(t, a, sc, "Removing web-1")
	got := onLoop(a, func() []string {
		var levels []string
		for _, e := range a.ubxLog {
			levels = append(levels, e.title+": "+e.level)
		}
		return levels
	})
	if !slices.Equal(got, []string{"Stopping web-1: done", "Removing web-1: refused"}) {
		t.Fatalf("log: %v", got)
	}
}

// TestSecretsAreStoredInTheTerminalAndListedByName: K lists the secrets
// by name; a new one is stored by unibox in this terminal, where it asks
// for the token itself, and d removes one after asking.
func TestSecretsAreStoredInTheTerminalAndListedByName(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"secret rm copilot --json": uniboxtest.OK("removed copilot"),
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	typeRunes(sc, "K")
	waitFor(t, a, sc, "Secrets · login keychain")
	waitFor(t, a, sc, "secret:github")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "a")
	waitFor(t, a, sc, "Store a secret")
	typeRunes(sc, "jfrog")
	pressButton(t, a, sc, frontForm(a), "Store")
	waitCalled(t, a, fake, "secret set jfrog")
	waitEditorState(t, a, func() bool { return strings.Contains(a.transient, "storing secret jfrog - done") })

	typeRunes(sc, "K")
	waitFor(t, a, sc, "secret:copilot")
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "j")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Remove secret copilot")
	typeRunes(sc, "y")
	waitCalled(t, a, fake, "secret rm copilot --json")
}

// TestAMissingValueIsSetForEveryProfile: V lists the values and what a
// profile asks for that is missing; Enter on that one sets it, for every
// profile unless chosen otherwise.
func TestAMissingValueIsSetForEveryProfile(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	resizeApp(a, sc, 140, 40)
	openSandboxes(t, a, sc)
	typeRunes(sc, "V")
	waitFor(t, a, sc, "Your values")
	for _, want := range []string{"git_name", "npm_scope", "profile web", "every profile", "jira_user", "missing"} {
		waitFor(t, a, sc, want)
	}
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Set jira_user")
	setField(t, a, frontForm(a), 0, "dev")
	pressButton(t, a, sc, frontForm(a), "Set")
	waitCalled(t, a, fake, "var set jira_user dev --json")
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
	waitFor(t, a, sc, "Default runtime")
	waitFor(t, a, sc, "msb stops when idle")
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel("msb stops when idle").(*tview.Checkbox).SetChecked(false)
		return true
	})
	pressButton(t, a, sc, form, "Save")
	waitCalled(t, a, fake, "config set runtimes.msb.stop_idle false --json")
	for _, call := range fake.Calls() {
		if strings.HasPrefix(call, "config set") && call != "config set runtimes.msb.stop_idle false --json" {
			t.Fatalf("set what did not change: %v", fake.Calls())
		}
	}
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
		waitFor(t, a, sc, "web-1")
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
			assertFormFits(t, a, sc, frontForm(a), "Default runtime", "sbx may be used", "msb stops when idle", "Save", "Cancel")
			assertLegible(t, a, sc, "unibox settings")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitGone(t, a, sc, "unibox settings")

			typeRunes(sc, "V")
			waitFor(t, a, sc, "jira_user")
			assertLegible(t, a, sc, "your values")
			waitFor(t, a, sc, "NORMAL")
			typeRunes(sc, "a")
			waitFor(t, a, sc, "New value")
			assertFormFits(t, a, sc, frontForm(a), "Name", "Value", "For", "every profile", "Set", "Cancel")
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
