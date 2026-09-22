package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
)

// cloneOnDisk makes a project look cloned, which is all the "cloned only"
// filter looks at.
func cloneOnDisk(t *testing.T, a *App, instance, path string) {
	t.Helper()
	dir := filepath.Join(a.projectDir(instance, path), ".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		close(done)
	})
	<-done
}

// TestClonedOnlyNarrowsBothLists: one key, both tabs.
func TestClonedOnlyNarrowsBothLists(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	cloneOnDisk(t, a, a.cfg.Instances[0].ID, "acme/gateway")
	waitFor(t, a, sc, "acme/billing")

	typeRunes(sc, "C")
	waitFor(t, a, sc, "Showing only the projects you have cloned")
	waitGone(t, a, sc, "acme/billing")
	waitFor(t, a, sc, "acme/gateway")
	waitFor(t, a, sc, "cloned only")

	// The merge request list is narrowed by the same setting.
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	if strings.Contains(a.screenText(sc), "Invoice rounding") {
		t.Error("the merge request of an uncloned project is still listed")
	}

	typeRunes(sc, "C")
	waitFor(t, a, sc, "Invoice rounding")
}

// TestHidingAProjectHidesItsMergeRequests
func TestHidingAProjectHidesItsMergeRequests(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	// The cursor starts on the newest project, acme/gateway. The row goes;
	// the status message naming it stays, so the row marker is the anchor.
	typeRunes(sc, "x")
	waitFor(t, a, sc, "acme/gateway hidden")
	waitGone(t, a, sc, "○ acme/gateway")
	waitFor(t, a, sc, "⊘ 1")

	typeRunes(sc, "M")
	waitFor(t, a, sc, "Invoice rounding")
	if strings.Contains(a.screenText(sc), "Rate limiting") {
		t.Error("the merge request of a hidden project is still listed")
	}

	// It is remembered.
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Filters.IsHidden(a.cfg.Instances[0].ID, "acme/gateway") {
		t.Fatalf("hidden = %+v", saved.Filters.Hidden)
	}
}

// TestHiddenPickerBringsThemBack: the modal is the one place a hidden project
// can still be found.
func TestHiddenPickerBringsThemBack(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "x")
	waitGone(t, a, sc, "○ acme/gateway")

	typeRunes(sc, "X")
	waitFor(t, a, sc, "Hidden projects")
	waitFor(t, a, sc, "space  hide / show")
	// Both are listed, the hidden one marked.
	waitFor(t, a, sc, "⊘ acme/gateway")
	waitFor(t, a, sc, "· acme/billing")

	// The modal lists them by path, so the hidden one is the second row.
	typeRunes(sc, "j")
	typeRunes(sc, " ")
	waitFor(t, a, sc, "0 hidden")
	if len(a.cfg.Filters.Hidden) != 0 {
		t.Fatalf("hidden = %+v", a.cfg.Filters.Hidden)
	}

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Hidden projects")
	waitFor(t, a, sc, "○ acme/gateway")
}

// TestHiddenPickerShowsAll
func TestHiddenPickerShowsAll(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		id := a.cfg.Instances[0].ID
		a.cfg.Filters.ToggleHidden(id, "acme/gateway")
		a.cfg.Filters.ToggleHidden(id, "acme/billing")
		a.applyFilters()
		close(done)
	})
	<-done

	typeRunes(sc, "X")
	waitFor(t, a, sc, "2 hidden")
	typeRunes(sc, "a")
	waitFor(t, a, sc, "2 project(s) are back")
	if len(a.cfg.Filters.Hidden) != 0 {
		t.Fatalf("hidden = %+v", a.cfg.Filters.Hidden)
	}
}

// TestSortOrderIsSharedAndRemembered
func TestSortOrderIsSharedAndRemembered(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	// By activity, the newest project is first.
	if got := firstRow(t, a, sc); !strings.Contains(got, "acme/gateway") {
		t.Fatalf("first row by activity = %q", got)
	}
	waitFor(t, a, sc, "by activity")

	typeRunes(sc, "o")
	waitFor(t, a, sc, "Sort both lists")
	waitFor(t, a, sc, "by name")
	typeRunes(sc, "j") // leave the input, land on the list
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "Sorted by name")
	if got := firstRow(t, a, sc); !strings.Contains(got, "acme/billing") {
		t.Fatalf("first row by name = %q", got)
	}

	// The merge request list follows the same setting.
	typeRunes(sc, "M")
	waitFor(t, a, sc, "by name")
	if got := firstRow(t, a, sc); !strings.Contains(got, "acme/billing") {
		t.Fatalf("first merge request row by name = %q", got)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Filters.Order() != config.SortName {
		t.Errorf("saved order = %q", saved.Filters.Order())
	}
}

// firstRow is the first data row of the visible table.
func firstRow(t *testing.T, a *App, sc tcell.SimulationScreen) string {
	t.Helper()
	lines := strings.Split(a.screenText(sc), "\n")
	for i, line := range lines {
		if strings.Contains(line, "PROJECT") && strings.Contains(line, "│") {
			if i+1 < len(lines) {
				return lines[i+1]
			}
		}
	}
	t.Fatalf("no table on screen:\n%s", a.screenText(sc))
	return ""
}

// TestFiltersSurviveARestart
func TestFiltersSurviveARestart(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	// Hide first: with "cloned only" on, this fixture has nothing on disk and
	// so no row to hide.
	typeRunes(sc, "x")
	waitFor(t, a, sc, "⊘ 1")
	typeRunes(sc, "C")
	waitFor(t, a, sc, "cloned only")
	time.Sleep(80 * time.Millisecond)

	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Filters.ClonedOnly || len(saved.Filters.Hidden) != 1 {
		t.Fatalf("filters = %+v", saved.Filters)
	}
	if !saved.Filters.Active() {
		t.Error("Active should report that something is narrowing the lists")
	}
}
