package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// confirmDeleteProject asks before removing a main clone and every merge
// request worktree hanging off it.
func (a *App) confirmDeleteProject(pr forge.Project) {
	path := pr.PathWithNamespace
	info := a.diskOf(pr.Instance, path)
	if !info.Cloned && len(info.MRs) == 0 {
		a.flash(path + " is not on disk")
		return
	}
	ws := a.newManager(pr.Instance, path, nil)
	r := ws.InspectProject(path)
	body := fmt.Sprintf("Delete [::b]%s[::-] from disk?\n\n%s", path, r.Dir)
	if n := len(r.MRDirs); n > 0 {
		body += fmt.Sprintf("\n\n…and %d merge request worktree(s) under\n%s", n, ws.MRRoot(path))
	}
	a.confirm("Delete repository", body, r.Warnings, func() {
		a.runTask("Deleting "+path, func(log func(string)) (string, error) {
			return "", a.newManager(pr.Instance, path, log).RemoveProject(path)
		})
	})
}

// confirmDeleteMR asks before removing the worktrees of a merge request.
func (a *App) confirmDeleteMR(mr forge.MergeRequest) {
	path := a.projectPathOfMR(mr)
	disk := a.diskOf(mr.Instance, path).MRs[mr.IID]
	if !disk.Branch && !disk.Review {
		a.flash(fmt.Sprintf("!%d is not on disk", mr.IID))
		return
	}
	ws := a.newManager(mr.Instance, path, nil)
	var dirs, warnings []string
	if disk.Branch {
		dir := a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)
		dirs = append(dirs, "branch   "+dir)
		warnings = append(warnings, ws.InspectDir(dir).Warnings...)
	}
	if disk.Review {
		dir := a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)
		dirs = append(dirs, "review   "+dir)
		warnings = append(warnings, ws.InspectDir(dir).Warnings...)
	}
	body := fmt.Sprintf("Delete the worktree(s) of [::b]%s !%d[::-]?\n\n%s\n\nThe main clone of the project stays.",
		path, mr.IID, strings.Join(dirs, "\n"))
	a.confirm("Delete merge request worktree", body, warnings, func() {
		a.runTask(fmt.Sprintf("Deleting !%d", mr.IID), func(log func(string)) (string, error) {
			return "", a.newManager(mr.Instance, path, log).RemoveMR(path, mr.IID, mr.SourceBranch)
		})
	})
}

// manageWorktrees deletes a project from disk. A bare clone with nothing else
// hanging off it goes straight through the usual confirmation; one with
// merge request or branch worktrees gets a navigable list first, so any one
// of them - or everything at once - can be removed on its own.
func (a *App) manageWorktrees(pr forge.Project) {
	path := pr.PathWithNamespace
	entries := a.newManager(pr.Instance, path, nil).WorktreeEntries(path)
	if len(entries) == 0 {
		a.confirmDeleteProject(pr)
		return
	}

	items := []pickItem{{Label: "[main clone] " + path,
		Sub: "deletes everything below, including every worktree"}}
	for _, e := range entries {
		items = append(items, pickItem{Label: e.Label, Sub: e.Kind, Data: e})
	}
	act := func(it pickItem) {
		if it.Data == nil {
			a.confirmDeleteProject(pr)
			return
		}
		a.confirmDeleteWorktreeEntry(pr, it.Data.(workspace.WorktreeEntry))
	}
	a.showPickerActions("Worktrees - "+path, items, act, nil, act)
}

