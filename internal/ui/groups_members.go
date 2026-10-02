package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

// A grouped worktree grows and shrinks one repository at a time: a takes one
// from the list of repositories into it, x lets one go. The rest of the group
// is not touched.

// addToGroup lists the repositories not in the group yet, filtered as you type,
// and goes on with the one picked.
func (a *App) addToGroup(r worktreeRow) {
	if !r.grouped() {
		a.flash("a adds a repository to a grouped worktree; Ctrl-W in Repositories makes one")
		return
	}
	in := map[projectKey]bool{}
	for _, m := range r.Members {
		in[projectKey{m.Instance, m.Path}] = true
	}
	var items []pickItem
	for _, pr := range a.projects {
		if in[projectKey{pr.Instance, pr.PathWithNamespace}] {
			continue
		}
		var sub []string
		if a.multiInstance() {
			sub = append(sub, a.instanceLabel(pr.Instance))
		}
		if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
			sub = append(sub, "cloned")
		}
		items = append(items, pickItem{Label: pr.PathWithNamespace, Sub: strings.Join(sub, " · "), Data: pr})
	}
	if len(items) == 0 {
		a.flash("every repository is in " + r.Path + " already")
		return
	}
	a.showPicker("Add a repository to - "+r.Path, items, func(it pickItem) {
		a.prepareGroupMember(r, it.Data.(forge.Project))
	})
}

// prepareGroupMember reads the branches of the repository joining the group,
// then asks which one to take.
func (a *App) prepareGroupMember(r worktreeRow, pr forge.Project) {
	if a.client(pr.Instance) == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
		return
	}
	g, err := workspace.ReadGroup(r.Dir)
	if err != nil {
		a.errorf("cannot read the grouped worktree: %v", err)
		return
	}
	taken := make([]string, len(g.Members))
	for i, m := range g.Members {
		taken[i] = m.Dir
	}
	name := workspace.NewMemberDirName(taken, pr.PathWithNamespace)
	a.runTask("Loading branches of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		choice, err := a.groupChoiceFor(ctx, pr, name)
		if err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			a.showGroupMemberForm(r, g, choice)
		})
		return "", nil
	})
}

// showGroupMemberForm asks for the branch of the repository joining the group.
// A group with a branch of its own gives it that branch - made from the one
// picked, or checked out when the repository has it already; a group without
// one checks the picked branch out.
func (a *App) showGroupMemberForm(r worktreeRow, g workspace.Group, c groupChoice) {
	shared := g.Branch
	has := shared != "" && slices.Contains(c.branches, shared)
	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	var pick *tview.DropDown
	switch {
	case has:
		form.AddTextView("", fmt.Sprintf("%s has the group's branch %s already: it is checked out.", c.dir, shared), 0, 2, true, false)
	default:
		options := make([]string, len(c.branches))
		for i, b := range c.branches {
			options[i] = c.option(b)
		}
		label := "Branch"
		hint := "The branch checked out in " + c.dir + "."
		if shared != "" {
			label = "From"
			hint = fmt.Sprintf("The group's branch %s is made in %s from this one.", shared, c.dir)
		}
		pick = addSelect(form, label, options, c.selected)
		form.AddTextView("", hint, 0, 2, true, false)
	}
	add := func() {
		member := workspace.GroupMember{Instance: c.project.Instance, Project: c.project.PathWithNamespace, Dir: c.dir}
		isNew := false
		switch {
		case has:
			member.Branch = shared
		default:
			idx, _ := pick.GetCurrentOption()
			picked := c.branches[max(idx, 0)]
			if shared == "" {
				if where, busy := c.busy[picked]; busy {
					a.flash(fmt.Sprintf("%s is checked out in %s - pick another", picked, where))
					return
				}
				member.Branch = picked
			} else {
				member.Branch, member.Base, isNew = shared, picked, true
			}
		}
		a.closeModal(pageForm)
		a.addGroupMember(r, c.project, member, isNew)
	}
	form.AddButton("Add", add)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized(fmt.Sprintf("Add %s to %s", c.project.PathWithNamespace, r.Path), form, 84, 9)
}

