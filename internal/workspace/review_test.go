package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

// newDivergedOrigin builds a bare repository where both the target branch and
// the merge request have moved on since they parted, which is the case that
// makes diffing against the tip of the target wrong.
//
// It returns the bare repo, the merge base and the merge request head.
func newDivergedOrigin(t *testing.T) (origin, base, head string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	bare := filepath.Join(dir, "origin.git")

	git(t, dir, "init", "--bare", "--initial-branch=main", bare)
	git(t, dir, "init", "--initial-branch=main", work)
	write(t, work, "keep.txt", "a\n")
	write(t, work, "mod.txt", "old\n")
	write(t, work, "del.txt", "bye\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "initial")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "origin", "main")
	base = git(t, work, "rev-parse", "HEAD")

	// Two commits on the merge request: a change, a deletion and an addition.
	git(t, work, "checkout", "-b", "feature/login")
	write(t, work, "mod.txt", "new\n")
	if err := os.Remove(filepath.Join(work, "del.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, work, "added.go", "package main\n")
	git(t, work, "add", "-A")
	git(t, work, "commit", "-m", "the change")
	write(t, work, "added.go", "package main\n\nfunc main() {}\n")
	git(t, work, "commit", "-am", "more")
	git(t, work, "push", "origin", "feature/login")
	git(t, work, "push", "origin", "HEAD:refs/merge-requests/1/head")
	head = git(t, work, "rev-parse", "HEAD")

	// ...and the target branch moves on afterwards.
	git(t, work, "checkout", "main")
	write(t, work, "keep.txt", "a\nmoved on\n")
	git(t, work, "commit", "-am", "target moved on")
	git(t, work, "push", "origin", "main")

	return bare, base, head
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func reviewMR() forge.MergeRequest {
	return forge.MergeRequest{
		IID: 1, SourceBranch: "feature/login", TargetBranch: "main",
		SourceProjectID: 1, TargetProjectID: 1,
	}
}

func newReviewManager(t *testing.T, origin string) (*Manager, forge.Project) {
	t.Helper()
	p := forge.Project{ID: 1, PathWithNamespace: "group/app", DefaultBranch: "main", HTTPURLToRepo: origin}
	opts := Options{Root: t.TempDir(), GitLabURL: "https://gl.example", Editor: "true"}
	return New(opts, func(string) {}), p
}

// TestReviewWorktreeStagesTheWholeChange is the point of the whole feature:
// what git reports as pending must be exactly what GitLab shows as Changes.
func TestReviewWorktreeStagesTheWholeChange(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)

	dir, err := m.EnsureMRReview(reviewMR(), p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	if want := m.ReviewDir("group/app", 1, "feature/login"); dir != want {
		t.Errorf("worktree at %q, want %q", dir, want)
	}

	// HEAD is the merge base, not the merge request.
	if got := git(t, dir, "rev-parse", "HEAD"); got != base {
		t.Errorf("HEAD = %s, want the merge base %s", got, base)
	}
	// The working tree holds the merge request.
	if got := readFile(t, dir, "added.go"); !strings.Contains(got, "func main()") {
		t.Errorf("added.go = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "del.txt")); !os.IsNotExist(err) {
		t.Error("the file the merge request deletes is still there")
	}

	// And the pending change equals the three dot diff GitLab renders.
	staged := git(t, dir, "diff", "--cached", "--name-status")
	want := git(t, m.ProjectDir("group/app"), "diff", "--name-status", base+".."+head)
	if staged != want {
		t.Errorf("staged change does not match the merge request diff:\ngot:\n%s\nwant:\n%s", staged, want)
	}
	// The target's own commit must not show up reversed, which is what
	// diffing against the tip of the target branch would do.
	if strings.Contains(staged, "keep.txt") {
		t.Errorf("the target branch's own change leaked into the diff:\n%s", staged)
	}
}

// TestReviewWorktreeRecordsItsMetadata: an editor has to be able to find out
// what it is looking at.
func TestReviewWorktreeRecordsItsMetadata(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)

	dir, err := m.EnsureMRReview(reviewMR(), p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	meta := m.ReadMeta(dir)
	if meta.IID != 1 || meta.Base != base || meta.Head != head {
		t.Errorf("meta = %+v, want iid 1, base %s, head %s", meta, base, head)
	}
	if meta.Mode != ModeReview || meta.Target != "main" || meta.Project != "group/app" {
		t.Errorf("meta = %+v", meta)
	}
	if got := git(t, dir, "config", "unagit.mr.base"); got != base {
		t.Errorf("git config unagit.mr.base = %q", got)
	}
}

// TestBranchAndReviewWorktreesAreIndependent: each carries its own metadata,
// which per-worktree config is what makes possible.
func TestBranchAndReviewWorktreesAreIndependent(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	mr := reviewMR()

	branchDir, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	reviewDir, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}

	if got := m.ReadMeta(branchDir).Mode; got != ModeBranch {
		t.Errorf("branch worktree mode = %q", got)
	}
	if got := m.ReadMeta(reviewDir).Mode; got != ModeReview {
		t.Errorf("review worktree mode = %q", got)
	}
	// The branch worktree is a normal checkout of the merge request branch.
	if got := m.Git().CurrentBranch(branchDir); got != "feature/login" {
		t.Errorf("branch worktree is on %q", got)
	}
	if got := git(t, branchDir, "status", "--porcelain"); got != "" {
		t.Errorf("branch worktree should be clean, got:\n%s", got)
	}
	// Its recorded base lets an editor show the same diff.
	if got := m.ReadMeta(branchDir).Base; got != base {
		t.Errorf("branch worktree base = %s, want the merge base %s", got, base)
	}
}

// TestReviewWorktreeKeepsYourEdits: reopening a review must not throw away
// notes typed into the files.
func TestReviewWorktreeKeepsYourEdits(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	mr := reviewMR()

	dir, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "added.go", "package main\n// REVIEW: naming?\n")
	write(t, dir, "notes.txt", "my notes\n")

	if _, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dir, "added.go"); !strings.Contains(got, "REVIEW: naming?") {
		t.Errorf("the edit was lost: %q", got)
	}
	if got := readFile(t, dir, "notes.txt"); got != "my notes\n" {
		t.Errorf("the scratch file was lost: %q", got)
	}
}

// TestReviewWorktreeFollowsAForcePush: a rebased merge request has to end up
// with a new base and a new head, not a merge of the two.
func TestReviewWorktreeFollowsAForcePush(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	mr := reviewMR()

	dir, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}

	// Rebase the merge request onto the new tip of main and force push it.
	work := t.TempDir()
	git(t, filepath.Dir(work), "clone", "-q", origin, work)
	git(t, work, "checkout", "-q", "feature/login")
	git(t, work, "rebase", "origin/main")
	git(t, work, "push", "--force", "origin", "feature/login")
	git(t, work, "push", "--force", "origin", "HEAD:refs/merge-requests/1/head")
	newBase := git(t, work, "rev-parse", "origin/main")
	newHead := git(t, work, "rev-parse", "HEAD")

	if _, err := m.EnsureMRReview(mr, p, Review{BaseSHA: newBase, HeadSHA: newHead}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != newBase {
		t.Errorf("HEAD = %s, want the new base %s", got, newBase)
	}
	// After the rebase the target's change is part of the base, so it must
	// still be absent from the pending diff.
	staged := git(t, dir, "diff", "--cached", "--name-status")
	if strings.Contains(staged, "keep.txt") {
		t.Errorf("target change leaked into the diff after the rebase:\n%s", staged)
	}
	if !strings.Contains(staged, "added.go") {
		t.Errorf("the merge request's own change is missing:\n%s", staged)
	}
}

// TestReviewFallsBackToTheLocalMergeBase: without GitLab's answer the base is
// worked out from the repository.
func TestReviewFallsBackToTheLocalMergeBase(t *testing.T) {
	origin, base, _ := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)

	dir, err := m.EnsureMRReview(reviewMR(), p, Review{})
	if err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != base {
		t.Errorf("HEAD = %s, want the merge base %s", got, base)
	}
}

// TestRemoveMRRemovesBothWorktrees
func TestRemoveMRRemovesBothWorktrees(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	mr := reviewMR()

	branchDir, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	reviewDir, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveMR("group/app", 1, "feature/login"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{branchDir, reviewDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still on disk", dir)
		}
	}
	if !Exists(m.ProjectDir("group/app")) {
		t.Error("the main clone was removed too")
	}
}

// TestInspectProjectSeesReviewEdits: only the reviewer's own edits count as
// work worth warning about, the staged merge request itself does not.
func TestInspectProjectSeesReviewEdits(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)

	dir, err := m.EnsureMRReview(reviewMR(), p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	if r := m.InspectProject("group/app"); len(r.Warnings) != 0 {
		t.Fatalf("a fresh review worktree should not warn, got %v", r.Warnings)
	}
	write(t, dir, "added.go", "package main\n// REVIEW\n")
	r := m.InspectProject("group/app")
	if len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "edited in the review") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestHeadRefFormatFollowsTheForge: GitHub publishes pull request heads under
// refs/pull/<n>/head, GitLab under refs/merge-requests/<n>/head.
func TestHeadRefFormatFollowsTheForge(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	bare := filepath.Join(dir, "origin.git")
	git(t, dir, "init", "--bare", "--initial-branch=main", bare)
	git(t, dir, "init", "--initial-branch=main", work)
	write(t, work, "readme.md", "hi\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "initial")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "origin", "main")
	git(t, work, "checkout", "-b", "feature/login")
	write(t, work, "login.go", "package main\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "add login")
	// Only GitHub's layout is published, so GitLab's format cannot find it.
	git(t, work, "push", "origin", "HEAD:refs/pull/1/head")

	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", TargetBranch: "main",
		SourceProjectID: 1, TargetProjectID: 1}
	opts := Options{Root: t.TempDir(), GitLabURL: "https://github.com", Editor: "true"}

	if _, err := New(opts, func(string) {}).EnsureMR(mr, forge.Project{PathWithNamespace: "acme/app", HTTPURLToRepo: bare}); err == nil {
		t.Fatal("GitLab's ref layout should not find a GitHub pull request")
	}

	opts.Root = t.TempDir()
	opts.HeadRefFormat = "refs/pull/%d/head"
	m := New(opts, func(string) {})
	wt, err := m.EnsureMR(mr, forge.Project{PathWithNamespace: "acme/app", HTTPURLToRepo: bare})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, "login.go")); err != nil {
		t.Errorf("the pull request content is missing: %v", err)
	}
	if got := m.ReadMeta(wt).IID; got != 1 {
		t.Errorf("meta iid = %d", got)
	}
}
