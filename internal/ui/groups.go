package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// A grouped worktree puts several repositories side by side in one directory,
// each a worktree of its own main clone, so that one editor - or one agent -
// opened there has all of them at hand. It is made from the repositories
// marked with space in Repositories, and lives in Worktrees as a single row.

// groupRow turns a grouped worktree found on disk into a row of the
// Worktrees list, with a row of its own for every member still there.
func (a *App) groupRow(g workspace.GroupDir) worktreeRow {
	row := worktreeRow{Path: g.Group.Name, Dir: g.Dir, Members: []worktreeRow{}}
	if row.Path == "" {
		row.Path = filepath.Base(g.Dir)
	}
	shared := ""
	for i, m := range g.Group.Members {
		dir := filepath.Join(g.Dir, m.Dir)
		if !workspace.Exists(dir) {
			continue
		}
		branch, moved := workspace.WorktreeHead(dir)
		row.Members = append(row.Members, worktreeRow{
			Instance: m.Instance, Path: m.Project, Branch: branch, Dir: dir, Moved: moved})
		if moved.After(row.Moved) {
			row.Moved = moved
		}
		if i == 0 {
			shared = branch
		} else if branch != shared {
			shared = ""
		}
	}
	if len(row.Members) > 0 {
		row.Branch = shared
	}
	if len(row.Members) > 0 {
		row.Instance = row.Members[0].Instance
	}
	return row
}

// groupChoice is one repository of a grouped worktree about to be made: the
// branches it can start from or check out, and its directory in the group.
type groupChoice struct {
	project  forge.Project
	dir      string
	branches []string
	selected int
}

// startGroupWorktree loads the branches of every marked repository, then asks
// how the grouped worktree should be made.
func (a *App) startGroupWorktree(projects []forge.Project, ed *editors.Editor) {
	for _, pr := range projects {
		if a.client(pr.Instance) == nil {
			a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
			return
		}
	}
	paths := make([]string, len(projects))
	for i, pr := range projects {
		paths[i] = pr.PathWithNamespace
	}
	dirs := workspace.MemberDirNames(paths)

	a.runTask(fmt.Sprintf("Loading branches of %d repositories", len(projects)), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		choices := make([]groupChoice, len(projects))
		errs := make([]error, len(projects))
		var wg sync.WaitGroup
		for i, pr := range projects {
			wg.Add(1)
			go func() {
				defer wg.Done()
				choices[i], errs[i] = a.groupChoiceFor(ctx, pr, dirs[i])
			}()
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return "", err
		}
		for _, c := range choices {
			log(fmt.Sprintf("%s: %d branch(es)", c.project.PathWithNamespace, len(c.branches)))
		}
		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			a.showGroupWorktreeForm(choices, ed)
		})
		return "", nil
	})
}

// groupChoiceFor reads one repository's branches: the forge's, the default
// first, and those of the clone that were never pushed.
func (a *App) groupChoiceFor(ctx context.Context, pr forge.Project, dir string) (groupChoice, error) {
	branches, err := a.client(pr.Instance).ProjectBranches(ctx, pr)
	if err != nil {
		return groupChoice{}, fmt.Errorf("%s: %w", pr.PathWithNamespace, err)
	}
	c := groupChoice{project: pr, dir: dir}
	known := map[string]bool{}
	for _, b := range branches {
		known[b.Name] = true
		if b.Default || b.Name == pr.DefaultBranch {
			c.branches = append([]string{b.Name}, c.branches...)
		} else {
			c.branches = append(c.branches, b.Name)
		}
	}
	if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		mgr := a.pathManager(pr.Instance, pr.PathWithNamespace)
		for _, name := range mgr.Git().LocalBranches(mgr.ProjectDir(pr.PathWithNamespace)) {
			if !known[name] {
				c.branches = append(c.branches, name)
			}
		}
	}
	if len(c.branches) == 0 {
		return groupChoice{}, fmt.Errorf("%s has no branch yet", pr.PathWithNamespace)
	}
	return c, nil
}

// Labels of the fields above the repositories.
const (
	labelGroupBranch = "New branch"
	labelGroupFolder = "Folder"
)

