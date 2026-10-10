package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCommitLogs: Ctrl-L lists the commits of a repository not yet cloned
// from the server, of a clone from git - what points at each, what is not
// pushed - and of a merge request; the pane under the list says who made the
// commit under the cursor and the rest of its message, and Enter opens the
// commit's detail, Esc coming back to the log.
func TestCommitLogs(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main on the server)")
	waitFor(t, a, sc, "Add rate limiting")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")

	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Count requests per client", "Clients are told apart by their token.")
	gitIn(t, p.clone, "push", "-q")
	commitIn(t, p.clone, "c.txt", "Not pushed yet")
	p.rescan()

	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	text := a.screenText(sc)
	if line := strings.Split(text, "\n")[lineOf(text, "Not pushed yet")]; !strings.Contains(line, "↑") || !strings.Contains(line, "HEAD→main") {
		t.Errorf("the newest commit is not marked unpushed and HEAD: %q", line)
	}
	if line := strings.Split(text, "\n")[lineOf(text, "Count requests")]; !strings.Contains(line, "origin/main") {
		t.Errorf("origin's copy is not shown where it is: %q", line)
	}
	assertLegible(t, a, sc, "a commit log")

	typeRunes(sc, "j")
	waitFor(t, a, sc, "Clients are told apart")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "FILES")
	waitFor(t, a, sc, "b.txt")
	assertLegible(t, a, sc, "a commit's detail")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "FILES")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	if got := a.screenText(sc); !strings.Contains(got, "Clients are told apart") {
		t.Errorf("the log did not come back on the same commit:\n%s", got)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway !7")
	waitFor(t, a, sc, "Token bucket")
	waitFor(t, a, sc, "jane ·")
	waitFor(t, a, sc, "review from here")
}

// TestCheckOutACommitAndComeBack: C in a clone's log puts HEAD on an older
// commit; the row then says which commit and how far behind its branch it is,
// and B goes back to the branch.
func TestCheckOutACommitAndComeBack(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	first := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	gitIn(t, p.clone, "push", "-q")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "jC")
	waitFor(t, a, sc, "@"+first[:8])
	waitFor(t, a, sc, "↓1 main")
	if got := gitIn(t, p.clone, "rev-parse", "HEAD"); got != first {
		t.Errorf("HEAD is at %s, want %s", got, first)
	}

	typeRunes(sc, "B")
	waitFor(t, a, sc, "acme/gateway")
	waitGone(t, a, sc, "@"+first[:8])
	if got := gitIn(t, p.clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("B left the clone on %s", got)
	}
}

// TestABranchAndAWorktreeFromACommit: n starts a branch at the commit under
// the cursor, Ctrl-W a branch at it in a worktree of its own; the clone
// stays where it was.
func TestABranchAndAWorktreeFromACommit(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	first := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "jn")
	waitFor(t, a, sc, "New Branch at "+first[:8])
	assertLegible(t, a, sc, "the name of a branch at a commit")
	typeRunes(sc, "old-state")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "r")
	waitFor(t, a, sc, "created old-state at "+first[:8])
	if got := gitIn(t, p.clone, "rev-parse", "old-state"); got != first {
		t.Errorf("old-state is at %s, want %s", got, first)
	}
	waitFor(t, a, sc, "old-state") // the log, read again, shows the branch

	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyCtrlW, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "New Worktree at "+first[:8])
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "r")
	dir := onLoop(a, func() string {
		return a.pathManager(a.projects[0].Instance, "acme/gateway").WorktreeDir("acme/gateway", "at-"+first[:7])
	})
	waitForPath(t, a, sc, dir, true)
	if got := gitIn(t, dir, "rev-parse", "HEAD"); got != first {
		t.Errorf("the worktree is at %s, want %s", got, first)
	}
	if got := gitIn(t, p.clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("the clone moved to %s", got)
	}
}

