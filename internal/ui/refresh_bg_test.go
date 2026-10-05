package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestARepositorysMergeRequestsCanBeHidden: x keeps a repository's merge
// requests out of the list while the repository stays listed, View Options
// names it, and space there lists them again.
func TestARepositorysMergeRequestsCanBeHidden(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gx")
	waitGone(t, a, sc, "Rate limiting")
	waitGone(t, a, sc, "Drop the old client")
	waitFor(t, a, sc, "Invoice rounding")
	waitFor(t, a, sc, "1 repo(s)' MRs")

	typeRunes(sc, "1")
	waitFor(t, a, sc, "acme/gateway") // the repository itself stays
	// Its MR column says its merge requests are hidden; billing's does not.
	if line := lineAt(a.screenText(sc), "acme/gateway"); !strings.Contains(line, glyphHidden) {
		t.Errorf("the repository does not show its merge requests hidden: %q", line)
	}
	if line := lineAt(a.screenText(sc), "acme/billing"); strings.Contains(line, glyphHidden) {
		t.Errorf("a repository whose merge requests show is marked: %q", line)
	}
	assertLegible(t, a, sc, "a repository with its merge requests hidden")

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

// TestRefreshProgressWrapsRatherThanHides: on a terminal too narrow for the
// summary and the progress side by side, the progress takes a line of its
// own above the summary, whole, the summary staying the bottom line and the
// list giving up the room.
func TestRefreshProgressWrapsRatherThanHides(t *testing.T) {
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

	for _, width := range []int{80, 50, 24} {
		resize(sc, width, 24)
		// The spinner redraws the header; once it has, the job is a line
		// of its own above the summary, which is the last line.
		deadline := time.Now().Add(patience)
		for {
			text := a.screenText(sc)
			lines := strings.Split(text, "\n")
			summary := lineOf(text, "NORMAL")
			whole := strings.Join(lines[:max(0, summary)], " ")
			if summary >= 0 && summary == len(lines)-1-trailingBlank(lines) && !strings.Contains(lines[summary], "refreshing") &&
				strings.Contains(strings.Join(strings.Fields(whole), " "), "refreshing merge requests") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("at %d columns the progress is not whole above the summary:\n%s", width, text)
			}
			time.Sleep(20 * time.Millisecond)
		}
		if width >= 50 && !strings.Contains(lineAt(a.screenText(sc), "NORMAL"), "? help") {
			t.Errorf("at %d columns the help hint left the summary's line", width)
		}
	}
	assertLegible(t, a, sc, "a wrapped progress line")
	release.Do(func() { close(hold) })
	waitGone(t, a, sc, "refreshing")
	// With nothing running the line is one row again.
	if text := a.screenText(sc); strings.TrimSpace(strings.Split(text, "\n")[23]) == "" {
		t.Errorf("the status line kept its extra row:\n%s", text)
	}
}

// trailingBlank counts the empty lines at the end of a screen's text.
func trailingBlank(lines []string) int {
	n := 0
	for i := len(lines) - 1; i >= 0 && strings.TrimSpace(lines[i]) == ""; i-- {
		n++
	}
	return n
}

// lineAt is the line of text that holds what, or "".
func lineAt(text, what string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, what) {
			return line
		}
	}
	return ""
}

// TestRefreshingARowSaysSoAtTheRight: r on a merge request runs as a job
// at the right of the status line, as R does, and what it found is said
// there too, the summary on the left as it was.
func TestRefreshingARowSaysSoAtTheRight(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	jobs := make(chan string, 1)
	changeOnLoop(a, func() {
		a.refreshMRRow(a.mrs[0])
		jobs <- a.jobLine()
	})
	if got := <-jobs; !strings.Contains(got, "refreshing !") {
		t.Errorf("r is not a job on the status line: %q", got)
	}
	waitFor(t, a, sc, "is up to date")
	line := lineAt(a.screenText(sc), "is up to date")
	if !strings.Contains(line, "? help") || strings.Index(line, "is up to date") < strings.Index(line, "merge requests") {
		t.Errorf("the result is not at the right of the status line: %q", line)
	}
	if onLoop(a, a.modalOpen) {
		t.Error("a passing word opened a box")
	}
}

// TestATaskSpinsTheStepUnderWay: the line a task logged last turns a
// spinner while it runs and is marked done when it ends.
func TestATaskSpinsTheStepUnderWay(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	changeOnLoop(a, func() {
		a.runTaskThen("Doing a thing", func(log func(string)) (string, error) {
			log("first step")
			log("second step")
			<-release
			return "", nil
		}, func(string) {})
	})
	waitFor(t, a, sc, "second step")
	frames := []rune(theme.Glyphs.Spinner)
	step := func() string { return strings.TrimSpace(strings.Trim(lineAt(a.screenText(sc), "second step"), "│ ")) }
	turned := step()
	if !strings.ContainsRune(string(frames), []rune(turned)[0]) {
		t.Fatalf("the step under way has no spinner: %q", turned)
	}
	if first := strings.TrimSpace(strings.Trim(lineAt(a.screenText(sc), "first step"), "│ ")); first != "first step" {
		t.Errorf("a finished step still has a mark: %q", first)
	}
	deadline := time.Now().Add(patience)
	for step() == turned {
		if time.Now().After(deadline) {
			t.Fatal("the spinner does not turn")
		}
		time.Sleep(20 * time.Millisecond)
	}
	assertLegible(t, a, sc, "a task under way")
	once.Do(func() { close(release) })
	waitGone(t, a, sc, "second step")
}

// TestHidingKeepsTheCursorsPlace: x on a row in the middle of the list
// takes the row away and leaves the cursor on the one that took its place,
// so sorting through the list goes on from there, not from the top.
func TestHidingKeepsTheCursorsPlace(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	selected := func() int {
		return onLoop(a, func() int {
			if i := a.mrsPane.selectedIndex(); i >= 0 {
				return a.mrs[i].IID
			}
			return 0
		})
	}
	typeRunes(sc, "j") // !9, between !7 and !8
	waitFor(t, a, sc, "Invoice rounding")
	deadline := time.Now().Add(patience)
	for selected() != 9 {
		if time.Now().After(deadline) {
			t.Fatalf("the cursor is on !%d, not !9", selected())
		}
		time.Sleep(10 * time.Millisecond)
	}
	typeRunes(sc, "x")
	waitGone(t, a, sc, "Invoice rounding")
	if got := selected(); got != 8 {
		t.Errorf("after hiding !9's repository the cursor is on !%d, want !8, the next row", got)
	}
}
