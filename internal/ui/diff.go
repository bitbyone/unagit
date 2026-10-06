package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

// D shows in Hunk what a row's working tree has not committed; Alt-D
// everything since the branch's base - its commits and what is not committed.
// One commit at a time is the commit log's (Ctrl-L). A grouped worktree
// is one review of all its repositories: Hunk reads one repository at a time,
// so their changes are put together into a single patch, each path under its
// repository's folder.

func (a *App) executable(name string) (string, error) {
	if a.findExecutable != nil {
		return a.findExecutable(name)
	}
	return exec.LookPath(name)
}

// hunkBinary is where Hunk is, once the integration is on; otherwise it says
// why D does nothing.
func (a *App) hunkBinary() (string, bool) {
	bin, err := a.executable("hunk")
	switch {
	case err != nil:
		a.flash("hunk is not on PATH - install it, see Settings › Integrations")
		return "", false
	case !a.hunkOn():
		a.flash("D needs Hunk - enable it in Settings › Integrations")
		return "", false
	}
	return bin, true
}

// hunkOn says whether the Hunk integration is on: as chosen in Settings, or,
// until something was chosen, whenever hunk is installed.
func (a *App) hunkOn() bool {
	if on := a.cfg.Integrations.Hunk; on != nil {
		return *on
	}
	_, err := a.executable("hunk")
	return err == nil
}

// diffTarget is one working tree D can show: name tells it apart in a grouped
// worktree, base is the branch its change is measured from, "" when unknown.
type diffTarget struct {
	mgr  *workspace.Manager
	dir  string
	name string
	base string
}

// diffView is one thing Hunk is shown: hunk with args in dir, or - for a
// grouped worktree - one patch of all its repositories.
type diffView struct {
	dir     string
	args    []string
	targets []diffTarget // a patch of these, each from HEAD or from its base
	since   bool
}

// diffKey is D: always what is not committed - staged, unstaged and new files.
// In a review worktree that is the whole merge request.
func (a *App) diffKey(dir string, targets []diffTarget) {
	bin, ok := a.hunkBinary()
	if !ok {
		return
	}
	v := diffView{dir: dir, args: []string{"diff"}}
	if len(targets) > 1 || len(targets) == 1 && targets[0].name != "" {
		v = diffView{dir: dir, targets: targets}
	}
	go a.runView(bin, v)
}

// diffSince is Alt-D: everything since the base - the branch's commits and
// what is not committed - in one view, a grouped worktree's repositories
// together. Finding the base takes git, so it runs off the event loop.
func (a *App) diffSince(dir string, targets []diffTarget) {
	bin, ok := a.hunkBinary()
	if !ok {
		return
	}
	grouped := len(targets) > 1 || len(targets) == 1 && targets[0].name != ""
	go func() {
		froms := make([]string, len(targets))
		known := false
		for i, t := range targets {
			froms[i] = t.mgr.ChangeBase(t.dir, t.base)
			known = known || froms[i] != "HEAD"
		}
		if !known {
			a.tv.QueueUpdateDraw(func() { a.flash("no base known to measure from - D shows what is not committed") })
			return
		}
		v := diffView{dir: dir, args: []string{"diff", froms[0]}}
		if grouped {
			v = diffView{dir: dir, targets: targets, since: true}
		}
		a.runView(bin, v)
	}()
}

// runView shows one view in Hunk. A grouped worktree's is put together first:
// every repository's change in one patch, each path under its folder.
func (a *App) runView(bin string, v diffView) {
	if v.targets == nil {
		a.runHunk(bin, v.dir, v.args...)
		return
	}
	var patch strings.Builder
	for _, t := range v.targets {
		from := "HEAD"
		if v.since {
			from = t.mgr.ChangeBase(t.dir, t.base)
		}
		part, err := t.mgr.ChangePatch(t.dir, t.name+"/", from)
		if err != nil {
			a.tv.QueueUpdateDraw(func() { a.errorf("%s: %v", t.name, err) })
			return
		}
		patch.WriteString(part)
	}
	if patch.Len() == 0 {
		a.tv.QueueUpdateDraw(func() { a.note("nothing has changed here") })
		return
	}
	file, err := os.CreateTemp("", "unagit-*.patch")
	if err == nil {
		_, err = file.WriteString(patch.String())
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("cannot write the patch: %v", err) })
		return
	}
	defer os.Remove(file.Name())
	a.runHunk(bin, v.dir, "patch", file.Name())
}

// projectDiff is a clone measured against its upstream: what is not on
// origin yet.
func (a *App) projectDiff(pr forge.Project) (string, []diffTarget, bool) {
	dir := a.projectDir(pr.Instance, pr.PathWithNamespace)
	if !workspace.Exists(dir) {
		a.flash(pr.PathWithNamespace + " is not cloned - C clones it")
		return "", nil, false
	}
	base := strings.TrimPrefix(a.repoSync[projectKey{pr.Instance, pr.PathWithNamespace}].Upstream.Name, "origin/")
	return dir, []diffTarget{{mgr: a.pathManager(pr.Instance, pr.PathWithNamespace), dir: dir, base: base}}, true
}

// worktreeDiff is a worktree measured against the branch it was made from,
// or every member of a grouped one against theirs.
func (a *App) worktreeDiff(r worktreeRow) (string, []diffTarget) {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	var targets []diffTarget
	for _, m := range members {
		base := a.wtRemote[m.Dir].Base
		if base == "" {
			base = m.Base
		}
		t := diffTarget{mgr: a.pathManager(m.Instance, m.Path), dir: m.Dir, base: base}
		if r.grouped() {
			t.name = filepath.Base(m.Dir)
		}
		targets = append(targets, t)
	}
	return r.Dir, targets
}

// diffMR shows a merge request: its review worktree, where the whole change is
// not committed, or its branch worktree against the target.
func (a *App) diffMR(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	path := project.PathWithNamespace
	mgr := a.pathManager(mr.Instance, path)
	review := a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	branch := a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	switch {
	case workspace.Exists(review):
		a.diffKey(review, nil)
	case workspace.Exists(branch):
		a.diffKey(branch, []diffTarget{{mgr: mgr, dir: branch, base: mr.TargetBranch}})
	default:
		// Nothing on disk yet: make the review, as C would - the repository
		// cloned if it has to be - and show it, so going down the list with
		// D does not stop to clone first.
		if _, ok := a.hunkBinary(); !ok {
			return
		}
		client := a.client(mr.Instance)
		integrate := a.cfg.Integrations.Incomm
		a.runTaskThen(fmt.Sprintf("Preparing %s !%d for Hunk", path, mr.IID), func(log func(string)) (string, error) {
			return a.prepareReview(mr, project, client, "", integrate, log)
		}, func(dir string) {
			a.refreshDisk()
			a.mrsPane.reload()
			a.projectsPane.reload()
			if !workspace.Exists(dir) {
				a.flash(fmt.Sprintf("!%d could not be put on disk", mr.IID))
				return
			}
			a.diffMR(mr)
		})
	}
}

// runHunk hands Hunk the terminal the way a terminal editor gets it, and reads
// the disk again afterwards. It runs off the event loop.
func (a *App) runHunk(bin, dir string, args ...string) {
	a.tv.Suspend(func() {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "hunk failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "press enter to return to unagit")
			var s string
			fmt.Scanln(&s)
		}
	})
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })
}
