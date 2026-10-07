package ui

import (
	"testing"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// order is the hits in the order they were sorted into, by what names them.
func order(hits []scored, name func(idx int) string) []string {
	var out []string
	for _, h := range hits {
		out = append(out, name(h.idx))
	}
	return out
}

func allHits(n int) []scored {
	hits := make([]scored, n)
	for i := range hits {
		hits[i] = scored{idx: i}
	}
	return hits
}

func sameOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: %v, want %v", what, got, want)
		}
	}
}

// TestRepositoryOrders: by edits and by size the most first, and by remote
// the furthest behind, then whatever else is out of step, then the rest -
// each falling back on the activity.
func TestRepositoryOrders(t *testing.T) {
	t.Parallel()
	now := time.Now()
	names := []string{"old", "behind1", "ahead", "behind5", "insync", "uncloned", "failed"}
	var projects []forge.Project
	for i, n := range names {
		// The later in the list, the more recent.
		projects = append(projects, forge.Project{Instance: "s", PathWithNamespace: n, LastActivityAt: now.Add(time.Duration(i) * time.Hour)})
	}
	key := func(n string) projectKey { return projectKey{"s", n} }
	a := &App{cfg: &config.Config{}, disk: map[projectKey]diskInfo{}, repoSync: map[projectKey]remoteState{},
		repoSize: map[projectKey]int64{}, fetchFailed: map[projectKey]string{}}
	for _, n := range names {
		if n != "uncloned" {
			a.disk[key(n)] = diskInfo{Cloned: true}
			a.repoSync[key(n)] = remoteState{Upstream: gitx.Upstream{Name: "origin/main"}}
		}
	}
	a.repoSync[key("behind1")] = remoteState{Upstream: gitx.Upstream{Name: "origin/main", Behind: 1}, Edits: 2}
	a.repoSync[key("behind5")] = remoteState{Upstream: gitx.Upstream{Name: "origin/main", Behind: 5, Ahead: 1}}
	a.repoSync[key("ahead")] = remoteState{Upstream: gitx.Upstream{Name: "origin/main", Ahead: 3}, Edits: 7}
	a.fetchFailed[key("failed")] = "no route"
	a.repoSize[key("old")] = 900
	a.repoSize[key("insync")] = 50
	// A size of a clone that is gone is not counted.
	a.repoSize[key("uncloned")] = 5000

	name := func(idx int) string { return projects[idx].PathWithNamespace }
	sortBy := func(o string) []string {
		a.cfg.Filters.SetOrder(config.ListRepositories, o)
		hits := allHits(len(projects))
		a.sortProjects(hits, projects)
		return order(hits, name)
	}
	sameOrder(t, "by remote", sortBy(config.SortRemote),
		[]string{"behind5", "behind1", "failed", "ahead", "uncloned", "insync", "old"})
	sameOrder(t, "by edits", sortBy(config.SortEdits),
		[]string{"ahead", "behind1", "failed", "uncloned", "insync", "behind5", "old"})
	sameOrder(t, "by size", sortBy(config.SortSize),
		[]string{"old", "insync", "failed", "uncloned", "behind5", "ahead", "behind1"})
}

// TestMergeRequestOrders: by new commits the most counted first, then those
// that have some uncounted; by comments the open threads first.
func TestMergeRequestOrders(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a := &App{cfg: &config.Config{}, mrFresh: map[mrKey]int{}}
	for i := range 5 {
		a.mrs = append(a.mrs, forge.MergeRequest{Instance: "s", ID: i + 1, IID: i + 1, UpdatedAt: now.Add(time.Duration(i) * time.Hour)})
	}
	a.mrFresh[keyOfMR(a.mrs[0])] = 4
	a.mrFresh[keyOfMR(a.mrs[1])] = -1
	a.mrFresh[keyOfMR(a.mrs[2])] = 9
	a.mrs[0].Comments = 12
	a.mrs[1].Comments, a.mrs[1].Unresolved, a.mrs[1].UnresolvedKnown = 3, 2, true
	a.mrs[3].Comments, a.mrs[3].Unresolved, a.mrs[3].UnresolvedKnown = 5, 2, true

	name := func(idx int) string { return a.mrs[idx].Title }
	for i := range a.mrs {
		a.mrs[i].Title = string(rune('A' + i))
	}
	sortBy := func(o string) []string {
		a.cfg.Filters.SetOrder(config.ListMergeRequests, o)
		hits := allHits(len(a.mrs))
		a.sortMRs(hits)
		return order(hits, name)
	}
	sameOrder(t, "by new commits", sortBy(config.SortNew), []string{"C", "A", "B", "E", "D"})
	sameOrder(t, "by comments", sortBy(config.SortComments), []string{"D", "B", "A", "E", "C"})
}

// TestWorktreeOrderByEdits counts every member of a grouped worktree.
func TestWorktreeOrderByEdits(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a := &App{cfg: &config.Config{}, wtRemote: map[string]remoteState{
		"/one": {Edits: 1}, "/m1": {Edits: 2}, "/m2": {Edits: 2},
	}}
	a.worktrees = []worktreeRow{
		{Path: "one", Dir: "/one", Moved: now},
		{Path: "clean", Dir: "/clean", Moved: now.Add(time.Hour)},
		{Path: "group", Dir: "/g", Moved: now.Add(-time.Hour), Members: []worktreeRow{{Dir: "/m1"}, {Dir: "/m2"}}},
	}
	a.cfg.Filters.SetOrder(config.ListWorktrees, config.SortEdits)
	hits := allHits(len(a.worktrees))
	a.sortWorktrees(hits)
	sameOrder(t, "by edits", order(hits, func(idx int) string { return a.worktrees[idx].Path }),
		[]string{"group", "one", "clean"})
}