// showGroupWorktreeForm asks for what the grouped worktree should be: a branch
// made in every repository or none, the folder they go in, and a branch of
// each repository - where the new one starts, or the one checked out.
func (a *App) showGroupWorktreeForm(choices []groupChoice, ed *editors.Editor) {
	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	branch := form.AddInputField(labelGroupBranch, "", 0, nil, nil).GetFormItemByLabel(labelGroupBranch).(*tview.InputField)
	folder := form.AddInputField(labelGroupFolder, "", 0, nil, nil).GetFormItemByLabel(labelGroupFolder).(*tview.InputField)
	// The folder follows the branch until it is given a name of its own.
	named := ""
	branch.SetChangedFunc(func(text string) {
		if folder.GetText() == named {
			named = workspace.Sanitize(strings.TrimSpace(text))
			folder.SetText(named)
		}
	})
	form.AddTextView("", "New branch: made in each repository, from its branch below.\n"+
		"Left empty: each repository checks out its branch below.", 0, 2, true, false)
	selects := make([]*tview.DropDown, len(choices))
	for i, c := range choices {
		selects[i] = addSelect(form, c.dir, c.branches, c.selected)
	}

	create := func() {
		newBranch := strings.TrimSpace(branch.GetText())
		name := workspace.Sanitize(strings.TrimSpace(folder.GetText()))
		if name == "" {
			name = workspace.Sanitize(newBranch)
		}
		if name == "" {
			a.flash("enter a folder, or a new branch to name it after")
			return
		}
		dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), name)
		if _, err := os.Stat(dir); err == nil {
			a.flash(tildePath(dir) + " already exists - choose another folder")
			return
		}
		plan := workspace.Group{Name: name, Branch: newBranch, Created: time.Now()}
		for i, c := range choices {
			_, picked := selects[i].GetCurrentOption()
			m := workspace.GroupMember{Instance: c.project.Instance, Project: c.project.PathWithNamespace,
				Dir: c.dir, Branch: picked}
			if newBranch != "" {
				m.Branch, m.Base = newBranch, picked
			}
			plan.Members = append(plan.Members, m)
		}
		a.closeModal(pageForm)
		projects := make([]forge.Project, len(choices))
		for i, c := range choices {
			projects[i] = c.project
		}
		a.createGroupWorktree(dir, plan, projects, ed)
	}
	form.AddButton("Create", create)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized(fmt.Sprintf("Grouped worktree · %d repositories", len(choices)), form, 84, 11+2*len(choices))
}

