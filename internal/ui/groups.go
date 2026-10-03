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
	"github.com/tobola/unagit/internal/incomm"
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
			Instance: m.Instance, Path: m.Project, Branch: branch, Dir: dir, Moved: moved, Base: m.Base, Group: g.Dir})
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
	// busy says where a branch is already checked out; git will not check it
	// out a second time, so without a new branch it cannot be picked.
	busy map[string]string
}

// offered is what the select of a repository offers: to check out, only the
// branches checked out nowhere, since git checks a branch out once and the
// others could only be refused; to start a new branch from, every branch.
func (c groupChoice) offered(checkout bool) []string {
	if !checkout {
		return c.branches
	}
	var free []string
	for _, b := range c.branches {
		if _, busy := c.busy[b]; !busy {
			free = append(free, b)
		}
	}
	return free
}

// noFreeBranch is what a select says when every branch of its repository is
// checked out somewhere already.
const noFreeBranch = "every branch is checked out elsewhere - type a new branch"

// branchOptions are a select's options for a list of branches: cut to fit the
// dialog, since a select wider than it is drawn over its frame.
func branchOptions(branches []string) []string {
	if len(branches) == 0 {
		return []string{noFreeBranch}
	}
	options := make([]string, len(branches))
	for i, b := range branches {
		options[i] = trunc(b, 56)
	}
	return options
}

// branchIndex is where a branch is in a list, or 0.
func branchIndex(branches []string, name string) int {
	for i, b := range branches {
		if b == name {
			return i
		}
	}
	return 0
}

// startGroupWorktree loads the branches of every marked repository, then asks
// how the grouped worktree should be made.
func (a *App) startGroupWorktree(projects []forge.Project) {
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
			a.showGroupWorktreeForm(choices)
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
		mainDir := mgr.ProjectDir(pr.PathWithNamespace)
		for _, name := range mgr.Git().LocalBranches(mainDir) {
			if !known[name] {
				c.branches = append(c.branches, name)
			}
		}
		c.busy = map[string]string{}
		for branch, dir := range mgr.Git().CheckedOut(mainDir) {
			if sameDir(dir, mainDir) {
				c.busy[branch] = "the main clone"
			} else {
				c.busy[branch] = filepath.Base(dir)
			}
		}
	}
	if len(c.branches) == 0 {
		return groupChoice{}, fmt.Errorf("%s has no branch yet", pr.PathWithNamespace)
	}
	return c, nil
}

// sameDir compares two directories as the file system sees them: git reports
// the real path, and the configured one may go through a symlink.
func sameDir(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// Hints under the branch, for the two ways a grouped worktree is made.
const (
	hintGroupNew      = "Made in every repository from the branch below;\np rebases it onto that branch until it is pushed."
	hintGroupExisting = "Each repository checks out the branch below;\nonly branches checked out nowhere are offered."
)

// Labels of the fields above the repositories.
const (
	labelGroupBranch = "New branch"
	labelGroupFolder = "Folder"
)

// showGroupWorktreeForm asks for what the grouped worktree should be: a branch
// made in every repository or none, the folder they go in, and a branch of
// each repository - where the new one starts, or the one checked out.
func (a *App) showGroupWorktreeForm(choices []groupChoice) {
	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	branch := form.AddInputField(labelGroupBranch, "", 0, nil, nil).GetFormItemByLabel(labelGroupBranch).(*tview.InputField)
	folder := form.AddInputField(labelGroupFolder, "", 0, nil, nil).GetFormItemByLabel(labelGroupFolder).(*tview.InputField)
	form.AddTextView("", hintGroupExisting, 0, 2, true, false)
	hint := form.GetFormItem(form.GetFormItemCount() - 1).(*tview.TextView)
	selects := make([]*tview.DropDown, len(choices))
	// What each select offers now, and the branch picked in each of its two
	// meanings - to check out, to start from - so that each is kept while the
	// other is shown.
	shown := make([][]string, len(choices))
	picked := map[bool][]string{true: make([]string, len(choices)), false: make([]string, len(choices))}
	checkingOut := false
	offer := func(checkout bool) {
		for i, c := range choices {
			if selects[i] != nil {
				if idx, _ := selects[i].GetCurrentOption(); idx >= 0 && idx < len(shown[i]) {
					picked[checkingOut][i] = shown[i][idx]
				}
			}
			shown[i] = c.offered(checkout)
			at := branchIndex(shown[i], picked[checkout][i])
			if selects[i] == nil {
				selects[i] = addSelect(form, c.dir, branchOptions(shown[i]), at)
				continue
			}
			selects[i].SetOptions(branchOptions(shown[i]), nil)
			selects[i].SetCurrentOption(at)
		}
		checkingOut = checkout
		fitSelects(form)
	}
	// The folder follows the branch until it is given a name of its own; the
	// hint says what the branches below mean now, and they offer what can be
	// picked for it.
	named := ""
	branch.SetChangedFunc(func(text string) {
		if folder.GetText() == named {
			named = workspace.Sanitize(strings.TrimSpace(text))
			folder.SetText(named)
		}
		checkout := strings.TrimSpace(text) == ""
		if checkout {
			hint.SetText(hintGroupExisting)
		} else {
			hint.SetText(hintGroupNew)
		}
		if checkout != checkingOut {
			offer(checkout)
		}
	})
	for i, c := range choices {
		if c.selected >= 0 && c.selected < len(c.branches) {
			picked[true][i], picked[false][i] = c.branches[c.selected], c.branches[c.selected]
		}
	}
	offer(true)

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
			idx, _ := selects[i].GetCurrentOption()
			if len(shown[i]) == 0 {
				a.flash(fmt.Sprintf("every branch of %s is checked out elsewhere - give the group a new branch", c.dir))
				return
			}
			picked := shown[i][max(idx, 0)]
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
		a.createGroupWorktree(dir, plan, projects)
	}
	form.AddButton("Create", create)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized(fmt.Sprintf("Grouped worktree · %d repositories", len(choices)), form, 84, 11+2*len(choices))
}

