package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestARepositorysMergeRequestsCanBeHidden: Alt-H keeps a repository's merge
// requests out of the list while the repository stays listed, View Options
// names it, and space there lists them again.
func TestARepositorysMergeRequestsCanBeHidden(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyRune, 'h', tcell.ModAlt)
	waitGone(t, a, sc, "Rate limiting")
	waitGone(t, a, sc, "Drop the old client")
	waitFor(t, a, sc, "Invoice rounding")
	waitFor(t, a, sc, "1 repo(s)' MRs")

	typeRunes(sc, "1")
	waitFor(t, a, sc, "acme/gateway") // the repository itself stays

	typeRunes(sc, "2")
	typeRunes(sc, "v")
	waitFor(t, a, sc, "its merge requests")
	assertLegible(t, a, sc, "the view options with a repository hidden")
	// The hidden repository is the last row.
	typeRunes(sc, "G ")
	waitFor(t, a, sc, "nothing hidden")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Rate limiting")
}

// TestRefreshAsksOnlyAboutWhatIsShown: a refresh leaves the merge requests
// the list hides out of its questions, and they keep what was last known.
func TestRefreshAsksOnlyAboutWhatIsShown(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].Pipeline = "success"
			}
		}
		a.cfg.Filters.ToggleMRsOf(a.cfg.Instances[0].ID, "acme/gateway")
		a.applyFilters()
		return true
	})
	// Held, or the refresh can be over before the screen shows it.
	hold := make(chan struct{})
	srv.holdMRList.Store(hold)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(hold) }) })
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Invoice rounding")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "refreshing merge requests")
	release.Do(func() { close(hold) })
	waitGone(t, a, sc, "refreshing merge requests")
	if n := srv.mr7Pipelines.Load(); n != 0 {
		t.Errorf("a hidden merge request's pipeline was asked for %d time(s)", n)
	}
	got := onLoop(a, func() string {
		for _, mr := range a.mrs {
			if mr.IID == 7 {
				return mr.Pipeline
			}
		}
		return "missing"
	})
	if got != "success" {
		t.Errorf("!7 lost what was known of it: pipeline %q", got)
	}
}

// TestRefreshRunsBehindTheList: R opens no dialog; the header turns a
// spinner and says what is under way, the list can be moved in meanwhile,
// and a second R does not start a second refresh.
func TestRefreshRunsBehindTheList(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	hold := make(chan struct{})
	srv.holdMRList.Store(hold)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(hold) }) })
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "refreshing merge requests")
	if onLoop(a, a.modalOpen) {
		t.Fatalf("the refresh opened a dialog:\n%s", a.screenText(sc))
	}
	first := lineOf(a.screenText(sc), "refreshing merge requests")
	// The job sits at the right-hand end, after the list's summary and
	// just before the help hint.
	if lines := strings.Split(a.screenText(sc), "\n"); first >= 0 {
		line := lines[first]
		summary, job, help := strings.Index(line, "NORMAL"), strings.Index(line, "refreshing"), strings.Index(line, "? help")
		if !(summary >= 0 && summary < job && job < help) || strings.Contains(line[job:help], "indexed") {
			t.Errorf("the job is not at the right of the header, before the help:\n%s", line)
		}
	}
	frame := func() string { return onLoop(a, func() string { return a.jobLine() }) }
	turned := frame()
	deadline := time.Now().Add(patience)
	for frame() == turned {
		if time.Now().After(deadline) {
			t.Fatal("the spinner does not turn")
		}
		time.Sleep(20 * time.Millisecond)
	}
	before := onLoop(a, a.mrsPane.selectedIndex)
	typeRunes(sc, "j")
	waitSelectedNot(t, a, before)
	typeRunes(sc, "R")
	waitFor(t, a, sc, "already refreshing merge requests")
	assertLegible(t, a, sc, "the list while a refresh runs")

	release.Do(func() { close(hold) })
	waitGone(t, a, sc, "refreshing merge requests")
	if first < 0 {
		t.Error("the job was not in the header")
	}
}

// waitSelectedNot waits until the merge request cursor leaves a row.
func waitSelectedNot(t *testing.T, a *App, was int) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for onLoop(a, a.mrsPane.selectedIndex) == was {
		if time.Now().After(deadline) {
			t.Fatal("the cursor did not move while the refresh ran")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
