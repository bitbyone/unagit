package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestBranchesFromRepositories: b lists every branch with where it is; Enter
// would switch the clone; the default is never deleted; d deletes a branch
// in the clone, saying what is lost; D deletes one everywhere.
func TestBranchesFromRepositories(t *testing.T) {
	t.Parallel()
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
	closeMessage(t, a, sc)

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

// TestNewBranchFromTheManager: n makes a branch from the one under the
// cursor - here one only origin has - and the list comes back with the
// cursor on it, not switched to; Enter then switches the clone to it.
func TestNewBranchFromTheManager(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	srv.liveBranches.Store(func(int) []string {
		return strings.Fields(gitIn(t, p.origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
	})
	gitIn(t, p.clone, "push", "-q", "origin", "main:upstream-only")
	p.rescan()

	typeRunes(sc, "g")
	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	typeRunes(sc, "/upstream")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL   j/k")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New branch - acme/gateway")
	text := a.screenText(sc)
	if !strings.Contains(strings.Split(text, "\n")[lineOf(text, labelBranchFrom)], "upstream-only") {
		t.Errorf("the new branch does not start from the one under the cursor:\n%s", text)
	}
	assertLegible(t, a, sc, "the new branch form")

	typeRunes(sc, "feature/x")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "c") // c is Cancel: c-reate is lit as r
	waitGone(t, a, sc, "New branch - acme/gateway")
	if strings.Contains(gitIn(t, p.clone, "branch", "--list", "feature/x"), "feature/x") {
		t.Fatal("Cancel made the branch")
	}

	waitFor(t, a, sc, "Branches - acme/gateway")
	typeRunes(sc, "/upstream")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL   j/k")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New branch - acme/gateway")
	typeRunes(sc, "feature/x")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "r")
	waitFor(t, a, sc, "created feature/x from upstream-only")
	if got := gitIn(t, p.clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("making the branch switched the clone to %s", got)
	}
	if got := gitIn(t, p.clone, "config", "branch.feature/x.unagitbase"); got != "upstream-only" {
		t.Errorf("the base recorded is %q", got)
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // closes the note
	waitGone(t, a, sc, "created feature/x")
	if lineOf(a.screenText(sc), "feature/x") < 0 {
		t.Fatalf("the new branch is not listed:\n%s", a.screenText(sc))
	}
	// Enter switches, opens no editor, closes the list and says nothing:
	// the row shows the branch.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Branches - acme/gateway")
	waitGone(t, a, sc, "Switching acme/gateway")
	waitFor(t, a, sc, "feature/x")
	if onLoop(a, func() bool { return a.modalOpen() }) {
		t.Errorf("something is still open after the switch:\n%s", a.screenText(sc))
	}
	if got := onLoop(a, func() int { return len(a.sessions.List()) }); got != 0 {
		t.Errorf("switching opened an editor: %d session(s)", got)
	}
	if got := gitIn(t, p.clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature/x" {
		t.Errorf("Enter on the new branch left the clone on %s", got)
	}
}

// TestTheNewBranchFormFitsItsFrame draws the form at several sizes.
func TestTheNewBranchFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, srv := newTestAppSrv(t)
			waitFor(t, a, sc, "acme/gateway")
			p := newRealProject(t, a, "acme/gateway")
			srv.liveBranches.Store(func(int) []string {
				return strings.Fields(gitIn(t, p.origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
			})
			p.rescan()
			resize(sc, size.w, size.h)
			typeRunes(sc, "g")
			typeRunes(sc, "b")
			waitFor(t, a, sc, "NORMAL   j/k")
			typeRunes(sc, "n")
			waitFor(t, a, sc, "New branch - acme/gateway")
			form := currentForm(a)
			frame := onLoop(a, func() rect {
				x, y, w, h := form.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is drawn over:\n%s", y, a.screenText(sc))
					break
				}
			}
			text := a.screenText(sc)
			for _, want := range []string{labelBranchName, labelBranchFrom, "Create", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			assertLegible(t, a, sc, "the new branch form")
		})
	}
}