// addGroupMember clones the repository if it has to, checks it out into the
// group's folder and writes it into the group's description. Nothing is
// written down unless the worktree is there.
func (a *App) addGroupMember(r worktreeRow, pr forge.Project, member workspace.GroupMember, isNew bool) {
	a.runTaskThen(fmt.Sprintf("Adding %s to %s", pr.PathWithNamespace, r.Path), func(log func(string)) (string, error) {
		mgr := a.newManager(pr.Instance, pr.PathWithNamespace, log)
		if err := mgr.PrepareGroupMember(pr); err != nil {
			return "", err
		}
		if err := mgr.CheckGroupMember(pr, member.Branch, member.Base, isNew); err != nil {
			return "", err
		}
		dir := filepath.Join(r.Dir, member.Dir)
		if err := mgr.AddGroupMember(pr, dir, member.Branch, member.Base, isNew); err != nil {
			return "", err
		}
		g, err := workspace.ReadGroup(r.Dir)
		if err == nil {
			g.Members = append(g.Members, member)
			err = workspace.WriteGroup(r.Dir, g)
		}
		if err != nil {
			_ = mgr.RemoveGroupMember(pr.PathWithNamespace, dir)
			return "", err
		}
		log(fmt.Sprintf("%s is in %s as %s, on %s", pr.PathWithNamespace, r.Path, member.Dir, member.Branch))
		return r.Dir, nil
	}, func(dir string) {
		a.showWorktreeAt(dir)
		a.note(fmt.Sprintf("added %s to %s", pr.PathWithNamespace, r.Path))
	})
}

// removeFromGroup lists the repositories of the group and lets the one picked
// go, after saying what would be lost with it. Its branch stays in the
// repository. The last one stays: d deletes the group.
func (a *App) removeFromGroup(r worktreeRow) {
	if !r.grouped() {
		a.flash("x takes a repository out of a grouped worktree")
		return
	}
	if len(r.Members) < 2 {
		a.flash(r.Path + " holds one repository - d deletes the group")
		return
	}
	var items []pickItem
	for _, m := range r.Members {
		items = append(items, pickItem{Label: filepath.Base(m.Dir), Sub: m.Path + " · " + m.Branch, Data: m})
	}
	a.showPicker("Take a repository out of - "+r.Path, items, func(it pickItem) {
		m := it.Data.(worktreeRow)
		warnings := a.newManager(m.Instance, m.Path, nil).InspectDir(m.Dir).Warnings
		body := fmt.Sprintf("Take [::b]%s[::-] out of %s?\n\n%s\n\nIts branch %s stays in the repository.",
			esc(m.Path), esc(r.Path), esc(tildePath(m.Dir)), esc(m.Branch))
		a.confirmWith("Take out of the group", body, "Take out", warnings, func() {
			a.runTask(fmt.Sprintf("Taking %s out of %s", m.Path, r.Path), func(log func(string)) (string, error) {
				if err := a.newManager(m.Instance, m.Path, log).RemoveGroupMember(m.Path, m.Dir); err != nil {
					return "", err
				}
				g, err := workspace.ReadGroup(r.Dir)
				if err != nil {
					return "", err
				}
				name := filepath.Base(m.Dir)
				g.Members = slices.DeleteFunc(g.Members, func(gm workspace.GroupMember) bool { return gm.Dir == name })
				if err := workspace.WriteGroup(r.Dir, g); err != nil {
					return "", err
				}
				// An empty folder left behind would only confuse the next one.
				_ = os.Remove(m.Dir)
				return "", nil
			})
		})
	})
}