// confirmDeleteWorktreeEntry asks before removing a single worktree found by
// manageWorktrees, leaving the main clone and every other worktree alone.
func (a *App) confirmDeleteWorktreeEntry(pr forge.Project, e workspace.WorktreeEntry) {
	ws := a.newManager(pr.Instance, pr.PathWithNamespace, nil)
	var warnings []string
	for _, dir := range e.Dirs {
		warnings = append(warnings, ws.InspectDir(dir).Warnings...)
	}
	body := fmt.Sprintf("Delete the %s worktree [::b]%s[::-]?\n\n%s\n\nThe main clone of the project stays.",
		e.Kind, e.Label, strings.Join(e.Dirs, "\n"))
	a.confirm("Delete worktree", body, warnings, func() {
		a.runTask("Deleting "+e.Label, func(log func(string)) (string, error) {
			ws := a.newManager(pr.Instance, pr.PathWithNamespace, log)
			for _, dir := range e.Dirs {
				if err := ws.RemoveWorktreeDir(pr.PathWithNamespace, dir); err != nil {
					return "", err
				}
			}
			return "", nil
		})
	})
}

// showWorktreePicker lists the project's branches - local and remote - and
// opens the chosen one in its own worktree; 'n' offers a brand new branch
// instead.
func (a *App) showWorktreePicker(pr forge.Project) {
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
		return
	}
	a.runTask("Loading branches of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		branches, err := client.ProjectBranches(ctx, pr)
		if err != nil {
			return "", err
		}
		log(fmt.Sprintf("%d branch(es)", len(branches)))

		known := make(map[string]bool, len(branches))
		items := make([]pickItem, 0, len(branches))
		for _, b := range branches {
			known[b.Name] = true
			sub := strings.TrimSpace(humanAge(b.CommittedDate) + "  " + b.CommitTitle)
			if b.CommittedDate.IsZero() {
				sub = b.CommitShortID
			}
			if b.Default {
				sub = "default  " + sub
			}
			items = append(items, pickItem{Label: b.Name, Sub: sub, Data: b.Name})
		}
		// A branch that only exists locally - never pushed - would otherwise
		// be invisible here, even though it can still be given its own
		// worktree.
		if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
			mgr := a.pathManager(pr.Instance, pr.PathWithNamespace)
			for _, name := range mgr.Git().LocalBranches(mgr.ProjectDir(pr.PathWithNamespace)) {
				if known[name] {
					continue
				}
				items = append(items, pickItem{Label: name, Sub: "local only", Data: name})
			}
		}

		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			onSelect := func(it pickItem) { a.createWorktree(pr, it.Data.(string), false) }
			onNew := func() { a.promptNewWorktreeBranch(pr) }
			a.showPickerActions("Worktree branch - "+pr.PathWithNamespace, items, onSelect, onNew, nil)
		})
		return "", nil
	})
}

// promptNewWorktreeBranch asks for the name of a brand new branch, created
// from the main clone's current HEAD, and opens it in its own worktree.
func (a *App) promptNewWorktreeBranch(pr forge.Project) {
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Branch name", "", 40, nil, nil)
	form.AddTextView("", "Created from the current HEAD of the main checkout.", 40, 2, true, false)
	apply := func() {
		name := strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		if name == "" {
			a.flash("enter a branch name")
			return
		}
		a.closeModal(pageForm)
		a.createWorktree(pr, name, true)
	}
	form.AddButton("Create", apply)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("New worktree branch - "+pr.PathWithNamespace, form, 10)
}

// createWorktree materialises a plain branch worktree and opens the editor
// there, the same way openMR does for a merge request's.
func (a *App) createWorktree(pr forge.Project, branch string, isNew bool) {
	a.runTaskOpening(fmt.Sprintf("Opening %s (%s)", pr.PathWithNamespace, branch),
		session.Record{
			Instance: pr.Instance,
			Server:   a.instanceLabel(pr.Instance),
			Project:  pr.PathWithNamespace,
			Title:    branch,
			Mode:     session.ModeBranch,
		}, func(log func(string)) (string, error) {
			return a.newManager(pr.Instance, pr.PathWithNamespace, log).EnsureWorktree(pr, branch, isNew)
		})
}

