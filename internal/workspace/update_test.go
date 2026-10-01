package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
	// A rebase writes commits, and the user running the tests may have no
	// identity configured.
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "test"}, {"GIT_AUTHOR_EMAIL", "test@example.com"},
		{"GIT_COMMITTER_NAME", "test"}, {"GIT_COMMITTER_EMAIL", "test@example.com"}} {
		t.Setenv(kv[0], kv[1])
	}
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
	assertUntouched(t, f, head, status)
	if readFile(t, f.clone, "b.txt") != "an edit elsewhere\n" {
		t.Error("the edit did not come back from the autostash")
	}
}

func TestUpdatePassesOverABranchWithoutUpstream(t *testing.T) {
	f := newUpdateFixture(t)
	git(t, f.clone, "checkout", "-b", "local-only")
	if _, err := f.m.UpdateClone(f.clone); !errors.Is(err, ErrNotTracking) {
		t.Errorf("got %v, want ErrNotTracking", err)
	}
}
