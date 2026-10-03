package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestBranchesFromRepositories: b lists every branch with where it is; Enter
// would switch the clone; the default is never deleted; d deletes a branch
// in the clone, saying what is lost; D deletes one everywhere.
func TestBranchesFromRepositories(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	srv.liveBranches.Store(func(int) []string {
		return strings.Fields(gitIn(t, p.origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
	})
	srv.deleteBranch.Store(func(_ int, branch string) { gitIn(t, p.origin, "branch", "-D", branch) })
	gitIn(t, p.clone, "branch", "mine")
	gitIn(t, p.clone, "checkout", "-q", "mine")
	commitIn(t, p.clone, "m.txt", "only here")
	gitIn(t, p.clone, "checkout", "-q", "main")
	gitIn(t, p.clone, "push", "-q", "-u", "origin", "main:shared")
	gitIn(t, p.clone, "branch", "shared", "origin/shared")
	p.rescan()

	typeRunes(sc, "g")
	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	waitFor(t, a, sc, "Enter check out in the main clone")
	text := a.screenText(sc)
	for _, want := range []string{"mine", "local only", "shared", "local · origin", "default"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not listed:\n%s", want, text)
		}
	}
	assertLegible(t, a, sc, "the branches")

	// main is first: the default is never deleted.
	typeRunes(sc, "D")
	waitFor(t, a, sc, "main is the default branch - it is not deleted")

	pick := func(name string) {
		t.Helper()
		typeRunes(sc, "b")
		waitFor(t, a, sc, "Branches - acme/gateway")
		typeRunes(sc, "/"+name)
		waitFor(t, a, sc, "FILTER")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitFor(t, a, sc, "NORMAL   j/k")
	}
	pick("mine")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "1 commit(s) nowhere else are lost")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "deleted mine in the clone")
	if strings.Contains(gitIn(t, p.clone, "branch", "--list", "mine"), "mine") {
		t.Error("mine is still in the clone")
	}

	pick("shared")
	typeRunes(sc, "D")
	waitFor(t, a, sc, "in the clone and on origin?")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "deleted shared in the clone and on origin")
	if strings.Contains(gitIn(t, p.clone, "branch", "-a"), "shared") {
		t.Errorf("shared is left: %s", gitIn(t, p.clone, "branch", "-a"))
	}
	if strings.Contains(gitIn(t, p.origin, "branch"), "shared") {
		t.Error("shared is still on origin")
	}
}
