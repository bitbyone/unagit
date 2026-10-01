package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// hiddenMark is what a project kept out of the lists is drawn with.
const hiddenMark = "⊘"

// passesFilters reports whether a project survives the shared filters. Both
// lists ask the same question, so a merge request disappears with the project
// it belongs to.
func (a *App) passesFilters(instance, path string) bool {
	f := &a.cfg.Filters
	if f.IsHidden(instance, path) {
		return false
	}
	if f.ClonedOnly && !a.diskOf(instance, path).Cloned {
		return false
	}
	return true
}

// filterSummary is the part of a list's header that says what is being left
// out, so a narrowed list never looks like an empty one. grouped is whether
// that list is drawn under headings, which each list decides for itself.
func (a *App) filterSummary(grouped bool) string {
	f := &a.cfg.Filters
	parts := []string{sortLabel(f.Order())}
	if f.ClonedOnly {
		parts = append(parts, tag(colOn)+"cloned only"+tagEnd+tag(colMuted))
	}
	if n := len(f.Hidden); n > 0 {
		parts = append(parts, fmt.Sprintf("%s%s %d%s", tag(colWarn), hiddenMark, n, tagEnd)+tag(colMuted))
	}
	if grouped {
		parts = append(parts, tag(colOn)+"grouped"+tagEnd+tag(colMuted))
	}
	return " · " + strings.Join(parts, " · ")
}

func sortLabel(order string) string {
	if order == config.SortName {
		return "by name"
	}
	return "by activity"
}

// applyFilters saves the shared settings and redraws both lists with them.
func (a *App) applyFilters() {
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the filters: %v", err)
		return
	}
	a.projectsPane.reload()
	a.mrsPane.reload()
}

// toggleClonedOnly narrows both lists to what is on disk, or widens them again.
func (a *App) toggleClonedOnly() {
	a.cfg.Filters.ClonedOnly = !a.cfg.Filters.ClonedOnly
	a.applyFilters()
	if a.cfg.Filters.ClonedOnly {
		a.note("Showing only the repositories you have cloned")
		return
	}
	a.note("Showing every repository again")
}

// hideProject takes the project under the cursor out of both lists, or puts
// it back.
func (a *App) hideProject(instance, path string) {
	if path == "" {
		return
	}
	hidden := a.cfg.Filters.ToggleHidden(instance, path)
	a.applyFilters()
	if hidden {
		a.note(fmt.Sprintf("%s hidden · X manages the hidden ones", path))
		return
	}
	a.note(path + " is back")
}

// toggleGrouping gathers the merge requests under their project, or lets
// them run flat again.
func (a *App) toggleGrouping() {
	a.cfg.Filters.GroupByProject = !a.cfg.Filters.GroupByProject
	a.applyFilters()
	if a.cfg.Filters.GroupByProject {
		a.note("Merge requests grouped by project, sorted " + sortLabel(a.cfg.Filters.Order()) + " inside each")
		return
	}
	a.note("Merge requests listed flat again")
}

// toggleRepositoryGrouping gathers the repositories under the group they
// live in, or lets them run flat again.
func (a *App) toggleRepositoryGrouping() {
	a.cfg.Filters.GroupRepositories = !a.cfg.Filters.GroupRepositories
	a.applyFilters()
	if a.cfg.Filters.GroupRepositories {
		a.note("Repositories grouped by group, sorted " + sortLabel(a.cfg.Filters.Order()) + " inside each")
		return
	}
	a.note("Repositories listed flat again")
}

// favouritesFirst is the order picker's switch for the favourites, which
// holds whichever sort is chosen.
const favouritesFirst = "favourites"

// showSortPicker chooses the order both lists are drawn in.
func (a *App) showSortPicker() {
	f := &a.cfg.Filters
	favourites := pickItem{Label: favouriteMark + " favourites first: on", Sub: "Enter: in order with the rest", Data: favouritesFirst}
	if !f.FavouritesFirst() {
		favourites.Label, favourites.Sub = favouriteMark+" favourites first: off", "Enter: ahead of the rest"
	}
	items := []pickItem{
		{Label: sortLabel(config.SortActivity), Sub: "what moved most recently, first", Data: config.SortActivity},
		{Label: sortLabel(config.SortName), Sub: "by path, merge requests by number", Data: config.SortName},
		favourites,
	}
	a.showPicker("Sort both lists", items, func(it pickItem) {
		if it.Data == favouritesFirst {
			f.FavouritesInPlace = !f.FavouritesInPlace
			a.applyFilters()
			if f.FavouritesFirst() {
				a.note("Favourites first, set apart from the rest")
			} else {
				a.note("Favourites in order with the rest")
			}
			return
		}
		f.Sort = it.Data.(string)
		a.applyFilters()
		a.note("Sorted " + sortLabel(f.Order()))
	})
}