// TestCopyACommit: y offers the link, a line for a chat - repository,
// branch, commit and subject, then the link - and the ids; a commit no
// remote has offers no link, since there is no page to link to.
func TestCopyACommit(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	gitIn(t, p.clone, "push", "-q", "origin", "main")
	sha := gitIn(t, p.clone, "rev-parse", "HEAD")
	onLoop(a, func() bool { a.projects[0].WebURL = "https://gl.test/acme/gateway"; return true })
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy · "+sha[:8])
	text := a.screenText(sc)
	link := "https://gl.test/acme/gateway/-/commit/" + sha
	for _, want := range []string{link, "acme/gateway · main · " + sha[:8] + " · Count requests per client " + link[:20], "Commit id"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not offered:\n%s", want, text)
		}
	}

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Copy · "+sha[:8])
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log ·")
	commitIn(t, p.clone, "c.txt", "Bill them")
	local := gitIn(t, p.clone, "rev-parse", "HEAD")
	p.rescan()
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy · "+local[:8])
	if text := a.screenText(sc); strings.Contains(text, "https://") || !strings.Contains(text, "Commit id") {
		t.Errorf("a local commit's copy:\n%s", text)
	}
}

// TestDiffInALogBringsTheCommit: D on a merge request's commit while the
// repository is not on disk clones it, fetches the merge request, and shows
// the commit in Hunk; the log comes back after.
func TestDiffInALogBringsTheCommit(t *testing.T) {
	t.Parallel()
	hunk, prepare := fakeHunk(t)
	a, sc, srv := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	head := mrOnOrigin(t, srv, p, "Add a token bucket")
	must(t, os.RemoveAll(p.clone))
	onLoop(a, func() bool {
		for i := range a.projects {
			a.projects[i].HTTPURLToRepo = p.origin
		}
		a.reindexProjects()
		return true
	})
	p.rescan()

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway !7")
	typeRunes(sc, "D")
	waitForLog(t, hunk, p.clone+" show "+head)
	waitFor(t, a, sc, "Commit Log · acme/gateway !7")
	if got := gitIn(t, p.clone, "cat-file", "-t", head); got != "commit" {
		t.Errorf("the clone does not have the commit: %s", got)
	}
}

// TestTheLogSaysWhoWroteEachCommit: the log has an author column, and the
// repository's .mailmap puts right a name a commit recorded badly.
func TestTheLogSaysWhoWroteEachCommit(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(filepath.Join(p.clone, ".mailmap"), []byte("Tomáš Hurýn <test@example.com>\n"), 0o644))
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	line := lineAt(a.screenText(sc), "Count requests per client")
	if !strings.Contains(line, "Tomáš Hurýn") {
		t.Errorf("the row does not name its author as the mailmap does: %q", line)
	}
	assertLegible(t, a, sc, "the log with its authors")
}

// TestADialogsListHasItsItemsActions: Alt-Enter on a list in a dialog lists
// what can be done with the item under the cursor, over the dialog, which
// stays; : there offers only what can be done from anywhere.
func TestADialogsListHasItsItemsActions(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gJ")
	waitFor(t, a, sc, "unit tests")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Run Job")
	for _, want := range []string{"Show Log", "Open Job in Browser", "Open Pipeline in Browser"} {
		waitFor(t, a, sc, want)
	}
	assertLegible(t, a, sc, "a job's actions")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Run Job")
	if !onLoop(a, func() bool { return a.pages.HasPage(pagePicker) }) {
		t.Fatal("closing the actions closed the jobs")
	}

	typeRunes(sc, ":")
	waitFor(t, a, sc, "Switch Theme…")
	if text := a.screenText(sc); strings.Contains(text, "Refresh All") || strings.Contains(text, "Run Job") {
		t.Errorf(": over a dialog offers more than the global actions:\n%s", text)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Switch Theme…")
	waitFor(t, a, sc, "unit tests")

	// Enter in the actions does what Enter in the list does.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Run Job")
	typeRunes(sc, "show log")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "--- FAIL: TestBucket")
}

// TestALogsPipelineIsInItsColours: a commit's pipeline in the log is drawn
// as every list draws one, its mark and its word in the state's colour.
func TestALogsPipelineIsInItsColours(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"success", "failed"} {
		_, colour := ciMark(status)
		sub := logSub(logCommit{CI: status})
		if !strings.Contains(sub, tag(colour)+esc(status)) {
			t.Errorf("%s is not in its colour: %q", status, sub)
		}
	}
}

func TestUndraftedTitles(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Draft: Rate limiting": "Rate limiting", "[Draft] Fix": "Fix", "WIP: x": "x",
		"Drafting rules": "Drafting rules", "Draft:": "Draft:",
	} {
		if got := undrafted(in); got != want {
			t.Errorf("undrafted(%q) = %q, want %q", in, got, want)
		}
	}
}
