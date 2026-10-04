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
		if said = onLoop(a, func() string { return a.mrsPane.statusMessage }); strings.Contains(said, "removed the worktrees") {
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
	if !strings.Contains(line, "●1") || !strings.Contains(line, "✗") {
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
	waitFor(t, a, sc, "NORMAL   j/k")
	typeRunes(sc, "m")
	waitFor(t, a, sc, "!7 is already open from feat/rate")
}

// TestALogFollowsTheTerminal: the subjects are cut where the dialog ends,
// again after the terminal changes size.
func TestALogFollowsTheTerminal(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	long := "Count requests per client and tell them apart by the token they present, a very long subject indeed"
	commitIn(t, p.clone, "b.txt", long)
	p.rescan()
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	waitFor(t, a, sc, long[:90])
	resize(sc, 100, 30)
	waitGone(t, a, sc, long[:60])
	waitFor(t, a, sc, long[:40])
	if line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), long[:40])]; !strings.Contains(line, "just now") {
		t.Errorf("the row lost its age to the subject: %q", line)
	}
}
