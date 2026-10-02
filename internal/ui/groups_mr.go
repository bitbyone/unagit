package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// n on a grouped worktree opens one merge request in every repository of it,
// from the branch they share, with one title and one description. Each targets
// the branch its repository's worktree was made from unless changed, and once
// every address is known each description gains the links to the others - a
// reviewer of one sees the rest of the change.

// groupMR is one repository of a grouped worktree, about to get a merge
// request, or having one already.
type groupMR struct {
	member  worktreeRow
	project forge.Project
	client  forge.Provider
	name    string   // its folder in the group
	targets []string // the branches it can merge into
	target  int      // the one proposed
	open    *forge.MergeRequest
}

// groupMergeRequests reads what the form needs - every repository's branches,
// and the commits the group adds, for a title and a description - and shows it.
func (a *App) groupMergeRequests(r worktreeRow) {
	if len(r.Members) == 0 {
		a.flash(r.Path + " holds no repository")
		return
	}
	members := make([]groupMR, len(r.Members))
	for i, m := range r.Members {
		client := a.client(m.Instance)
		if client == nil {
			a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(m.Instance))
			return
		}
		g := groupMR{member: m, project: a.worktreeProject(m), client: client, name: filepath.Base(m.Dir)}
		if mr, ok := a.openMRFor(m); ok {
			mr.ProjectPath = a.projectPathOfMR(mr)
			g.open = &mr
		}
		members[i] = g
	}
	bases := make([]string, len(members))
	for i, m := range members {
		bases[i] = a.wtRemote[m.member.Dir].Base
		if bases[i] == "" {
			bases[i] = m.member.Base
		}
	}
	a.runTask("Preparing merge requests for "+r.Path, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var commits []gitx.CommitMsg
		errs := make([]error, len(members))
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := range members {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m := &members[i]
				if m.open != nil {
					return
				}
				branches, err := m.client.ProjectBranches(ctx, m.project)
				if err != nil {
					errs[i] = fmt.Errorf("%s: %w", m.project.PathWithNamespace, err)
					return
				}
				want, fallback := bases[i], m.project.DefaultBranch
				for _, b := range branches {
					if b.Name == m.member.Branch {
						continue
					}
					if b.Default && fallback == "" {
						fallback = b.Name
					}
					m.targets = append(m.targets, b.Name)
				}
				if len(m.targets) == 0 {
					errs[i] = fmt.Errorf("%s has no other branch to merge %s into", m.project.PathWithNamespace, m.member.Branch)
					return
				}
				m.target = -1
				for j, t := range m.targets {
					if t == want || m.target < 0 && t == fallback {
						m.target = j
					}
				}
				m.target = max(m.target, 0)
				git := a.pathManager(m.member.Instance, m.member.Path).Git()
				if mine, err := git.CommitsAhead(m.member.Dir, "origin/"+m.targets[m.target]); err == nil {
					mu.Lock()
					commits = append(commits, mine...)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return "", err
		}
		title, description := mergeRequestDefaults(r.Branch, commits)
		if title == "" {
			title = humanBranch(r.Branch)
		}
		log(fmt.Sprintf("%d repositories, %d commit(s)", len(members), len(commits)))
		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			a.showGroupMRForm(r, members, title, description)
		})
		return "", nil
	})
}

// showGroupMRForm asks for the one title and description, and for the target
// of every repository that gets a merge request.
func (a *App) showGroupMRForm(r worktreeRow, members []groupMR, title, description string) {
	gitlab := false
	for _, m := range members {
		gitlab = gitlab || m.open == nil && m.client.Kind() == forge.KindGitLab
	}
	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	form.AddInputField("Title", title, 0, nil, nil)
	addTextArea(form, "Description", description, 5)
	selects := map[int]interface{ GetCurrentOption() (int, string) }{}
	var already []string
	for i, m := range members {
		if m.open != nil {
			already = append(already, fmt.Sprintf("%s !%d", m.name, m.open.IID))
			continue
		}
		selects[i] = addSelect(form, m.name+" into", m.targets, m.target)
	}
	if len(already) > 0 {
		form.AddTextView("", "Already open, linked from the new ones: "+strings.Join(already, ", "), 0, 2, true, false)
	}
	addCheckbox(form, "Draft", false)
	if gitlab {
		addCheckbox(form, labelDeleteBranch, false)
		addCheckbox(form, labelSquash, false)
	}
	checked := func(label string) bool {
		box, ok := form.GetFormItemByLabel(label).(*tview.Checkbox)
		return ok && box.IsChecked()
	}
	create := func() {
		base := forge.NewMergeRequest{
			Title:       strings.TrimSpace(form.GetFormItemByLabel("Title").(*tview.InputField).GetText()),
			Description: form.GetFormItemByLabel("Description").(*tview.TextArea).GetText(),
			Draft:       checked("Draft"),
		}
		if base.Title == "" {
			a.flash("enter a title")
			return
		}
		if len(selects) == 0 {
			a.flash("every repository has its merge request open already")
			return
		}
		reqs := map[int]forge.NewMergeRequest{}
		for i, sel := range selects {
			req := base
			req.SourceBranch = members[i].member.Branch
			_, req.TargetBranch = sel.GetCurrentOption()
			if members[i].client.Kind() == forge.KindGitLab {
				req.RemoveSourceBranch = checked(labelDeleteBranch)
				req.Squash = checked(labelSquash)
			}
			reqs[i] = req
		}
		a.closeModal(pageForm)
		a.createGroupMRs(r, members, reqs)
	}
	form.AddButton("Create", create)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	height := 14 + 2*len(selects)
	if len(already) > 0 {
		height += 3
	}
	if gitlab {
		height += 4
	}
	a.showFormModalSized(fmt.Sprintf("New merge requests · %s · %s", r.Path, a.worktreeBranch(r)), form, 92, height)
}

