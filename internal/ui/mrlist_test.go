package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

// TestARefreshTidiesClosedMergeRequests: after r, the worktrees of a merge
// request no longer open are gone without a question; one with work of the
// user's in it is kept and named.
func TestARefreshTidiesClosedMergeRequests(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	root := workspace.WorktreeRoot(p.clone)
	must(t, os.MkdirAll(root, 0o755))
	done := filepath.Join(root, "5-old")
	busy := filepath.Join(root, "6-busy")
	gitIn(t, p.clone, "worktree", "add", "-q", "-b", "old", done)
	gitIn(t, p.clone, "worktree", "add", "-q", "-b", "busy", busy)
	must(t, os.WriteFile(filepath.Join(busy, "a.txt"), []byte("mine\n"), 0o644))
	open := filepath.Join(root, "7-feat-rate")
	gitIn(t, p.clone, "worktree", "add", "-q", "-b", "feat/rate", open)
	p.rescan()

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "R")
	// The summary runs past the status bar; it is read whole.
	said := ""
	for deadline := time.Now().Add(patience); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		// Kept worktrees make it a warning, in a box of its own.
		if said = messageText(a); strings.Contains(said, "removed the worktrees") {
			break
		}
	}
	for _, want := range []string{"removed the worktrees of closed acme/gateway !5", "kept the worktrees of closed acme/gateway !6 (1 uncommitted change(s))"} {
		if !strings.Contains(said, want) {
			t.Errorf("the summary does not say %q: %q", want, said)
		}
	}
	if _, err := os.Stat(done); !os.IsNotExist(err) {
		t.Errorf("the closed merge request's worktree is still there: %v", err)
	}
	for _, dir := range []string{busy, open} {
		if !workspace.Exists(dir) {
			t.Errorf("%s was removed", dir)
		}
	}
}

// TestAKeptReviewIsListedInWorktrees: the review of a merge request no
// longer open, kept for an edit of the reviewer's, is a row of Worktrees with
// its merge request's number - the files the merge request adds not taken
// for edits - and d deletes it, naming the edit first.
func TestAKeptReviewIsListedInWorktrees(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	head := mrOnOrigin(t, srv, p, "Add a token bucket")
	mr := onLoop(a, func() forge.MergeRequest {
		for _, m := range a.mrs {
			if m.IID == 7 {
				return m
			}
		}
		return forge.MergeRequest{}
	})
	project := onLoop(a, func() forge.Project { return a.mrProject(mr) })
	dir, err := a.newManager(mr.Instance, project.PathWithNamespace, nil).
		EnsureMRReview(mr, project, workspace.Review{HeadSHA: head})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("mine\n"), 0o644))
	gitIn(t, dir, "add", "-N", "mine.txt")
	// Something reset the index: the file the merge request adds is
	// untracked now, and still the merge request's.
	gitIn(t, dir, "rm", "-q", "--cached", "rate0.txt")

	typeRunes(sc, "3")
	changeOnLoop(a, func() {
		var open []forge.MergeRequest
		for _, m := range a.mrs {
			if m.IID != 7 {
				open = append(open, m)
			}
		}
		a.mrs = open
		a.refreshDisk()
	})
	// The branch is read from the worktree in the background, with the edits.
	waitFor(t, a, sc, "feat/rate")
	screen := a.screenText(sc)
	line := strings.Split(screen, "\n")[lineOf(screen, "MR closed")]
	for _, want := range []string{glyphReviewWorktree, "acme/gateway", "feat/rate", "!7"} {
		if !strings.Contains(line, want) {
			t.Errorf("the kept review's row lacks %q: %q", want, line)
		}
	}
	edits := onLoop(a, func() int {
		for _, r := range a.worktrees {
			if r.Review == 7 {
				return a.worktreeEditCount(r)
			}
		}
		return -1
	})
	if edits != 1 {
		t.Errorf("the kept review counts %d edits, want the reviewer's one", edits)
	}
	assertLegible(t, a, sc, "a kept review in Worktrees")

	at := onLoop(a, func() int {
		for i, r := range a.worktrees {
			if r.Review == 7 {
				a.worktreesPane.selectWhere(func(j int) bool { return j == i })
				return i
			}
		}
		return -1
	})
	waitSelected(t, a, a.worktreesPane, at)
	typeRunes(sc, "d")
	waitFor(t, a, sc, "1 file(s) edited in the review")
	typeRunes(sc, "d")
	waitGone(t, a, sc, "MR closed")
	if workspace.Exists(dir) {
		t.Errorf("the kept review %s is still on disk", dir)
	}
}

// TestTheMergeRequestRowSaysWhatIsNewAndHowCIWent: NEW counts what was pushed
// since the review last checked out the head; CI is the pipeline's mark.
func TestTheMergeRequestRowSaysWhatIsNewAndHowCIWent(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	head := mrOnOrigin(t, srv, p, "Add a token bucket")
	mr := onLoop(a, func() forge.MergeRequest {
		for _, m := range a.mrs {
			if m.IID == 7 {
				return m
			}
		}
		return forge.MergeRequest{}
	})
	project := onLoop(a, func() forge.Project { return a.mrProject(mr) })
	dir, err := a.newManager(mr.Instance, project.PathWithNamespace, nil).
		EnsureMRReview(mr, project, workspace.Review{HeadSHA: head})
	must(t, err)
	newer := mrOnOrigin(t, srv, p, "Answer the review")
	gitIn(t, dir, "fetch", "-q", "origin", "refs/merge-requests/7/head")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].SHA, a.mrs[i].Pipeline = newer, "failed"
			}
		}
		a.refreshDisk()
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "●1")
	line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), "Rate limiting")]
	if !strings.Contains(line, "●1") || !strings.Contains(line, " "+glyphCIDone+" ") {
		t.Errorf("the row does not say what is new and how CI went: %q", line)
	}
	assertLegible(t, a, sc, "the merge request list")
}

