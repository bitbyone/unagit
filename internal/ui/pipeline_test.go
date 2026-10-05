package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestAPipelineUpClose: J lists the jobs with the failed one under the
// cursor; Enter reads its log, in its colours, and Esc comes back; R
// runs it again.
func TestAPipelineUpClose(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "Pipeline · acme/gateway !7 · ✗ failed")
	waitFor(t, a, sc, "unit tests")
	assertLegible(t, a, sc, "a pipeline's jobs")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "--- FAIL: TestBucket")
	assertLegible(t, a, sc, "a job's log in colour")
	text := a.screenText(sc)
	if strings.Contains(text, "section_start") || strings.Contains(text, "[0K") || strings.Contains(text, "progress 10%") {
		t.Errorf("the log is not plain:\n%s", text)
	}
	if !strings.Contains(text, "Running tests") || !strings.Contains(text, "progress 100%") {
		t.Errorf("the log lost its text:\n%s", text)
	}
	// Ctrl-D in the log scrolls it and is not the list's draft key.
	sc.InjectKey(tcell.KeyCtrlU, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyCtrlD, 0, tcell.ModNone)
	waitFor(t, a, sc, "--- FAIL: TestBucket")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	// The pipeline's title shows under the log too; the log has to be gone
	// before R, or R goes to it.
	waitGone(t, a, sc, "--- FAIL: TestBucket")
	waitFor(t, a, sc, "Pipeline · acme/gateway !7")

	typeRunes(sc, "R")
	waitFor(t, a, sc, "unit tests runs again")
	if srv.retried.Load() != 1 {
		t.Errorf("retried %d time(s), want 1", srv.retried.Load())
	}
}

// TestJobsStillToRun: the jobs that wait - a manual one, a trigger job -
// are listed with their own marks; R starts the manual one, and Enter on
// the trigger job lists the pipeline it started, Esc coming back.
func TestJobsStillToRun(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "Pipeline · acme/gateway !7")
	waitFor(t, a, sc, glyphManual+"  deploy")
	waitFor(t, a, sc, glyphTrigger+" e2e")
	text := a.screenText(sc)
	if lint, tests, deploy := strings.Index(text, "lint"), strings.Index(text, "unit tests"), strings.Index(text, "deploy"); !(lint < tests && tests < deploy) {
		t.Errorf("the jobs are not in the order of their stages:\n%s", text)
	}
	assertLegible(t, a, sc, "jobs still to run")

	typeRunes(sc, "G") // e2e, the last
	waitFor(t, a, sc, "Enter lists its jobs")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "browser tests")
	waitFor(t, a, sc, glyphTrigger+" e2e · ")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "browser tests")
	waitFor(t, a, sc, "unit tests")

	typeRunes(sc, "k") // deploy, manual
	waitFor(t, a, sc, "waits to be started")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "deploy started")
	if srv.played.Load() != 1 || srv.retried.Load() != 0 {
		t.Errorf("played %d, retried %d; want the manual job played", srv.played.Load(), srv.retried.Load())
	}
}

// TestARunningPipelineIsFollowed: the jobs of a running pipeline and the
// log of a running job are read again as they go, the log keeping to its
// end, and the following stops once the job is done.
func TestARunningPipelineIsFollowed(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	changeOnLoop(a, func() { a.ciEvery = 50 * time.Millisecond })
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, glyphTrigger+" e2e")
	typeRunes(sc, "G")
	waitFor(t, a, sc, "Enter lists its jobs")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "browser tests")
	waitFor(t, a, sc, "running · following")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "step one")
	waitFor(t, a, sc, "browser tests · running · following")
	srv.e2eLog.Store("step two\n")
	waitFor(t, a, sc, "step two")
	srv.e2eLog.Store("step two\nall green\n")
	srv.e2eDone.Store(true)
	waitFor(t, a, sc, "all green")
	waitFor(t, a, sc, "browser tests · success")
	waitGone(t, a, sc, "following")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "all green")
	// Back on the child pipeline, read again and no longer followed.
	waitFor(t, a, sc, glyphTrigger+" e2e · "+glyphCheck+" success")
	if strings.Contains(a.screenText(sc), "following") {
		t.Errorf("a finished pipeline is still followed:\n%s", a.screenText(sc))
	}
}

