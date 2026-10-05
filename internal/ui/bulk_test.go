package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestMarkedRepositoriesActTogether: with rows marked, x hides them all at
// once and the marks are done with; the actions of the marks are what
// Alt-Enter lists.
func TestMarkedRepositoriesActTogether(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, " ") // gateway, and on to billing
	typeRunes(sc, " ")
	waitFor(t, a, sc, "SELECT 2")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · 2 marked repositories")
	for _, want := range []string{"Hide or Unhide All", "Hide or List Their MRs", "Refresh All Marked", "Star or Unstar All", "New Grouped Worktree…"} {
		waitFor(t, a, sc, want)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · 2 marked")

	typeRunes(sc, "H")
	waitFor(t, a, sc, "2 repositories' merge requests hidden")
	if !onLoop(a, func() bool {
		return a.cfg.Filters.HidesMRsOf(a.cfg.Instances[0].ID, "acme/gateway") && a.cfg.Filters.HidesMRsOf(a.cfg.Instances[0].ID, "acme/billing")
	}) {
		t.Error("H did not hide the merge requests of both")
	}
	if n := onLoop(a, func() int { return len(a.projectsPane.marks) }); n != 0 {
		t.Errorf("%d marks are left after the action", n)
	}

	typeRunes(sc, "g  ")
	waitFor(t, a, sc, "SELECT 2")
	typeRunes(sc, "x")
	waitFor(t, a, sc, "2 repositories hidden")
	waitGone(t, a, sc, "○ acme/billing")
	waitGone(t, a, sc, "○ acme/gateway")
}

// TestMarkedMergeRequestsActTogether: space marks merge requests too, the
// marked rows drawn as such; H hides the authors of all of them at once.
func TestMarkedMergeRequestsActTogether(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			a.mrs[i].Author.Username = map[int]string{7: "jane", 8: "jane", 9: "bob"}[a.mrs[i].IID]
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "  ") // !7 and !9
	waitFor(t, a, sc, "SELECT 2")
	assertLegible(t, a, sc, "marked merge requests")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · 2 marked merge requests")
	waitFor(t, a, sc, "Hide Their Authors")
	waitFor(t, a, sc, "Mark All as Reviewed")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · 2 marked")
	typeRunes(sc, "H")
	waitFor(t, a, sc, "2 authors hidden")
	waitGone(t, a, sc, "Rate limiting")
	waitGone(t, a, sc, "Invoice rounding")
}

// TestARepositorysSizeCountsItsWorktrees: SIZE is the clone and every
// worktree of it, wherever it was made, and nothing counted twice.
func TestARepositorysSizeCountsItsWorktrees(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	beside := filepath.Join(filepath.Dir(p.clone), ".unagit", "gateway", "7-feat")
	gitIn(t, p.clone, "worktree", "add", "-q", "-b", "feat", beside)
	must(t, os.WriteFile(filepath.Join(beside, "big.bin"), make([]byte, 200_000), 0o644))
	clone, worktree := repoUsage(p.clone), int64(0)
	for _, dir := range repoDirs(p.clone) {
		if dir == beside || strings.HasSuffix(dir, filepath.Join(".unagit", "gateway", "7-feat")) {
			worktree = 1
		}
	}
	if worktree == 0 {
		t.Fatalf("the worktree is not among the repository's folders: %v", repoDirs(p.clone))
	}
	if clone < 200_000 {
		t.Errorf("the repository takes %d bytes; its worktree alone has 200000", clone)
	}
	p.rescan()
	waitFor(t, a, sc, "SIZE")
	want := humanBytes(clone)
	deadline := time.Now().Add(patience)
	for !strings.Contains(lineAt(a.screenText(sc), "acme/gateway"), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the clone's row does not say %s:\n%s", want, a.screenText(sc))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if line := lineAt(a.screenText(sc), "acme/billing"); strings.Contains(line, " KB") {
		t.Errorf("a repository not cloned has a size: %q", line)
	}
}
