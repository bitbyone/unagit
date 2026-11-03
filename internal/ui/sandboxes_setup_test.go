package ui

import (
	"fmt"
	"os"
	"path/filepath"
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
	waitFor(t, a, sc, "Set up Unibox")
	for _, want := range []string{"installed 0.5", "Sandboxes tab", "Envs", "api, web", "Values of api", "1 missing",
		"Tokens of web", "Sandbox api-box", "not made", "Sandbox web", "unibox doctor"} {
		waitFor(t, a, sc, want)
	}
	doSetupStep(t, a, sc, "Sandbox api-box")
	waitCalled(t, a, fake, "sandbox create -e api")
	typeRunes(sc, "I")
	waitFor(t, a, sc, "Set up Unibox")
	doSetupStep(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "Tokens of web")
	waitFor(t, a, sc, "artifactory")
}

// uniboxInitFixture is a clone of acme/gateway holding the team's sbxenv
// files, two of them and one in a directory unibox passes over.
func uniboxInitFixture(t *testing.T) (*App, tcell.SimulationScreen, *uniboxtest.Fake, string) {
	t.Helper()
	a, sc, fake := setupFixture(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	for _, f := range []string{"sandboxes/dev.sbxenv.yaml", "sandboxes/ci.sbxenv.yaml", ".hidden/old.sbxenv.yaml", "node_modules/x/y.sbxenv.yaml"} {
		must(t, os.MkdirAll(filepath.Join(p.clone, filepath.Dir(f)), 0o755))
		must(t, os.WriteFile(filepath.Join(p.clone, f), []byte("sandbox: x\n"), 0o644))
	}
	changeOnLoop(a, a.refreshDisk)
	return a, sc, fake, p.clone
}

// TestUniboxInitPicksTheFiles: on a clone, Unibox Init lists its sbxenv
// files and hands the one chosen to unibox init, in this terminal; i
// hands every file.
func TestUniboxInitPicksTheFiles(t *testing.T) {
	t.Parallel()
	a, sc, fake, clone := uniboxInitFixture(t)
	pickMuxAction(t, a, sc, "Unibox Init")
	waitFor(t, a, sc, "Unibox Init · acme/gateway")
	waitFor(t, a, sc, "sandboxes/dev.sbxenv.yaml")
	text := dumpScreen(sc)
	if strings.Contains(text, "old.sbxenv") || strings.Contains(text, "y.sbxenv") {
		t.Errorf("hidden directories and node_modules are no part of the repository:\n%s", text)
	}
	ci, dev := filepath.Join(clone, "sandboxes/ci.sbxenv.yaml"), filepath.Join(clone, "sandboxes/dev.sbxenv.yaml")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitCalled(t, a, fake, "init --file "+ci+" "+clone)
	waitFor(t, a, sc, "Set up Unibox")
	sc.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	// The guide went back to the files it was opened from.
	waitGone(t, a, sc, "Set up Unibox")
	waitFor(t, a, sc, "Unibox Init · acme/gateway")
	typeRunes(sc, "i")
	waitCalled(t, a, fake, "init --file "+ci+" --file "+dev+" "+clone)
	// With files marked, i takes those alone.
	waitFor(t, a, sc, "Set up Unibox")
	sc.InjectKey(tcell.KeyEscape, 0, tcell.ModNone)
	waitFor(t, a, sc, "Unibox Init · acme/gateway")
	typeRunes(sc, "jj ")
	waitFor(t, a, sc, "1 marked")
	typeRunes(sc, "i")
	waitCalled(t, a, fake, "init --file "+dev+" "+clone)
}

// TestUniboxInitClonesFirst: a repository not cloned is cloned by unibox
// itself when the person says so, from the address unagit would use.
func TestUniboxInitClonesFirst(t *testing.T) {
	t.Parallel()
	a, sc, fake := setupFixture(t)
	waitFor(t, a, sc, "acme/gateway")
	address := onLoop(a, func() string { return a.uniboxCloneAddress(a.projects[0]) })
	pickMuxAction(t, a, sc, "Unibox Init")
	waitFor(t, a, sc, "is not cloned")
	// The question's buttons answer to their lit letters.
	keys := buttonKeys([]string{"Cancel", "Clone Here", "Let Unibox Clone"})
	typeRunes(sc, string(keys[2]))
	waitCalled(t, a, fake, "init "+address)
}

// TestUniboxInitFitsItsFrame: the files' picker at several sizes.
func TestUniboxInitFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{120, 34}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _, _ := uniboxInitFixture(t)
			resizeApp(a, sc, size.w, size.h)
			pickMuxAction(t, a, sc, "Unibox Init")
			waitFor(t, a, sc, "Unibox Init · acme/gateway")
			assertLegible(t, a, sc, "unibox init")
		})
	}
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
			waitFor(t, a, sc, "Set up Unibox")
			assertLegible(t, a, sc, "the setup")
		})
	}
}
