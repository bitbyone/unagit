package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/board"
)

// TestTheBoardKeepsTasksAsTheyAreWorkedOn walks a task through the board:
// written, put above the one before, moved along, commented, done - gone
// from the board at once with the time set to nothing, still in History
// and opened again from there - then a space of its own and back, and a
// task another unagit wrote coming by itself. What is on disk is checked
// as well as what is on screen.
func TestTheBoardKeepsTasksAsTheyAreWorkedOn(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, func(a *App) { a.cfg.Board.HideDoneAfter = "0m" })
	waitFor(t, a, sc, "acme/gateway")
	store := board.Open(a.cfg.BoardDir())
	tasks := func() []board.Task {
		tasks, err := store.Tasks(board.DefaultSpace.ID)
		if err != nil {
			t.Fatal(err)
		}
		return tasks
	}
	find := func(title string) board.Task {
		for _, task := range tasks() {
			if task.Title == title {
				return task
			}
		}
		t.Fatalf("no task %q on disk: %+v", title, tasks())
		return board.Task{}
	}
	write := func(title string) {
		typeRunes(sc, "n")
		waitFor(t, a, sc, "New Task · Task Board")
		typeRunes(sc, title)
		sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
		waitGone(t, a, sc, "New Task · Task Board")
		waitFor(t, a, sc, title)
	}

	typeRunes(sc, "6")
	waitFor(t, a, sc, "No task yet - n writes one.")
	write("Write the plan")
	write("Ship it")
	if got := tasks(); len(got) != 2 || got[0].Title != "Ship it" || got[0].State != board.Todo {
		t.Fatalf("a new task goes to the top of its column: %+v", got)
	}

	// The cursor stays on the task written: L takes it along.
	typeRunes(sc, "L")
	waitTrue(t, "Ship it moved to in progress", func() bool { return find("Ship it").State == board.Doing })
	if story := find("Ship it").Story; len(story) != 2 || story[1].What != "moved to In progress" {
		t.Fatalf("the story: %+v", story)
	}

	// c comments: the detail opens on the composer.
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comment · Ship it")
	typeRunes(sc, "tried the canary")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "tried the canary")
	waitFor(t, a, sc, "moved to In progress")
	if c := find("Ship it").Comments; len(c) != 1 || c[0].Body != "tried the canary" {
		t.Fatalf("comments: %+v", c)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Task · Task Board")

	// Done, with no time on the board: History has it, x opens it again.
	shown := func(col int) []string {
		return onLoop(a, func() []string {
			var out []string
			for _, task := range a.board.list(col) {
				out = append(out, task.Title)
			}
			return out
		})
	}
	typeRunes(sc, "x")
	waitTrue(t, "Ship it done", func() bool { return find("Ship it").State == board.Done })
	if done := shown(2); len(done) != 0 {
		t.Fatalf("a task done stayed on the board with no time to stay: %v", done)
	}
	typeRunes(sc, "y")
	waitFor(t, a, sc, "History 1")
	if got := shown(boardHistory); !slices.Equal(got, []string{"Ship it"}) {
		t.Fatalf("History: %v", got)
	}
	typeRunes(sc, "x")
	waitTrue(t, "Ship it open again", func() bool { return find("Ship it").State == board.Todo })
	if task := find("Ship it"); !task.Done.IsZero() {
		t.Error("a task opened again kept its time done")
	}
	typeRunes(sc, "y")

	// A space of its own is empty; ] goes back to the first.
	typeRunes(sc, "p")
	waitFor(t, a, sc, "Spaces")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New Space")
	typeRunes(sc, "Acme")
	// Enter goes from the name to the button, as in every form.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Acme · 0 to do")
	if got := onLoop(a, func() string { return a.cfg.State.BoardSpace }); got == board.DefaultSpace.ID || got == "" {
		t.Fatalf("the space open is %q", got)
	}
	typeRunes(sc, "]")
	waitFor(t, a, sc, "Write the plan")

	// Another unagit writes a task: it comes by itself.
	now := time.Now()
	if err := store.Add(board.Task{ID: board.NewID(), Space: board.DefaultSpace.ID, Title: "From elsewhere",
		State: board.Doing, Rank: "a", Created: now, Moved: now}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, sc, "From elsewhere")
	titles := []string{}
	for _, task := range tasks() {
		titles = append(titles, task.Title)
	}
	if !slices.Equal(titles, []string{"Ship it", "Write the plan", "From elsewhere"}) {
		t.Errorf("the board's order: %v", titles)
	}

	// The mouse carries a card to another column, above the card there.
	fx, fy := at(t, a, sc, "Write the plan")
	tx, ty := at(t, a, sc, "From elsewhere")
	press(sc, fx, fy, tcell.ButtonPrimary, tcell.ModNone)
	press(sc, fx+2, fy, tcell.ButtonPrimary, tcell.ModNone)
	press(sc, tx, ty-1, tcell.ButtonPrimary, tcell.ModNone)
	release(sc, tx, ty-1, tcell.ModNone)
	waitTrue(t, "Write the plan carried to in progress", func() bool { return find("Write the plan").State == board.Doing })
	if w, f := find("Write the plan"), find("From elsewhere"); w.Rank >= f.Rank {
		t.Errorf("the card carried went below the one it was put above: %q >= %q", w.Rank, f.Rank)
	}
}
