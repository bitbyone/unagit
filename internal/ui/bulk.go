package ui

import (
	"fmt"

	"github.com/tobola/unagit/internal/forge"
)

// Space marks rows of Repositories and Merge requests, and the keys then act
// on every marked row at once, as they would on one: x and H hide, r
// refreshes, Ctrl-F stars. A key that toggles turns the state of them all
// one way: when every marked row is already so, back; else the rest are
// made so too, never some one way and some the other. The marks are done
// with once the key has acted, since the rows it hid are gone.

// toggleAll sets a state on every one of n things: off when all of them
// have it, else on where it is missing. It reports which way it went.
func toggleAll(n int, has func(i int) bool, toggle func(i int)) bool {
	all := true
	for i := range n {
		if !has(i) {
			all = false
			break
		}
	}
	for i := range n {
		if has(i) == all {
			toggle(i)
		}
	}
	return !all
}

// counted is n and the word that fits it, "1 repository", "3 repositories".
func counted(n int, one, more string) string {
	return fmt.Sprintf("%d %s", n, plural(n, one, more))
}

// markedRepositoryActions are what can be done with the marked repositories
// together.
func (a *App) markedRepositoryActions(p *pane, picked []forge.Project) []uiAction {
	done := func() { p.marks = nil }
	return []uiAction{
		{name: "New Grouped Worktree…", about: "Make one folder holding a worktree of each marked repository, on a new branch of its own.", keys: "Ctrl-W", rank: 10, run: func() { a.startGroupWorktree(picked) }},
		{name: "Hide or Unhide All", about: "Take every marked repository out of the lists, or, when all are hidden, bring them back.", keys: "x", rank: 30, run: func() {
			done()
			hid := toggleAll(len(picked),
				func(i int) bool { return a.cfg.Filters.IsHidden(picked[i].Instance, picked[i].PathWithNamespace) },
				func(i int) { a.cfg.Filters.ToggleHidden(picked[i].Instance, picked[i].PathWithNamespace) })
			a.applyFilters()
			if hid {
				a.note(fmt.Sprintf("%s hidden · X manages the hidden ones", counted(len(picked), "repository", "repositories")))
				return
			}
			a.note(fmt.Sprintf("%s back", counted(len(picked), "repository is", "repositories are")))
		}},
		{name: "Hide or List Their MRs", about: "Keep the marked repositories' merge requests out of Merge requests and its refreshes, or list them again.", keys: "H", rank: 35, run: func() {
			done()
			hid := toggleAll(len(picked),
				func(i int) bool { return a.cfg.Filters.HidesMRsOf(picked[i].Instance, picked[i].PathWithNamespace) },
				func(i int) { a.cfg.Filters.ToggleMRsOf(picked[i].Instance, picked[i].PathWithNamespace) })
			a.applyFilters()
			if hid {
				a.note(fmt.Sprintf("%s %s' merge requests hidden · v in Merge requests shows them again", glyphHidden, counted(len(picked), "repository", "repositories")))
				return
			}
			a.note(fmt.Sprintf("the merge requests of %s are listed again", counted(len(picked), "repository", "repositories")))
		}},
		{name: "Refresh All Marked", about: "Fetch every marked clone and read again where it stands.", keys: "r", rank: 40, run: func() {
			done()
			fetched := 0
			for _, pr := range picked {
				if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
					a.refreshProjectRow(pr)
					fetched++
				}
			}
			if fetched == 0 {
				a.note("none of them is cloned - nothing to fetch")
			}
			p.reload()
		}},
		{name: "Star or Unstar All", about: "Make every marked repository a favourite, or, when all are, none of them.", keys: "Ctrl-F", rank: 45, run: func() {
			done()
			starred := toggleAll(len(picked),
				func(i int) bool { return a.cfg.Filters.IsFavourite(picked[i].Instance, picked[i].PathWithNamespace, 0) },
				func(i int) { a.cfg.Filters.ToggleFavourite(picked[i].Instance, picked[i].PathWithNamespace, 0) })
			a.applyFilters()
			if starred {
				a.note(glyphFavourite + " " + counted(len(picked), "favourite", "favourites") + " more")
				return
			}
			a.note(counted(len(picked), "repository is", "repositories are") + " no longer favourites")
		}},
		{name: "Toggle Mark", about: "Mark the row under the cursor, or take its mark away.", keys: "space", rank: 20, run: p.toggleMark},
		{name: "Clear Marks", about: "Take every mark away.", keys: "Esc", rank: 900, run: p.clearMarks},
	}
}

