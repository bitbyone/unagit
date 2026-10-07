package ui

import (
	"cmp"
	"sort"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// sortLabel is how an order is named in a list's header and its picker.
func sortLabel(order string) string {
	switch order {
	case config.SortName:
		return "by name"
	case config.SortEdits:
		return "by edits"
	case config.SortSize:
		return "by size"
	case config.SortRemote:
		return "by remote"
	case config.SortNew:
		return "by new commits"
	case config.SortComments:
		return "by comments"
	}
	return "by activity"
}

// sortAbout says what comes first in an order, under it in the picker.
func sortAbout(list, order string) string {
	switch order {
	case config.SortName:
		if list == config.ListMergeRequests {
			return "by repository, then by number"
		}
		return "by path"
	case config.SortEdits:
		return "the most files with uncommitted changes first"
	case config.SortSize:
		return "what takes the most disk first"
	case config.SortRemote:
		return "furthest behind origin first, then any other out of step"
	case config.SortNew:
		return "the most commits since your last review first"
	case config.SortComments:
		return "the most open threads first, then the most comments"
	}
	return "what moved most recently, first"
}

// listOfTab is the list a main tab shows, as the configuration names it.
func listOfTab(tab string) string {
	switch tab {
	case pageMRs:
		return config.ListMergeRequests
	case pageWorktrees:
		return config.ListWorktrees
	}
	return config.ListRepositories
}

// order is the sort a list is drawn in.
func (a *App) order(list string) string { return a.cfg.Filters.Order(list) }

// rankThenNewest sorts the hits by a rank, highest first, and what ranks
// the same by its time, newest first: every order but the name falls back
// on the activity, so rows with nothing to say stay where the eye expects.
func rankThenNewest(hits []scored, rank func(idx int) []int, when func(idx int) time.Time) {
	sort.SliceStable(hits, func(i, j int) bool {
		l, r := rank(hits[i].idx), rank(hits[j].idx)
		for k := range l {
			if c := cmp.Compare(l[k], r[k]); c != 0 {
				return c > 0
			}
		}
		return when(hits[i].idx).After(when(hits[j].idx))
	})
}

// sortProjects puts the repositories in the list's order.
func (a *App) sortProjects(hits []scored, projects []forge.Project) {
	keyOf := func(idx int) projectKey {
		return projectKey{projects[idx].Instance, projects[idx].PathWithNamespace}
	}
	newest := func(idx int) time.Time { return projects[idx].LastActivityAt }
	switch a.order(config.ListRepositories) {
	case config.SortName:
		sort.SliceStable(hits, func(i, j int) bool {
			return projects[hits[i].idx].PathWithNamespace < projects[hits[j].idx].PathWithNamespace
		})
	case config.SortEdits:
		rankThenNewest(hits, func(idx int) []int {
			key := keyOf(idx)
			if !a.disk[key].Cloned {
				return []int{0}
			}
			return []int{a.repoSync[key].Edits}
		}, newest)
	case config.SortSize:
		rankThenNewest(hits, func(idx int) []int {
			key := keyOf(idx)
			if !a.disk[key].Cloned {
				return []int{0}
			}
			return []int{int(a.repoSize[key])}
		}, newest)
	case config.SortRemote:
		rankThenNewest(hits, func(idx int) []int { return a.remoteOrder(keyOf(idx)) }, newest)
	default:
		sort.SliceStable(hits, func(i, j int) bool {
			return newest(hits[i].idx).After(newest(hits[j].idx))
		})
	}
}

// remoteOrder ranks a clone for the order by remote: behind origin first,
// the furthest behind ahead of the rest, then whatever else is not in step
// with it - diverged elsewhere, unpushed, no upstream, a failed fetch -
// and last what is in step, not cloned or not looked at yet.
func (a *App) remoteOrder(key projectKey) []int {
	if !a.disk[key].Cloned {
		return []int{0, 0}
	}
	if _, failed := a.fetchFailed[key]; failed {
		return []int{1, 0}
	}
	st, known := a.repoSync[key]
	switch {
	case !known:
		return []int{0, 0}
	case st.Upstream.Name != "" && !st.Upstream.Gone && st.Upstream.Behind > 0:
		return []int{2, st.Upstream.Behind}
	case remoteRank(st) > 0:
		return []int{1, 0}
	}
	return []int{0, 0}
}

// sortMRs puts the merge requests in the list's order.
func (a *App) sortMRs(hits []scored) {
	newest := func(idx int) time.Time { return a.mrSortTime(a.mrs[idx]) }
	switch a.order(config.ListMergeRequests) {
	case config.SortName:
		sort.SliceStable(hits, func(i, j int) bool {
			left, right := a.mrs[hits[i].idx], a.mrs[hits[j].idx]
			if lp, rp := a.projectPathOfMR(left), a.projectPathOfMR(right); lp != rp {
				return lp < rp
			}
			return left.IID < right.IID
		})
	case config.SortNew:
		rankThenNewest(hits, func(idx int) []int {
			// A review that cannot count its new commits still has some,
			// so it comes after every counted one and before none.
			switch n := a.mrFresh[keyOfMR(a.mrs[idx])]; {
			case n > 0:
				return []int{2, n}
			case n < 0:
				return []int{1, 0}
			}
			return []int{0, 0}
		}, newest)
	case config.SortComments:
		rankThenNewest(hits, func(idx int) []int {
			mr := a.mrs[idx]
			unresolved := 0
			if mr.UnresolvedKnown {
				unresolved = mr.Unresolved
			}
			return []int{unresolved, mr.Comments}
		}, newest)
	default:
		sort.SliceStable(hits, func(i, j int) bool {
			return newest(hits[i].idx).After(newest(hits[j].idx))
		})
	}
}

// sortWorktrees puts the worktrees in the list's order.
func (a *App) sortWorktrees(hits []scored) {
	newest := func(idx int) time.Time { return a.worktrees[idx].Moved }
	switch a.order(config.ListWorktrees) {
	case config.SortName:
		sort.SliceStable(hits, func(i, j int) bool {
			l, r := a.worktrees[hits[i].idx], a.worktrees[hits[j].idx]
			if l.Path != r.Path {
				return l.Path < r.Path
			}
			return l.Branch < r.Branch
		})
	case config.SortEdits:
		rankThenNewest(hits, func(idx int) []int { return []int{a.worktreeEditCount(a.worktrees[idx])} }, newest)
	default:
		sort.SliceStable(hits, func(i, j int) bool {
			return newest(hits[i].idx).After(newest(hits[j].idx))
		})
	}
}
