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

// frontForm is the form of the dialog in front.
func frontForm(a *App) *tview.Form {
	return onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
}

// TestCommitAndPushFromRepositories: c on a clone commits its versioned
// files, Push sends the commit with it after asking, and a file git does not
// track stays out of both. EDITS says what is left.
func TestCommitAndPushFromRepositories(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(filepath.Join(p.clone, "a.txt"), []byte("edited\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p.clone, "new.txt"), []byte("new\n"), 0o644))
	p.rescan()
	waitForRow(t, a, sc, "acme/gateway", "1/1")

	typeRunes(sc, "gc")
	waitFor(t, a, sc, "Commit · acme/gateway · 1 file(s)")
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel("Message").(*tview.TextArea).SetText("Say it again", false)
		return true
	})
	pressButton(t, a, sc, form, "Commit and Push")
	waitFor(t, a, sc, "Commit and push")
	typeRunes(sc, "p")
	waitFor(t, a, sc, "and pushed")

	if got := gitIn(t, p.clone, "log", "-1", "--format=%s"); got != "Say it again" {
		t.Errorf("subject = %q", got)
	}
	if got, want := gitIn(t, p.origin, "rev-parse", "main"), gitIn(t, p.clone, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin has %s, the clone %s", got, want)
	}
	if got := gitIn(t, p.clone, "status", "--porcelain"); got != "?? new.txt" {
		t.Errorf("left on disk: %q", got)
	}
	waitForRow(t, a, sc, "acme/gateway", "/1")
}

// TestPushFromRepositories: P sends the clone's commits origin lacks, after
// asking - Force Push offered beside Push - and says so when there is
// nothing to send.
func TestPushFromRepositories(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	p.rescan()
	waitForRow(t, a, sc, "acme/gateway", glyphAhead+"1")

	typeRunes(sc, "gP")
	waitFor(t, a, sc, "Push 1 commit(s) of main")
	waitFor(t, a, sc, "Count requests per client")
	waitFor(t, a, sc, "f force push")
	assertLegible(t, a, sc, "the question before a push")
	typeRunes(sc, "p")
	waitFor(t, a, sc, "pushed main")
	if got, want := gitIn(t, p.origin, "rev-parse", "main"), gitIn(t, p.clone, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin has %s, the clone %s", got, want)
	}
	waitForRow(t, a, sc, "acme/gateway", glyphCheck)
	typeRunes(sc, "P")
	waitFor(t, a, sc, "nothing to push")
}

// TestEditCommitMessage: e in a clone's log writes the message of a commit
// not pushed again, keeping the commits after it; a pushed one is refused.
func TestEditCommitMessage(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	pushed := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "b.txt", "Cuont requests")
	commitIn(t, p.clone, "c.txt", "Bill them")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	// Found among a commit's actions, not in the hint, and only for a
	// commit not pushed - though e on a pushed one still says why.
	if strings.Contains(a.screenText(sc), "edit message") {
		t.Errorf("the log's hint names editing a message:\n%s", a.screenText(sc))
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Edit Commit Message…")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Edit Commit Message…")
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Show Pipelines…")
	if strings.Contains(a.screenText(sc), "Edit Commit Message") {
		t.Errorf("a pushed commit's actions offer to edit its message:\n%s", a.screenText(sc))
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Show Pipelines…")
	typeRunes(sc, "e")
	waitFor(t, a, sc, pushed[:8]+" is on origin already")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "is on origin already")

	typeRunes(sc, "ke")
	waitFor(t, a, sc, "Edit Commit Message · ")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			resizeApp(a, sc, size.w, size.h)
			waitFor(t, a, sc, "Cuont requests")
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
			for _, want := range []string{"Message", "Save", "Cancel"} {
				if !strings.Contains(a.screenText(sc), want) {
					t.Errorf("%q is not on screen:\n%s", want, a.screenText(sc))
				}
			}
			assertLegible(t, a, sc, "editing a commit's message")
		})
	}
	resizeApp(a, sc, 160, 44)
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel("Message").(*tview.TextArea).SetText("Count requests\n\nPer client.", false)
		return true
	})
	pressButton(t, a, sc, form, "Save")
	waitFor(t, a, sc, "is edited")

	if got := gitIn(t, p.clone, "log", "-1", "--format=%B", "HEAD~1"); got != "Count requests\n\nPer client." {
		t.Errorf("message = %q", got)
	}
	if got := gitIn(t, p.clone, "log", "-1", "--format=%s"); got != "Bill them" {
		t.Errorf("the commit after it = %q", got)
	}
	if got := gitIn(t, p.clone, "rev-parse", "HEAD~2"); got != pushed {
		t.Errorf("the pushed commit moved: %s, want %s", got, pushed)
	}
}

// TestUndoCommit: u takes the newest commit back when no remote has it -
// offered among its actions, its changes left to commit again - and is
// not offered for a pushed one.
func TestUndoCommit(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	pushed := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "b.txt", "Count requests")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Undo Commit")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Undo Commit")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Show Pipelines…")
	if strings.Contains(a.screenText(sc), "Undo Commit") {
		t.Error("a pushed commit offers to be undone")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Show Pipelines…")

	typeRunes(sc, "ku")
	waitFor(t, a, sc, "its changes wait to be committed again")
	if got := gitIn(t, p.clone, "rev-parse", "HEAD"); got != pushed {
		t.Errorf("HEAD is %s, want %s", got, pushed)
	}
	if got := gitIn(t, p.clone, "status", "--porcelain"); got != "A  b.txt" {
		t.Errorf("the undone commit's changes: %q", got)
	}
}
