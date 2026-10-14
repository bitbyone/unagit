package ui

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

type serverFilterProvider struct {
	forge.Provider
	projects atomic.Int32
	mrs      atomic.Int32
}

func (p *serverFilterProvider) GroupProjects(ctx context.Context, group forge.Group, subgroups bool) ([]forge.Project, error) {
	p.projects.Add(1)
	return p.Provider.GroupProjects(ctx, group, subgroups)
}

func (p *serverFilterProvider) GroupMergeRequests(ctx context.Context, group forge.Group, subgroups bool) ([]forge.MergeRequest, error) {
	p.mrs.Add(1)
	return p.Provider.GroupMergeRequests(ctx, group, subgroups)
}

func TestServerFilterSticksAndRefreshKeepsOtherServers(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	var first, second string
	var other *serverFilterProvider
	changeOnLoop(a, func() {
		first = a.cfg.Instances[0].ID
		a.cfg.Instances[0].Name = "first"
		inst := *a.cfg.AddInstance(config.Instance{ID: "second", Name: "second", URL: "https://second.example", Groups: []config.Group{{ID: 1, FullPath: "acme", Scope: config.ScopeSubgroups}}})
		second = inst.ID
		other = &serverFilterProvider{Provider: a.clients[first]}
		a.clients[second] = other
		for _, pr := range append([]forge.Project(nil), a.projects...) {
			pr.Instance = second
			a.projects = append(a.projects, pr)
		}
		for _, mr := range append([]forge.MergeRequest(nil), a.mrs...) {
			mr.Instance = second
			mr.Title = "Other server's " + mr.Title
			mr.Pipeline = "success"
			a.mrs = append(a.mrs, mr)
		}
		if a.me == nil {
			a.me = map[string]string{}
		}
		a.me[second] = "second-user"
		a.cfg.Filters.ToggleFavourite(second, "acme/gateway", 7)
		a.reindexProjects()
		a.projectsPane.reload()
	})
	typeRunes(sc, "vjjjj ")
	waitFor(t, a, sc, "Filter by server")
	for _, width := range []int{120, 80, 60} {
		resizeApp(a, sc, width, 26)
		for _, label := range []string{"all", "first", "second", "second.example"} {
			waitFor(t, a, sc, label)
		}
		assertLegible(t, a, sc, "the server filter")
		if width == 80 {
			t.Logf("server filter at 80x26:\n%s", a.screenText(sc))
		}
	}
	resizeApp(a, sc, 120, 34)
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Filter by server")
	waitFor(t, a, sc, "server: first")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Repositories")
	loaded, err := config.LoadFrom(a.cfg.Dir())
	if err != nil || loaded.Filters.Server != first {
		t.Fatalf("the server ID did not persist: %v, %v", loaded, err)
	}
	if count := onLoop(a, func() int { return len(a.filterProjects(a.projects, "")) }); count != 2 {
		t.Fatalf("the server filter shows %d repositories, want two", count)
	}
	typeRunes(sc, "R")
	waitFor(t, a, sc, "4 repositories indexed")
	if other.projects.Load() != 0 {
		t.Fatal("refresh asked a server hidden by the filter")
	}
	typeRunes(sc, "2R")
	waitFor(t, a, sc, "3 open merge requests")
	waitGone(t, a, sc, "refreshing merge requests")
	if other.mrs.Load() != 0 {
		t.Fatal("the merge request refresh asked a hidden server")
	}
	if !onLoop(a, func() bool {
		return len(a.mrs) == 6 && a.me[second] == "second-user" && a.cfg.Filters.IsFavourite(second, "acme/gateway", 7)
	}) {
		t.Fatal("refresh discarded another server's cache, identity or favourite")
	}
	for _, mr := range onLoop(a, func() []forge.MergeRequest { return append([]forge.MergeRequest(nil), a.mrs...) }) {
		if mr.Instance == second && mr.Pipeline != "success" {
			t.Error("a hidden server's merge request lost its cached details")
		}
	}
	typeRunes(sc, "1vjjjj ")
	waitFor(t, a, sc, "Filter by server")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Filter by server")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Repositories")
	if count := onLoop(a, func() int { return len(a.filterProjects(a.projects, "")) }); count != 4 {
		t.Fatalf("all did not restore the cached repositories: %d", count)
	}
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Other server's")
	for _, tab := range []string{"1", "2", "3"} {
		typeRunes(sc, tab+"?")
		waitFor(t, a, sc, "unagit · keys")
		assertLegible(t, a, sc, "the server filter's help")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "unagit · keys")
	}
}

func TestServerFilterKeepsMixedGroupsWhole(t *testing.T) {
	t.Parallel()
	a := &App{cfg: &config.Config{Filters: config.Filters{Server: "first"}}}
	a.worktrees = []worktreeRow{
		{Instance: "first", Path: "acme/one"},
		{Instance: "second", Path: "acme/two"},
		{Path: "mixed", Members: []worktreeRow{{Instance: "first", Path: "acme/one"}, {Instance: "second", Path: "acme/two"}}},
	}
	if got := a.filterWorktrees(""); len(got) != 2 || len(a.worktrees[2].Members) != 2 {
		t.Errorf("server filtering changed the mixed group: %v", got)
	}
}
