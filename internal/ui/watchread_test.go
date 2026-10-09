package ui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/watch"
)

// TestAWatchIsReadInFullOnlyWhenItsFingerprintMoves: while GraphQL gives
// the same fingerprint, the merge request is not read again; a new one has
// it read at once.
func TestAWatchIsReadInFullOnlyWhenItsFingerprintMoves(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	srv.mu.Lock()
	srv.print = "p1"
	srv.mu.Unlock()
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	a, _, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	read := func() time.Time {
		snap, _ := watch.Open(cfg.WatchDir()).State()
		return snap.States[w.Key()].Read
	}
	// Several turns, the fingerprint the same.
	details := srv.details.Load()
	since := read()
	for range 4 {
		last := read()
		waitTrue(t, "the watch is not read any more", func() bool { return read().After(last) })
	}
	if read().Equal(since) || srv.details.Load() != details {
		t.Fatalf("read in full %d more times with the fingerprint unchanged", srv.details.Load()-details)
	}
	srv.mu.Lock()
	srv.print = "p2"
	srv.status = "failed"
	srv.mu.Unlock()
	waitTrue(t, "a new fingerprint was not read in full", func() bool { return srv.details.Load() > details })
	waitState(t, cfg, w.Key(), "failed")
	_ = a
}

// TestAMergeRequestsActivityIsNews: new comments, a new approval and new
// commits are each news, as the pipeline's changes are.
func TestAMergeRequestsActivityIsNews(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	srv.mu.Lock()
	srv.commits = 3
	srv.mu.Unlock()
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	waitTrue(t, "the activity was not read", func() bool {
		snap, _ := watch.Open(cfg.WatchDir()).State()
		return snap.States[w.Key()].Known
	})
	srv.mu.Lock()
	srv.comments, srv.approvers = 2, []string{"john"}
	srv.mu.Unlock()
	waitFor(t, a, sc, "2 new comments")
	waitFor(t, a, sc, "approved by john")
	srv.mu.Lock()
	srv.commits, srv.sha = 5, "cccc3333"
	srv.mu.Unlock()
	waitFor(t, a, sc, "2 new commits · head cccc333")
	waitTrue(t, "the activity was not notified", func() bool { return notified.Load() >= 3 })
}

// TestAServerShortOfItsLimitIsLeftAlone: once the rate limit's headers say
// little is left, the watches stop asking until it resets, and the row
// says why.
func TestAServerShortOfItsLimitIsLeftAlone(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	srv.mu.Lock()
	srv.remaining = 5
	srv.mu.Unlock()
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	_, _, _ = watchApp(t, cfg, &notified)
	waitTrue(t, "the watch does not say the server is short", func() bool {
		snap, _ := watch.Open(cfg.WatchDir()).State()
		return strings.Contains(snap.States[w.Key()].Error, "rate limit")
	})
	asked := srv.details.Load()
	snap, _ := watch.Open(cfg.WatchDir()).State()
	last := snap.Seq
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if srv.details.Load() != asked {
			t.Fatalf("asked %d more times while short of the limit", srv.details.Load()-asked)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if snap, _ := watch.Open(cfg.WatchDir()).State(); snap.Seq != last {
		t.Fatalf("news while nothing was read: %+v", snap.Events)
	}
}

// TestABranchDeletedOnOriginEndsItsWatch: as a merged merge request does.
func TestABranchDeletedOnOriginEndsItsWatch(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	srv.mu.Lock()
	srv.branches = map[string]bool{"feat-x": true}
	srv.mu.Unlock()
	cfg := writeTestConfig(t, srv.URL)
	w := watch.Watch{Kind: watch.KindPipeline, Instance: cfg.Instances[0].ID, Project: "acme/gateway", ProjectID: 1, Branch: "feat-x", Since: time.Now()}
	if _, err := watch.Open(cfg.WatchDir()).Change(func(ws []watch.Watch) []watch.Watch { return append(ws, w) }); err != nil {
		t.Fatal(err)
	}
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	srv.mu.Lock()
	srv.branches = map[string]bool{}
	srv.mu.Unlock()
	waitFor(t, a, sc, "branch deleted on origin · no longer watched")
	waitTrue(t, "the watch of a deleted branch stayed", func() bool {
		ws, _ := watch.Open(cfg.WatchDir()).Watches()
		return len(ws) == 0
	})
}

// TestAWatchedBranchSaysHowFarBehindItsBaseItIs: read from the clone on
// disk, by the refs it has - no request.
func TestAWatchedBranchSaysHowFarBehindItsBaseItIs(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	commitIn(t, dir, "a", "first")
	gitIn(t, dir, "checkout", "-q", "-b", "feat")
	gitIn(t, dir, "checkout", "-q", "main")
	commitIn(t, dir, "b", "second")
	commitIn(t, dir, "c", "third")
	gitIn(t, dir, "checkout", "-q", "feat")
	id := a.cfg.Instances[0].ID
	changeOnLoop(a, func() {
		a.worktrees = append(a.worktrees, worktreeRow{Instance: id, Path: "acme/gateway", Branch: "feat", Dir: dir, Base: "main"})
	})
	w := watch.Watch{Kind: watch.KindPipeline, Instance: id, Project: "acme/gateway", Branch: "feat"}
	f := &watchFollower{ctx: context.Background(), app: a}
	got := f.readBehind([]watch.Watch{w})[w.Key()]
	if got.Base != "main" || got.Behind != 2 {
		t.Fatalf("read %+v, want 2 behind main", got)
	}
	// The default branch itself has nothing to be behind.
	main := watch.Watch{Kind: watch.KindPipeline, Instance: id, Project: "acme/gateway", Branch: "main"}
	if _, ok := f.readBehind([]watch.Watch{main})[main.Key()]; ok {
		t.Fatal("the default branch was measured against itself")
	}
}
