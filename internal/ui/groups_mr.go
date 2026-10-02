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
// links all of the group's merge requests to one another - the new ones and
// those open from an earlier round alike, so that adding a repository later
// reaches every description. A repository that cannot take part - a push it
// would have to force, nothing to merge - is named and left out; the rest go
// ahead.
func (a *App) createGroupMRs(r worktreeRow, members []groupMR, reqs map[int]forge.NewMergeRequest) {
	type linkedMR struct {
		mr     forge.MergeRequest
		client forge.Provider
		desc   string // as created; "" for one open already, read when linking
		isNew  bool
	}
	states := map[string]remoteState{}
	knowns := map[string]bool{}
	for _, m := range members {
		states[m.member.Dir], knowns[m.member.Dir] = a.wtRemote[m.member.Dir]
	}
	a.runTask("Creating merge requests for "+r.Path, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var linked []linkedMR
		var skipped []string
		// push sends what origin lacks, never by force, and says why when it
		// cannot.
		push := func(m groupMR, git *gitx.Git) string {
			st, known := states[m.member.Dir], knowns[m.member.Dir]
			if why := pushBlocked(st, known); why != "" {
				return why
			}
			if st.ForceFrom != "" {
				return "it was rebased and needs a force push first - P"
			}
			if st.Upstream.Name != "" && st.Upstream.Ahead == 0 {
				return ""
			}
			log(fmt.Sprintf("Pushing %s (%s)", m.project.PathWithNamespace, m.member.Branch))
			if err := git.Push(m.member.Dir, m.member.Branch, st.Upstream.Name == ""); err != nil {
				return firstLine(err.Error())
			}
			return ""
		}
		for i, m := range members {
			git := a.newManager(m.member.Instance, m.member.Path, log).Git()
			if m.open != nil {
				// Open from an earlier round: new commits go to it.
				if why := push(m, git); why != "" {
					skipped = append(skipped, fmt.Sprintf("%s !%d not pushed: %s", m.name, m.open.IID, why))
				}
				linked = append(linked, linkedMR{mr: *m.open, client: m.client})
				continue
			}
			req, ok := reqs[i]
			if !ok {
				continue
			}
			if ahead, err := git.CommitsAhead(m.member.Dir, "origin/"+req.TargetBranch); err == nil && len(ahead) == 0 {
				skipped = append(skipped, fmt.Sprintf("%s: nothing to merge into %s", m.name, req.TargetBranch))
				continue
			}
			if why := push(m, git); why != "" {
				skipped = append(skipped, m.name+": "+why)
				continue
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
			linked = append(linked, linkedMR{mr: *mr, client: m.client, desc: req.Description, isNew: true})
		}

		// Every address is known only now. Each description's list of the others
		// is written afresh, so a later round neither repeats nor misses one.
		if len(linked) > 1 {
			all := make([]forge.MergeRequest, len(linked))
			for i, l := range linked {
				all[i] = l.mr
			}
			for _, l := range linked {
				desc := l.desc
				if !l.isNew {
					det, err := l.client.MergeRequestDetail(ctx, l.mr)
					if err != nil {
						skipped = append(skipped, fmt.Sprintf("%s !%d: cannot read its description to link it: %s",
							l.mr.ProjectPath, l.mr.IID, firstLine(err.Error())))
						continue
					}
					desc = det.Description
				}
				updated := relatedDescription(desc, l.mr, all)
				if !l.isNew && updated == desc {
					continue
				}
				if err := l.client.UpdateMergeRequestDescription(ctx, l.mr, updated); err != nil {
					skipped = append(skipped, fmt.Sprintf("%s !%d: the links to the others: %s",
						l.mr.ProjectPath, l.mr.IID, firstLine(err.Error())))
				}
			}
			log("Linked them to one another")
		}

		a.tv.QueueUpdateDraw(func() {
			var lines, urls []string
			made := 0
			for _, l := range linked {
				state := "linked"
				if l.isNew {
					a.adoptMergeRequest(l.mr)
					state, made = "created", made+1
					if l.mr.WebURL != "" {
						urls = append(urls, l.mr.WebURL)
					}
				}
				lines = append(lines, fmt.Sprintf("[::b]%s !%d[::-]  %s  %s", esc(l.mr.ProjectPath), l.mr.IID,
					tag(colDim)+state+tagEnd, esc(l.mr.WebURL)))
			}
			body := fmt.Sprintf("%d merge request(s) created.\n\n%s", made, strings.Join(lines, "\n"))
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

// relatedDescription is a description with the list of every other merge
// request of the group at its end, as markdown both forges render. A list
// written by an earlier round is replaced, not added to; what the author wrote
// above it stays as it is.
func relatedDescription(desc string, self forge.MergeRequest, all []forge.MergeRequest) string {
	desc = withoutRelated(desc)
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
	if desc != "" {
		desc += "\n\n"
	}
	return desc + "---\n" + relatedHeading + "\n" + strings.Join(links, "\n") + "\n"
}

// withoutRelated is a description without the list unagit wrote at its end.
func withoutRelated(desc string) string {
	desc = strings.ReplaceAll(desc, "\r\n", "\n")
	marker := "---\n" + relatedHeading
	if i := strings.LastIndex(desc, marker); i >= 0 && (i == 0 || desc[i-1] == '\n') {
		desc = desc[:i]
	}
	return strings.TrimRight(desc, "\n ")
}
