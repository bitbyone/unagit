package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestAPipelineUpClose: J lists the jobs with the failed one under the
// cursor; Enter reads its log, in its colours, and Esc comes back; R
// runs it again, the jobs staying on screen and saying so in their edge.
func TestAPipelineUpClose(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "Pipeline · acme/gateway !7 · "+glyphCIDone+" failed")
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

	gate := make(chan struct{})
	srv.retryGate.Store(gate)
	typeRunes(sc, "R")
	waitFor(t, a, sc, "Retrying unit tests…")
	if text := a.screenText(sc); !strings.Contains(text, "Pipeline · acme/gateway !7") || strings.Contains(text, "j/k scroll") {
		t.Errorf("the jobs did not stay, or a log came over them:\n%s", text)
	}
	close(gate)
	waitFor(t, a, sc, "unit tests runs again")
	waitFor(t, a, sc, "Pipeline · acme/gateway !7")
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
	waitFor(t, a, sc, "lists its jobs")
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
	waitFor(t, a, sc, "lists its jobs")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "browser tests")
	waitFor(t, a, sc, "running · following")
	// The mark of the running job turns while the list is open.
	first := rowWith(a, sc, "browser tests")
	deadline := time.Now().Add(patience)
	for rowWith(a, sc, "browser tests") == first {
		if time.Now().After(deadline) {
			t.Fatalf("the running job's mark does not turn: %q", first)
		}
		time.Sleep(50 * time.Millisecond)
	}

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
	waitFor(t, a, sc, glyphTrigger+" e2e · "+glyphCIDone+" success")
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
	typeRunes(sc, "kk") // off the failed job and its earlier attempt, onto lint
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
	waitFor(t, a, sc, "1 pipeline failed")
	waitFor(t, a, sc, glyphCIDone)
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

// TestEarlierPipelinesAndAttempts: a job run again keeps its earlier
// attempt under it, whose log can still be read; P lists the merge
// request's earlier pipelines, Enter one's jobs, and Esc comes back the
// way it went.
func TestEarlierPipelinesAndAttempts(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, glyphRetried+" unit tests")
	// The cursor is on the failure that counts, not on the attempt before.
	waitFor(t, a, sc, "unit tests · failed · 1m15s")
	text := a.screenText(sc)
	// The jobs are a table: named columns, when each began and by whom.
	if head, row := lineAt(text, "STARTED"), lineAt(text, "1m15s"); !strings.Contains(head, "STAGE") || !strings.Contains(head, "BY") ||
		!strings.Contains(row, "6y ago") || !strings.Contains(row, "jane") {
		t.Errorf("the jobs do not say when and by whom:\n%s", text)
	}
	if lineOf(text, glyphRetried+" unit tests") > lineOf(text, glyphCIDone+"  test    unit tests") {
		t.Errorf("the earlier attempt is not above the one that followed:\n%s", text)
	}
	typeRunes(sc, "k")
	waitFor(t, a, sc, "an earlier attempt")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "--- FAIL: TestFlaky")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "TestFlaky")

	typeRunes(sc, "P")
	waitFor(t, a, sc, "Pipelines · acme/gateway !7")
	waitFor(t, a, sc, "#80")
	if text := a.screenText(sc); !strings.Contains(lineAt(text, "PIPELINE"), "BY") || !strings.Contains(lineAt(text, "#90"), "jane") {
		t.Errorf("the pipelines do not say by whom:\n%s", text)
	}
	assertLegible(t, a, sc, "the pipelines of a merge request")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, glyphCIDone+" success")
	waitGone(t, a, sc, "deploy")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Pipelines · acme/gateway !7")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "deploy")
}

// TestACommitsPipelines: J in a commit log reads the pipelines of the
// commit under the cursor; one goes straight to its jobs, and Esc comes
// back to the log.
func TestACommitsPipelines(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	p.rescan()
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "J")
	waitFor(t, a, sc, "Pipeline · acme/gateway · ")
	waitFor(t, a, sc, "build")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
}

// TestFilesInColour: a commit's detail lists the files it changed with the
// lines gained in the colour of good and those lost in that of bad - from
// the forge when the commit is not on disk.
func TestFilesInColour(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Add rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go")
	text := a.screenText(sc)
	line := lineAt(text, "limit.go")
	if !strings.Contains(line, "+2") || !strings.Contains(line, "-1") || !strings.Contains(line, "++-") {
		t.Errorf("the file's counts are not there: %q", line)
	}
	if !strings.Contains(lineAt(text, "logo.png"), "binary") {
		t.Errorf("a binary file is not said to be one:\n%s", text)
	}
	row := lineOf(text, "limit.go")
	col := len([]rune(line[:strings.Index(line, "++-")]))
	if _, style := cellAt(a, sc, col, row); func() bool { fg, _, _ := style.Decompose(); return fg.Hex() != colOn.Hex() }() {
		t.Error("added lines are not in the colour of good")
	}
	if _, style := cellAt(a, sc, col+2, row); func() bool { fg, _, _ := style.Decompose(); return fg.Hex() != colBad.Hex() }() {
		t.Error("deleted lines are not in the colour of bad")
	}
	assertLegible(t, a, sc, "a commit's files")
}

