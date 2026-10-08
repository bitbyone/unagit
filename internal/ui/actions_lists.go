package ui

import (
	"fmt"
	"strings"

	"github.com/tobola/unagit/internal/config"
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
		{name: "Filter", about: "Narrow the list by typing; the letters need not be next to each other.", keys: "/", rank: 300, run: p.startFilter},
		{name: "Go to Repositories", about: "Every repository of the servers and groups you picked.", keys: "1", rank: 900, when: func() bool { return a.currentTab() != pageProjects },
			run: func() { a.switchTab(pageProjects) }},
		{name: "Go to Merge Requests", about: "The open merge requests of those repositories.", keys: "2", rank: 900, when: func() bool { return a.currentTab() != pageMRs },
			run: func() { a.switchTab(pageMRs) }},
		{name: "Go to Worktrees", about: "Every worktree on disk, plain and grouped.", keys: "3", rank: 900, when: func() bool { return a.currentTab() != pageWorktrees },
			run: func() { a.switchTab(pageWorktrees) }},
		{name: "Go to Agents", about: "Every coding agent at work, and what each is doing.", keys: "4", rank: 905, when: func() bool { return a.currentTab() != pageAgents },
			run: func() { a.switchTab(pageAgents) }},
		{name: "Go to Settings", about: "Servers, groups, tags, integrations and security.", keys: "5", rank: 910, run: func() { a.switchTab(pageSettings) }},
		{name: "Help", about: "Every key of every screen, the ones that work here lit.", keys: "?", rank: 950, run: a.showHelp},
		{name: "Quit", about: "Leave unagit. Window editors it opened stay open.", keys: "q", rank: 999, run: a.tv.Stop},
	}
}

// openWeb opens an address in the browser, when there is one.
func (a *App) openWeb(url string) {
	if url == "" {
		a.flash("no address known for it")
		return
	}
	_ = openBrowser(url)
	a.done("opened " + url)
}

// filterActions are the filters Repositories and Merge requests share.
func (a *App) filterActions() []uiAction {
	return []uiAction{
		a.sortAction(),
		{name: "Toggle Cloned Only", about: "Show only the repositories on disk, or every one again.", keys: "L", rank: 410, run: a.toggleClonedOnly},
	}
}

// sortAction chooses the order of the list on screen.
func (a *App) sortAction() uiAction {
	return uiAction{name: "Sort By…", about: "Choose an order for this list, including directory visits in Repositories and Worktrees.", keys: "o", rank: 400, run: a.showSortPicker}
}

