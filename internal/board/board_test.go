package board

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// TestARankFallsBetweenItsNeighbours: whatever the order of insertions -
// at the top, at the bottom, between two that are close - a new rank sorts
// strictly between its neighbours, so a column is ordered by its ranks
// alone and never has to be renumbered.
func TestARankFallsBetweenItsNeighbours(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1, 2))
	ranks := []string{}
	for range 2000 {
		at := r.IntN(len(ranks) + 1)
		before, after := "", ""
		if at > 0 {
			before = ranks[at-1]
		}
		if at < len(ranks) {
			after = ranks[at]
		}
		got := Between(before, after)
		if before != "" && got <= before || after != "" && got >= after {
			t.Fatalf("Between(%q, %q) = %q", before, after, got)
		}
		ranks = slices.Insert(ranks, at, got)
	}
	if !slices.IsSorted(ranks) {
		t.Fatal("the ranks are out of order")
	}
	// The same place again and again stays short enough to keep.
	a, b := "", ""
	for range 200 {
		b = Between(a, b)
	}
	if len(b) > 50 {
		t.Errorf("200 insertions at the top made a rank %d long", len(b))
	}
}

// TestTheStoreKeepsTasksInTheirSpacesAndOrder: tasks come back by state and
// rank, a change is made to the task as it is on disk, a task moved to
// another space leaves the first, and a space taken away takes its tasks
// to the space named - or with it.
func TestTheStoreKeepsTasksInTheirSpacesAndOrder(t *testing.T) {
	t.Parallel()
	s := Open(t.TempDir())
	if spaces, err := s.Spaces(); err != nil || len(spaces) != 1 || spaces[0] != DefaultSpace {
		t.Fatalf("a new board's spaces: %v, %v", spaces, err)
	}
	now := time.Now()
	home := DefaultSpace.ID
	add := func(title, state, rank string) Task {
		task := Task{ID: NewID(), Space: home, Title: title, State: state, Rank: rank, Created: now}
		if err := s.Add(task); err != nil {
			t.Fatal(err)
		}
		return task
	}
	add("second", Todo, "m")
	first := add("first", Todo, "c")
	add("working", Doing, "a")
	done := add("shipped", Done, "a")
	titles := func(space string) []string {
		tasks, err := s.Tasks(space)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, task := range tasks {
			out = append(out, task.Title)
		}
		return out
	}
	if got := titles(home); !slices.Equal(got, []string{"first", "second", "working", "shipped"}) {
		t.Fatalf("tasks: %v", got)
	}

	// Another unagit renamed it meanwhile: a change keeps that.
	if _, err := s.Change(home, first.ID, func(task *Task) error { task.Title = "renamed elsewhere"; return nil }); err != nil {
		t.Fatal(err)
	}
	moved, err := s.Change(home, first.ID, func(task *Task) error { task.SetState(Doing, now); return nil })
	if err != nil || moved.Title != "renamed elsewhere" || moved.State != Doing || len(moved.Story) != 1 {
		t.Fatalf("changed: %+v, %v", moved, err)
	}

	spaces, err := s.ChangeSpaces(func(sp []Space) ([]Space, error) {
		return append(sp, Space{ID: "acme", Name: "Acme"}), nil
	})
	if err != nil || len(spaces) != 2 || spaces[1].Order != 1 {
		t.Fatalf("spaces: %v, %v", spaces, err)
	}
	if _, err := s.Change(home, done.ID, func(task *Task) error { task.Space = "acme"; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := titles("acme"); !slices.Equal(got, []string{"shipped"}) {
		t.Fatalf("acme: %v", got)
	}
	if err := s.RemoveSpace(home, "acme"); err != nil {
		t.Fatal(err)
	}
	if got := titles("acme"); len(got) != 4 || got[3] != "shipped" {
		t.Fatalf("acme after the default space went: %v", got)
	}
	if spaces, _ := s.Spaces(); len(spaces) != 1 || spaces[0].ID != "acme" {
		t.Fatalf("spaces after removing one: %v", spaces)
	}
	if _, err := s.ChangeSpaces(func([]Space) ([]Space, error) { return nil, nil }); err == nil {
		t.Error("the last space went")
	}
}

// TestATaskDoneLeavesTheBoardAfterItsTime: done longer ago than the time
// set, a task is hidden; "never" keeps it; only done tasks are hidden.
func TestATaskDoneLeavesTheBoardAfterItsTime(t *testing.T) {
	t.Parallel()
	now := time.Now()
	day, err := ParseAfter("1d")
	if err != nil || day != 24*time.Hour {
		t.Fatalf("1d = %v, %v", day, err)
	}
	never, _ := ParseAfter("never")
	if _, err := ParseAfter("soon"); err == nil {
		t.Error("soon was taken for a time")
	}
	task := Task{State: Doing, Moved: now.Add(-48 * time.Hour)}
	if task.HiddenAt(now, day) {
		t.Error("a task in progress was hidden")
	}
	task.SetState(Done, now.Add(-25*time.Hour))
	if !task.HiddenAt(now, day) || task.HiddenAt(now, never) || task.HiddenAt(now, 48*time.Hour) {
		t.Error("a task done a day and an hour ago")
	}
	task.SetState(Todo, now)
	if !task.Done.IsZero() || task.HiddenAt(now, 0) {
		t.Error("a task opened again kept its time done")
	}
}
