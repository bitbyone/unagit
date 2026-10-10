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
	// What leaves origin is named: the commit as it was before the rebase.
	waitFor(t, a, sc, "A force push takes these off origin:")
	if !strings.Contains(a.screenText(sc), "mine") {
		t.Errorf("the question does not name the commit leaving origin:\n%s", a.screenText(sc))
	}
	typeRunes(sc, "f")
	waitFor(t, a, sc, "in sync")
	if gitIn(t, p.origin, "rev-parse", "feat/x") != gitIn(t, dir, "rev-parse", "HEAD") {
		t.Error("origin does not have the rebased branch")
	}
}

// TestSetBaseThenRebaseOnto: a branch made with plain git has no base, so
// Rebase onto Base is not offered; Set Base… records one from a list that
// puts the default first, and then it is. Rebase onto… puts the pushed
// branch on another branch, the base staying, and P force-pushes it.
func TestSetBaseThenRebaseOnto(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	srv.liveBranches.Store(func(int) []string {
		return strings.Fields(gitIn(t, p.origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
	})
	other := p.elsewhere("main")
	gitIn(t, other, "checkout", "-q", "-b", "release")
	commitIn(t, other, "r.txt", "release")
	gitIn(t, other, "push", "-q", "origin", "release")
	dir := p.worktree("feat/x")
	commitIn(t, dir, "x.txt", "mine")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/x")
	p.rescan()

	typeRunes(sc, "3")
	waitFor(t, a, sc, "in sync")
	acts := func() []uiAction {
		for _, r := range a.worktrees {
			if r.Branch == "feat/x" {
				return a.worktreeListActions(a.worktreesPane, r)
			}
		}
		return nil
	}
	waitOffered(t, a, acts, "Rebase onto Base", false)
	waitOffered(t, a, acts, "Set Base…", true)

	act := func(name string) {
		t.Helper()
		sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
		waitFor(t, a, sc, "Open With…")
		typeRunes(sc, name)
		waitFor(t, a, sc, name)
		sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	}
	act("Set Base…")
	waitFor(t, a, sc, "Base of feat/x")
	if row := rowWith(a, sc, "main"); !strings.Contains(row, "default") {
		t.Errorf("main is not offered as the default: %q", row)
	}
	if strings.Contains(a.screenText(sc), "the base now") {
		t.Error("a branch with no base is said to have one")
	}
	assertLegible(t, a, sc, "the base picker")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "feat/x is now based on main")
	if got := gitIn(t, p.clone, "config", "branch.feat/x.unagitBase"); got != "main" {
		t.Fatalf("the base recorded is %q", got)
	}
	waitOffered(t, a, acts, "Rebase onto Base", true)

	act("Rebase onto…")
	waitFor(t, a, sc, "Rebase feat/x onto")
	if row := rowWith(a, sc, "main"); !strings.Contains(row, "the base now") {
		t.Errorf("the base is not first and named: %q", row)
	}
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "force push required")
	if readFileUI(t, dir, "r.txt") != "release\n" {
		t.Error("the branch is not on top of release")
	}
	if got := gitIn(t, p.clone, "config", "branch.feat/x.unagitBase"); got != "main" {
		t.Errorf("the rebase changed the base to %q", got)
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
