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
// cursor; Enter reads its log, cleaned of colours, and Esc comes back; R
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
	text := a.screenText(sc)
	if strings.Contains(text, "section_start") || strings.Contains(text, "[0K") || strings.Contains(text, "progress 10%") {
		t.Errorf("the log is not plain:\n%s", text)
	}
	if !strings.Contains(text, "Running tests") || !strings.Contains(text, "progress 100%") {
		t.Errorf("the log lost its text:\n%s", text)
	}
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
	typeRunes(sc, "j")
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

func TestCleanLog(t *testing.T) {
	t.Parallel()
	got := cleanLog("\x1b[32;1mok\x1b[0;m\nsection_end:12:build\r\x1b[0Kdone\n1%\r50%\r100%")
	if got != "ok\ndone\n100%" {
		t.Errorf("cleanLog = %q", got)
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