// createGroupMRs pushes what origin lacks, opens the merge requests, and then
// links every new one to all the others. A repository that cannot take part -
// a push it would have to force, nothing to merge - is named and left out; the
// rest go ahead.
func (a *App) createGroupMRs(r worktreeRow, members []groupMR, reqs map[int]forge.NewMergeRequest) {
	type made struct {
		mr     forge.MergeRequest
		client forge.Provider
		desc   string
	}
	states := map[string]remoteState{}
	knowns := map[string]bool{}
	for _, m := range members {
		states[m.member.Dir], knowns[m.member.Dir] = a.wtRemote[m.member.Dir]
	}
	a.runTask("Creating merge requests for "+r.Path, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var created []made
		var linked []forge.MergeRequest
		var skipped []string
		for i, m := range members {
			if m.open != nil {
				linked = append(linked, *m.open)
				continue
			}
			req, ok := reqs[i]
			if !ok {
				continue
			}
			st, known := states[m.member.Dir], knowns[m.member.Dir]
			if why := pushBlocked(st, known); why != "" || st.ForceFrom != "" {
				if why == "" {
					why = "it was rebased and needs a force push first - P"
				}
				skipped = append(skipped, m.name+": "+why)
				continue
			}
			git := a.newManager(m.member.Instance, m.member.Path, log).Git()
			if ahead, err := git.CommitsAhead(m.member.Dir, "origin/"+req.TargetBranch); err == nil && len(ahead) == 0 {
				skipped = append(skipped, fmt.Sprintf("%s: nothing to merge into %s", m.name, req.TargetBranch))
				continue
			}
			if st.Upstream.Name == "" || st.Upstream.Ahead > 0 {
				log(fmt.Sprintf("Pushing %s (%s)", m.project.PathWithNamespace, req.SourceBranch))
				if err := git.Push(m.member.Dir, req.SourceBranch, st.Upstream.Name == ""); err != nil {
					skipped = append(skipped, m.name+": "+firstLine(err.Error()))
					continue
				}
			}
			log(fmt.Sprintf("Creating the merge request of %s into %s", m.project.PathWithNamespace, req.TargetBranch))
			mr, err := m.client.CreateMergeRequest(ctx, m.project, req)
			if err != nil {
				skipped = append(skipped, m.name+": "+firstLine(err.Error()))
				continue
			}
			mr.Instance = m.project.Instance
			if mr.ProjectPath == "" {
				mr.ProjectPath = m.project.PathWithNamespace
			}
			log(fmt.Sprintf("Created %s !%d", mr.ProjectPath, mr.IID))
			created = append(created, made{mr: *mr, client: m.client, desc: req.Description})
			linked = append(linked, *mr)
		}

		// Every address is known only now: each new description gets the others.
		if len(linked) > 1 {
			for _, c := range created {
				desc := relatedDescription(c.desc, c.mr, linked)
				if err := c.client.UpdateMergeRequestDescription(ctx, c.mr, desc); err != nil {
					skipped = append(skipped, fmt.Sprintf("%s !%d: the links to the others: %s",
						c.mr.ProjectPath, c.mr.IID, firstLine(err.Error())))
				}
			}
			log("Linked them to one another")
		}

		a.tv.QueueUpdateDraw(func() {
			var lines []string
			var urls []string
			for _, c := range created {
				a.adoptMergeRequest(c.mr)
				lines = append(lines, fmt.Sprintf("[::b]%s !%d[::-]  %s", esc(c.mr.ProjectPath), c.mr.IID, esc(c.mr.WebURL)))
				if c.mr.WebURL != "" {
					urls = append(urls, c.mr.WebURL)
				}
			}
			body := fmt.Sprintf("%d merge request(s) created.\n\n%s", len(created), strings.Join(lines, "\n"))
			if len(skipped) > 0 {
				body += "\n\n" + tag(colWarn) + "Left out:" + tagEnd + "\n" + esc(strings.Join(skipped, "\n"))
			}
			a.closeModal(pageTask)
			a.refreshDisk()
			a.confirmWith("Merge requests created", body, "Open in browser", nil, func() {
				for _, u := range urls {
					_ = openBrowser(u)
				}
			})
		})
		return "", nil
	})
}

// relatedHeading starts the part of a description unagit writes; whatever came
// before it is the author's.
const relatedHeading = "Related merge requests:"

// relatedDescription is a description with the links to every other merge
// request of the group appended, as markdown both forges render.
func relatedDescription(desc string, self forge.MergeRequest, all []forge.MergeRequest) string {
	var links []string
	for _, mr := range all {
		if mr.ProjectPath == self.ProjectPath && mr.IID == self.IID {
			continue
		}
		links = append(links, fmt.Sprintf("- [%s !%d](%s) %s", mr.ProjectPath, mr.IID, mr.WebURL, mr.Title))
	}
	if len(links) == 0 {
		return desc
	}
	desc = strings.TrimRight(desc, "\n")
	if desc != "" {
		desc += "\n\n"
	}
	return desc + "---\n" + relatedHeading + "\n" + strings.Join(links, "\n") + "\n"
}
