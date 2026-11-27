package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/board"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/session"
)

// TestTaskLinksAreAlive: a task's links say what they are now - a
// worktree gone is dimmed and its story says so, a merge request has its
// number - and things are linked from both ends: Link… on the board,
// Add to Task… on a merge request. An editor open in a linked worktree is
// found on the card, and o carries on in it, moving the task along.
func TestTaskLinksAreAlive(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	store := board.Open(a.cfg.BoardDir())
	here := t.TempDir()
	gitIn(t, here, "init", "-q")
	gone := filepath.Join(t.TempDir(), "removed-worktree")
	now := time.Now()
	task := board.Task{ID: "t1", Space: board.DefaultSpace.ID, Title: "Rate limits", State: board.Todo, Rank: "m", Created: now, Moved: now,
		Links: []board.Link{
			{Kind: board.KindWorktree, Key: gone, Path: gone, Label: "acme/gateway · old", Seen: board.LinkState{State: "on disk", Since: now}},
			{Kind: board.KindWorktree, Key: here, Path: here, Label: "acme/gateway · feat/rate", Seen: board.LinkState{State: "on disk", Since: now}},
		}}
	must(t, store.Add(task))
	stored := func() board.Task {
		tasks, err := store.Tasks(board.DefaultSpace.ID)
		must(t, err)
		for _, x := range tasks {
			if x.ID == task.ID {
				return x
			}
		}
		t.Fatalf("the task is gone: %+v", tasks)
		return board.Task{}
	}
	link := func(kind string) (board.Link, bool) {
		for _, l := range stored().Links {
			if l.Kind == kind {
				return l, true
			}
		}
		return board.Link{}, false
	}

	typeRunes(sc, "6")
	waitFor(t, a, sc, "acme/gateway · old · removed")
	waitTrue(t, "the worktree gone written down", func() bool {
		x := stored()
		return x.Links[0].Seen.Gone && slices.ContainsFunc(x.Story, func(e board.Event) bool { return e.What == "acme/gateway · old removed" })
	})
	if x := stored(); x.Links[1].Seen.Gone || x.Links[1].Seen.Since.After(now) {
		t.Errorf("the worktree there changed: %+v", x.Links[1].Seen)
	}

	// i links a repository from the list.
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Link · Rate limits")
	typeRunes(sc, "/Repository")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Repository · Rate limits")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "a repository linked", func() bool { _, ok := link(board.KindRepository); return ok })
	if l, _ := link(board.KindRepository); l.Seen.State == "" {
		t.Errorf("a repository linked without what it is: %+v", l)
	}

	// Add to Task… on a merge request links it to the task chosen.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	mr := onLoop(a, func() forge.MergeRequest {
		i := a.mrsPane.selectedIndex()
		return a.mrs[i]
	})
	sc.InjectKey(tcell.KeyRune, '.', tcell.ModNone)
	waitFor(t, a, sc, "Actions ·")
	typeRunes(sc, "Add to Task")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Add to Task · ")
	waitFor(t, a, sc, "New Task…")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "the merge request linked", func() bool { l, ok := link(board.KindMR); return ok && l.IID == mr.IID })
	if l, _ := link(board.KindMR); l.Seen.MR == nil || l.Seen.MR.Title != mr.Title || l.Seen.State != "open" {
		t.Errorf("the merge request linked as %+v", l.Seen)
	}
	typeRunes(sc, "6")
	waitFor(t, a, sc, fmt.Sprintf("!%d", mr.IID))

	// An editor open in the worktree is found on the card, and o goes to
	// it; the task moves to In progress.
	_, err := a.sessions.Add(session.Record{PID: os.Getpid(), Dir: here, Editor: "zed", Since: now})
	must(t, err)
	onLoop(a, func() bool { a.refreshOpenEditors(); return true })
	waitTrue(t, "the editor read", func() bool { return onLoop(a, func() bool { return len(a.editorsOpen) > 0 }) })
	onLoop(a, func() bool { clear(a.board.laid); return true })
	typeRunes(sc, "R")
	waitFor(t, a, sc, "Zed · ")
	typeRunes(sc, "o")
	waitFor(t, a, sc, "has a window of its own")
	waitTrue(t, "the task in progress", func() bool { return stored().State == board.Doing })
}

// TestATaskLinksNothingToCarryOnIn: o on a task without links says how to
// give it some, and leaves it where it is.
func TestATaskLinksNothingToCarryOnIn(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	store := board.Open(a.cfg.BoardDir())
	now := time.Now()
	must(t, store.Add(board.Task{ID: "t", Space: board.DefaultSpace.ID, Title: "Bare", State: board.Todo, Rank: "m", Created: now, Moved: now}))
	typeRunes(sc, "6")
	waitFor(t, a, sc, "Bare")
	typeRunes(sc, "o")
	waitFor(t, a, sc, "nothing to carry on in")
	tasks, err := store.Tasks(board.DefaultSpace.ID)
	must(t, err)
	if tasks[0].State != board.Todo {
		t.Errorf("a task with nothing to carry on in moved to %s", tasks[0].State)
	}
}

// TestTidyingUpAfterATaskFitsAndOffersOnlyWhatIsSafe: the question a task
// done asks ticks what can go, names what holds work without offering it,
// keeps its frame at every size, and runs what is left ticked.
func TestTidyingUpAfterATaskFitsAndOffersOnlyWhatIsSafe(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()
			a, sc, _ := newTestAppSrv(t)
			resizeApp(a, sc, size.w, size.h)
			waitFor(t, a, sc, "acme/gateway")
			ran := make(chan string, 2)
			onLoop(a, func() bool {
				a.board.showTidyUp(board.Task{Title: "Ship it"}, []tidy{
					{label: "Worktree acme/gateway · feat/rate", run: func(func(string)) error { ran <- "worktree"; return nil }},
					{label: "Review of !7", why: "2 file(s) edited in the review", run: func(func(string)) error { ran <- "review"; return nil }},
				})
				return true
			})
			waitFor(t, a, sc, "Done · Ship it")
			text := a.screenText(sc)
			for _, want := range []string{"Worktree acme/gateway", "Review of !7", "stays: 2 file(s) edited in the review", "Tidy Up", "Keep All"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			f := frontForm(a)
			form := onLoop(a, func() rect {
				x, y, w, h := f.GetRect()
				return rect{x, y, w, h}
			})
			for y := form.y + 1; y < form.y+form.h-1; y++ {
				if r, _ := cellAt(a, sc, form.x+form.w-1, y); r != '│' {
					t.Fatalf("the frame's right border is broken on row %d:\n%s", y, text)
				}
			}
			assertLegible(t, a, sc, "the tidy-up question")
			pressButton(t, a, sc, f, "Tidy Up")
			select {
			case got := <-ran:
				if got != "worktree" {
					t.Errorf("ran %s", got)
				}
			case <-time.After(patience):
				t.Fatalf("nothing ran:\n%s", a.screenText(sc))
			}
			select {
			case got := <-ran:
				t.Errorf("what holds work ran too: %s", got)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}
