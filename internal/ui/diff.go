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

// D shows in Hunk the change a directory holds: a clone's uncommitted work, a
// worktree's branch since its base, a review's whole merge request. A grouped
// worktree is one review of all its repositories - Hunk reads one repository at
// a time, so their changes are put together into a single patch, each path
// under its repository's directory.

// hunkBinary is where Hunk is, once the integration is on; otherwise it says
// why D does nothing.
func (a *App) hunkBinary() (string, bool) {
	bin, err := exec.LookPath("hunk")
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
	_, err := exec.LookPath("hunk")
	return err == nil
}

// diffProject shows what is not committed in a repository's main clone.
func (a *App) diffProject(pr forge.Project) {
	dir := a.projectDir(pr.Instance, pr.PathWithNamespace)
	if !workspace.Exists(dir) {
		a.flash(pr.PathWithNamespace + " is not cloned - C clones it")
		return
	}
	if bin, ok := a.hunkBinary(); ok {
		go a.runHunk(bin, dir, "diff")
	}
}

// diffWorktree shows a worktree's branch since its base, uncommitted work
// included, or every repository of a grouped one.
func (a *App) diffWorktree(r worktreeRow) {
	bin, ok := a.hunkBinary()
	if !ok {
		return
	}
	if r.grouped() {
		a.diffGroup(bin, r)
		return
	}
	base := a.wtRemote[r.Dir].Base
	mgr := a.pathManager(r.Instance, r.Path)
	go func() {
		args := []string{"diff"}
		if from := mgr.ChangeBase(r.Dir, base); from != "HEAD" {
			args = append(args, from)
		}
		a.runHunk(bin, r.Dir, args...)
	}()
}

// diffGroup puts the changes of every member into one patch and opens it.
func (a *App) diffGroup(bin string, r worktreeRow) {
	type member struct {
		mgr       *workspace.Manager
		dir, name string
		base      string
	}
	var members []member
	for _, m := range r.Members {
		base := a.wtRemote[m.Dir].Base
		if base == "" {
			base = m.Base
		}
		members = append(members, member{a.pathManager(m.Instance, m.Path), m.Dir, filepath.Base(m.Dir), base})
	}
	go func() {
		var patch strings.Builder
		for _, m := range members {
			part, err := m.mgr.ChangePatch(m.dir, m.name+"/", m.mgr.ChangeBase(m.dir, m.base))
			if err != nil {
				a.tv.QueueUpdateDraw(func() { a.errorf("%s: %v", m.name, err) })
				return
			}
			patch.WriteString(part)
		}
		if patch.Len() == 0 {
			a.tv.QueueUpdateDraw(func() { a.note("nothing has changed in " + r.Path) })
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
		a.runHunk(bin, r.Dir, "patch", file.Name())
	}()
}

// diffMR shows a merge request's change: the review worktree holds all of it
// as unstaged edits; without one, the branch worktree against its target.
func (a *App) diffMR(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	path := project.PathWithNamespace
	review := a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	branch := a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	switch {
	case workspace.Exists(review):
		if bin, ok := a.hunkBinary(); ok {
			go a.runHunk(bin, review, "diff")
		}
	case workspace.Exists(branch):
		bin, ok := a.hunkBinary()
		if !ok {
			return
		}
		mgr := a.pathManager(mr.Instance, path)
		go func() {
			args := []string{"diff"}
			if from := mgr.ChangeBase(branch, mr.TargetBranch); from != "HEAD" {
				args = append(args, from)
			}
			a.runHunk(bin, branch, args...)
		}()
	default:
		a.flash(fmt.Sprintf("!%d is not on disk - C makes its review", mr.IID))
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