// forgetClosedFavourites unstars the merge requests a refresh of these
// servers no longer lists: merged or closed, they will not come back.
func (a *App) forgetClosedFavourites(asked []config.Instance, open []forge.MergeRequest) {
	servers := map[string]bool{}
	for _, inst := range asked {
		servers[inst.ID] = true
	}
	listed := map[config.Favourite]bool{}
	for _, mr := range open {
		listed[config.Favourite{Instance: mr.Instance, Path: a.projectPathOfMR(mr), IID: mr.IID}] = true
	}
	if a.cfg.Filters.ForgetClosedFavourites(servers, listed) == 0 {
		return
	}
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the favourites: %v", err)
	}
}

// toggleFavourite stars a repository (iid 0) or a merge request, or takes the
// star away again.
func (a *App) toggleFavourite(instance, path string, iid int, what string) {
	if path == "" {
		return
	}
	starred := a.cfg.Filters.ToggleFavourite(instance, path, iid)
	a.applyFilters()
	if starred {
		a.note(favouriteMark + " " + what + " is a favourite")
		return
	}
	a.note(what + " is no longer a favourite")
}

// showHiddenPicker manages which repositories stay out of the lists. Every
// project of every server is listed, hidden ones included: this is the one
// place they can be found again.
func (a *App) showHiddenPicker() {
	type row struct{ instance, path string }
	a.showToggles(toggles{
		title: "Hidden repositories",
		verb:  "hide/show",
		items: func() []toggleItem {
			var items []toggleItem
			for _, p := range a.projects {
				marker, text := tag(colDim)+"·"+tagEnd, p.PathWithNamespace
				if a.cfg.Filters.IsHidden(p.Instance, p.PathWithNamespace) {
					marker = tag(colWarn) + hiddenMark + tagEnd
					text = tag(colMuted) + text + tagEnd
				}
				label := marker + " " + text
				if a.multiInstance() {
					label += "   " + tag(colDim) + a.instanceLabel(p.Instance) + tagEnd
				}
				items = append(items, toggleItem{Label: label,
					Search: p.PathWithNamespace + " " + a.instanceLabel(p.Instance),
					Data:   row{p.Instance, p.PathWithNamespace}})
			}
			sort.SliceStable(items, func(i, j int) bool {
				return items[i].Data.(row).path < items[j].Data.(row).path
			})
			return items
		},
		toggle: func(it toggleItem) {
			r := it.Data.(row)
			a.cfg.Filters.ToggleHidden(r.instance, r.path)
			a.applyFilters()
		},
		status: func() string { return fmt.Sprintf("%d hidden", len(a.cfg.Filters.Hidden)) },
		keys: []toggleKey{{key: 'a', hint: "show all", run: func() {
			if n := a.cfg.Filters.ShowAll(); n > 0 {
				a.applyFilters()
				a.note(fmt.Sprintf("%d repositor%s back", n, plural(n, "y is", "ies are")))
			}
		}}},
	})
}

// selectedProjectOf reports which project the cursor is on, whichever list it
// is in.
func (a *App) selectedProjectOf(p *pane) (instance, path string) {
	i := p.selectedIndex()
	if i < 0 {
		return "", ""
	}
	if p == a.projectsPane && i < len(a.projects) {
		pr := a.projects[i]
		return pr.Instance, pr.PathWithNamespace
	}
	if p == a.mrsPane && i < len(a.mrs) {
		mr := a.mrs[i]
		return mr.Instance, a.projectPathOfMR(mr)
	}
	return "", ""
}

// filterKeysFor wires the shared filter keys into a list.
func (a *App) filterKeysFor(p *pane) func(*tcell.EventKey) bool {
	return func(ev *tcell.EventKey) bool {
		if ev.Key() != tcell.KeyRune {
			return false
		}
		switch ev.Rune() {
		case 'L':
			a.toggleClonedOnly()
			return true
		case 'x':
			instance, path := a.selectedProjectOf(p)
			a.hideProject(instance, path)
			return true
		case 'X':
			a.showHiddenPicker()
			return true
		case 'o':
			a.showSortPicker()
			return true
		}
		return false
	}
}