// createGroupWorktree makes every member, or none: every repository is cloned
// and fetched first, then checked for what would make git refuse, and only
// then are the worktrees added. One that fails after all takes the others
// back with it. Worktrees then shows it, for the user to open.
func (a *App) createGroupWorktree(dir string, plan workspace.Group, projects []forge.Project) {
	integrate := a.cfg.Integrations.Incomm
	a.runTaskThen("Creating grouped worktree "+plan.Name, func(log func(string)) (string, error) {
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
		// An editor or an agent opened on the folder keeps its Incomm comments
		// here, not in some folder above it that happens to have a store.
		if integrate {
			if err := incomm.Prepare(dir); err != nil {
				log("! could not prepare the folder for Incomm: " + err.Error())
			}
		}
		log(fmt.Sprintf("Done: %d repositories in %s", len(projects), tildePath(dir)))
		return dir, nil
	}, func(dir string) {
		a.projectsPane.clearMarks()
		a.showWorktreeAt(dir)
	})
}

// openGroup opens the editor on the folder holding every member, as it is.
func (a *App) openGroup(r worktreeRow, ed *editors.Editor) {
	a.openNow(r.Dir, session.Record{
		Instance: r.Instance,
		Server:   a.instanceLabel(r.Instance),
		Project:  r.Path,
		Title:    r.Branch,
		Mode:     session.ModeGroup,
	}, ed)
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
// had to leave alone and why. A member Ctrl-R rebased is force-pushed, after
// one question for all of them.
func (a *App) pushGroup(r worktreeRow) {
	type push struct {
		member      worktreeRow
		setUpstream bool
		lease       string
	}
	var pushes []push
	var skipped, forced []string
	for _, m := range r.Members {
		st, known := a.wtRemote[m.Dir]
		if why := pushBlocked(st, known); why != "" {
			skipped = append(skipped, m.Path+": "+why)
			continue
		}
		if st.ForceFrom != "" {
			pushes = append(pushes, push{member: m, lease: st.ForceFrom})
			forced = append(forced, m.Path+"  ("+m.Branch+")")
			continue
		}
		if st.Upstream.Name != "" && st.Upstream.Ahead == 0 {
			continue
		}
		// A branch new to origin with no commits of its own would only put an
		// empty branch there.
		if st.Upstream.Name == "" && st.Own == 0 {
			continue
		}
		pushes = append(pushes, push{member: m, setUpstream: st.Upstream.Name == ""})
	}
	if len(pushes) == 0 {
		if len(skipped) > 0 {
			a.flash(skipped[0])
		} else {
			a.flash("nothing to push from " + r.Path + ": what is committed is on origin already")
		}
		return
	}
	run := func() {
		a.runTask("Pushing "+r.Path, func(log func(string)) (string, error) {
			for _, s := range skipped {
				log("! " + s)
			}
			for _, p := range pushes {
				m := p.member
				git := a.newManager(m.Instance, m.Path, log).Git()
				var err error
				if p.lease != "" {
					log(fmt.Sprintf("Force-pushing %s (%s)", m.Path, m.Branch))
					err = forcePush(git, m.Dir, m.Branch, p.lease)
				} else {
					log(fmt.Sprintf("Pushing %s (%s)", m.Path, m.Branch))
					err = git.Push(m.Dir, m.Branch, p.setUpstream)
				}
				if err != nil {
					return "", fmt.Errorf("%s: %w", m.Path, err)
				}
			}
			return "", nil
		})
	}
	if len(forced) == 0 {
		run()
		return
	}
	body := fmt.Sprintf("These were rebased, so origin's copy has to be replaced:\n\n%s\n\n"+
		"Force-push them? Only origin's copy as it was before the rebase is replaced: "+
		"if anyone pushed since, git refuses.", esc(strings.Join(forced, "\n")))
	a.confirmWith("Force push", body, "Force push", nil, run)
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
