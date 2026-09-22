package ui

import (
	"encoding/json"
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

// TestCommentCountInTheList: the number is worth seeing before opening
// anything.
func TestCommentCountInTheList(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, "COM")

	lines := strings.Split(a.screenText(sc), "\n")
	var rate, invoice string
	for _, l := range lines {
		if strings.Contains(l, "Rate limiting") {
			rate = l
		}
		if strings.Contains(l, "Invoice rounding") {
			invoice = l
		}
	}
	// The fixture gives !7 four comments and !9 none.
	if !strings.Contains(rate, " 4 ") {
		t.Errorf("the count is missing from %q", rate)
	}
	if strings.Contains(invoice, " 0 ") {
		t.Errorf("a merge request with no comments should show nothing: %q", invoice)
	}
}

// TestGroupByProject: the merge requests gather under their project and keep
// the shared order inside it.
func TestGroupByProject(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Merge requests grouped by project")
	waitFor(t, a, sc, "grouped")

	lines := strings.Split(a.screenText(sc), "\n")
	at := func(needle string) int {
		for i, l := range lines {
			if strings.Contains(l, needle) {
				return i
			}
		}
		t.Fatalf("%q is not on screen:\n%s", needle, strings.Join(lines, "\n"))
		return -1
	}
	// The project heading says how many, and its merge requests follow it in
	// the shared order - newest first.
	gateway := at("acme/gateway  (2)")
	rate := at("Rate limiting")
	drop := at("Drop the old client")
	billing := at("acme/billing  (1)")
	invoice := at("Invoice rounding")
	if !(gateway < rate && rate < drop && drop < billing && billing < invoice) {
		t.Fatalf("order: heading %d, !7 %d, !8 %d, heading %d, !9 %d",
			gateway, rate, drop, billing, invoice)
	}

	// The cursor skips the headings: the first row is a merge request.
	if got := a.mrsPane.selectedIndex(); got < 0 {
		t.Fatal("no merge request is selected")
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")

	// Switching the order re-sorts inside the groups.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		a.cfg.Filters.Sort = config.SortName
		a.applyFilters()
		close(done)
	})
	<-done
	lines = strings.Split(a.screenText(sc), "\n")
	if at("acme/billing  (1)") > at("acme/gateway  (2)") {
		t.Error("by name, billing should come before gateway")
	}

	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "listed flat again")
	if strings.Contains(a.screenText(sc), "acme/gateway  (2)") {
		t.Error("the headings are still there")
	}
}

// TestStaleIndexSaysSo: a cache written before a field existed leaves its
// column empty, which on its own looks like the feature not working.
func TestStaleIndexSaysSo(t *testing.T) {
	srv := fakeGitLab(t)
	cfg := writeTestConfig(t, srv.URL)

	// Rewrite the merge request cache the way an older unagit would have: no
	// version, and no comment counts.
	raw, err := os.ReadFile(config.IndexPath("mrs"))
	if err != nil {
		t.Fatal(err)
	}
	var cached map[string]any
	if err := json.Unmarshal(raw, &cached); err != nil {
		t.Fatal(err)
	}
	delete(cached, "version")
	out, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.IndexPath("mrs"), out, 0o600); err != nil {
		t.Fatal(err)
	}

	a, sc := startApp(t, New(cfg, testVault(t, cfg)))
	waitFor(t, a, sc, "The cached index is from an older unagit")
	if !onLoop(a, func() bool { return a.staleMRs }) {
		t.Error("the merge request index was not noticed as stale")
	}

	// Refreshing clears it, and brings back what the old cache could not
	// hold. The task closes itself when it succeeds, so the effect is what
	// gets waited for rather than the line it logs.
	typeRunes(sc, "M")
	typeRunes(sc, "r")
	stale := func() bool { return onLoop(a, func() bool { return a.staleMRs }) }
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && stale() {
		time.Sleep(20 * time.Millisecond)
	}
	if stale() {
		t.Fatal("still marked stale after a refresh")
	}
	waitFor(t, a, sc, "Rate limiting")
	for _, line := range strings.Split(a.screenText(sc), "\n") {
		if strings.Contains(line, "Rate limiting") && !strings.Contains(line, " 4 ") {
			t.Errorf("the comment count did not arrive with the refresh: %q", line)
		}
	}
}
