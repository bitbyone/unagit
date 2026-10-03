package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// The branches of a repository, managed in one list: where each one is - in
// the clone, on origin, or both, and how far apart - whether it is checked
// out and where, and keys to delete it here, there, or everywhere. Opened from
// Repositories, Enter also switches the main clone to one. Opened from a
// worktree there is nothing to switch, so Enter does nothing there. n makes
// a new branch from the one under the cursor; it comes back into the list,
// and switching to it is the next step, and the user's.
//
// The default branch and a protected one are never deleted, a branch checked
// out somewhere cannot be deleted here (git refuses), and one with an open
// merge request is not deleted on origin, since that would close or break the
// merge request.

// branchInfo is one branch as the manager shows it.
type branchInfo struct {
	name          string
	local, remote bool
	// where is the checkout that has it: "the main clone", a worktree's
	// folder, or "" when it is out nowhere.
	where     string
	upstream  gitx.Upstream
	onlyHere  int // commits of the local branch that no branch of origin has
	isDefault bool
	protected bool
	when      time.Time
	title     string
}

// branchScope is what a branch manager is opened for.
type branchScope struct {
	project forge.Project
	// checkout is set in Repositories: Enter switches the main clone.
	checkout bool
	// focus is the branch the cursor starts on.
	focus string
	// done is what was just done, said once the list is back over it.
	done string
}

// showBranchManager loads the branches of a repository and lists them.
func (a *App) showBranchManager(scope branchScope) {
	pr := scope.project
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
		return
	}
	cloned := a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned
	a.runTask("Loading branches of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		remote, err := client.ProjectBranches(ctx, pr)
		if err != nil {
			return "", err
		}
		branches := a.readBranches(pr, remote, cloned)
		log(fmt.Sprintf("%d branch(es)", len(branches)))
		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			a.listBranches(scope, branches)
		})
		return "", nil
	})
}

// readBranches puts together what origin and the clone say of the branches.
// It runs off the event loop.
func (a *App) readBranches(pr forge.Project, remote []forge.Branch, cloned bool) []branchInfo {
	byName := map[string]*branchInfo{}
	var order []string
	get := func(name string) *branchInfo {
		if b, ok := byName[name]; ok {
			return b
		}
		byName[name] = &branchInfo{name: name}
		order = append(order, name)
		return byName[name]
	}
	for _, rb := range remote {
		b := get(rb.Name)
		b.remote = true
		b.isDefault = rb.Default || rb.Name == pr.DefaultBranch
		b.protected = rb.Protected
		b.when, b.title = rb.CommittedDate, rb.CommitTitle
	}
	if cloned {
		mgr := a.pathManager(pr.Instance, pr.PathWithNamespace)
		git := mgr.Git()
		mainDir := mgr.ProjectDir(pr.PathWithNamespace)
		upstreams := git.BranchUpstreams(mainDir)
		for _, name := range git.LocalBranches(mainDir) {
			b := get(name)
			b.local = true
			b.upstream = upstreams[name]
			b.onlyHere = git.OnlyHere(mainDir, name)
			if name == pr.DefaultBranch {
				b.isDefault = true
			}
		}
		for name, dir := range git.CheckedOut(mainDir) {
			if b, ok := byName[name]; ok {
				b.where = checkoutName(dir, mainDir)
			}
		}
	}
	out := make([]branchInfo, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	// The default first, then the most recently moved; a branch only here
	// has no date from origin and comes last.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].isDefault != out[j].isDefault {
			return out[i].isDefault
		}
		return out[i].when.After(out[j].when)
	})
	return out
}

// checkoutName says which checkout a directory is, in a few words: the main
// clone, a grouped worktree by its folder, or a worktree by its own.
func checkoutName(dir, mainDir string) string {
	switch {
	case sameDir(dir, mainDir):
		return "the main clone"
	case filepath.Base(filepath.Dir(filepath.Dir(dir))) == "groups":
		return "group " + filepath.Base(filepath.Dir(dir))
	}
	return "worktree " + filepath.Base(dir)
}