// TestMineAndToReview: the view options narrow the merge requests to those of
// whom the token belongs to, written or to review, and leave the drafts out.
func TestMineAndToReview(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.me = map[string]string{a.cfg.Instances[0].ID: "me"}
		for i := range a.mrs {
			switch a.mrs[i].IID {
			case 7:
				a.mrs[i].Author.Username = "me"
			case 8:
				a.mrs[i].Author.Username = "jane"
				a.mrs[i].Reviewers = []forge.User{{Username: "me"}}
			case 9:
				a.mrs[i].Author.Username = "bob"
				a.mrs[i].Draft = true
			}
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Invoice rounding")
	typeRunes(sc, "v")
	waitFor(t, a, sc, "View · Merge requests")
	typeRunes(sc, "jjj ") // only mine
	waitFor(t, a, sc, "· mine")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Drop the old client")
	waitFor(t, a, sc, "Rate limiting")

	typeRunes(sc, "v")
	waitFor(t, a, sc, "View · Merge requests")
	typeRunes(sc, "jjj j ") // mine off, to review on
	waitFor(t, a, sc, "to review")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Drop the old client")
	waitGone(t, a, sc, "Rate limiting")

	typeRunes(sc, "v")
	waitFor(t, a, sc, "View · Merge requests")
	typeRunes(sc, "jjjj ") // to review off
	typeRunes(sc, "j ")    // hide drafts
	waitFor(t, a, sc, "no drafts")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Invoice rounding")
	waitFor(t, a, sc, "Rate limiting")
}

// TestBranchesKnowTheirMergeRequests: a branch with a merge request open says
// so, and m on it does not open a second one.
func TestBranchesKnowTheirMergeRequests(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	srv.liveBranches.Store(func(int) []string {
		return strings.Fields(gitIn(t, p.origin, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
	})
	mrOnOrigin(t, srv, p, "Add a token bucket")
	p.rescan()

	typeRunes(sc, "gb")
	waitFor(t, a, sc, "Branches - acme/gateway")
	waitFor(t, a, sc, "!7 open")
	typeRunes(sc, "/feat/rate")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL   ")
	typeRunes(sc, "m")
	waitFor(t, a, sc, "!7 is already open from feat/rate")
}

// TestALogFollowsTheTerminal: the subjects are cut where the dialog ends,
// again after the terminal changes size.
func TestALogFollowsTheTerminal(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	long := "Count requests per client and tell them apart by the token they present, a very long subject indeed"
	commitIn(t, p.clone, "b.txt", long)
	p.rescan()
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	waitFor(t, a, sc, long[:90])
	resizeApp(a, sc, 100, 30)
	waitGone(t, a, sc, long[:60])
	waitFor(t, a, sc, long[:36])
	if line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), long[:36])]; !strings.Contains(line, "just now") {
		t.Errorf("the row lost its age to the subject: %q", line)
	}
}

// TestCommentsAndApprovalsRead: COM is open/resolved/all where the forge can
// tell, else the comments alone; APPR is approvals of those asked for, with
// a mark when one is yours; each in exactly the cells it says.
func TestCommentsAndApprovalsRead(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		mr       forge.MergeRequest
		com, apr string
	}{
		{forge.MergeRequest{UnresolvedKnown: true, Unresolved: 2, Resolved: 4, Comments: 9}, "2/4/9", ""},
		{forge.MergeRequest{UnresolvedKnown: true, Comments: 3}, "0/0/3", ""},
		{forge.MergeRequest{Comments: 6}, "6", ""},
		{forge.MergeRequest{ApprovedBy: []string{"jane"}, ApprovalsRequired: 2}, "", "1/2"},
		{forge.MergeRequest{ApprovedBy: []string{"me", "jane"}, ApprovalsRequired: 2}, "", glyphApproved + "2/2"},
		{forge.MergeRequest{ApprovedBy: []string{"jane"}}, "", "1"},
		{forge.MergeRequest{ApprovedBy: []string{"me"}}, "", glyphApproved + "1"},
	} {
		com, comW := commentWords(c.mr)
		if got := plainText(com); got != c.com || comW != len([]rune(c.com)) {
			t.Errorf("COM of %+v = %q (%d cells), want %q", c.mr, got, comW, c.com)
		}
		apr, aprW := approvalWords(c.mr, "me")
		if got := plainText(apr); got != c.apr || aprW != len([]rune(c.apr)) {
			t.Errorf("APPR of %+v = %q (%d cells), want %q", c.mr, got, aprW, c.apr)
		}
	}
}

// TestTheDetailSpellsTheCommentsOut: the detail says what COM says, in
// words: the comments in all, the threads resolved and those open.
func TestTheDetailSpellsTheCommentsOut(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].UnresolvedKnown, a.mrs[i].Resolved, a.mrs[i].Unresolved = true, 4, 5
			}
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "in total · 4 resolved threads · 5 unresolved")
}

// TestTheTotalCountsOnlyShownRepositories: the merge requests of a
// repository whose merge requests are hidden are carried in the index as
// last read, and are not in the total of the header.
func TestTheTotalCountsOnlyShownRepositories(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, "3/3 merge requests")
	a.tv.QueueUpdateDraw(func() {
		a.cfg.Filters.ToggleMRsOf(a.cfg.Instances[0].ID, "acme/billing")
		a.mrsPane.reload()
	})
	waitFor(t, a, sc, "2/2 merge requests")
}