// showBranchPicker lists the project's branches and switches the main clone to
// the chosen one before opening the editor.
func (a *App) showBranchPicker(pr forge.Project) {
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
		return
	}
	a.runTask("Loading branches of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		branches, err := client.ProjectBranches(ctx, pr)
		if err != nil {
			return "", err
		}
		log(fmt.Sprintf("%d branch(es)", len(branches)))
		items := make([]pickItem, 0, len(branches))
		for _, b := range branches {
			sub := strings.TrimSpace(humanAge(b.CommittedDate) + "  " + b.CommitTitle)
			if b.CommittedDate.IsZero() {
				sub = b.CommitShortID
			}
			if b.Default {
				sub = "default  " + sub
			}
			items = append(items, pickItem{Label: b.Name, Sub: sub, Data: b.Name})
		}
		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			a.showPicker("Branch - "+pr.PathWithNamespace, items, func(it pickItem) {
				branch := it.Data.(string)
				a.runTaskOpening(fmt.Sprintf("Switching %s to %s", pr.PathWithNamespace, branch),
					session.Record{
						Instance: pr.Instance,
						Server:   a.instanceLabel(pr.Instance),
						Project:  pr.PathWithNamespace,
						Title:    branch,
						Mode:     session.ModeRepository,
					}, func(log func(string)) (string, error) {
						return a.newManager(pr.Instance, pr.PathWithNamespace, log).SwitchBranch(pr, branch)
					})
			})
		})
		return "", nil
	})
}

// showProjectScopePicker limits the merge request list to a single project.
func (a *App) showProjectScopePicker() {
	counts := map[projectKey]int{}
	for _, mr := range a.mrs {
		counts[projectKey{mr.Instance, a.projectPathOfMR(mr)}]++
	}
	keys := make([]projectKey, 0, len(counts))
	for key := range counts {
		if key.Path != "" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Instance != keys[j].Instance {
			return keys[i].Instance < keys[j].Instance
		}
		return keys[i].Path < keys[j].Path
	})
	items := []pickItem{{Label: "(all repositories)", Sub: fmt.Sprintf("%d merge requests", len(a.mrs)), Data: projectKey{}}}
	for _, key := range keys {
		sub := fmt.Sprintf("%d open", counts[key])
		if a.multiInstance() {
			sub = a.instanceLabel(key.Instance) + " · " + sub
		}
		items = append(items, pickItem{Label: key.Path, Sub: sub, Data: key})
	}
	a.showPicker("Limit merge requests to a repository", items, func(it pickItem) {
		a.mrProjectScope = it.Data.(projectKey)
		a.mrsPane.reload()
		a.setStatus("")
	})
}

// confirmSwitchRemotes offers to repoint the clones already on disk after the
// protocol of a server changed. New clones follow the setting on their own;
// these would otherwise keep talking over the old one.
func (a *App) confirmSwitchRemotes(inst config.Instance, was string) {
	var cloned []forge.Project
	for _, p := range a.projects {
		if p.Instance == inst.ID && a.diskOf(p.Instance, p.PathWithNamespace).Cloned {
			cloned = append(cloned, p)
		}
	}
	if len(cloned) == 0 {
		a.note(fmt.Sprintf("%s will be cloned over %s from now on", inst.Label(), inst.Protocol()))
		return
	}
	body := fmt.Sprintf("%s now clones over [::b]%s[::-] instead of %s.\n\n"+
		"Repoint the %d repositor%s already on disk?\n\n"+
		"Their worktrees follow along; nothing else is touched.",
		inst.Label(), inst.Protocol(), was, len(cloned), plural(len(cloned), "y", "ies"))

	a.confirmWith("Switch remotes", body, "Switch", nil, func() {
		a.runTask("Switching remotes of "+inst.Label(), func(log func(string)) (string, error) {
			switched := 0
			for _, p := range cloned {
				url, err := a.newManager(p.Instance, p.PathWithNamespace, nil).SetRemote(p)
				if err != nil {
					log(fmt.Sprintf("! %s: %v", p.PathWithNamespace, err))
					continue
				}
				if url == "" {
					continue
				}
				log(fmt.Sprintf("%s → %s", p.PathWithNamespace, url))
				switched++
			}
			log(fmt.Sprintf("Done: %d repositor%s switched.", switched, plural(switched, "y", "ies")))
			return "", nil
		})
	})
}

// plural picks the ending that fits the count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
