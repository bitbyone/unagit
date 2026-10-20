package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

// setupFixture is unibox with its runtime not signed in and claude's
// login missing from profile web.
func setupFixture(t *testing.T) (*App, tcell.SimulationScreen, *uniboxtest.Fake) {
	t.Helper()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"runtime ls --json":   uniboxtest.JSON(`{"api_version":1,"runtimes":[{"name":"sbx","available":false,"enabled":true,"default":true,"error":"/opt/homebrew/bin/sbx failed: error: not signed in to Docker\n  try: sbx login"},{"name":"msb","available":true,"enabled":true,"experimental":true}]}`),
		"task check * --json": uniboxtest.JSON(`{"api_version":1,"ready":false,"needs":[{"code":"login_required","message":"claude signs in by token, but no token is stored","agent":"claude"},{"code":"branch_not_pushed","message":"push it first","repository":"envs"}],"repositories":[]}`),
		"init * --yes":        {Out: "laid out\n"},
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	return a, sc, fake
}

// doSetupStep moves the guide's cursor to the step named so and does it.
func doSetupStep(t *testing.T, a *App, sc tcell.SimulationScreen, name string) {
	t.Helper()
	waitFor(t, a, sc, "NORMAL")
	at := onLoop(a, func() int {
		for i, s := range a.setupSteps(a.ubx.overview, nil) {
			if s.name == name {
				return i
			}
		}
		return -1
	})
	if at < 0 {
		t.Fatalf("no step %q", name)
	}
	// The cursor may start where the list was left; k goes to the top.
	typeRunes(sc, strings.Repeat("k", 20)+strings.Repeat("j", at))
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
}

// TestTheSetupSaysWhereUniboxStands: I lists every step with how it
// stands - the runtime not signed in, with the command that fixes it, a
// login a profile lacks, a profile that does not load - and a need of the
// repository the check was given is left out; Enter on a profile's need
// goes to what does it.
func TestTheSetupSaysWhereUniboxStands(t *testing.T) {
	t.Parallel()
	a, sc, fake := setupFixture(t)
	resizeApp(a, sc, 140, 44)
	openSandboxes(t, a, sc)
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up unibox")
	for _, want := range []string{"unibox", "installed 0.3", "Sandboxes tab", "Runtime sbx", "not ready", "Environments repository", "/envs",
		"Profiles", "web, broken", "Image of web", "built", "web ready to start", "1 thing to do", "Profile broken", "does not load"} {
		waitFor(t, a, sc, want)
	}
	if !fake.Called("task check /envs -p web --agent claude --json") {
		t.Fatalf("the profile's agents are not checked: %v", fake.Calls())
	}
	doSetupStep(t, a, sc, "Runtime sbx")
	// sbx login is not here: what matters is that it was tried.
	waitFor(t, a, sc, "sbx login did not finish")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up unibox")
	doSetupStep(t, a, sc, "web ready to start")
	waitFor(t, a, sc, "Sign in · web")
}

// TestInitUniboxHereLaysACloneOut: Init unibox Here on a clone asks
// first while another repository holds the profiles, then lays it out.
func TestInitUniboxHereLaysACloneOut(t *testing.T) {
	t.Parallel()
	a, sc, fake := setupFixture(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	pickMuxAction(t, a, sc, "Init unibox Here")
	waitFor(t, a, sc, "unibox keeps its profiles in /envs")
	typeRunes(sc, "y")
	waitCalled(t, a, fake, "init --dir "+p.clone+" --yes")
}

// TestANewProfileIsAdded: the guide's Profiles step adds one, with the
// agents ticked and its resources.
func TestANewProfileIsAdded(t *testing.T) {
	t.Parallel()
	a, sc, fake := setupFixture(t)
	resizeApp(a, sc, 140, 44)
	openSandboxes(t, a, sc)
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up unibox")
	doSetupStep(t, a, sc, "Profiles")
	waitFor(t, a, sc, "Add a unibox profile")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "New unibox profile")
	form := frontForm(a)
	setField(t, a, form, 0, "api")
	pressButton(t, a, sc, form, "Add")
	waitCalled(t, a, fake, "init api --cpus 4 --memory 8g --agents claude --yes")
}

// TestTheNewProfileFormFitsItsFrame: at several sizes.
func TestTheNewProfileFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{120, 34}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := setupFixture(t)
			resizeApp(a, sc, size.w, size.h)
			openSandboxes(t, a, sc)
			typeRunes(sc, "I")
			waitFor(t, a, sc, "Set up unibox")
			assertLegible(t, a, sc, "the setup")
			doSetupStep(t, a, sc, "Profiles")
			waitFor(t, a, sc, "Add a unibox profile")
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			waitFor(t, a, sc, "New unibox profile")
			assertFormFits(t, a, sc, frontForm(a), "Name", "Claude Code", "Codex", "Copilot CLI", "CPUs", "Memory", "Template", "Add", "Cancel")
			assertLegible(t, a, sc, "a new profile")
		})
	}
}

// TestANewProfileIsCopiedFromATemplateRepository: the guide's Profiles
// step offers a template; the repositories are listed, a clone's template
// read with unibox template inspect, its profiles shown with what they
// bring, and the one picked copied with init --from under a name of its
// own when the repository has one of the template's.
func TestANewProfileIsCopiedFromATemplateRepository(t *testing.T) {
	t.Parallel()
	fake, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{
		"runtime ls --json":         uniboxtest.JSON(`{"api_version":1,"runtimes":[{"name":"sbx","available":true,"enabled":true,"default":true}]}`),
		"task check * --json":       uniboxtest.JSON(`{"api_version":1,"ready":true,"needs":[],"repositories":[]}`),
		"template inspect * --json": uniboxtest.JSON(`{"api_version":1,"name":"gateway","source":"/x","shared":[],"profiles":[{"name":"web","agents":["claude"],"vars":{"git_name":"your name"},"secrets":[],"exists":true},{"name":"data","agents":["codex"],"vars":{},"secrets":["pypi"],"exists":false}]}`),
		"init * --yes":              {Out: "copied\n"},
	})
	a, sc, _ := newTestAppSrv(t, prepare)
	resizeApp(a, sc, 140, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	openSandboxes(t, a, sc)
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up unibox")
	doSetupStep(t, a, sc, "Profiles")
	waitFor(t, a, sc, "Add a unibox profile")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Template repository")
	waitFor(t, a, sc, "not cloned")
	assertLegible(t, a, sc, "the template repositories")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Profiles of template gateway")
	waitFor(t, a, sc, "you have one of this name")
	waitFor(t, a, sc, "Asks for your values of")
	waitFor(t, a, sc, "git_name.")
	assertLegible(t, a, sc, "a template's profiles")
	if !fake.Called("template inspect " + p.clone + " --json") {
		t.Fatalf("the clone was not read as a template: %v", fake.Calls())
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Copy profile web")
	form := frontForm(a)
	assertFormFits(t, a, sc, form, "Name", "Copy", "Cancel")
	setField(t, a, form, 0, "web2")
	pressButton(t, a, sc, form, "Copy")
	waitCalled(t, a, fake, "init web2 --from "+p.clone+" --use web --yes")
}
