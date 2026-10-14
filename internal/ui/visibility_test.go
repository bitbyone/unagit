package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

type visibilityProvider struct{ forge.Provider }

func (p visibilityProvider) GroupProjects(ctx context.Context, group forge.Group, subgroups bool) ([]forge.Project, error) {
	projects, err := p.Provider.GroupProjects(ctx, group, subgroups)
	for i := range projects {
		projects[i].Visibility = forge.VisibilityPublic
		if projects[i].ID == 1 {
			projects[i].Visibility = forge.VisibilityPrivate
		}
	}
	return projects, err
}

func TestVisibilityRefreshKeepsTheListAndSkipsHiddenFetches(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	gw := newRealProject(t, a, "acme/gateway")
	bl := newRealProject(t, a, "acme/billing")
	gw.rescan()
	bl.rescan()
	gitIn(t, bl.clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing-origin"))
	changeOnLoop(a, func() {
		id := a.cfg.Instances[0].ID
		a.clients[id] = visibilityProvider{a.clients[id]}
		a.cfg.Filters.RepositoryVisibility = forge.VisibilityPrivate
		a.projects[0].Visibility = forge.VisibilityPrivate
		a.projects[1].Visibility = forge.VisibilityPublic
		a.projectsPane.reload()
	})
	typeRunes(sc, "R")
	waitFor(t, a, sc, "2 repositories indexed")
	waitFetched(t, a)
	if failed := onLoop(a, func() bool {
		_, failed := a.fetchFailed[projectKey{a.cfg.Instances[0].ID, "acme/billing"}]
		return failed
	}); failed {
		t.Error("refresh fetched a repository hidden by visibility")
	}
	if count := onLoop(a, func() int { return len(a.projects) }); count != 2 {
		t.Errorf("refresh discarded the hidden repository: %d rows", count)
	}
}

func TestRepositoryVisibilityPickerKeepsTheCacheAndOtherLists(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.projects[0].Visibility = forge.VisibilityPrivate
		a.projects[1].Visibility = forge.VisibilityPublic
		for i, visibility := range []string{forge.VisibilityInternal, "future", ""} {
			pr := a.projects[1]
			pr.ID = 100 + i
			pr.PathWithNamespace = "acme/" + map[string]string{forge.VisibilityInternal: "internal", "future": "future", "": "unknown"}[visibility]
			pr.Visibility = visibility
			a.projects = append(a.projects, pr)
		}
		a.projectsPane.reload()
	})
	typeRunes(sc, "vjjj ")
	waitFor(t, a, sc, "Repository visibility")
	for _, width := range []int{120, 80, 60} {
		resizeApp(a, sc, width, 26)
		for _, label := range []string{"public", "private", "internal", "future", "unknown"} {
			waitFor(t, a, sc, label)
		}
		assertLegible(t, a, sc, "the visibility picker")
	}
	resizeApp(a, sc, 120, 34)
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Repository visibility")
	waitFor(t, a, sc, "visibility: private")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Repositories")
	waitFor(t, a, sc, "private only")
	waitGone(t, a, sc, "acme/billing")
	if got := onLoop(a, func() int { return len(a.projects) }); got != 5 {
		t.Fatalf("filtering discarded cached repositories: %d", got)
	}
	loaded, err := config.LoadFrom(a.cfg.Dir())
	if err != nil || loaded.Filters.RepositoryVisibility != forge.VisibilityPrivate {
		t.Fatalf("the visibility preference did not persist: %v, %v", loaded, err)
	}
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Invoice rounding")
	typeRunes(sc, "1vjjj ")
	waitFor(t, a, sc, "Repository visibility")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Repository visibility")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Repositories")
	waitFor(t, a, sc, "acme/billing")
	for _, visibility := range []string{forge.VisibilityPublic, forge.VisibilityInternal, "future", "unknown"} {
		changeOnLoop(a, func() { a.cfg.Filters.RepositoryVisibility = visibility; a.projectsPane.reload() })
		if count := onLoop(a, func() int { return len(a.filterProjects(a.projects, "")) }); count != 1 {
			t.Errorf("%s: %d repositories shown, want one", visibility, count)
		}
	}
}