// TestBrowserKeysStay: a key of a list that opens the browser is made by
// browserKey, which keeps the list open; a pickKey written by hand around
// openWeb once closed the commit log behind the browser.
func TestBrowserKeysStay(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	must(t, err)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		must(t, err)
		src := string(data)
		for at := strings.Index(src, "pickKey{"); at >= 0; {
			// The literal, to its closing brace.
			depth, end := 0, at+len("pickKey")
			for i := end; i < len(src); i++ {
				if src[i] == '{' {
					depth++
				} else if src[i] == '}' {
					depth--
					if depth == 0 {
						end = i
						break
					}
				}
			}
			// browserKey's own literal is the one place it may be.
			inBrowserKey := strings.HasSuffix(src[:at], "return ") && strings.Contains(src[max(0, at-400):at], "func (a *App) browserKey(")
			if lit := src[at:end]; !inBrowserKey && (strings.Contains(lit, "openWeb(") || strings.Contains(lit, "openBrowser(")) {
				t.Errorf("%s: a list's key opens the browser by hand; make it with browserKey:\n%s", file, lit)
			}
			next := strings.Index(src[end:], "pickKey{")
			if next < 0 {
				break
			}
			at = end + next
		}
	}
}

// TestTheLogStaysBehindTheBrowser: w in a commit log opens the commit and
// leaves the log open, what was opened said in its edge, not on the main
// screen's status line. Serial: it swaps the browser.
func TestTheLogStaysBehindTheBrowser(t *testing.T) {
	opened := make(chan string, 2)
	saved := openBrowser
	openBrowser = func(url string) error { opened <- url; return nil }
	t.Cleanup(func() { openBrowser = saved })

	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Add rate limiting")
	typeRunes(sc, "w")
	select {
	case <-opened:
	case <-time.After(patience):
		t.Fatal("w opened nothing")
	}
	waitFor(t, a, sc, "opened ")
	if !onLoop(a, func() bool { return a.pages.HasPage(pagePicker) }) {
		t.Fatalf("w closed the log:\n%s", a.screenText(sc))
	}
	if said := onLoop(a, func() string { return a.transient }); said != "" {
		t.Errorf("the word went to the main screen's status line: %q", said)
	}
}

// TestARunningPipelineTurnsAndIsFollowed: a shown merge request whose
// pipeline runs has a turning mark in CI, is asked about again until the
// pipeline has ended, and then the watching stops.
func TestARunningPipelineTurnsAndIsFollowed(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	changeOnLoop(a, func() { a.ciAskEvery = 1500 * time.Millisecond })
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 9 {
				a.mrs[i].Pipeline = "running"
			}
		}
		a.mrsPane.reload()
	})
	frames := []rune(theme.Glyphs.CIRunning)
	seen := map[rune]bool{}
	deadline := time.Now().Add(patience)
	for len(seen) < 2 {
		line := rowWith(a, sc, "Invoice rounding")
		for _, f := range frames {
			if strings.ContainsRune(line, f) {
				seen[f] = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the running pipeline's mark does not turn: %q", line)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The fixture's pipeline for !9 passed: asked again, it ends.
	for onLoop(a, func() bool { return a.ciWatching }) {
		if time.Now().After(deadline) {
			t.Fatal("the watching never stopped")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := onLoop(a, func() string {
		for _, mr := range a.mrs {
			if mr.IID == 9 {
				return mr.Pipeline
			}
		}
		return ""
	}); got != "success" {
		t.Errorf("!9's pipeline is %q after following it", got)
	}
}

// TestJobStatesAreInTheirColours: a job's mark and its state are drawn in
// the state's colour, as the CI column draws it.
func TestJobStatesAreInTheirColours(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "lint")
	// lint passed, and the cursor is on the failed unit tests, not on it.
	text := a.screenText(sc)
	row := lineOf(text, "check   lint")
	line := strings.Split(text, "\n")[row]
	markCol := len([]rune(line[:strings.Index(line, glyphCIDone)]))
	wordCol := len([]rune(line[:strings.Index(line, "success")]))
	for what, col := range map[string]int{"mark": markCol, "state": wordCol} {
		_, style := cellAt(a, sc, col, row)
		if fg, _, _ := style.Decompose(); fg.Hex() != role("ci.success").Hex() {
			t.Errorf("the %s of a job that passed is %06x, not the colour of success", what, fg.Hex())
		}
	}
	assertLegible(t, a, sc, "jobs in their colours")
}

// TestOnlyWhatRunsTurns: a running pipeline's mark is a frame of the
// turning circle; one that waits its turn is the empty circle, standing
// still, in the same colour.
func TestOnlyWhatRunsTurns(t *testing.T) {
	t.Parallel()
	running, runColour := ciMark("running")
	if !strings.Contains(theme.Glyphs.CIRunning, running) {
		t.Errorf("running is drawn %q, not a frame of %q", running, theme.Glyphs.CIRunning)
	}
	for _, status := range []string{"created", "pending", "waiting_for_resource", "preparing"} {
		mark, colour := ciMark(status)
		if mark != glyphCIIdle || colour != runColour {
			t.Errorf("%s is drawn %q, want a still %q in the running colour", status, mark, glyphCIIdle)
		}
	}
}

// TestTheLogOfABranchJobIsFollowed: the log of a running job of a
// repository's own pipeline - not a child pipeline's - takes the lines the
// job writes while it is open.
func TestTheLogOfABranchJobIsFollowed(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	srv.mainRunning.Store(true)
	changeOnLoop(a, func() { a.ciEvery = 50 * time.Millisecond })
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "running · following")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "compiling")
	srv.mainLog.Store("linking\n")
	waitFor(t, a, sc, "linking")
}