// status is a branch's state in a few words, in columns.
func (b branchInfo) status() string {
	state := "local · origin"
	switch {
	case b.local && !b.remote:
		state = "local only"
	case !b.local:
		state = "origin only"
	}
	sync := ""
	if b.local && b.remote {
		switch u := b.upstream; {
		case u.Name == "":
			sync = "not tracking"
		case u.Ahead > 0 && u.Behind > 0:
			sync = fmt.Sprintf("↑%d ↓%d", u.Ahead, u.Behind)
		case u.Ahead > 0:
			sync = fmt.Sprintf("↑%d", u.Ahead)
		case u.Behind > 0:
			sync = fmt.Sprintf("↓%d", u.Behind)
		default:
			sync = "in sync"
		}
	}
	marks := []string{}
	if b.where != "" {
		marks = append(marks, "out in "+b.where)
	}
	if b.isDefault {
		marks = append(marks, "default")
	}
	if b.protected {
		marks = append(marks, "protected")
	}
	if !b.when.IsZero() {
		marks = append(marks, humanAge(b.when)+"  "+b.title)
	}
	return fmt.Sprintf("%-14s  %-12s  %s", state, sync, strings.Join(marks, " · "))
}

// guarded says why a branch may not be deleted at all, or "".
func (b branchInfo) guarded() string {
	switch {
	case b.isDefault:
		return b.name + " is the default branch - it is not deleted"
	case b.protected:
		return b.name + " is protected - it is not deleted"
	}
	return ""
}

// listBranches shows the branches, the cursor on scope.focus.
func (a *App) listBranches(scope branchScope, branches []branchInfo) {
	pr := scope.project
	width := 0
	for _, b := range branches {
		width = max(width, len([]rune(b.name)))
	}
	items := make([]pickItem, len(branches))
	start := 0
	for i, b := range branches {
		items[i] = pickItem{Label: fmt.Sprintf("%-*s", width, b.name), Sub: b.status(), Data: b}
		if b.name == scope.focus {
			start = i
		}
	}
	again := func(focus, done string) {
		next := scope
		next.focus, next.done = focus, done
		a.showBranchManager(next)
	}
	opts := pickerOptions{start: start, keys: []pickKey{
		{keys: "n", hint: "new", run: func(it pickItem) { a.newBranch(pr, branches, it.Data.(branchInfo).name, again) }},
		{keys: "d", hint: "delete here", run: func(it pickItem) { a.deleteBranch(pr, it.Data.(branchInfo), true, false, again) }},
		{keys: "D", hint: "everywhere", run: func(it pickItem) { a.deleteBranch(pr, it.Data.(branchInfo), true, true, again) }},
		{keys: "Alt-D", hint: "on origin", run: func(it pickItem) { a.deleteBranch(pr, it.Data.(branchInfo), false, true, again) }},
	}}
	var onSelect func(pickItem)
	if scope.checkout {
		opts.enterHint = "check out in the main clone"
		onSelect = func(it pickItem) { a.switchMainClone(pr, it.Data.(branchInfo).name, again) }
	}
	a.showPickerWith("Branches - "+pr.PathWithNamespace, items, opts, onSelect)
	if scope.done != "" {
		a.done(scope.done)
	}
}

// newBranch asks for the name of a new branch and what it grows from - the
// branch the cursor was on, to begin with - makes it in the clone, and brings
// the list back with the cursor on it.
func (a *App) newBranch(pr forge.Project, branches []branchInfo, from string, again func(focus, done string)) {
	if !a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.flash(pr.PathWithNamespace + " is not cloned - C clones it, then n makes a branch")
		return
	}
	names := make([]string, len(branches))
	selected := 0
	for i, b := range branches {
		names[i] = b.name
		if b.name == from {
			selected = i
		}
	}
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(labelBranchName, "", 0, nil, nil)
	addSelect(form, labelBranchFrom, names, selected)
	apply := func() {
		name := strings.TrimSpace(form.GetFormItemByLabel(labelBranchName).(*tview.InputField).GetText())
		_, base := form.GetFormItemByLabel(labelBranchFrom).(*tview.DropDown).GetCurrentOption()
		if name == "" {
			a.flash("enter a name for the branch")
			return
		}
		for _, b := range branches {
			if b.name == name {
				a.flash(name + " already exists - pick it in the list")
				return
			}
		}
		a.closeModal(pageForm)
		a.runTaskThen("Creating "+name, func(log func(string)) (string, error) {
			return "", a.newManager(pr.Instance, pr.PathWithNamespace, log).NewBranch(pr, name, base)
		}, func(string) {
			again(name, fmt.Sprintf("created %s from %s", name, base))
		})
	}
	form.AddButton("Create", apply)
	// Cancel goes back to the list the form was opened from.
	form.AddButton("Cancel", func() {
		a.closeModal(pageForm)
		again(from, "")
	})
	a.showFormModalSized("New branch - "+pr.PathWithNamespace, form, 64, 9)
}

