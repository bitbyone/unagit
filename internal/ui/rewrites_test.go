package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestRewriteHistoryUndoesASquash: a squash made in the log is in Rewrite
// History (H); its detail names the commits it took; u asks, then puts the
// two commits back, and the row reads as undone. Both changes come to the
// Activity log as local history, which H narrows the log to, and Enter on
// one of them opens Rewrite History there.
func TestRewriteHistoryUndoesASquash(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "c.txt", "Count requests")
	commitIn(t, p.clone, "d.txt", "Bill them")
	before := gitIn(t, p.clone, "rev-parse", "HEAD")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "  ")
	waitFor(t, a, sc, "· 2 marked")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "Squash 2 Commits")
	pressButton(t, a, sc, frontForm(a), "Save")
	waitFor(t, a, sc, "squashed 2 commits into")

	typeRunes(sc, "H")
	waitFor(t, a, sc, "Rewrite History · acme/gateway")
	waitFor(t, a, sc, "squashed 2 commits")
	assertLegible(t, a, sc, "the rewrite history")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "BEFORE, NOT AFTER")
	inOrder(t, a.screenText(sc), "BEFORE, NOT AFTER", "Bill them", "Count requests", "AFTER, NOT BEFORE")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "BEFORE, NOT AFTER")
	waitFor(t, a, sc, "Rewrite History · acme/gateway")

	typeRunes(sc, "u")
	waitFor(t, a, sc, "goes back to "+before[:8])
	typeRunes(sc, "u")
	waitFor(t, a, sc, "undid: squashed 2 commits")
	waitFor(t, a, sc, "undone")
	if got := gitIn(t, p.clone, "rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	if got := gitIn(t, p.clone, "status", "--porcelain"); got != "" {
		t.Errorf("the undo left %q not committed", got)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Rewrite History")

	// The Activity log has both, and H narrows it to them.
	typeRunes(sc, "4")
	waitFor(t, a, sc, "Squashed")
	waitFor(t, a, sc, "Undone")
	typeRunes(sc, "H")
	waitFor(t, a, sc, "Log · local history")
	assertLegible(t, a, sc, "the local history in the Activity log")
	typeRunes(sc, "L")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Rewrite History · acme/gateway")
	if !strings.Contains(a.screenText(sc), "undid: squashed 2 commits") {
		t.Errorf("the record is not listed:\n%s", a.screenText(sc))
	}
}

// TestADeletedBranchIsMadeAgain: a branch deleted in the branch manager is
// in its Rewrite History (H), and u makes it again where it was.
func TestADeletedBranchIsMadeAgain(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	srv.liveBranches.Store(func(int) []string {
		return strings.Fields(gitIn(t, p.origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
	})
	gitIn(t, p.clone, "checkout", "-q", "-b", "mine")
	commitIn(t, p.clone, "m.txt", "only here")
	tip := gitIn(t, p.clone, "rev-parse", "HEAD")
	gitIn(t, p.clone, "checkout", "-q", "main")
	p.rescan()

	typeRunes(sc, "g")
	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	typeRunes(sc, "/mine")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL   ")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete branch")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "deleted mine in the clone")

	typeRunes(sc, "H")
	waitFor(t, a, sc, "Rewrite History · acme/gateway")
	waitFor(t, a, sc, "deleted mine")
	typeRunes(sc, "u")
	waitFor(t, a, sc, "Make mine again at "+tip[:8])
	typeRunes(sc, "u")
	waitFor(t, a, sc, "undid: deleted mine")
	if got := gitIn(t, p.clone, "rev-parse", "mine"); got != tip {
		t.Errorf("mine = %s, want %s", got, tip)
	}
}