// TestTheJobsFollowTheirPipeline: the list of a running pipeline's jobs
// changes in place as the jobs do, the cursor where it was.
func TestTheJobsFollowTheirPipeline(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	changeOnLoop(a, func() { a.ciEvery = 50 * time.Millisecond })
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, glyphTrigger+" e2e")
	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "running · following")
	list := onLoop(a, func() *tview.List { l, _ := a.tv.GetFocus().(*tview.List); return l })
	srv.e2eDone.Store(true)
	waitFor(t, a, sc, "browser tests · success")
	waitGone(t, a, sc, "following")
	if same := onLoop(a, func() bool { l, _ := a.tv.GetFocus().(*tview.List); return l == list }); !same {
		t.Error("the jobs were opened again rather than put in place")
	}
}

// TestTheBrowserLeavesThePipelineOpen: w and W open the job and the
// pipeline in the browser and the jobs stay on screen, the cursor where it
// was. Serial: it swaps the browser.
func TestTheBrowserLeavesThePipelineOpen(t *testing.T) {
	opened := make(chan string, 4)
	saved := openBrowser
	openBrowser = func(url string) error { opened <- url; return nil }
	t.Cleanup(func() { openBrowser = saved })

	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "Pipeline · acme/gateway !7")
	waitFor(t, a, sc, "unit tests")
	list := onLoop(a, func() *tview.List { l, _ := a.tv.GetFocus().(*tview.List); return l })
	if list == nil {
		t.Fatal("the jobs are not a list in focus")
	}
	cursor := func() string {
		return onLoop(a, func() string {
			main, _ := list.GetItemText(list.GetCurrentItem())
			return main
		})
	}
	typeRunes(sc, "k") // off the failed job, onto lint
	waitFor(t, a, sc, "lint · success")
	before := cursor()
	for _, key := range []string{"w", "W"} {
		typeRunes(sc, key)
		select {
		case <-opened:
		case <-time.After(patience):
			t.Fatalf("%s opened nothing", key)
		}
		waitFor(t, a, sc, "opened ")
		waitFor(t, a, sc, "Pipeline · acme/gateway !7")
		if !onLoop(a, func() bool { return a.pages.HasPage(pagePicker) }) {
			t.Fatalf("%s closed the jobs:\n%s", key, a.screenText(sc))
		}
		if got := cursor(); got != before {
			t.Errorf("after %s the cursor is on %q, not %q", key, got, before)
		}
	}
}

// TestMarkAsReviewed: V takes the head as seen, without a review on disk;
// what is pushed after it shows as new, and the mark is kept beside the
// indexes.
func TestMarkAsReviewed(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	head := mrOnOrigin(t, srv, p, "Add a token bucket")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].SHA = head
			}
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gV")
	waitFor(t, a, sc, "!7 marked as reviewed at "+head[:8])
	data, err := os.ReadFile(a.cfg.IndexPath("seen"))
	must(t, err)
	if !strings.Contains(string(data), head) {
		t.Errorf("the mark was not kept: %s", data)
	}

	newer := mrOnOrigin(t, srv, p, "Answer the review")
	gitIn(t, p.clone, "fetch", "-q", "origin", "refs/merge-requests/7/head")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].SHA = newer
			}
		}
		a.refreshDisk()
	})
	waitFor(t, a, sc, "●1")
}

// TestARefreshSaysWhereToLook: the summary after r names what failed.
func TestARefreshSaysWhereToLook(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "1 pipeline(s) failed")
	waitFor(t, a, sc, "✗")
}

