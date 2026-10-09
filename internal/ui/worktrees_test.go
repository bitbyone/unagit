package ui

import (
	"os"
	"path/filepath"
	"slices"
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
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", now.Add(-2*time.Hour))
	makeWorktree(t, a, "acme/billing", "wt-oh-my-god", "ref: refs/heads/oh-my-god", now.Add(-time.Minute))
	// A merge request's own worktree is not one of these.
	makeWorktree(t, a, "acme/gateway", "7-feat-rate", "ref: refs/heads/feat/rate", now)
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })

	typeRunes(sc, "3")
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
	if !strings.Contains(text, "Worktrees [3]") {
		t.Errorf("the tab bar should offer the page:\n%s", text)
	}
}

func TestWorktreesFilterNarrowsTheList(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", now)
	makeWorktree(t, a, "acme/billing", "wt-oh-my-god", "ref: refs/heads/oh-my-god", now)
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })
	typeRunes(sc, "3")
	waitFor(t, a, sc, "2/2 worktrees")

	typeRunes(sc, "/")
	typeRunes(sc, "billing")
	waitFor(t, a, sc, "1/2 worktrees")
	waitGone(t, a, sc, "feat/x")
}

func TestWorktreesTabIsEmptyWithoutWorktrees(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "3")
	waitFor(t, a, sc, "0/0 worktrees")
}

// TestWorktreesSayHowBigAndHowOld: SIZE is measured in the background and
// summed from the files, CREATED is when the worktree was made, and both give
// way when the terminal is narrow.
func TestWorktreesSayHowBigAndHowOld(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feature/audit-log")
	must(t, os.WriteFile(filepath.Join(dir, "big.bin"), make([]byte, 3<<20), 0o644))
	made := time.Now().Add(-50 * time.Hour)
	must(t, os.Chtimes(filepath.Join(dir, ".git"), made, made))
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "3.0 MB")
	row := rowWith(a, sc, "feature/audit-log")
	if !strings.Contains(row, "2d ago") {
		t.Errorf("CREATED does not say two days: %q", row)
	}
	text := a.screenText(sc)
	if !strings.Contains(text, "SIZE") || !strings.Contains(text, "CREATED") {
		t.Errorf("the columns are not headed:\n%s", text)
	}

	for w := 80; w >= 40; w -= 2 {
		h := worktreeHeaderAt(t, a, sc, w)
		if !slices.Contains(h, "RMT") {
			t.Fatalf("RMT gave way before the new columns at %d: %v", w, h)
		}
		if !slices.Contains(h, "CREATED") {
			break
		}
	}
	waitGone(t, a, sc, "CREATED")
}

// TestCIOfBranches: a refresh reads the newest pipeline of each clone's
// branch and each worktree's into a CI column and keeps it for the next
// start. A repository not cloned is not asked about, a worktree shows its
// branch's pipeline and not its merge request's, which m goes to, and a
// grouped worktree leaves the column empty.
func TestCIOfBranches(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	instance := a.cfg.Instances[0].ID
	cloneOnDisk(t, a, instance, "acme/gateway")

	targets := onLoop(a, func() []branchKey { return a.repositoryCITargets() })
	if len(targets) != 1 || targets[0] != (branchKey{instance, "acme/gateway", "main"}) {
		t.Errorf("only the clone's branch is asked about, got %v", targets)
	}
	typeRunes(sc, "R")
	waitFor(t, a, sc, " CI ")
	waitForRow(t, a, sc, "acme/gateway", glyphCIDone)
	if row := rowWith(a, sc, "acme/billing"); containsField(row, glyphCIDone) {
		t.Errorf("acme/billing is not cloned, yet its row has a pipeline: %q", row)
	}
	saved := onLoop(a, func() string {
		b, _ := os.ReadFile(a.cfg.IndexPath("pipelines"))
		return string(b)
	})
	if !strings.Contains(saved, `"project": "acme/gateway"`) {
		t.Errorf("the pipelines are not kept for the next start:\n%s", saved)
	}

	// feat/rate has !7 open, whose pipeline failed; the branch's passed.
	makeWorktree(t, a, "acme/gateway", "wt-rate", "ref: refs/heads/feat/rate", time.Now())
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.askBranchCI(a.worktreeCITargets(), "")
	})
	typeRunes(sc, "3")
	waitFor(t, a, sc, "feat/rate")
	waitForRow(t, a, sc, "feat/rate", glyphCIDone)
	status := onLoop(a, func() string {
		return a.worktreeCI(worktreeRow{Instance: instance, Path: "acme/gateway", Branch: "feat/rate"})
	})
	if status != "success" {
		t.Errorf("the worktree shows %q, not its branch's pipeline", status)
	}

	typeRunes(sc, "m")
	waitFor(t, a, sc, "Rate limiting")
	deadline := time.Now().Add(patience)
	for {
		iid := onLoop(a, func() int {
			if a.currentTab() != pageMRs {
				return 0
			}
			if i := a.mrsPane.selectedIndex(); i >= 0 && i < len(a.mrs) {
				return a.mrs[i].IID
			}
			return 0
		})
		if iid == 7 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("m did not bring the cursor to !7 (on !%d):\n%s", iid, a.screenText(sc))
		}
		time.Sleep(30 * time.Millisecond)
	}

	// A group has a branch in each repository and no pipeline of its own.
	group := onLoop(a, func() string {
		member := worktreeRow{Instance: instance, Path: "acme/gateway", Branch: "feat/rate", Dir: "/x"}
		return a.worktreeCI(worktreeRow{Path: "g", Branch: "feat/rate", Dir: "/g", Members: []worktreeRow{member}})
	})
	if group != "" {
		t.Errorf("a grouped worktree shows a pipeline: %q", group)
	}
}
