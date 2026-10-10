package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestSquashInTheLog: space marks commits in a clone's log, the band on
// them and their count in the title; Squash Commits… is offered for two
// marked next to each other and not for one, nor for two with a commit
// between; Esc takes the marks off before it closes the log. The form
// starts with both messages, oldest first; with a pushed commit among them
// it asks first, naming the force push, and the log then shows what is
// only here and what only on origin.
func TestSquashInTheLog(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	srv.protected.Store(func(_ int, branch string) bool { return branch == "main" })
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Add the limiter", "About the limiter.")
	gitIn(t, p.clone, "push", "-q")
	pushed := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "c.txt", "Count requests", "About counting.")
	commitIn(t, p.clone, "d.txt", "Bill them", "About billing.")
	p.rescan()

	openLog := func() {
		t.Helper()
		sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
		waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
		waitFor(t, a, sc, "About billing.")
	}
	// offers says whether the actions of the commit under the cursor
	// include squashing.
	offers := func() bool {
		t.Helper()
		sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
		waitFor(t, a, sc, "Copy…")
		has := strings.Contains(a.screenText(sc), "Squash Commits…")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "Copy…")
		return has
	}
	typeRunes(sc, "g")
	openLog()
	if !strings.Contains(a.screenText(sc), "space mark") {
		t.Errorf("the log's hint does not say space marks:\n%s", a.screenText(sc))
	}
	typeRunes(sc, " ")
	waitFor(t, a, sc, "· 1 marked")
	waitFor(t, a, sc, "About counting.")
	text := a.screenText(sc)
	row := lineOf(text, "Bill them")
	line := strings.Split(text, "\n")[row]
	col := len([]rune(line[:strings.Index(line, "Bill them")]))
	if _, style := cellAt(a, sc, col, row); onLoop(a, func() bool { _, bg, _ := style.Decompose(); return bg != colMarked }) {
		t.Errorf("the marked commit is not on the marks' band")
	}
	assertLegible(t, a, sc, "a log with a commit marked")
	if offers() {
		t.Error("squashing is offered for one commit")
	}
	typeRunes(sc, " ")
	waitFor(t, a, sc, "· 2 marked")
	if !offers() {
		t.Error("squashing is not offered for two commits next to each other")
	}
	// With commits marked, what works on one commit is neither offered nor
	// hinted, and its key says why.
	waitFor(t, a, sc, "s squash")
	waitFor(t, a, sc, "y copy · space mark")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Copy…")
	for _, single := range []string{"Show Details", "New Branch Here…", "Check Out Commit", "Edit Commit Message…", "Show Changes Since"} {
		if strings.Contains(a.screenText(sc), single) {
			t.Errorf("%q is offered for commits marked:\n%s", single, a.screenText(sc))
		}
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Copy…")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New Branch Here… works on one item")
	closeMessage(t, a, sc)
	waitFor(t, a, sc, "· 2 marked")

	// Esc takes the marks off, and the log stays.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "marked")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")

	// Two with one between them: not offered, and s says why.
	typeRunes(sc, "g ")
	waitFor(t, a, sc, "· 1 marked")
	typeRunes(sc, "j ")
	waitFor(t, a, sc, "· 2 marked")
	if offers() {
		t.Error("squashing is offered for two commits with one between them")
	}
	typeRunes(sc, "s")
	waitFor(t, a, sc, "not next to each other")
	closeMessage(t, a, sc)
	waitFor(t, a, sc, "· 2 marked")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "marked")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")

	// Count requests and the pushed limiter, under it past the line.
	openLog()
	typeRunes(sc, "j  ")
	waitFor(t, a, sc, "· 2 marked")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "Squash 2 Commits")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			resizeApp(a, sc, size.w, size.h)
			waitFor(t, a, sc, "Squash 2 Commits")
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
			for _, want := range []string{"Message", "Save", "Cancel", "Add the limiter"} {
				if !strings.Contains(a.screenText(sc), want) {
					t.Errorf("%q is not on screen:\n%s", want, a.screenText(sc))
				}
			}
			assertLegible(t, a, sc, "squashing commits")
		})
	}
	resizeApp(a, sc, 160, 44)
	text = a.screenText(sc)
	inOrder(t, text, "Add the limiter", "About the limiter.", "Count requests", "About counting.")
	pressButton(t, a, sc, frontForm(a), "Save")
	waitFor(t, a, sc, "1 of these commits is on origin")
	if !strings.Contains(a.screenText(sc), "force push") {
		t.Errorf("the question does not name the force push:\n%s", a.screenText(sc))
	}
	// main is the default branch, and the server protects it.
	waitFor(t, a, sc, "main is the default branch")
	waitFor(t, a, sc, "origin protects main")
	// Cancel goes back to the log, the marks kept.
	typeRunes(sc, "c")
	waitFor(t, a, sc, "· 2 marked")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "Squash 2 Commits")
	pressButton(t, a, sc, frontForm(a), "Save")
	waitFor(t, a, sc, "1 of these commits is on origin")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "squashed 2 commits into")
	waitFor(t, a, sc, "── only on origin")
	inOrder(t, a.screenText(sc), "── only here", "Bill them", "── only on origin", "── shared", "initial")
	if got := gitIn(t, p.clone, "log", "--format=%s", "HEAD~2..HEAD"); got != "Bill them\nAdd the limiter" {
		t.Errorf("history = %q", got)
	}
	if got := gitIn(t, p.clone, "config", "branch.main.unagitrebasedfrom"); got != pushed {
		t.Errorf("the lease noted is %q, origin had %s", got, pushed)
	}

	// P on the clone offers the force push, naming what leaves origin.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")
	typeRunes(sc, "P")
	waitFor(t, a, sc, "A force push takes these off origin:")
	if text := a.screenText(sc); lineOf(text, pushed[:7]) < lineOf(text, "takes these off origin") {
		t.Errorf("the pushed commit is not named as leaving origin:\n%s", text)
	}
	assertLegible(t, a, sc, "the force push question")
}