// hideAction hides the repository of the row, or shows it again.
func (a *App) hideAction(p *pane) uiAction {
	return uiAction{name: "Hide or Unhide", about: "Take this repository out of the lists, or bring it back; X lists what is hidden.", keys: "x", rank: 470, run: func() {
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
		a.browseFilesAction(func() { a.browseProject(pr) }),
		{name: "Open", about: "Open the clone in your favourite editor, here, cloning it first when it is not on disk.", keys: "Ctrl-O", rank: 10, run: func() { p.onOpen(false) }},
		a.openAction("repository", pr.PathWithNamespace, openTarget{"Open", func(ed *editors.Editor, place editorPlace) { a.openProjectIn(pr, ed, place) }}),
		{name: "Open With…", about: "Choose the editor, then open the clone here.", keys: "Alt-O", rank: 15, run: func() { p.onOpen(true) }},
		{name: "New Worktree…", about: "Check a branch out in a directory of its own beside the clone, an existing branch or a new one.", keys: "Ctrl-W", rank: 20, run: func() { a.showWorktreePicker(pr) }},
		{name: "Pull", about: "Fetch origin and fast-forward the clone's branch when nothing local is in the way.", keys: "p", rank: 30, when: cloned, run: func() { a.updateProject(pr) }},
		{name: "Show Merge Requests", about: "Switch to Merge requests, narrowed to this repository.", keys: "m", rank: 40, run: func() {
			a.mrProjectScope = key
			a.mrsPane.reload()
			a.switchTab(pageMRs)
		}},
		{name: "Show Details", about: "Open the column on the right: what is on disk, the latest commits, the pipeline.", keys: "Enter", rank: 45, run: p.enter},
		{name: "Open in Browser", about: "Open the repository's page on the server.", keys: "w", rank: 50, when: func() bool { return pr.WebURL != "" }, run: func() {
			a.openWeb(pr.WebURL)
		}},
		{name: "Branches…", about: "Switch the clone to another branch, see where each stands against origin, delete those you are done with.", keys: "b", rank: 60, run: func() {
			a.showBranchManager(branchScope{project: pr, checkout: true})
		}},
		{name: "Refresh", about: "Fetch this clone from origin, read its branch's pipeline and see where it stands, leaving the rest of the list as it is.", keys: "r", rank: 57, run: func() { a.refreshProjectRow(pr) }},
		{name: mrsHiddenName(a.cfg.Filters.HidesMRsOf(pr.Instance, pr.PathWithNamespace)), about: "Keep the repository's merge requests out of the list and out of refreshes, the repository still here; again lists them.", keys: "H", rank: 470,
			run: func() { a.toggleMRsOf(pr.Instance, pr.PathWithNamespace) }},
		{name: "Show Commit Log", about: "What is out in the clone, newest first, or the server's default branch before it is cloned: diff, check out, branch from a commit.", keys: "Ctrl-L", rank: 58, run: func() { a.repositoryLog(pr) }},
		{name: "Show Pipeline…", about: "The jobs of the newest pipeline of the clone's branch, or of the default branch before it is cloned: read a log, retry, open it.", keys: "J", rank: 62, run: func() {
			branch := pr.DefaultBranch
			if d := a.diskOf(pr.Instance, pr.PathWithNamespace); d.Cloned {
				branch = d.Branch
			}
			a.showBranchPipeline(pr.Instance, pr.PathWithNamespace, branch)
		}},
		{name: "Back to Branch", about: "Leave the commit checked out from the log and check out again the branch it came from.", keys: "B", rank: 59,
			when: func() bool { return strings.HasPrefix(a.diskOf(pr.Instance, pr.PathWithNamespace).Branch, "@") },
			run:  func() { a.backToBranch(pr, a.projectDir(pr.Instance, pr.PathWithNamespace)) }},
		{name: "Clone", about: "Clone it under its root without starting an editor.", keys: "C", rank: 70, when: notCloned, run: func() { a.cloneProject(pr) }},
		{name: "Copy…", about: "Copy the web link, the path, the branch or the directory to the clipboard.", keys: "y", rank: 80, run: func() { a.yankProject(pr) }},
		{name: "Show Uncommitted Changes", about: "Show in Hunk what is not committed: staged, unstaged and new files.", keys: "D", rank: 90, when: cloned, run: func() {
			if dir, targets, ok := a.projectDiff(pr); ok {
				a.diffKey(dir, targets)
			}
		}},
		{name: "Show Changes Since Base", about: "The branch's commits since its base and what is not committed, in Hunk; one commit is the commit log's.", keys: "Alt-D", rank: 95, when: cloned, run: func() {
			if dir, targets, ok := a.projectDiff(pr); ok {
				a.diffSince(dir, targets)
			}
		}},
		{name: "Toggle Favourite", about: "Star it, so it can be kept at the top; again takes the star away.", keys: "Ctrl-F", rank: 100, run: func() {
			a.toggleFavourite(pr.Instance, pr.PathWithNamespace, 0, pr.PathWithNamespace)
		}},
		{name: "Edit Tags…", about: "Put your tags on this repository or take them off.", keys: "Ctrl-T", rank: 110, run: func() { a.showRepositoryTags(pr.Instance, pr.PathWithNamespace) }},
		{name: "Toggle Mark", about: "Mark repositories to put them together in one grouped worktree with Ctrl-W.", keys: "space", rank: 120, run: p.toggleMark},
		{name: "Set Clone Directory…", about: "Choose the exact directory this repository is cloned into, instead of its group's root.", keys: "e", rank: 130, when: notCloned, run: func() { a.showProjectDirectory(pr) }},
		a.hideAction(p),
		{name: "Delete from Disk…", about: "Delete the clone or one of its worktrees from disk; asks first and lists what would be lost.", keys: "d", rank: 800, when: func() bool {
			info := a.diskOf(pr.Instance, pr.PathWithNamespace)
			return info.Cloned || len(info.MRs) > 0
		}, run: func() { a.manageWorktrees(pr) }},
	}
	return append(acts, a.openingActions(openTarget{"Open", func(ed *editors.Editor, place editorPlace) { a.openProjectIn(pr, ed, place) }})...)
}

// repositoriesActions are what Repositories itself can do.
func (a *App) repositoriesActions(p *pane) []uiAction {
	acts := []uiAction{
		a.runningEditorsAction("E"),
		a.runningAgentsAction("Alt-A"),
		{name: "Refresh All", about: "Ask the servers for the repositories again, and the pipelines of the clones' branches; the list is a cache until then.", keys: "R", rank: 10, run: a.refreshProjects},
		{name: "New Repository…", about: "Create a repository on a server and clone it.", rank: 20, run: a.showNewRepository},
		{name: "Measure Sizes Again", about: "Measure what every clone takes on disk, its worktrees counted; otherwise only a refresh measures them again.", rank: 40,
			when: func() bool { return a.wantsSizes(config.ListRepositories) }, run: func() {
				if !a.wantsSizes(config.ListRepositories) {
					a.flash("the SIZE column is hidden - show it in View Options (v)")
					return
				}
				a.loadRepoSizes(true)
			}},
		{name: "Pull All Clones", about: "Fetch every clone and fast-forward those origin has moved past.", keys: "Alt-P", rank: 30, run: a.updateAllClones},
		{name: "View Starred Repositories…", about: "The repositories you starred on GitHub: read a README, open one in the browser, or clone it.", rank: 35,
			when: a.hasGitHub, run: a.showStarred},
		{name: "Pull All Favourites", about: "Fetch the starred clones and fast-forward those origin has moved past.", rank: 31, run: a.updateFavouriteClones},
		{name: "View Options…", about: "What the list shows: grouping, favourites first, cloned only, and which columns.", keys: "v", rank: 410, run: a.showViewOptions},
		{name: "Toggle Grouping", about: "Group the repositories under their groups, or list them flat.", keys: "Ctrl-G", rank: 420, run: a.toggleRepositoryGrouping},
		{name: "Filter by Tags…", about: "Show only the repositories wearing the tags you choose.", keys: "f", rank: 430, run: a.showTagFilter},
		{name: "Clear Tag Filter", about: "Show the repositories of every tag again.", keys: "F", rank: 440, when: func() bool { return len(a.cfg.Filters.Tags) > 0 }, run: func() {
			if len(a.cfg.Filters.Tags) == 0 {
				return
			}
			a.cfg.Filters.Tags = nil
			a.applyFilters()
			a.note("Showing every tag again")
		}},
	}
	acts = append(acts, a.filterActions()...)
	// Which repositories are hidden is Repositories' to say; Merge requests
	// hides only their merge requests (x there, View Options).
	acts = append(acts, uiAction{name: "Hidden Repositories…", about: "See what x hid from the lists and bring any of it back.", keys: "X", rank: 480, run: a.showHiddenPicker})
	return append(acts, a.listActions(p)...)
}

// mergeRequestActions are what can be done with one merge request.
func (a *App) mergeRequestActions(p *pane, mr forge.MergeRequest) []uiAction {
	path := a.projectPathOfMR(mr)
	draftName, draftAbout := "Mark as Draft", "Mark it as a draft, not ready to merge; the same key makes it ready again."
	if mr.Draft {
		draftName, draftAbout = "Mark as Ready", "Take the draft mark off, so it can be reviewed and merged."
	}
	onDisk := func() bool {
		d := a.diskOf(mr.Instance, path).MRs[mr.IID]
		return d.Branch || d.Review
	}
	acts := []uiAction{
		a.browseFilesAction(func() { a.browseMR(mr) }),
		{name: "Review", about: "Open a review worktree: the whole change as unstaged edits on the merge base, so the editor's gutter shows it.", keys: "Ctrl-R", rank: 10, run: func() { a.openMRReview(mr, nil) }},
		{name: "Open", about: "Open a worktree of the source branch in your favourite editor, here, for committing to it.", keys: "Ctrl-O", rank: 11, run: func() { p.onOpen(false) }},
		a.openAction("merge_request", fmt.Sprintf("%s !%d", path, mr.IID),
			openTarget{"Open", func(ed *editors.Editor, place editorPlace) { a.openMRIn(mr, ed, place) }},
			openTarget{"Review", func(ed *editors.Editor, place editorPlace) { a.openMRReviewIn(mr, ed, place) }}),
		{name: "Show Conversation", about: "Read the merge request's threads and write a comment.", keys: "c", rank: 25, run: func() { a.showComments(mr) }},
		{name: "Show Details", about: "Open the column on the right: description, pipeline, approvals, changes.", keys: "Enter", rank: 30, run: p.enter},
		{name: "Open in Browser", about: "Open the merge request's page on the server.", keys: "w", rank: 35, when: func() bool { return mr.WebURL != "" }, run: func() {
			a.openWeb(mr.WebURL)
		}},
		{name: "Show Commit Log", about: "The merge request's commits, those new since your last review marked: diff one, or review from it.", keys: "Ctrl-L", rank: 15, run: func() { a.mergeRequestLog(mr) }},
		{name: "Refresh", about: "Ask the server about this merge request alone: its state, head, pipeline, approvals and threads.", keys: "r", rank: 16, run: func() { a.refreshMRRow(mr) }},
		{name: "Mark as Reviewed", about: "Take the head as seen without opening the review - read in the browser, or in Hunk - so NEW counts only what is pushed after.", keys: "V", rank: 17, run: func() { a.markReviewed(mr) }},
		{name: "Show Pipeline…", about: "The jobs of the head's pipeline, the first that failed under the cursor: read its log, retry it, open it.", keys: "J", rank: 37, run: func() { a.showMRPipeline(mr, 0) }},
		{name: "Approve…", about: "Approve the merge request on the server; asks first.", keys: "A", rank: 40, run: func() { a.approveMR(mr, nil) }},
		{name: "Merge…", about: "Merge it on the server - now, or once its pipeline succeeds - after saying what stands in the way.", keys: "M", rank: 41, run: func() { a.mergeMR(mr) }},
		{name: draftName, about: draftAbout, keys: "Ctrl-D", rank: 42, run: func() { a.toggleDraft(mr) }},
		{name: "Reviewers…", about: "Choose who is asked to review: space asks or withdraws, Esc saves.", keys: "a", rank: 43, run: func() { a.editReviewers(mr) }},
		{name: "Publish Comments", about: "Post the comments you wrote in Incomm to the merge request.", keys: "P", rank: 45, run: func() { a.publishMR(mr) }},
		{name: "Review With…", about: "Choose the editor, then open the review here.", keys: "Alt-R", rank: 50, run: func() {
			a.withEditor(true, func(ed *editors.Editor) { a.openMRReview(mr, ed) })
		}},
		{name: "Open With…", about: "Choose the editor, then open the worktree of the source branch here.", keys: "Alt-O", rank: 55, run: func() { p.onOpen(true) }},
		{name: "Pull Branch", about: "Fetch and fast-forward the branch worktree to the source branch.", keys: "p", rank: 60, run: func() { a.updateMR(mr) }},
		{name: "Prepare Review", about: "Make the review worktree without starting an editor.", keys: "C", rank: 65, run: func() { a.cloneMRReview(mr) }},
		{name: "Show Changes", about: "Show the whole merge request in Hunk, making its review first when nothing is on disk; one commit is the commit log's.", keys: "D", rank: 70, run: func() { a.diffMR(mr) }},
		{name: "Copy…", about: "Copy the web link, the !reference or the source branch to the clipboard.", keys: "y", rank: 80, run: func() { a.yankMR(mr) }},
		{name: "Toggle Favourite", about: "Star it, so it can be kept at the top; again takes the star away.", keys: "Ctrl-F", rank: 100, run: func() {
			a.toggleFavourite(mr.Instance, path, mr.IID, fmt.Sprintf("%s !%d", path, mr.IID))
		}},
		{name: "Hide Author", about: "Keep this author's merge requests out of the list - a bot's, most often; View Options shows them again.", keys: "H", rank: 475,
			when: func() bool { return mr.Author.Username != "" }, run: func() { a.hideAuthor(mr) }},
		// Hiding a whole repository belongs to Repositories; here x only
		// keeps its merge requests out of this list.
		{name: "Hide Repository's Merge Requests", about: "Keep this repository's merge requests out of the list and out of refreshes, the repository still listed in Repositories; View Options shows them again.", keys: "x", rank: 470,
			run: func() { a.toggleMRsOf(mr.Instance, path) }},
		{name: "Close Merge Request…", about: "Close it without merging; asks first. Its branch stays.", keys: "", rank: 790, run: func() { a.closeMR(mr) }},
		{name: "Delete Worktrees…", about: "Delete its branch and review worktrees; asks first and lists what would be lost.", keys: "d", rank: 800, when: onDisk, run: func() { a.confirmDeleteMR(mr) }},
	}
	return append(acts, a.openingActions(
		openTarget{"Open", func(ed *editors.Editor, place editorPlace) { a.openMRIn(mr, ed, place) }},
		openTarget{"Review", func(ed *editors.Editor, place editorPlace) { a.openMRReviewIn(mr, ed, place) }})...)
}

// mergeRequestsActions are what Merge requests itself can do.
func (a *App) mergeRequestsActions(p *pane) []uiAction {
	acts := []uiAction{
		a.runningEditorsAction("E"),
		a.runningAgentsAction("Alt-A"),
		{name: "Refresh All", about: "Ask the servers for the open merge requests again, with their pipelines, approvals and threads.", keys: "R", rank: 10, run: a.refreshMRs},
		{name: "Filter by Repository…", about: "Show only the merge requests of one repository.", keys: "f", rank: 20, run: a.showProjectScopePicker},
		{name: "Clear Repository Filter", about: "Show the merge requests of every repository again.", keys: "F", rank: 25, when: func() bool { return a.mrProjectScope.Path != "" },
			run: func() {
				a.mrProjectScope = projectKey{}
				p.reload()
				a.note("repository filter cleared")
			}},
		{name: "View Options…", about: "What the list shows: grouping, favourites first, cloned only, the authors kept out of it, and which columns.", keys: "v", rank: 410, run: a.showMRViewOptions},
		{name: "Toggle Grouping", about: "Group the merge requests under their repositories, or list them flat.", keys: "Ctrl-G", rank: 420, run: a.toggleGrouping},
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
		a.browseFilesAction(func() { a.browseWorktree(r) }),
		{name: "Open", about: "Open the worktree in your favourite editor, here.", keys: "Ctrl-O", rank: 10, run: func() { open(false) }},
		{name: "Open With…", about: "Choose the editor, then open the worktree here.", keys: "Alt-O", rank: 15, run: func() { open(true) }},
		{name: "Back to Branch", about: "Leave the commit checked out from the log and check out again the branch it came from.", keys: "B", rank: 18,
			when: func() bool { return r.Branch == "(detached)" }, run: func() { a.backToBranch(a.worktreeProject(r), r.Dir) }},
		{name: "Pull", about: "Bring the branch up to origin; a branch not yet pushed is rebased onto its base.", keys: "p", rank: 20, run: func() { a.updateWorktree(r) }},
		{name: "Commit All…", about: "Commit every change in the worktree, with a message you write.", keys: commitKey, rank: 25, run: func() { a.commitWorktree(r) }},
		{name: "Push", about: "Push the branch to origin, setting up its upstream the first time.", keys: "P", rank: 30, run: func() {
			if r.grouped() {
				a.pushGroup(r)
			} else {
				a.pushWorktree(r)
			}
		}},
		{name: "New Merge Request…", about: "Open a merge request from this branch on the server.", keys: "n", rank: 35, when: single, run: func() { a.newMergeRequest(r) }},
		{name: "New Linked Merge Requests…", about: "Open a merge request in each repository of the group, each linking the others.", keys: "n", rank: 35, when: grouped, run: func() { a.groupMergeRequests(r) }},
		{name: "Show Uncommitted Changes", about: "Show in Hunk what is not committed: staged, unstaged and new files.", keys: "D", rank: 40, run: func() { a.diffKey(a.worktreeDiff(r)) }},
		{name: "Show Changes Since Base", about: "The branch's commits since its base and what is not committed, in Hunk; one commit is the commit log's.", keys: "Alt-D", rank: 45, run: func() {
			dir, targets := a.worktreeDiff(r)
			a.diffSince(dir, targets)
		}},
		{name: "Rebase onto Base", about: "Rebase the branch onto its base even once pushed; P then force-pushes with a lease.", keys: "Ctrl-R", rank: 50, run: func() { a.rebaseWorktree(r) }},
		{name: "Show Pipeline…", about: "The jobs of the branch's newest pipeline, as CI shows it; in a group, of the repository you pick.", keys: "J", rank: 52, run: func() { a.worktreePipeline(r) }},
		{name: "Go to Merge Request", about: "Switch to Merge requests with the cursor on the one open from this branch; in a group, the one you pick.", keys: "m", rank: 36,
			when: func() bool { return len(a.worktreeMRs(r)) > 0 }, run: func() { a.goToWorktreeMR(r) }},
		{name: "Add Repository…", about: "Add a worktree of another repository to this group, on the group's branch.", keys: "a", rank: 60, when: grouped, run: func() { a.addToGroup(r) }},
		{name: "Copy…", about: "Copy the directory or the branch to the clipboard.", keys: "y", rank: 70, run: func() { a.yankWorktree(r) }},
		{name: "Branches…", about: "See where the repository's branches stand against origin and delete those you are done with.", keys: "b", rank: 72, when: single, run: func() {
			if r.grouped() {
				a.flash("branches are a repository's - light its block, or open the worktree with Enter")
				return
			}
			a.showBranchManager(branchScope{project: a.worktreeProject(r), focus: r.Branch})
		}},
	}
	openIn := func(ed *editors.Editor, place editorPlace) {
		if r.grouped() {
			a.openGroupIn(r, ed, place)
		} else {
			a.openWorktreeIn(r, ed, place)
		}
	}
	acts = append(acts, a.openAction("worktree", r.Path, openTarget{"Open", openIn}))
	return append(acts, a.openingActions(openTarget{"Open", openIn})...)
}

// worktreeListActions are worktreeActions as the list has them: taking a
// repository out and deleting are keys of the list's own.
func (a *App) worktreeListActions(p *pane, r worktreeRow) []uiAction {
	acts := a.worktreeActions(r, p.onOpen, "c")
	return append(acts,
		uiAction{name: "Refresh", about: "Fetch this worktree's repository, read its branch's pipeline and bring in its merge request's comments, leaving the rest as it is.", keys: "r", rank: 38,
			run: func() { a.refreshWorktreeRow(r) }},
		uiAction{name: "Show Commit Log", about: "The worktree's history, newest first: diff, check out, branch from a commit.", keys: "Ctrl-L", rank: 39, when: func() bool { return !r.grouped() },
			run: func() { a.worktreeLog(r) }},
		uiAction{name: "Open View", about: "Open the worktree's view: its state, commits and merge request, block by block.", keys: "Enter", rank: 5, run: p.enter},
		uiAction{name: "Remove from Group…", about: "Remove one repository's worktree from the group; its branch stays.", keys: "x", rank: 65, when: func() bool { return r.grouped() },
			run: func() { a.removeFromGroup(r) }},
		uiAction{name: "Delete from Disk…", about: "Delete the worktree from disk; asks first and lists what would be lost.", keys: "d", rank: 800, run: func() {
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
		a.runningEditorsAction("E"),
		a.runningAgentsAction("Alt-A"),
		{name: "Refresh All", about: "Look at the disk again - what each worktree takes measured anew - fetch origin for every worktree, read the pipelines of their branches and bring in new comments.", keys: "R", rank: 10, run: func() {
			a.refreshDisk()
			a.loadWorktreeSizes(true)
			a.loadRepoSizes(true)
			a.fetchWorktrees()
			a.askBranchCI(a.worktreeCITargets(), "reading pipelines")
		}},
		{name: "Pull All Worktrees", about: "Bring every worktree up to origin.", keys: "Alt-P", rank: 20, run: a.updateAllWorktrees},
		{name: "View Options…", about: "Which columns the list shows.", keys: "v", rank: 410, run: a.showWorktreeViewOptions},
		a.sortAction(),
	}
	return append(acts, a.listActions(p)...)
}
