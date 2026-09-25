package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/workspace"
)

// makeWorktree lays down what git leaves for a branch worktree of a project,
// under the hidden worktree root next to the clone.
func makeWorktree(t *testing.T, a *App, project, name, head string, moved time.Time) {
	t.Helper()
	instance := a.cfg.Instances[0].ID
	clone := a.projectDir(instance, project)
	dir := filepath.Join(workspace.WorktreeRoot(clone), name)
	gitDir := filepath.Join(clone, ".git", "worktrees", name)
	for _, d := range []string{dir, filepath.Join(gitDir, "logs")} {
		must(t, os.MkdirAll(d, 0o755))
	}
	must(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head+"\n"), 0o644))
	log := filepath.Join(gitDir, "logs", "HEAD")
	must(t, os.WriteFile(log, []byte("x\n"), 0o644))
	must(t, os.Chtimes(log, moved, moved))
}

func TestWorktreesTabListsBranchWorktreesAcrossRepositories(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", now.Add(-2*time.Hour))
	makeWorktree(t, a, "acme/billing", "wt-oh-my-god", "ref: refs/heads/oh-my-god", now.Add(-time.Minute))
	// A merge request's own worktree is not one of these.
	makeWorktree(t, a, "acme/gateway", "7-feat-rate", "ref: refs/heads/feat/rate", now)
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })

	typeRunes(sc, "W")
	waitFor(t, a, sc, "2/2 worktrees")
	waitFor(t, a, sc, "feat/x")
	waitFor(t, a, sc, "oh-my-god")
	text := a.screenText(sc)
	if strings.Contains(text, "feat/rate") {
		t.Errorf("a merge request worktree does not belong on this list:\n%s", text)
	}
	// The most recently moved first, and every row says which repository it is.
	if strings.Index(text, "oh-my-god") > strings.Index(text, "feat/x") {
		t.Errorf("the newest activity should come first:\n%s", text)
	}
	if !strings.Contains(text, "acme/billing") || !strings.Contains(text, "acme/gateway") {
		t.Errorf("rows should name their repository:\n%s", text)
	}
	if !strings.Contains(text, "Worktrees [W]") {
		t.Errorf("the tab bar should offer the page:\n%s", text)
	}
}

func TestWorktreesFilterNarrowsTheList(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", now)
	makeWorktree(t, a, "acme/billing", "wt-oh-my-god", "ref: refs/heads/oh-my-god", now)
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })
	typeRunes(sc, "W")
	waitFor(t, a, sc, "2/2 worktrees")

	typeRunes(sc, "/")
	typeRunes(sc, "billing")
	waitFor(t, a, sc, "1/2 worktrees")
	waitGone(t, a, sc, "feat/x")
}

func TestWorktreesTabIsEmptyWithoutWorktrees(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "W")
	waitFor(t, a, sc, "0/0 worktrees")
}
