package ui

import (
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/watch"
)

// TestLocalChanges: what moved in a clone, said as news - commits, branches
// and tags made or deleted - and nothing of what unagit moved itself, or of
// a branch moved back onto nothing new.
func TestLocalChanges(t *testing.T) {
	t.Parallel()
	w := localWatch("gl", "acme/api", "/src/api")
	before := map[string]string{"refs/heads/main": "a1", "refs/heads/old": "b1", "refs/heads/back": "c2", "refs/tags/v1": "d1", "refs/heads/mine": "e1"}
	after := map[string]string{"refs/heads/main": "a3", "refs/heads/back": "c1", "refs/heads/feat": "f1", "refs/tags/v2": "g1", "refs/heads/mine": "e2"}
	own := map[string]string{"refs/heads/mine": "e2"}
	commits := func(from, to string) (int, string, string) {
		switch from + ".." + to {
		case "a1..a3":
			return 2, "Claude", "Refill the bucket"
		}
		return 0, "", ""
	}
	var got []string
	for _, e := range localChanges(w, before, after, own, commits) {
		if !e.Quiet || e.Key != w.Key() || e.Level != watch.Info {
			t.Errorf("not a quiet event of the watch: %+v", e)
		}
		got = append(got, e.Heading+": "+e.Line)
	}
	want := []string{
		"Branch created: Branch feat made at f1",
		"New commits: 2 new commits on main by Claude: Refill the bucket",
		"Branch deleted: Branch old deleted",
		"Tag deleted: Tag v1 deleted",
		"Tag created: Tag v2 made at g1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestAWatchedCloneSaysWhatCameFromOutside: a commit and a branch made
// beside unagit come to the Activity log as quiet news - counted, never a
// toast or a notification - and a tag unagit made itself does not.
func TestAWatchedCloneSaysWhatCameFromOutside(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	commitIn(t, dir, "a.txt", "Start")
	w := localWatch(cfg.Instances[0].ID, "acme/api", dir)
	w.Since = time.Now()
	store := watch.Open(cfg.WatchDir())
	if _, err := store.Change(func(ws []watch.Watch) []watch.Watch { return append(ws, w) }); err != nil {
		t.Fatal(err)
	}
	var notified atomic.Int64
	a, _, _ := watchApp(t, cfg, &notified)
	waitTrue(t, "the clone was never read", func() bool {
		_, ok := store.LocalRefs(w.Key())
		return ok && gitx.OwnRefs(dir) != nil
	})

	if _, err := gitx.New("", nil).Run(dir, "tag", "v1"); err != nil {
		t.Fatal(err)
	}
	commitIn(t, dir, "b.txt", "Refill the bucket")
	gitIn(t, dir, "branch", "agent/x")

	lines := func() string {
		return onLoop(a, func() string {
			var out []string
			for _, e := range a.activityLog {
				if e.Key == w.Key() {
					out = append(out, e.Line)
				}
			}
			return strings.Join(out, "\n")
		})
	}
	waitTrue(t, "the commit and the branch never came", func() bool {
		l := lines()
		return strings.Contains(l, "New commit on main by ") && strings.Contains(l, "Refill the bucket") && strings.Contains(l, "Branch agent/x made")
	})
	if l := lines(); strings.Contains(l, "v1") {
		t.Errorf("unagit's own tag is news:\n%s", l)
	}
	if n := onLoop(a, func() int { return len(a.toasts) }); n != 0 || notified.Load() != 0 {
		t.Errorf("a clone's news was loud: %d toasts, %d notified", n, notified.Load())
	}
	if onLoop(a, a.activityNews) == 0 {
		t.Error("the news is not counted on the tab")
	}
}
