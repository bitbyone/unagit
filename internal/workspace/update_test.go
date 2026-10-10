package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

// updateFixture is a clone tracking origin's main, and a second clone that
// moves origin on, the way a colleague would.
type updateFixture struct {
	m      *Manager
	clone  string
	pusher string
}

func newUpdateFixture(t *testing.T) updateFixture {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	git(t, dir, "init", "--bare", "--initial-branch=main", bare)
	clone := filepath.Join(dir, "clone")
	git(t, dir, "clone", bare, clone)
	git(t, clone, "checkout", "-b", "main")
	write(t, clone, "a.txt", "one\n")
	write(t, clone, "b.txt", "one\n")
	git(t, clone, "add", ".")
	git(t, clone, "commit", "-m", "initial")
	git(t, clone, "push", "-u", "origin", "main")
	pusher := filepath.Join(dir, "pusher")
	git(t, dir, "clone", bare, pusher)
	return updateFixture{m: New(Options{Root: dir}, func(string) {}), clone: clone, pusher: pusher}
}

// moveOrigin commits a change to file on origin's main.
func (f updateFixture) moveOrigin(t *testing.T, file, content string) {
	t.Helper()
	write(t, f.pusher, file, content)
	git(t, f.pusher, "commit", "-am", "theirs: "+file)
	git(t, f.pusher, "push", "origin", "main")
}

func TestUpdateFastForwardsACleanClone(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	f.moveOrigin(t, "a.txt", "two\n")
	got, err := f.m.UpdateClone(f.clone)
	if err != nil || got != UpdateFastForward {
		t.Fatalf("got %q, %v", got, err)
	}
	if readFile(t, f.clone, "a.txt") != "two\n" {
		t.Error("the clone did not move")
	}
	if got, err := f.m.UpdateClone(f.clone); err != nil || got != UpdateCurrent {
		t.Errorf("a second update: %q, %v", got, err)
	}
}

func TestUpdateRebasesLocalCommitsAndEdits(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	f.moveOrigin(t, "a.txt", "two\n")
	write(t, f.clone, "b.txt", "mine\n")
	git(t, f.clone, "commit", "-am", "mine")
	write(t, f.clone, "c.txt", "not committed\n")
	write(t, f.clone, "b.txt", "mine, and more\n")

	got, err := f.m.UpdateClone(f.clone)
	if err != nil || got != UpdateRebased {
		t.Fatalf("got %q, %v", got, err)
	}
	if readFile(t, f.clone, "a.txt") != "two\n" {
		t.Error("origin's change did not arrive")
	}
	if git(t, f.clone, "log", "-1", "--format=%s") != "mine" {
		t.Error("the local commit is not on top")
	}
	if readFile(t, f.clone, "b.txt") != "mine, and more\n" || readFile(t, f.clone, "c.txt") != "not committed\n" {
		t.Error("the uncommitted edits did not survive")
	}
	if git(t, f.clone, "stash", "list") != "" {
		t.Error("the autostash was left behind")
	}
}

// assertUntouched fails unless HEAD and the working tree are what they were.
func assertUntouched(t *testing.T, f updateFixture, head, status string) {
	t.Helper()
	if got := git(t, f.clone, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved from %s to %s", head, got)
	}
	if got := git(t, f.clone, "status", "--porcelain"); got != status {
		t.Errorf("the working tree changed:\n%s\nwas:\n%s", got, status)
	}
	if _, err := os.Stat(filepath.Join(f.clone, ".git", "rebase-merge")); err == nil {
		t.Error("a rebase was left in progress")
	}
	if git(t, f.clone, "stash", "list") != "" {
		t.Error("a stash was left behind")
	}
}

func TestUpdateRefusesEditsThatCollide(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	f.moveOrigin(t, "a.txt", "two\n")
	write(t, f.clone, "a.txt", "my edit\n")
	head, status := git(t, f.clone, "rev-parse", "HEAD"), git(t, f.clone, "status", "--porcelain")

	_, err := f.m.UpdateClone(f.clone)
	if !errors.Is(err, ErrNothingDone) {
		t.Fatalf("got %v, want a refusal", err)
	}
	assertUntouched(t, f, head, status)
	if readFile(t, f.clone, "a.txt") != "my edit\n" {
		t.Error("the edit was lost")
	}
}

