package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestRebaseOntoBaseThenForcePush: EDITS counts what is not committed; Ctrl-R
// puts a pushed branch on top of its base, the row asks for a force push, and
// P does it after asking.
func TestRebaseOntoBaseThenForcePush(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/x")
	gitIn(t, p.clone, "config", "branch.feat/x.unagitBase", "main")
	commitIn(t, dir, "x.txt", "mine")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/x")
	other := p.elsewhere("main")
	commitIn(t, other, "z.txt", "main moved on")
	gitIn(t, other, "push", "-q", "origin", "main")
	must(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("edited\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))
	p.rescan()

	typeRunes(sc, "3")
	waitFor(t, a, sc, "RMT")
	waitFor(t, a, sc, "in sync")
	if row := rowWith(a, sc, "feat/x"); !containsField(row, "1/1") {
		t.Errorf("EDITS does not count one versioned file and one unversioned: %q", row)
	}

	sc.InjectKey(tcell.KeyCtrlR, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "force push required")
	if readFileUI(t, dir, "z.txt") != "main moved on\n" || readFileUI(t, dir, "a.txt") != "edited\n" {
		t.Error("the branch is not on top of main with its edits")
	}

	typeRunes(sc, "P")
	waitFor(t, a, sc, "Force push")
	typeRunes(sc, "f")
	waitFor(t, a, sc, "in sync")
	if gitIn(t, p.origin, "rev-parse", "feat/x") != gitIn(t, dir, "rev-parse", "HEAD") {
		t.Error("origin does not have the rebased branch")
	}
}

func readFileUI(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rowWith is the screen line that holds text, or "".
func rowWith(a *App, sc tcell.SimulationScreen, text string) string {
	for _, line := range strings.Split(a.screenText(sc), "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

// containsField reports whether a line has want as a word of its own.
func containsField(line, want string) bool {
	for _, f := range strings.Fields(line) {
		if f == want {
			return true
		}
	}
	return false
}