// Labels of the new branch form.
const (
	labelBranchName = "Name"
	labelBranchFrom = "From"
)

// switchMainClone checks a branch out in the main clone and brings the list
// back, the branch now out there. It opens no editor: opening is Ctrl-O's,
// when the user wants it.
func (a *App) switchMainClone(pr forge.Project, branch string, again func(focus, done string)) {
	a.runTaskThen(fmt.Sprintf("Switching %s to %s", pr.PathWithNamespace, branch),
		func(log func(string)) (string, error) {
			_, err := a.newManager(pr.Instance, pr.PathWithNamespace, log).SwitchBranch(pr, branch)
			return "", err
		}, func(string) {
			a.refreshDisk()
			a.projectsPane.reload()
			again(branch, "switched the clone to "+branch)
		})
}

// openMROn is the merge request open from a branch of a repository, if any.
func (a *App) openMROn(pr forge.Project, branch string) (forge.MergeRequest, bool) {
	for _, mr := range a.mrs {
		if mr.Instance == pr.Instance && mr.SourceBranch == branch && a.projectPathOfMR(mr) == pr.PathWithNamespace {
			return mr, true
		}
	}
	return forge.MergeRequest{}, false
}

// deleteBranch deletes a branch in the clone (here), on origin (there), or
// both, once asked; what cannot be done is said instead, and nothing is done.
// Afterwards the manager comes back, the cursor where it was.
func (a *App) deleteBranch(pr forge.Project, b branchInfo, here, there bool, again func(focus, done string)) {
	if why := b.guarded(); why != "" {
		a.flash(why)
		return
	}
	// Everywhere is wherever it is.
	if here && there {
		here, there = b.local, b.remote
	}
	switch {
	case here && !b.local:
		a.flash(b.name + " is only on origin - Alt-D deletes it there")
		return
	case there && !b.remote:
		a.flash(b.name + " is not on origin")
		return
	case here && b.where != "":
		a.flash(b.name + " is checked out in " + b.where + " - git will not delete it")
		return
	}
	if there {
		if mr, ok := a.openMROn(pr, b.name); ok {
			a.flash(fmt.Sprintf("!%d is open on %s - close it first", mr.IID, b.name))
			return
		}
	}
	var where, warnings []string
	if here {
		where = append(where, "in the clone")
		if b.onlyHere > 0 {
			warnings = append(warnings, fmt.Sprintf("%d commit(s) nowhere else are lost", b.onlyHere))
		}
	}
	if there {
		where = append(where, "on origin")
		if b.local && b.upstream.Behind > 0 && !here {
			warnings = append(warnings, fmt.Sprintf("%d commit(s) only origin has are lost", b.upstream.Behind))
		}
	}
	body := fmt.Sprintf("Delete [::b]%s[::-] %s?", esc(b.name), strings.Join(where, " and "))
	switch {
	case here && !there && b.remote:
		body += "\n\norigin keeps it."
	case there && !here && b.local:
		body += "\n\nThe clone keeps it, no longer tracking origin."
	}
	a.confirm("Delete branch", body, warnings, func() {
		client := a.client(pr.Instance)
		cloned := a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned
		a.runTaskThen("Deleting "+b.name, func(log func(string)) (string, error) {
			mgr := a.newManager(pr.Instance, pr.PathWithNamespace, log)
			mainDir := mgr.ProjectDir(pr.PathWithNamespace)
			if there {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if err := client.DeleteBranch(ctx, pr, b.name); err != nil {
					return "", err
				}
				log("deleted on origin")
				if cloned {
					mgr.Git().ForgetRemoteBranch(mainDir, b.name)
				}
			}
			if here {
				if err := mgr.Git().DeleteLocalBranch(mainDir, b.name); err != nil {
					return "", err
				}
				log("deleted in the clone")
			}
			return "", nil
		}, func(string) {
			a.refreshDisk()
			again(b.name, fmt.Sprintf("deleted %s %s", b.name, strings.Join(where, " and ")))
		})
	})
}