func TestUpdateRollsBackAConflictingRebase(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	f.moveOrigin(t, "a.txt", "two\n")
	write(t, f.clone, "a.txt", "mine\n")
	git(t, f.clone, "commit", "-am", "mine")
	write(t, f.clone, "b.txt", "an edit elsewhere\n")
	head, status := git(t, f.clone, "rev-parse", "HEAD"), git(t, f.clone, "status", "--porcelain")

	_, err := f.m.UpdateClone(f.clone)
	if !errors.Is(err, ErrNothingDone) {
		t.Fatalf("got %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "in a.txt") {
		t.Errorf("the refusal does not say where: %v", err)
	}
	assertUntouched(t, f, head, status)
	if readFile(t, f.clone, "b.txt") != "an edit elsewhere\n" {
		t.Error("the edit did not come back from the autostash")
	}
}

func TestUpdatePassesOverABranchWithoutUpstream(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	git(t, f.clone, "checkout", "-b", "local-only")
	if _, err := f.m.UpdateClone(f.clone); !errors.Is(err, ErrNotTracking) {
		t.Errorf("got %v, want ErrNotTracking", err)
	}
}

// TestUpdateRebasesANewBranchOntoItsBase: a branch not pushed yet follows the
// branch it was made from; once pushed, it follows its own upstream.
func TestUpdateRebasesANewBranchOntoItsBase(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	git(t, f.clone, "branch", "--no-track", "feat/x", "origin/main")
	if err := f.m.git.SetBranchBase(f.clone, "feat/x", "main"); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, f.clone, "worktree", "add", wt, "feat/x")
	write(t, wt, "b.txt", "mine\n")
	git(t, wt, "commit", "-am", "mine")
	f.moveOrigin(t, "a.txt", "two\n")

	if bases := f.m.git.BranchBases(f.clone); bases["feat/x"] != "main" {
		t.Fatalf("the base was not recorded: %v", bases)
	}
	got, err := f.m.UpdateBranch(wt, "main")
	if err != nil || got != UpdateRebased {
		t.Fatalf("got %q, %v", got, err)
	}
	if readFile(t, wt, "a.txt") != "two\n" || git(t, wt, "log", "-1", "--format=%s") != "mine" {
		t.Error("the branch was not rebased onto origin's main")
	}

	// Pushed, it has an upstream of its own, and the base no longer counts.
	git(t, wt, "push", "-u", "origin", "feat/x")
	f.moveOrigin(t, "a.txt", "three\n")
	if got, err := f.m.UpdateBranch(wt, "main"); err != nil || got != UpdateCurrent {
		t.Errorf("a pushed branch went after its base: %q, %v", got, err)
	}
}

// TestRebaseOntoBaseOfAPushedBranch: Ctrl-R moves a pushed branch onto its
// base and notes where origin's copy stood; the force push replaces exactly
// that, and refuses once someone has pushed over it.
func TestRebaseOntoBaseOfAPushedBranch(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	git(t, f.clone, "checkout", "-q", "-b", "feat/x")
	if err := f.m.git.SetBranchBase(f.clone, "feat/x", "main"); err != nil {
		t.Fatal(err)
	}
	write(t, f.clone, "b.txt", "mine\n")
	git(t, f.clone, "commit", "-am", "mine")
	git(t, f.clone, "push", "-u", "origin", "feat/x")
	pushed := git(t, f.clone, "rev-parse", "HEAD")
	f.moveOrigin(t, "a.txt", "two\n")

	got, err := f.m.RebaseOntoBase(f.clone, "main")
	if err != nil || got != UpdateRebased {
		t.Fatalf("got %q, %v", got, err)
	}
	if readFile(t, f.clone, "a.txt") != "two\n" {
		t.Error("the branch is not on top of main")
	}
	if mark := f.m.git.RebasedFrom(f.clone)["feat/x"]; mark != pushed {
		t.Fatalf("noted %q, origin had %s", mark, pushed)
	}

	// Someone pushes over origin's copy: the lease no longer holds.
	git(t, f.pusher, "fetch", "-q")
	git(t, f.pusher, "checkout", "-q", "feat/x")
	write(t, f.pusher, "c.txt", "theirs\n")
	git(t, f.pusher, "add", ".")
	git(t, f.pusher, "commit", "-m", "theirs")
	git(t, f.pusher, "push", "origin", "feat/x")
	if err := f.m.git.ForcePush(f.clone, "feat/x", pushed); err == nil {
		t.Fatal("the force push replaced a commit pushed after the rebase")
	}

	// With the lease on what origin has now, it goes through.
	theirs := git(t, f.pusher, "rev-parse", "HEAD")
	if err := f.m.git.ForcePush(f.clone, "feat/x", theirs); err != nil {
		t.Fatal(err)
	}
	if git(t, f.pusher, "ls-remote", "origin", "feat/x")[:40] != git(t, f.clone, "rev-parse", "HEAD") {
		t.Error("origin does not have the rebased branch")
	}
}

