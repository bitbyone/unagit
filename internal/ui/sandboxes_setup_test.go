package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

// setupFixture is unibox with the overview's envs: api lacking a value
// and its sandbox, web lacking a token.
func setupFixture(t *testing.T) (*App, tcell.SimulationScreen, *uniboxtest.Fake) {
	t.Helper()
	fake, prepare := fakeUnibox(t, nil)
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
// stands - each env's values, its tokens and its sandbox - and Enter does
// one: a sandbox not made is created in this terminal, the tokens listed.
func TestTheSetupSaysWhereUniboxStands(t *testing.T) {
	t.Parallel()
	a, sc, fake := setupFixture(t)
	resizeApp(a, sc, 140, 44)
	openSandboxes(t, a, sc)
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up unibox")
	for _, want := range []string{"installed 0.5", "Sandboxes tab", "Envs", "api, web", "Values of api", "1 missing",
		"Tokens of web", "Sandbox api-box", "not made", "Sandbox web", "unibox doctor"} {
		waitFor(t, a, sc, want)
	}
	doSetupStep(t, a, sc, "Sandbox api-box")
	waitCalled(t, a, fake, "sandbox create -e api")
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up unibox")
	doSetupStep(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "artifactory")
}

// TestSetUpUniboxFromHereRunsInit: on a clone - the team's repository of
// sbxenv files - unibox init walks it in this terminal.
func TestSetUpUniboxFromHereRunsInit(t *testing.T) {
	t.Parallel()
	a, sc, fake := setupFixture(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	changeOnLoop(a, a.refreshDisk)
	pickMuxAction(t, a, sc, "Set Up unibox from Here")
	waitCalled(t, a, fake, "init "+p.clone)
}

// TestTheSetupFitsItsFrame: at several sizes.
func TestTheSetupFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{120, 34}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := setupFixture(t)
			resizeApp(a, sc, size.w, size.h)
			openSandboxes(t, a, sc)
			typeRunes(sc, "I")
			waitFor(t, a, sc, "Set up unibox")
			assertLegible(t, a, sc, "the setup")
		})
	}
}
