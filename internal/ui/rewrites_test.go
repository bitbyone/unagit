package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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
	// Esc goes back to the log it was opened from, then to the screen.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Rewrite History")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")

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

// TestRecoverFromTheReflog: a commit a reset in a terminal threw away is
// in the log's Recover from Reflog (R); u asks, and the branch is back on
// it - which Rewrite History can undo.
func TestRecoverFromTheReflog(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "c.txt", "Count requests")
	lost := gitIn(t, p.clone, "rev-parse", "HEAD")
	gitIn(t, p.clone, "reset", "-q", "--hard", "HEAD~1")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "Recover from Reflog · acme/gateway (main)")
	waitFor(t, a, sc, "reset: moving to HEAD~1")
	assertLegible(t, a, sc, "the reflog")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "THERE, NOT NOW")
	inOrder(t, a.screenText(sc), "THERE, NOT NOW", lost[:7]+" Count requests", "NOW, NOT THERE")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Recover from Reflog")
	typeRunes(sc, "u")
	waitFor(t, a, sc, "back at "+lost[:8])
	typeRunes(sc, "g")
	waitFor(t, a, sc, "went back to "+lost[:8])
	if got := gitIn(t, p.clone, "rev-parse", "HEAD"); got != lost {
		t.Errorf("HEAD = %s, want %s", got, lost)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Commit Log")
	typeRunes(sc, "H")
	waitFor(t, a, sc, "went back to "+lost[:8]+" from the reflog")
}

// TestShelveAndUnshelve: s in the Changes dialog shelves under a name; the
// Shelf lists it, and u puts it back into the checkout.
func TestShelveAndUnshelve(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(filepath.Join(p.clone, "a.txt"), []byte("half done\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p.clone, "new.txt"), []byte("new\n"), 0o644))
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlK, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "new.txt")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "Shelve Changes · acme/gateway (main)")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			resizeApp(a, sc, size.w, size.h)
			waitFor(t, a, sc, "Shelve Changes · ")
			form := frontForm(a)
			frame := onLoop(a, func() rect {
				x, y, w, h := form.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Fatalf("row %d: the frame's right border is drawn over:\n%s", y, a.screenText(sc))
				}
			}
			for _, want := range []string{labelShelfName, "Shelves", "Shelve ", "Cancel"} {
				if !strings.Contains(a.screenText(sc), want) {
					t.Errorf("%q is not on screen:\n%s", want, a.screenText(sc))
				}
			}
			assertLegible(t, a, sc, "the shelve form")
		})
	}
	resizeApp(a, sc, 160, 44)
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel(labelShelfName).(*tview.InputField).SetText("Half done")
		return true
	})
	pressButton(t, a, sc, form, "Shelve")
	waitFor(t, a, sc, "shelved as Half done")
	if got := gitIn(t, p.clone, "status", "--porcelain"); got != "" {
		t.Fatalf("not committed after shelving: %q", got)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "new.txt")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "FILTER")
	typeRunes(sc, "Shelf…")
	waitFor(t, a, sc, "Shelf…")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Shelf · acme/gateway (main)")
	waitFor(t, a, sc, "Half done")
	waitFor(t, a, sc, "2 files")
	assertLegible(t, a, sc, "the shelf")
	typeRunes(sc, "u")
	waitFor(t, a, sc, "unshelved Half done")
	if got := gitIn(t, p.clone, "status", "--porcelain"); got != "M a.txt\n?? new.txt" {
		t.Errorf("after unshelving: %q", got)
	}
}