// unpublishBranches takes the branches of a worktree off origin: a push made
// too early, or work that is over. The local branches and the worktrees stay;
// they only stop tracking origin. A branch with an open merge request is left
// alone, since taking it away would close or break the merge request. In a
// grouped worktree the repositories are picked first - none to begin with -
// since what is over for one is seldom over for all.
func (a *App) unpublishBranches(r worktreeRow) {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	var onOrigin []unpublishable
	for _, m := range members {
		u := a.wtRemote[m.Dir].Upstream
		if u.Name == "" || u.Gone {
			continue
		}
		name := filepath.Base(m.Dir)
		if !r.grouped() {
			name = m.Path
		}
		c := unpublishable{member: m, name: name, remote: strings.TrimPrefix(u.Name, "origin/"), behind: u.Behind}
		if mr, ok := a.openMRFor(m); ok {
			c.mr = mr.IID
		}
		onOrigin = append(onOrigin, c)
	}
	if len(onOrigin) == 0 {
		a.flash(r.Path + " has no branch on origin")
		return
	}
	if !r.grouped() {
		if c := onOrigin[0]; c.mr != 0 {
			a.flash(fmt.Sprintf("!%d is open on %s - close it first", c.mr, c.remote))
			return
		}
		a.confirmUnpublish(r, onOrigin)
		return
	}
	picked := map[string]bool{}
	a.showToggles(toggles{
		title: "Delete on origin - " + r.Path,
		verb:  "pick",
		items: func() []toggleItem {
			items := make([]toggleItem, len(onOrigin))
			for i, c := range onOrigin {
				label := fmt.Sprintf("%s %s  %s", tagMark(picked[c.name]), esc(c.name), tag(colBranch)+esc(c.remote)+tagEnd)
				if c.mr != 0 {
					label = fmt.Sprintf("   %s  %s", esc(c.name), tag(colDim)+fmt.Sprintf("!%d is open on it, it stays", c.mr)+tagEnd)
				}
				items[i] = toggleItem{Label: label, Search: c.name, Data: c}
			}
			return items
		},
		toggle: func(it toggleItem) {
			c := it.Data.(unpublishable)
			if c.mr != 0 {
				a.flash(fmt.Sprintf("!%d is open on %s - close it first", c.mr, c.remote))
				return
			}
			picked[c.name] = !picked[c.name]
		},
		status: func() string {
			n := 0
			for _, on := range picked {
				if on {
					n++
				}
			}
			return fmt.Sprintf("%s%d picked%s", tag(colDim), n, tagEnd)
		},
		keys: []toggleKey{{key: 'd', hint: "delete the picked", run: func() {
			var chosen []unpublishable
			for _, c := range onOrigin {
				if picked[c.name] {
					chosen = append(chosen, c)
				}
			}
			if len(chosen) == 0 {
				a.flash("pick a repository with space first")
				return
			}
			a.closeModal(pageToggles)
			a.confirmUnpublish(r, chosen)
		}}},
	})
}

// unpublishable is a worktree whose branch is on origin.
type unpublishable struct {
	member worktreeRow
	name   string
	remote string // the branch's name on origin
	behind int    // commits origin has that the worktree lacks
	mr     int    // the merge request open on it, 0 when none
}

// confirmUnpublish says what is about to be deleted, and what origin would
// lose with it, before it is.
func (a *App) confirmUnpublish(r worktreeRow, chosen []unpublishable) {
	var lines, warnings []string
	for _, c := range chosen {
		lines = append(lines, fmt.Sprintf("%s  origin/%s", c.name, c.remote))
		if c.behind > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: origin has %d commit(s) this worktree lacks; they go with it", c.name, c.behind))
		}
	}
	body := fmt.Sprintf("Delete these branches on origin?\n\n%s\n\nThe local branches and the worktree stay.", esc(strings.Join(lines, "\n")))
	a.confirmWith("Delete on origin", body, "Delete on origin", warnings, func() {
		a.runTaskNoting("Taking branches of "+r.Path+" off origin", func(log func(string)) (string, error) {
			var done []string
			for _, c := range chosen {
				git := a.newManager(c.member.Instance, c.member.Path, log).Git()
				if err := git.DeleteRemoteBranch(c.member.Dir, c.member.Branch, c.remote); err != nil {
					return "", fmt.Errorf("%s: %w", c.member.Path, err)
				}
				done = append(done, c.name+" "+c.remote)
			}
			return "deleted on origin: " + strings.Join(done, ", "), nil
		})
	})
}
