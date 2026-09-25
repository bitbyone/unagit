package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// linkedWorktree is what git leaves for a worktree: a .git file pointing at a
// directory that holds its HEAD.
func linkedWorktree(t *testing.T, head string, moved time.Time) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "wt")
	gitDir := filepath.Join(base, "main", ".git", "worktrees", "wt")
	for _, d := range []string{dir, filepath.Join(gitDir, "logs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644))
	must(os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head+"\n"), 0o644))
	log := filepath.Join(gitDir, "logs", "HEAD")
	must(os.WriteFile(log, []byte("x\n"), 0o644))
	must(os.Chtimes(log, moved, moved))
	return dir
}

func TestWorktreeHeadReadsTheBranchWithoutRunningGit(t *testing.T) {
	moved := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	dir := linkedWorktree(t, "ref: refs/heads/feature/login", moved)
	branch, at := WorktreeHead(dir)
	if branch != "feature/login" {
		t.Errorf("branch = %q, want the real name, slashes and all", branch)
	}
	if !at.Equal(moved) {
		t.Errorf("moved = %v, want %v", at, moved)
	}
}

func TestWorktreeHeadOfADetachedOrUnreadableWorktree(t *testing.T) {
	dir := linkedWorktree(t, "9fceb02d0ae598e95dc970b74767f19372d61af8", time.Now())
	if branch, _ := WorktreeHead(dir); branch != "(detached)" {
		t.Errorf("detached HEAD = %q", branch)
	}
	if branch, at := WorktreeHead(filepath.Join(t.TempDir(), "nothing")); branch != "" || !at.IsZero() {
		t.Errorf("unreadable = %q %v", branch, at)
	}
}

func TestWorktreeHeadOfAnOrdinaryCheckout(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if branch, _ := WorktreeHead(dir); branch != "main" {
		t.Errorf("branch = %q", branch)
	}
}