// createGroupWorktree makes every member, or none: every repository is cloned
// and fetched first, then checked for what would make git refuse, and only
// then are the worktrees added. One that fails after all takes the others
// back with it. The editor opens on the folder holding them all.
func (a *App) createGroupWorktree(dir string, plan workspace.Group, projects []forge.Project, ed *editors.Editor) {
	a.runTaskOpening("Creating grouped worktree "+plan.Name, session.Record{
		Instance: plan.Members[0].Instance,
		Server:   a.instanceLabel(plan.Members[0].Instance),
		Project:  plan.Name,
		Title:    plan.Branch,
		Mode:     session.ModeGroup,
	}, ed, func(log func(string)) (string, error) {
		isNew := plan.Branch != ""
		mgrs := make([]*workspace.Manager, len(projects))
		for i, pr := range projects {
			mgrs[i] = a.newManager(pr.Instance, pr.PathWithNamespace, log)
			if err := mgrs[i].PrepareGroupMember(pr); err != nil {
				return "", fmt.Errorf("%s: %w", pr.PathWithNamespace, err)
			}
		}
		var problems []error
		for i, pr := range projects {
			m := plan.Members[i]
			base := m.Base
			if !isNew {
				base = ""
			}
			if err := mgrs[i].CheckGroupMember(pr, m.Branch, base, isNew); err != nil {
				problems = append(problems, err)
			}
		}
		if err := errors.Join(problems...); err != nil {
			return "", fmt.Errorf("nothing was created:\n%w", err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		undo := func(made int) {
			log("! taking back what was made")
			for i := made - 1; i >= 0; i-- {
				pr := projects[i]
				_ = mgrs[i].RemoveGroupMember(pr.PathWithNamespace, filepath.Join(dir, plan.Members[i].Dir))
				if isNew {
					_, _ = mgrs[i].Git().Run(mgrs[i].ProjectDir(pr.PathWithNamespace), "branch", "-D", plan.Branch)
				}
			}
			_ = os.RemoveAll(dir)
		}
		for i, pr := range projects {
			m := plan.Members[i]
			if err := mgrs[i].AddGroupMember(pr, filepath.Join(dir, m.Dir), m.Branch, m.Base, isNew); err != nil {
				undo(i)
				return "", fmt.Errorf("%s: %w", pr.PathWithNamespace, err)
			}
		}
		if err := workspace.WriteGroup(dir, plan); err != nil {
			undo(len(projects))
			return "", err
		}
		log(fmt.Sprintf("Done: %d repositories in %s", len(projects), tildePath(dir)))
		a.tv.QueueUpdateDraw(func() {
			a.projectsPane.clearMarks()
			a.refreshDisk()
		})
		return dir, nil
	})
}

// openGroup brings every member up to date the way opening one worktree does,
// then opens the editor on the folder holding them.
func (a *App) openGroup(r worktreeRow, ed *editors.Editor) {
	a.runTaskOpening("Opening "+r.Path, session.Record{
		Instance: r.Instance,
		Server:   a.instanceLabel(r.Instance),
		Project:  r.Path,
		Title:    r.Branch,
		Mode:     session.ModeGroup,
	}, ed, func(log func(string)) (string, error) {
		for _, m := range r.Members {
			log(fmt.Sprintf("Updating %s (%s)", m.Path, m.Branch))
			if _, err := a.newManager(m.Instance, m.Path, log).UpdateWorktree(m.Dir); err != nil {
				return "", fmt.Errorf("%s: %w", m.Path, err)
			}
		}
		return r.Dir, nil
	})
}

// confirmDeleteGroup asks before removing a grouped worktree: every member,
// and whatever else was left in the folder. The branches stay in their
// repositories, and so do the main clones.
func (a *App) confirmDeleteGroup(r worktreeRow) {
	g, err := workspace.ReadGroup(r.Dir)
	if err != nil {
		a.errorf("cannot read the grouped worktree: %v", err)
		return
	}
	var lines, warnings []string
	for _, m := range r.Members {
		lines = append(lines, m.Path+"  ("+m.Branch+")")
		for _, w := range a.newManager(m.Instance, m.Path, nil).InspectDir(m.Dir).Warnings {
			warnings = append(warnings, filepath.Base(m.Dir)+": "+w)
		}
	}
	if extras := workspace.GroupExtras(r.Dir, g); len(extras) > 0 {
		warnings = append(warnings, "also holds "+strings.Join(extras, ", ")+", deleted with it")
	}
	body := fmt.Sprintf("Delete the grouped worktree [::b]%s[::-]?\n\n%s\n\n%s\n\nThe branches and the main clones stay.",
		esc(r.Path), esc(tildePath(r.Dir)), esc(strings.Join(lines, "\n")))
	a.confirm("Delete grouped worktree", body, warnings, func() {
		a.runTask("Deleting "+r.Path, func(log func(string)) (string, error) {
			for _, m := range r.Members {
				if err := a.newManager(m.Instance, m.Path, log).RemoveGroupMember(m.Path, m.Dir); err != nil {
					return "", fmt.Errorf("%s: %w", m.Path, err)
				}
			}
			log("Removing " + r.Dir)
			if err := os.RemoveAll(r.Dir); err != nil {
				return "", err
			}
			// The groups directory goes too once it is empty; Remove refuses otherwise.
			_ = os.Remove(filepath.Dir(r.Dir))
			return "", nil
		})
	})
}

// pushGroup pushes every member whose branch origin lacks, and says which it
// had to leave alone and why. Nothing is forced, as with one worktree.
func (a *App) pushGroup(r worktreeRow) {
	type push struct {
		member      worktreeRow
		setUpstream bool
	}
	var pushes []push
	var skipped []string
	for _, m := range r.Members {
		st, known := a.wtRemote[m.Dir]
		if why := pushBlocked(st, known); why != "" {
			skipped = append(skipped, m.Path+": "+why)
			continue
		}
		if st.Upstream.Name != "" && st.Upstream.Ahead == 0 {
			continue
		}
		pushes = append(pushes, push{m, st.Upstream.Name == ""})
	}
	if len(pushes) == 0 {
		if len(skipped) > 0 {
			a.flash(skipped[0])
		} else {
			a.flash("every repository of " + r.Path + " is already on origin")
		}
		return
	}
	a.runTask("Pushing "+r.Path, func(log func(string)) (string, error) {
		for _, s := range skipped {
			log("! " + s)
		}
		for _, p := range pushes {
			m := p.member
			log(fmt.Sprintf("Pushing %s (%s)", m.Path, m.Branch))
			if err := a.newManager(m.Instance, m.Path, log).Git().Push(m.Dir, m.Branch, p.setUpstream); err != nil {
				return "", fmt.Errorf("%s: %w", m.Path, err)
			}
		}
		return "", nil
	})
}

// groupMergeRequest asks which member to open a merge request for; each
// repository has merge requests of its own.
func (a *App) groupMergeRequest(r worktreeRow) {
	var items []pickItem
	for _, m := range r.Members {
		sub := m.Branch
		if mr, ok := a.openMRFor(m); ok {
			sub += fmt.Sprintf("  !%d already open", mr.IID)
		}
		items = append(items, pickItem{Label: m.Path, Sub: sub, Data: m})
	}
	if len(items) == 0 {
		a.flash(r.Path + " holds no repository")
		return
	}
	a.showPicker("Merge request for - "+r.Path, items, func(it pickItem) {
		a.newMergeRequest(it.Data.(worktreeRow))
	})
}

// showGroupDetail fills the detail column with a grouped worktree: the folder,
// then every repository in it with its branch, where that stands against
// origin, and its state on disk, which comes in after the outline.
func (a *App) showGroupDetail(r worktreeRow, focus bool) {
	p := a.worktreesPane
	p.detailSeq++
	seq := p.detailSeq
	title := r.Path + " · " + fmt.Sprintf("%d repositories", len(r.Members))

	outline := func(states map[string]string) string {
		d := &detailBuf{}
		d.title(r.Path)
		d.sub(fmt.Sprintf("grouped worktree · %d repositories", len(r.Members)))
		d.section("Grouped worktree")
		d.kv("Directory", esc(tildePath(r.Dir)))
		if r.Branch != "" {
			d.kv("Branch", tag(colBranch)+esc(r.Branch)+tagEnd)
		} else {
			d.kv("Branch", tag(colMuted)+esc(a.worktreeBranch(r))+tagEnd)
		}
		plain, colour := a.worktreeRemoteWords(r)
		d.kv("Remote", tag(colour)+esc(plain)+tagEnd)
		if !r.Moved.IsZero() {
			d.kv("Last moved", humanAge(r.Moved))
		}
		for _, m := range r.Members {
			d.section(filepath.Base(m.Dir))
			d.kv("Repository", esc(m.Path))
			if a.multiInstance() {
				d.kv("Server", esc(a.instanceLabel(m.Instance)))
			}
			d.kv("Branch", tag(colBranch)+esc(m.Branch)+tagEnd)
			st, known := a.wtRemote[m.Dir]
			plain, name, colour := remoteWords(st, known)
			remote := tag(colour) + esc(a.remoteSentence(st, known, plain)) + tagEnd
			if name != "" {
				remote += tag(colDim) + " with " + esc(name) + tagEnd
			}
			d.kv("Remote", remote)
			mrLine := tag(colDim) + "no open merge request" + tagEnd
			if mr, ok := a.openMRFor(m); ok {
				mrLine = tag(colAccent) + fmt.Sprintf("!%d", mr.IID) + tagEnd + " " + esc(mr.Title)
			}
			d.kv("Merge request", mrLine)
			d.kv("Directory", esc(filepath.Base(m.Dir)))
			state, ok := states[m.Dir]
			if !ok {
				state = tag(colMuted) + "looking …" + tagEnd
			}
			d.kv("State", state)
		}
		return d.String()
	}
	p.openDetail(title, outline(nil), focus)

	members := r.Members
	go func() {
		states := map[string]string{}
		for _, m := range members {
			state := tag(colOn) + "clean" + tagEnd
			if s := a.pathManager(m.Instance, m.Path).Git().Status(m.Dir).Describe(); s != "" {
				state = tag(colWarn) + esc(s) + tagEnd
			}
			states[m.Dir] = state
		}
		a.tv.QueueUpdateDraw(func() {
			if p.detailSeq != seq {
				return
			}
			p.setDetail(title, outline(states))
		})
	}()
}

// groupMembersOf lists the grouped worktrees that hold a worktree of the
// repository, so that deleting its main clone can say they would break.
func (a *App) groupMembersOf(instance, path string) []string {
	var out []string
	for _, r := range a.worktrees {
		for _, m := range r.Members {
			if m.Instance == instance && m.Path == path {
				out = append(out, r.Path)
			}
		}
	}
	return out
}