// markedMRs is the marked merge requests, in the list's order of data.
func (a *App) markedMRs() []forge.MergeRequest {
	var out []forge.MergeRequest
	for _, i := range a.mrsPane.marked() {
		if i < len(a.mrs) {
			out = append(out, a.mrs[i])
		}
	}
	return out
}

// markedMRActions are what can be done with the marked merge requests
// together.
func (a *App) markedMRActions(p *pane, picked []forge.MergeRequest) []uiAction {
	done := func() { p.marks = nil }
	// Their repositories and authors, each once.
	var repos []projectKey
	var authors []forge.MergeRequest
	seenRepo, seenAuthor := map[projectKey]bool{}, map[string]bool{}
	for _, mr := range picked {
		k := projectKey{mr.Instance, a.projectPathOfMR(mr)}
		if !seenRepo[k] {
			seenRepo[k] = true
			repos = append(repos, k)
		}
		if who := mr.Instance + "\x00" + mr.Author.Username; mr.Author.Username != "" && !seenAuthor[who] {
			seenAuthor[who] = true
			authors = append(authors, mr)
		}
	}
	return []uiAction{
		{name: "Hide Their Repositories' MRs", about: "Keep the merge requests of the marked ones' repositories out of the list and out of refreshes; View Options shows them again.", keys: "x", rank: 30, run: func() {
			done()
			for _, k := range repos {
				if !a.cfg.Filters.HidesMRsOf(k.Instance, k.Path) {
					a.cfg.Filters.ToggleMRsOf(k.Instance, k.Path)
				}
			}
			a.applyFilters()
			a.note(fmt.Sprintf("%s %s' merge requests hidden · v shows them again", glyphHidden, counted(len(repos), "repository", "repositories")))
		}},
		{name: "Hide Their Authors", about: "Keep the merge requests of the marked ones' authors out of the list; View Options shows them again.", keys: "H", rank: 35,
			when: func() bool { return len(authors) > 0 }, run: func() {
				done()
				for _, mr := range authors {
					if !a.cfg.Filters.HidesAuthor(mr.Instance, mr.Author.Username) {
						a.cfg.Filters.ToggleAuthor(mr.Instance, mr.Author.Username)
					}
				}
				a.applyFilters()
				a.note(fmt.Sprintf("%s %s hidden · v shows them again", glyphHidden, counted(len(authors), "author", "authors")))
			}},
		{name: "Refresh All Marked", about: "Ask the server about each marked merge request: its state, head, pipeline, approvals and threads.", keys: "r", rank: 40, run: func() {
			done()
			for _, mr := range picked {
				a.refreshMRRow(mr)
			}
			p.reload()
		}},
		{name: "Mark All as Reviewed", about: "Take the heads of the marked ones as seen, so NEW counts only what is pushed after.", keys: "V", rank: 45, run: func() {
			done()
			marked := 0
			if a.seen == nil {
				a.seen = map[string]string{}
			}
			for _, mr := range picked {
				if mr.SHA != "" {
					a.seen[seenKey(mr)] = mr.SHA
					marked++
				}
			}
			a.saveSeen()
			a.loadMRFresh()
			p.reload()
			a.done(fmt.Sprintf("%s marked as reviewed", counted(marked, "merge request", "merge requests")))
		}},
		{name: "Star or Unstar All", about: "Make every marked merge request a favourite, or, when all are, none of them.", keys: "Ctrl-F", rank: 50, run: func() {
			done()
			path := func(i int) string { return a.projectPathOfMR(picked[i]) }
			starred := toggleAll(len(picked),
				func(i int) bool { return a.cfg.Filters.IsFavourite(picked[i].Instance, path(i), picked[i].IID) },
				func(i int) { a.cfg.Filters.ToggleFavourite(picked[i].Instance, path(i), picked[i].IID) })
			a.applyFilters()
			if starred {
				a.note(glyphFavourite + " " + counted(len(picked), "favourite", "favourites") + " more")
				return
			}
			a.note(counted(len(picked), "merge request is", "merge requests are") + " no longer favourites")
		}},
		{name: "Toggle Mark", about: "Mark the row under the cursor, or take its mark away.", keys: "space", rank: 20, run: p.toggleMark},
		{name: "Clear Marks", about: "Take every mark away.", keys: "Esc", rank: 900, run: p.clearMarks},
	}
}
