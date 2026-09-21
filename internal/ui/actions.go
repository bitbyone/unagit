package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/gitlab"
)

// confirmDeleteProject asks before removing a main clone and every merge
// request worktree hanging off it.
func (a *App) confirmDeleteProject(pr gitlab.Project) {
	path := pr.PathWithNamespace
	info := a.disk[path]
	if !info.Cloned && len(info.MRs) == 0 {
		a.flash(path + " is not on disk")
		return
	}
	r := a.ws.InspectProject(path)
	body := fmt.Sprintf("Delete [::b]%s[::-] from disk?\n\n%s", path, r.Dir)
	if n := len(r.MRDirs); n > 0 {
		body += fmt.Sprintf("\n\n…and %d merge request worktree(s) under\n%s", n, a.ws.MRRoot(path))
	}
	a.confirm("Delete project", body, r.Warnings, func() {
		a.runTask("Deleting "+path, func(log func(string)) (string, error) {
			return "", a.newManager(log).RemoveProject(path)
		})
	})
}

// confirmDeleteMR asks before removing the worktrees of a merge request.
func (a *App) confirmDeleteMR(mr gitlab.MergeRequest) {
	path := a.projectPathOfMR(mr)
	disk := a.disk[path].MRs[mr.IID]
	if !disk.Branch && !disk.Review {
		a.flash(fmt.Sprintf("!%d is not on disk", mr.IID))
		return
	}
	var dirs []string
	var warnings []string
	if disk.Branch {
		dir := a.ws.MRDir(path, mr.IID, mr.SourceBranch)
		dirs = append(dirs, "branch   "+dir)
		warnings = append(warnings, a.ws.InspectDir(dir).Warnings...)
	}
	if disk.Review {
		dir := a.ws.ReviewDir(path, mr.IID, mr.SourceBranch)
		dirs = append(dirs, "review   "+dir)
		warnings = append(warnings, a.ws.InspectDir(dir).Warnings...)
	}
	body := fmt.Sprintf("Delete the worktree(s) of [::b]%s !%d[::-]?\n\n%s\n\nThe main clone of the project stays.",
		path, mr.IID, strings.Join(dirs, "\n"))
	a.confirm("Delete merge request worktree", body, warnings, func() {
		a.runTask(fmt.Sprintf("Deleting !%d", mr.IID), func(log func(string)) (string, error) {
			return "", a.newManager(log).RemoveMR(path, mr.IID, mr.SourceBranch)
		})
	})
}

// showBranchPicker lists the project's branches and switches the main clone to
// the chosen one before opening the editor.
func (a *App) showBranchPicker(pr gitlab.Project) {
	a.runTask("Loading branches of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		branches, err := a.client.ProjectBranches(ctx, pr.ID)
		if err != nil {
			return "", err
		}
		log(fmt.Sprintf("%d branch(es)", len(branches)))
		items := make([]pickItem, 0, len(branches))
		for _, b := range branches {
			sub := humanAge(b.Commit.CommittedDate) + "  " + b.Commit.Title
			if b.Default {
				sub = "default  " + sub
			}
			items = append(items, pickItem{Label: b.Name, Sub: sub, Data: b.Name})
		}
		a.tv.QueueUpdateDraw(func() {
			a.pages.RemovePage(pageTask)
			a.showPicker("Branch - "+pr.PathWithNamespace, items, func(it pickItem) {
				branch := it.Data.(string)
				a.runTask(fmt.Sprintf("Switching %s to %s", pr.PathWithNamespace, branch), func(log func(string)) (string, error) {
					return a.newManager(log).SwitchBranch(pr, branch)
				})
			})
		})
		return "", nil
	})
}

// showProjectScopePicker limits the merge request list to a single project.
func (a *App) showProjectScopePicker() {
	counts := map[string]int{}
	for _, mr := range a.mrs {
		counts[a.projectPathOfMR(mr)]++
	}
	paths := make([]string, 0, len(counts))
	for path := range counts {
		if path != "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	items := []pickItem{{Label: "(all projects)", Sub: fmt.Sprintf("%d merge requests", len(a.mrs)), Data: ""}}
	for _, path := range paths {
		items = append(items, pickItem{
			Label: path,
			Sub:   fmt.Sprintf("%d open", counts[path]),
			Data:  path,
		})
	}
	a.showPicker("Limit merge requests to project", items, func(it pickItem) {
		a.mrProjectScope = it.Data.(string)
		a.mrsPane.reload()
		a.setStatus("")
	})
}
