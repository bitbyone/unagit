package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
)

// separator is the line between the favourites and the rest, inside the frame.
const separator = "│────────"

// TestFavouriteRepositoriesComeFirst: Ctrl-F stars a repository, which then
// leads the list under a line, keeps the cursor, and is remembered. Grouped,
// the headings are the order and the star only marks the row; with
// favourites first turned off in the order picker it is the same flat.
func TestFavouriteRepositoriesComeFirst(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	if strings.Contains(a.screenText(sc), favouriteMark) {
		t.Fatal("a star before anything was starred")
	}

	// billing is second by activity; starred, it comes first.
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyCtrlF, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "acme/billing is a favourite")
	screen := a.screenText(sc)
	billing, line, gateway := lineOf(screen, "★ ○ acme/billing"), lineOf(screen, separator), lineOf(screen, "○ acme/gateway")
	if !(billing >= 0 && billing < line && line < gateway) {
		t.Fatalf("billing %d, separator %d, gateway %d:\n%s", billing, line, gateway, screen)
	}
	if got := a.projects[a.projectsPane.selectedIndex()].PathWithNamespace; got != "acme/billing" {
		t.Errorf("the cursor went to %s", got)
	}
	assertLegible(t, a, sc, "a starred repository")
	saved, err := config.LoadFrom(a.cfg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Filters.IsFavourite(a.projects[0].Instance, "acme/billing", 0) {
		t.Error("the favourite was not saved")
	}

	// Grouped: no separator, the order of activity, the star still there.
	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "· grouped")
	screen = a.screenText(sc)
	if strings.Contains(screen, separator) || lineOf(screen, "○ gateway") > lineOf(screen, "★  ○ billing") {
		t.Errorf("grouped, the favourites are not set apart:\n%s", screen)
	}
	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "listed flat again")

	// Favourites first, turned off in the order picker.
	typeRunes(sc, "o")
	waitFor(t, a, sc, "favourites first: on")
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Favourites in order with the rest")
	screen = a.screenText(sc)
	if strings.Contains(screen, separator) || lineOf(screen, "○ acme/gateway") > lineOf(screen, "★ ○ acme/billing") {
		t.Errorf("the favourite still leads:\n%s", screen)
	}
	typeRunes(sc, "o")
	waitFor(t, a, sc, "favourites first: off")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "favourites first: off")

	// Unstarred, the star column goes as well.
	sc.InjectKey(tcell.KeyCtrlF, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "no longer a favourite")
	if strings.Contains(a.screenText(sc), favouriteMark) {
		t.Errorf("a star is left:\n%s", a.screenText(sc))
	}
}

// TestFavouriteMergeRequestsComeFirst: the same for a merge request, which is
// starred on its own, not with its repository.
func TestFavouriteMergeRequestsComeFirst(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	// By activity: !7, !9, !8.
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyCtrlF, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "acme/gateway !8 is a favourite")
	screen := a.screenText(sc)
	drop, line, rate := lineOf(screen, "Drop the old client"), lineOf(screen, separator), lineOf(screen, "Rate limiting")
	if !(drop >= 0 && drop < line && line < rate) || !strings.Contains(screen, "★ ○ acme/gateway !8") {
		t.Fatalf("!8 %d, separator %d, !7 %d:\n%s", drop, line, rate, screen)
	}
	assertLegible(t, a, sc, "a starred merge request")

	typeRunes(sc, "1")
	waitFor(t, a, sc, "REPOSITORY")
	if strings.Contains(a.screenText(sc), favouriteMark) {
		t.Error("starring a merge request starred its repository")
	}
}

// TestRefreshForgetsClosedFavourites: a starred merge request that a refresh
// no longer lists is merged or closed, and its star goes with it, so the
// favourites in the config do not pile up. The rest stay.
func TestRefreshForgetsClosedFavourites(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	inst := onLoop(a, func() string { return a.cfg.Instances[0].ID })
	changeOnLoop(a, func() {
		f := &a.cfg.Filters
		f.ToggleFavourite(inst, "acme/gateway", 7)    // still open
		f.ToggleFavourite(inst, "acme/gateway", 99)   // merged since
		f.ToggleFavourite(inst, "acme/gateway", 0)    // a repository
		f.ToggleFavourite("another-server", "x/y", 5) // a server not asked
		a.applyFilters()
	})

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "R")
	forgotten := func() bool {
		return onLoop(a, func() bool { return !a.cfg.Filters.IsFavourite(inst, "acme/gateway", 99) })
	}
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) && !forgotten() {
		time.Sleep(20 * time.Millisecond)
	}
	saved, err := config.LoadFrom(a.cfg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	f := saved.Filters
	if f.IsFavourite(inst, "acme/gateway", 99) {
		t.Error("the merged merge request is still a favourite")
	}
	if !f.IsFavourite(inst, "acme/gateway", 7) || !f.IsFavourite(inst, "acme/gateway", 0) ||
		!f.IsFavourite("another-server", "x/y", 5) {
		t.Errorf("a favourite that should stay went: %+v", f.Favourites)
	}
}
