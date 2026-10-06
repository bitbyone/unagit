package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
)

// TestViewOptionsHideColumns: every list's View options name its columns in
// the order they stand, each with an eye; hiding one takes it out of the
// list and into the configuration, showing it brings it back, and the
// column a row is cannot be hidden.
func TestViewOptionsHideColumns(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	heading := func(word string) bool {
		return slices.Contains(strings.Fields(strings.Trim(lineAt(a.screenText(sc), "ACTIVITY"), "│ ")), word)
	}
	toggle := func(column string) {
		t.Helper()
		typeRunes(sc, "v")
		waitFor(t, a, sc, "Columns")
		typeRunes(sc, "/"+column)
		sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "Columns")
	}
	hidden := func(list, id string) bool {
		return onLoop(a, func() bool { return a.cfg.Filters.HidesColumn(list, id) })
	}
	saved := func(list, id string) bool {
		cfg, err := config.LoadFrom(onLoop(a, func() string { return a.cfg.Dir() }))
		return err == nil && cfg.Filters.HidesColumn(list, id)
	}

	// Repositories: the order reads as the list does, PATH goes and comes.
	typeRunes(sc, "v")
	waitFor(t, a, sc, "Columns")
	// The dialog's rows are told from the list behind it by their eye.
	eye := onLoop(a, func() string { return glyphColumnShown })
	text := a.screenText(sc)
	repository, branch, activity := lineOf(text, eye+"  REPOSITORY"), lineOf(text, eye+"  BRANCH"), lineOf(text, eye+"  ACTIVITY")
	if repository < 0 || repository > branch || branch > activity {
		t.Errorf("the columns are not in the list's order:\n%s", text)
	}
	if !strings.Contains(lineAt(text, eye+"  REPOSITORY"), "always shown") {
		t.Errorf("the repository column is not marked as always shown:\n%s", text)
	}
	assertLegible(t, a, sc, "the columns in View options")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Columns")
	if !heading("PATH") {
		t.Fatalf("PATH is not shown to begin with:\n%s", a.screenText(sc))
	}
	toggle("PATH")
	waitGone(t, a, sc, "PATH")
	if !hidden(config.ListRepositories, "path") || !saved(config.ListRepositories, "path") {
		t.Error("the hidden PATH is not kept in the configuration")
	}
	toggle("PATH")
	waitFor(t, a, sc, "PATH")
	if hidden(config.ListRepositories, "path") {
		t.Error("PATH did not come back")
	}
	// The repository column stays whatever is pressed on it.
	toggle("REPOSITORY")
	waitFor(t, a, sc, "REPOSITORY")
	if hidden(config.ListRepositories, "repository") {
		t.Error("the repository column was hidden")
	}

	// Merge requests and worktrees have their own.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "AUTHOR")
	toggle("AUTHOR")
	waitGone(t, a, sc, "AUTHOR")
	if !hidden(config.ListMergeRequests, "author") || hidden(config.ListRepositories, "author") {
		t.Error("AUTHOR is not hidden in the merge requests alone")
	}
	typeRunes(sc, "3")
	waitFor(t, a, sc, "CREATED")
	toggle("CREATED")
	waitGone(t, a, sc, "CREATED")
	if !hidden(config.ListWorktrees, "created") {
		t.Error("CREATED is not hidden in the worktrees")
	}
}