// TestRebaseOntoAnotherBranch: Rebase onto… puts the branch on top of a
// branch other than its base, and the base recorded stays.
func TestRebaseOntoAnotherBranch(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	git(t, f.pusher, "checkout", "-q", "-b", "release")
	write(t, f.pusher, "r.txt", "release\n")
	git(t, f.pusher, "add", ".")
	git(t, f.pusher, "commit", "-m", "release")
	git(t, f.pusher, "push", "-q", "origin", "release")
	git(t, f.clone, "checkout", "-q", "-b", "feat/x")
	if err := f.m.git.SetBranchBase(f.clone, "feat/x", "main"); err != nil {
		t.Fatal(err)
	}
	write(t, f.clone, "b.txt", "mine\n")
	git(t, f.clone, "commit", "-am", "mine")

	got, err := f.m.RebaseOnto(f.clone, "release")
	if err != nil || got != UpdateRebased {
		t.Fatalf("got %q, %v", got, err)
	}
	if readFile(t, f.clone, "r.txt") != "release\n" || git(t, f.clone, "log", "-1", "--format=%s") != "mine" {
		t.Error("the branch is not on top of release")
	}
	if base := f.m.git.BranchBases(f.clone)["feat/x"]; base != "main" {
		t.Errorf("the base became %q", base)
	}
	if _, err := f.m.RebaseOnto(f.clone, "nowhere"); !errors.Is(err, ErrNothingDone) {
		t.Errorf("a branch that does not exist: %v", err)
	}
}

// TestRebaseOntoAConflictLeavesTheBranch: a rebase that would conflict is
// refused, the branch exactly as it was.
func TestRebaseOntoAConflictLeavesTheBranch(t *testing.T) {
	t.Parallel()
	f := newUpdateFixture(t)
	git(t, f.pusher, "checkout", "-q", "-b", "release")
	write(t, f.pusher, "a.txt", "theirs\n")
	git(t, f.pusher, "commit", "-am", "theirs")
	git(t, f.pusher, "push", "-q", "origin", "release")
	git(t, f.clone, "checkout", "-q", "-b", "feat/x")
	write(t, f.clone, "a.txt", "mine\n")
	git(t, f.clone, "commit", "-am", "mine")
	write(t, f.clone, "b.txt", "an edit\n")
	head, status := git(t, f.clone, "rev-parse", "HEAD"), git(t, f.clone, "status", "--porcelain")

	if _, err := f.m.RebaseOnto(f.clone, "release"); !errors.Is(err, ErrNothingDone) {
		t.Fatalf("got %v, want a refusal", err)
	}
	assertUntouched(t, f, head, status)
}

// TestOpeningAMergeRequestLeavesItAndPUpdatesIt: opening an existing branch
// worktree does not fetch; UpdateMR brings it to the published head.
func TestOpeningAMergeRequestLeavesItAndPUpdatesIt(t *testing.T) {
	t.Parallel()
	origin := newOrigin(t)
	m, _, p := newManager(t, origin)
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", TargetBranch: "main", SourceProjectID: 1, TargetProjectID: 1}
	wt, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	before := git(t, wt, "rev-parse", "HEAD")

	// The author pushes another commit.
	work := filepath.Join(t.TempDir(), "author")
	git(t, filepath.Dir(work), "clone", "-q", "-b", "feature/login", origin, work)
	write(t, work, "more.go", "package main\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-qm", "more")
	git(t, work, "push", "-q", "origin", "feature/login", "HEAD:refs/merge-requests/1/head")

	if again, err := m.EnsureMR(mr, p); err != nil || again != wt {
		t.Fatalf("reopen: %q, %v", again, err)
	}
	if git(t, wt, "rev-parse", "HEAD") != before {
		t.Fatal("opening moved the worktree")
	}
	got, err := m.UpdateMR(mr, p)
	if err != nil || got != UpdateFastForward {
		t.Fatalf("got %q, %v", got, err)
	}
	if git(t, wt, "rev-parse", "HEAD") != git(t, work, "rev-parse", "HEAD") {
		t.Error("the worktree is not at the merge request's head")
	}
}
