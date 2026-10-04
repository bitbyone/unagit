package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)


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
		parts = append(parts, fmt.Sprintf("%s%s %d%s", tag(colWarn), glyphHidden, n, tagEnd)+tag(colMuted))
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
	favourites := pickItem{Label: glyphFavourite + " favourites first: on", Sub: "Enter: in order with the rest", Data: favouritesFirst}
	if !f.FavouritesFirst() {
		favourites.Label, favourites.Sub = glyphFavourite+" favourites first: off", "Enter: ahead of the rest"
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
		a.note(glyphFavourite + " " + what + " is a favourite")
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
					marker = tag(colWarn) + glyphHidden + tagEnd
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

// authorSummary is what the merge request header says of the hidden authors:
// how many are kept out, or that they are shown for now.
func (a *App) authorSummary() string {
	f := &a.cfg.Filters
	switch n := len(f.HiddenAuthors); {
	case n == 0:
		return ""
	case f.ShowHiddenAuthors:
		return fmt.Sprintf(" · %s%d hidden author(s) shown%s%s", tag(colDim), n, tagEnd, tag(colMuted))
	default:
		return fmt.Sprintf(" · %s%s %d author(s)%s%s", tag(colWarn), glyphHidden, n, tagEnd, tag(colMuted))
	}
}

// hideAuthor keeps an author's merge requests out of the list.
func (a *App) hideAuthor(mr forge.MergeRequest) {
	name := mr.Author.Username
	if a.cfg.Filters.HidesAuthor(mr.Instance, name) {
		return
	}
	a.cfg.Filters.ToggleAuthor(mr.Instance, name)
	a.applyFilters()
	a.note(fmt.Sprintf("%s %s's merge requests hidden · v shows them again", glyphHidden, name))
}

// showMRViewOptions switches what the merge request list shows, and lists the
// hidden authors: space on one shows that author's merge requests again.
func (a *App) showMRViewOptions() {
	f := &a.cfg.Filters
	type option struct {
		label string
		on    func() bool
		flip  func()
	}
	options := []option{
		{"grouped by repository (Ctrl-G)", func() bool { return f.GroupByProject }, func() { f.GroupByProject = !f.GroupByProject }},
		{"favourites first, flat (o)", f.FavouritesFirst, func() { f.FavouritesInPlace = !f.FavouritesInPlace }},
		{"only what is cloned (L)", func() bool { return f.ClonedOnly }, func() { f.ClonedOnly = !f.ClonedOnly }},
		{"only mine", func() bool { return f.OnlyMine }, func() { f.OnlyMine = !f.OnlyMine }},
		{"only those I review or am assigned", func() bool { return f.OnlyToReview }, func() { f.OnlyToReview = !f.OnlyToReview }},
		{"hide drafts", func() bool { return f.HideDrafts }, func() { f.HideDrafts = !f.HideDrafts }},
		{"hide the authors below", func() bool { return !f.ShowHiddenAuthors }, func() { f.ShowHiddenAuthors = !f.ShowHiddenAuthors }},
	}
	a.showToggles(toggles{
		title: "View · Merge requests",
		verb:  "on/off",
		items: func() []toggleItem {
			items := make([]toggleItem, 0, len(options)+len(f.HiddenAuthors))
			for i, o := range options {
				items = append(items, toggleItem{Label: tagMark(o.on()) + " " + o.label, Search: o.label, Data: i})
			}
			for _, h := range f.HiddenAuthors {
				label := "  " + tag(colWarn) + glyphHidden + tagEnd + " " + esc(h.Username)
				if a.multiInstance() {
					label += "   " + tag(colDim) + a.instanceLabel(h.Instance) + tagEnd
				}
				items = append(items, toggleItem{Label: label, Search: h.Username, Data: h})
			}
			return items
		},
		toggle: func(it toggleItem) {
			switch d := it.Data.(type) {
			case int:
				options[d].flip()
			case config.HiddenAuthor:
				f.ToggleAuthor(d.Instance, d.Username)
			}
			a.applyFilters()
		},
		status: func() string {
			if len(f.HiddenAuthors) == 0 {
				return tag(colDim) + "no author hidden · H on a merge request hides its author" + tagEnd
			}
			return fmt.Sprintf("%s%d author(s) hidden · space on one shows it again%s", tag(colDim), len(f.HiddenAuthors), tagEnd)
		},
	})
}

// passesMRFilters reports whether a merge request survives the filters only
// merge requests have: hidden authors, drafts, and whose they are. Whose is
// known once a refresh has asked each server who the token belongs to; until
// then a server's merge requests are not narrowed by it.
func (a *App) passesMRFilters(mr forge.MergeRequest) bool {
	f := &a.cfg.Filters
	if f.HidesAuthor(mr.Instance, mr.Author.Username) || f.HideDrafts && mr.Draft {
		return false
	}
	me := a.me[mr.Instance]
	if me == "" || !f.OnlyMine && !f.OnlyToReview {
		return true
	}
	if f.OnlyMine && mr.Author.Username == me {
		return true
	}
	if f.OnlyToReview {
		for _, u := range append(append([]forge.User{}, mr.Reviewers...), mr.Assignees...) {
			if u.Username == me {
				return true
			}
		}
	}
	return false
}

// whoseSummary is what the merge request header says of the filters on whose
// they are and of the drafts.
func (a *App) whoseSummary() string {
	f := &a.cfg.Filters
	var parts []string
	switch {
	case f.OnlyMine && f.OnlyToReview:
		parts = append(parts, "mine or to review")
	case f.OnlyMine:
		parts = append(parts, "mine")
	case f.OnlyToReview:
		parts = append(parts, "to review")
	}
	if (f.OnlyMine || f.OnlyToReview) && len(a.me) == 0 {
		parts[len(parts)-1] += " (R to learn who you are)"
	}
	if f.HideDrafts {
		parts = append(parts, "no drafts")
	}
	if len(parts) == 0 {
		return ""
	}
	return " · " + tag(colOn) + strings.Join(parts, " · ") + tagEnd + tag(colMuted)
}