// TestLogInColour: a job's log is drawn in its own colours, its brackets
// as text, without underline or black ink, and of GitLab's section markers
// and a progress bar's rewrites only the text that stays.
func TestLogInColour(t *testing.T) {
	t.Parallel()
	raw := "\x1b[32;1mok\x1b[0;m [red] [x]\n" +
		"section_end:12:build\r\x1b[0Kdone\n" +
		"1%\r50%\r100%\n" +
		"\x1b[4;31munder\x1b[0m \x1b[30mblack\n" +
		"\x1b[38;5;208morange\x1b[0m plain"
	sc := tcell.NewSimulationScreen("UTF-8")
	must(t, sc.Init())
	sc.SetSize(40, 8)
	view := tview.NewTextView().SetDynamicColors(true).SetTextColor(colText)
	view.SetText(logMarkup(raw))
	view.SetRect(0, 0, 40, 8)
	view.Draw(sc)
	sc.Show()

	cells, width, _ := sc.GetContents()
	row := func(y int) string {
		var b strings.Builder
		for x := 0; x < width; x++ {
			if r := cells[y*width+x].Runes; len(r) > 0 {
				b.WriteRune(r[0])
			}
		}
		return strings.TrimRight(b.String(), " ")
	}
	for y, want := range []string{"ok [red] [x]", "done", "100%", "under black", "orange plain"} {
		if got := row(y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}
	style := func(x, y int) (tcell.Color, tcell.AttrMask) {
		fg, _, attr := cells[y*width+x].Style.Decompose()
		return fg, attr
	}
	if fg, attr := style(0, 0); fg != tcell.ColorGreen || attr&tcell.AttrBold == 0 {
		t.Errorf("ok is %v %v, want bold green", fg, attr)
	}
	if fg, _ := style(4, 0); fg != colText {
		t.Errorf("after the reset the text is %v, want the view's own %v", fg, colText)
	}
	if fg, attr := style(0, 3); fg != tcell.ColorMaroon || attr&tcell.AttrUnderline != 0 {
		t.Errorf("under is %v %v, want red and not underlined", fg, attr)
	}
	if fg, _ := style(6, 3); fg != tcell.ColorGray {
		t.Errorf("black ink is %v, want grey", fg)
	}
	if fg, _ := style(0, 4); fg == colText {
		t.Error("an extended colour was lost")
	}
	if fg, _ := style(7, 4); fg != colText {
		t.Errorf("plain after an extended colour is %v", fg)
	}
}

// TestJShowsABranchsPipelineEverywhere: J in Repositories reads the pipeline
// of the clone's branch, in Worktrees the worktree's branch, and on a group
// it asks which repository first.
func TestJShowsABranchsPipelineEverywhere(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")

	// Not cloned: the default branch.
	typeRunes(sc, "g")
	typeRunes(sc, "J")
	waitFor(t, a, sc, "Pipeline · acme/gateway · main")
	waitFor(t, a, sc, "build")
	assertLegible(t, a, sc, "a branch's pipeline")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Pipeline · acme/gateway")

	// A worktree: its own branch.
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feature/audit-log")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "feature/audit-log")
	typeRunes(sc, "J")
	waitFor(t, a, sc, "Pipeline · acme/gateway · feature/audit-log")
	if got, _ := srv.pipelineRefs.Load().(string); got != "feature/audit-log" {
		t.Errorf("the pipeline was asked for %q", got)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Pipeline · acme/gateway")
}

// TestJOnAGroupAsksWhichRepository: a group's pipelines are its
// repositories'; J lists them, and the one picked shows its own.
func TestJOnAGroupAsksWhichRepository(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/ci")
	waitFor(t, a, sc, "feat-ci")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	typeRunes(sc, "J")
	waitFor(t, a, sc, "Pipeline of which repository · feat-ci")
	waitFor(t, a, sc, "acme/billing")
	assertLegible(t, a, sc, "the group's repositories")
	// The list is in the group's order; pick the gateway by name.
	typeRunes(sc, "/")
	typeRunes(sc, "gateway")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Pipeline · acme/gateway · feat/ci")
}

// TestHalfPage: Ctrl-D and Ctrl-U move a reader by half its height, and
// Ctrl-U stops at the top.
func TestHalfPage(t *testing.T) {
	t.Parallel()
	view := tview.NewTextView().SetScrollable(true)
	view.SetRect(0, 0, 40, 20)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	view.SetText(strings.Join(lines, "\n"))
	view.ScrollToBeginning()
	key := func(k tcell.Key) int {
		if !halfPage(view, tcell.NewEventKey(k, 0, tcell.ModNone)) {
			t.Fatalf("%v was not taken", k)
		}
		row, _ := view.GetScrollOffset()
		return row
	}
	for _, step := range []struct {
		key  tcell.Key
		want int
	}{{tcell.KeyCtrlD, 10}, {tcell.KeyCtrlD, 20}, {tcell.KeyCtrlU, 10}, {tcell.KeyCtrlU, 0}, {tcell.KeyCtrlU, 0}} {
		if got := key(step.key); got != step.want {
			t.Fatalf("after %v the top row is %d, want %d", step.key, got, step.want)
		}
	}
	if halfPage(view, tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone)) {
		t.Error("j was taken for a half page")
	}
}
