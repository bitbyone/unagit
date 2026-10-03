package ui

import (
	"fmt"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

// The actions of every screen, as the pickers list them and the keys find
// them (palette.go). The ranks put first what one usually comes for: opening
// and making worktrees before ordering and filtering, and what destroys
// something last.

// listActions are what every list has: moving between the screens, the
// filter, help, quitting.
func (a *App) listActions(p *pane) []uiAction {
	return []uiAction{
		{name: "Filter the list", keys: "/", rank: 300, run: p.startFilter},
		{name: "Go to Repositories", keys: "R", rank: 900, when: func() bool { return a.currentTab() != pageProjects },
			run: func() { a.switchTab(pageProjects) }},
		{name: "Go to Merge requests", keys: "M", rank: 900, when: func() bool { return a.currentTab() != pageMRs },
			run: func() { a.switchTab(pageMRs) }},
		{name: "Go to Worktrees", keys: "W", rank: 900, when: func() bool { return a.currentTab() != pageWorktrees },
			run: func() { a.switchTab(pageWorktrees) }},
		{name: "Go to Settings", keys: "S", rank: 910, run: func() { a.switchTab(pageSettings) }},
		{name: "Help: every key", keys: "?", rank: 950, run: a.showHelp},
		{name: "Quit unagit", keys: "q", rank: 999, run: a.tv.Stop},
	}
}

// openWeb opens an address in the browser, when there is one.
func (a *App) openWeb(url string) {
	if url == "" {
		a.flash("no address known for it")
		return
	}
	_ = openBrowser(url)
	a.note("opened " + url)
}

// filterActions are the filters Repositories and Merge requests share.
func (a *App) filterActions() []uiAction {
	return []uiAction{
		{name: "Order: by activity or by name", keys: "o", rank: 400, run: a.showSortPicker},
		{name: "Show only cloned repositories, or all", keys: "L", rank: 410, run: a.toggleClonedOnly},
		{name: "Manage hidden repositories", keys: "X", rank: 480, run: a.showHiddenPicker},
	}
}

// hideAction hides the repository of the row, or shows it again.
func (a *App) hideAction(p *pane) uiAction {
	return uiAction{name: "Hide the repository, or show it again", keys: "x", rank: 470, run: func() {
		instance, path := a.selectedProjectOf(p)
		a.hideProject(instance, path)
	}}
}

// repositoryActions are what can be done with one repository.
func (a *App) repositoryActions(p *pane, pr forge.Project) []uiAction {
	key := projectKey{pr.Instance, pr.PathWithNamespace}
	cloned := func() bool { return a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned }
	notCloned := func() bool { return !cloned() }
	acts := []uiAction{
		{name: "Open in the editor", keys: "Ctrl-O", rank: 10, run: func() { p.onOpen(false) }},
		{name: "Open in an editor you choose", keys: "Alt-O", rank: 15, run: func() { p.onOpen(true) }},
		{name: "Create a worktree for a branch", keys: "Ctrl-W", rank: 20, run: func() { a.showWorktreePicker(pr) }},
		{name: "Pull: bring the clone up to origin", keys: "p", rank: 30, when: cloned, run: func() { a.updateProject(pr) }},
		{name: "Show its merge requests", keys: "m", rank: 40, run: func() {
			a.mrProjectScope = key
			a.mrsPane.reload()
			a.switchTab(pageMRs)
		}},
		{name: "Show the detail", keys: "Enter", rank: 45, run: p.enter},
		{name: "Open in the browser", keys: "w", rank: 50, when: func() bool { return pr.WebURL != "" }, run: func() {
			a.openWeb(pr.WebURL)
		}},
		{name: "Switch the main clone to another branch", keys: "b", rank: 60, run: func() { a.showBranchPicker(pr, nil) }},
		{name: "Switch branch, in an editor you choose", keys: "Alt-B", rank: 65, run: func() {
			a.withEditor(true, func(ed *editors.Editor) { a.showBranchPicker(pr, ed) })
		}},
		{name: "Clone without opening", keys: "C", rank: 70, when: notCloned, run: func() { a.cloneProject(pr) }},
		{name: "Copy the link, path, branch or directory", keys: "y", rank: 80, run: func() { a.yankProject(pr) }},
		{name: "Show what is not committed in Hunk", keys: "D", rank: 90, when: cloned, run: func() {
			if dir, targets, ok := a.projectDiff(pr); ok {
				a.diffKey(dir, targets)
			}
		}},
		{name: "Show changes in Hunk: choose what", keys: "Alt-D", rank: 95, when: cloned, run: func() {
			if dir, targets, ok := a.projectDiff(pr); ok {
				a.diffMenu("Show in Hunk - "+pr.PathWithNamespace, dir, targets)
			}
		}},
		{name: "Star it, or take the star away", keys: "Ctrl-F", rank: 100, run: func() {
			a.toggleFavourite(pr.Instance, pr.PathWithNamespace, 0, pr.PathWithNamespace)
		}},
		{name: "Tag it", keys: "Ctrl-T", rank: 110, run: func() { a.showRepositoryTags(pr.Instance, pr.PathWithNamespace) }},
		{name: "Mark it for a grouped worktree", keys: "space", rank: 120, run: p.toggleMark},
		{name: "Set where it is cloned", keys: "e", rank: 130, when: notCloned, run: func() { a.showProjectDirectory(pr) }},
		a.hideAction(p),
		{name: "Delete from disk: the clone or a worktree", keys: "d", rank: 800, when: func() bool {
			info := a.diskOf(pr.Instance, pr.PathWithNamespace)
			return info.Cloned || len(info.MRs) > 0
		}, run: func() { a.manageWorktrees(pr) }},
	}
	return acts
}

// markedRepositoryActions are what can be done with the marked repositories
// together.
func (a *App) markedRepositoryActions(p *pane, picked []forge.Project) []uiAction {
	return []uiAction{
		{name: "Create one worktree holding them all", keys: "Ctrl-W", rank: 10, run: func() { a.startGroupWorktree(picked) }},
		{name: "Mark or unmark the row under the cursor", keys: "space", rank: 20, run: p.toggleMark},
		{name: "Forget the marks", keys: "Esc", rank: 30, run: p.clearMarks},
	}
}

// repositoriesActions are what Repositories itself can do.
func (a *App) repositoriesActions(p *pane) []uiAction {
	acts := []uiAction{
		{name: "Refresh the list from the servers", keys: "r", rank: 10, run: a.refreshProjects},
		{name: "Create a new repository", rank: 20, run: a.showNewRepository},
		{name: "Pull every clone origin has moved past", keys: "Alt-P", rank: 30, run: a.updateAllClones},
		{name: "View: tags, grouping, favourites first", keys: "v", rank: 410, run: a.showViewOptions},
		{name: "Group the list by group, or not", keys: "Ctrl-G", rank: 420, run: a.toggleRepositoryGrouping},
		{name: "Show only some tags", keys: "f", rank: 430, run: a.showTagFilter},
		{name: "Show every tag again", keys: "F", rank: 440, when: func() bool { return len(a.cfg.Filters.Tags) > 0 }, run: func() {
			if len(a.cfg.Filters.Tags) == 0 {
				return
			}
			a.cfg.Filters.Tags = nil
			a.applyFilters()
			a.note("Showing every tag again")
		}},
	}
	acts = append(acts, a.filterActions()...)
	return append(acts, a.listActions(p)...)
}

// mergeRequestActions are what can be done with one merge request.
func (a *App) mergeRequestActions(p *pane, mr forge.MergeRequest) []uiAction {
	path := a.projectPathOfMR(mr)
	onDisk := func() bool {
		d := a.diskOf(mr.Instance, path).MRs[mr.IID]
		return d.Branch || d.Review
	}
	return []uiAction{
		{name: "Review: the whole change as unstaged edits", keys: "Ctrl-R", rank: 10, run: func() { a.openMRReview(mr, nil) }},
		{name: "Review from a chosen commit", keys: "v", rank: 15, run: func() { a.pickReviewStart(mr, nil) }},
		{name: "Open the branch in the editor", keys: "Ctrl-O", rank: 20, run: func() { p.onOpen(false) }},
		{name: "Read the conversation, write a comment", keys: "c", rank: 25, run: func() { a.showComments(mr) }},
		{name: "Show the detail", keys: "Enter", rank: 30, run: p.enter},
		{name: "Open in the browser", keys: "w", rank: 35, when: func() bool { return mr.WebURL != "" }, run: func() {
			a.openWeb(mr.WebURL)
		}},
		{name: "Approve", keys: "A", rank: 40, run: func() { a.approveMR(mr, nil) }},
		{name: "Publish Incomm comments", keys: "P", rank: 45, run: func() { a.publishMR(mr) }},
		{name: "Review, in an editor you choose", keys: "Alt-R", rank: 50, run: func() {
			a.withEditor(true, func(ed *editors.Editor) { a.openMRReview(mr, ed) })
		}},
		{name: "Review from a commit, in an editor you choose", keys: "Alt-V", rank: 52, run: func() {
			a.withEditor(true, func(ed *editors.Editor) { a.pickReviewStart(mr, ed) })
		}},
		{name: "Open the branch in an editor you choose", keys: "Alt-O", rank: 55, run: func() { p.onOpen(true) }},
		{name: "Pull the branch worktree", keys: "p", rank: 60, run: func() { a.updateMR(mr) }},
		{name: "Make the review worktree without opening it", keys: "C", rank: 65, run: func() { a.cloneMRReview(mr) }},
		{name: "Show the change in Hunk", keys: "D", rank: 70, run: func() { a.diffMR(mr, false) }},
		{name: "Show in Hunk: choose what", keys: "Alt-D", rank: 75, run: func() { a.diffMR(mr, true) }},
		{name: "Copy the link, reference or branch", keys: "y", rank: 80, run: func() { a.yankMR(mr) }},
		{name: "Star it, or take the star away", keys: "Ctrl-F", rank: 100, run: func() {
			a.toggleFavourite(mr.Instance, path, mr.IID, fmt.Sprintf("%s !%d", path, mr.IID))
		}},
		a.hideAction(p),
		{name: "Delete its worktrees from disk", keys: "d", rank: 800, when: onDisk, run: func() { a.confirmDeleteMR(mr) }},
	}
}

// mergeRequestsActions are what Merge requests itself can do.
func (a *App) mergeRequestsActions(p *pane) []uiAction {
	acts := []uiAction{
		{name: "Refresh the list from the servers", keys: "r", rank: 10, run: a.refreshMRs},
		{name: "Show only one repository's", keys: "f", rank: 20, run: a.showProjectScopePicker},
		{name: "Show every repository's again", keys: "F", rank: 25, when: func() bool { return a.mrProjectScope.Path != "" },
			run: func() {
				a.mrProjectScope = projectKey{}
				p.reload()
				a.note("repository filter cleared")
			}},
		{name: "Group the list by repository, or not", keys: "Ctrl-G", rank: 420, run: a.toggleGrouping},
	}
	acts = append(acts, a.filterActions()...)
	return append(acts, a.listActions(p)...)
}

// worktreeActions are what can be done with one worktree, grouped or not;
// the worktree view offers the same for the block that is lit.
// The list commits with c, the view with C, since c there is the comments.
func (a *App) worktreeActions(r worktreeRow, open func(ask bool), commitKey string) []uiAction {
	grouped := func() bool { return r.grouped() }
	single := func() bool { return !r.grouped() }
	acts := []uiAction{
		{name: "Open in the editor", keys: "Ctrl-O", rank: 10, run: func() { open(false) }},
		{name: "Open in an editor you choose", keys: "Alt-O", rank: 15, run: func() { open(true) }},
		{name: "Pull: bring it up to origin", keys: "p", rank: 20, run: func() { a.updateWorktree(r) }},
		{name: "Commit everything", keys: commitKey, rank: 25, run: func() { a.commitWorktree(r) }},
		{name: "Push its commits", keys: "P", rank: 30, run: func() {
			if r.grouped() {
				a.pushGroup(r)
			} else {
				a.pushWorktree(r)
			}
		}},
		{name: "Open a merge request", keys: "n", rank: 35, when: single, run: func() { a.newMergeRequest(r) }},
		{name: "Open a merge request in each, linked", keys: "n", rank: 35, when: grouped, run: func() { a.groupMergeRequests(r) }},
		{name: "Show what is not committed in Hunk", keys: "D", rank: 40, run: func() { a.diffKey(a.worktreeDiff(r)) }},
		{name: "Show changes in Hunk: choose what", keys: "Alt-D", rank: 45, run: func() {
			dir, targets := a.worktreeDiff(r)
			a.diffMenu("Show in Hunk - "+r.Path, dir, targets)
		}},
		{name: "Rebase onto its base", keys: "Ctrl-R", rank: 50, run: func() { a.rebaseWorktree(r) }},
		{name: "Add a repository to the group", keys: "a", rank: 60, when: grouped, run: func() { a.addToGroup(r) }},
		{name: "Copy the directory or branch", keys: "y", rank: 70, run: func() { a.yankWorktree(r) }},
		{name: "Delete the branch on origin", rank: 700, run: func() { a.unpublishBranches(r) }},
	}
	return acts
}

// worktreeListActions are worktreeActions as the list has them: taking a
// repository out and deleting are keys of the list's own.
func (a *App) worktreeListActions(p *pane, r worktreeRow) []uiAction {
	acts := a.worktreeActions(r, p.onOpen, "c")
	return append(acts,
		uiAction{name: "Open its view", keys: "Enter", rank: 5, run: p.enter},
		uiAction{name: "Take a repository out of the group", keys: "x", rank: 65, when: func() bool { return r.grouped() },
			run: func() { a.removeFromGroup(r) }},
		uiAction{name: "Delete from disk", keys: "d", rank: 800, run: func() {
			if r.grouped() {
				a.confirmDeleteGroup(r)
				return
			}
			a.confirmDeleteWorktreeEntry(a.worktreeProject(r), workspace.WorktreeEntry{
				Label: r.Branch, Kind: "branch", Dirs: []string{r.Dir}})
		}},
	)
}

// worktreesActions are what Worktrees itself can do.
func (a *App) worktreesActions(p *pane) []uiAction {
	acts := []uiAction{
		{name: "Refresh: the disk, origin, comments", keys: "r", rank: 10, run: func() {
			a.refreshDisk()
			a.fetchWorktrees()
			a.note("looking at the disk, and asking origin")
		}},
		{name: "Pull every worktree", keys: "Alt-P", rank: 20, run: a.updateAllWorktrees},
	}
	return append(acts, a.listActions(p)...)
}
