package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/incomm"
	"github.com/tobola/unagit/internal/workspace"
)

// cleanUpClosed removes the worktrees of merge requests that are no longer
// open - merged or closed, gone from the list the refresh brought - without
// asking: they were made for a merge request, and it is over. One that holds
// work of the user's is kept and named instead: uncommitted or unpushed
// changes, a reviewer's own edits, comments not yet published. Only servers
// that were asked are looked at, and only repositories the index knows, so a
// merge request is never taken for closed because it was not looked for.
func (a *App) cleanUpClosed(instances []config.Instance, open []forge.MergeRequest, then func(removed, kept []string)) {
	asked := map[string]bool{}
	for _, inst := range instances {
		asked[inst.ID] = true
	}
	type mrAt struct {
		key projectKey
		iid int
	}
	isOpen := map[mrAt]bool{}
	for _, mr := range open {
		isOpen[mrAt{projectKey{mr.Instance, a.projectPathOfMR(mr)}, mr.IID}] = true
	}
	closed := map[projectKey][]int{}
	for key, info := range a.disk {
		if !asked[key.Instance] {
			continue
		}
		if _, known := a.projByKey[key]; !known {
			continue
		}
		for iid := range info.MRs {
			if !isOpen[mrAt{key, iid}] {
				closed[key] = append(closed[key], iid)
			}
		}
	}
	if len(closed) == 0 {
		then(nil, nil)
		return
	}
	go func() {
		var removed, kept []string
		for key, iids := range closed {
			mgr := a.newManager(key.Instance, key.Path, nil)
			gone := map[int]bool{}
			for _, iid := range iids {
				gone[iid] = true
			}
			for _, entry := range mgr.WorktreeEntries(key.Path) {
				if !gone[entry.IID] {
					continue
				}
				name := fmt.Sprintf("%s !%d", key.Path, entry.IID)
				if why := workOf(mgr, entry.Dirs); why != "" {
					kept = append(kept, name+" ("+why+")")
					continue
				}
				failed := false
				for _, dir := range entry.Dirs {
					if err := mgr.RemoveWorktreeDir(key.Path, dir); err != nil {
						failed = true
					}
				}
				if failed {
					kept = append(kept, name+" (could not be removed)")
				} else {
					removed = append(removed, name)
				}
			}
		}
		sort.Strings(removed)
		sort.Strings(kept)
		a.tv.QueueUpdateDraw(func() {
			a.mrsPane.reload()
			a.projectsPane.reload()
			a.worktreesPane.reload()
			then(removed, kept)
		})
	}()
}

// workOf says what of the user's is in a merge request's worktrees, or "".
// A review counts the reviewer's own edits, as deleting one does. A branch
// worktree counts uncommitted changes and commits its upstream lacks - but
// not a missing upstream: the source branch of a merged merge request is
// usually deleted, and that is no work of anyone's to keep.
func workOf(mgr *workspace.Manager, dirs []string) string {
	for _, dir := range dirs {
		if mgr.ReadMeta(dir).Mode == workspace.ModeReview {
			if w := mgr.InspectDir(dir).Warnings; len(w) > 0 {
				return w[0]
			}
		} else {
			st := mgr.Git().Status(dir)
			switch {
			case st.Dirty:
				return fmt.Sprintf("%d uncommitted change(s)", st.DirtyFiles)
			case !st.NoUpstream && st.Unpushed > 0:
				return fmt.Sprintf("%d unpushed commit(s)", st.Unpushed)
			}
		}
		if n := incomm.PendingIn(dir); n > 0 {
			return fmt.Sprintf("%d comment(s) not published", n)
		}
	}
	return ""
}

// sayRefreshed sums up what a refresh of the merge requests brought: what is
// new, what was pushed since your review, what failed, and what was tidied
// away - so the list says where to look first. Anything kept that could not
// be tidied makes it a warning.
func (a *App) sayRefreshed(before map[mrKey]bool, open []forge.MergeRequest, removed, kept []string) {
	var parts []string
	added, failed := 0, 0
	for _, mr := range open {
		if len(before) > 0 && !before[keyOfMR(mr)] {
			added++
		}
		if mr.Pipeline == "failed" {
			failed++
		}
	}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d new", added))
	}
	if n := len(a.mrFresh); n > 0 {
		parts = append(parts, fmt.Sprintf("%d with commits since your review", n))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d pipeline(s) failed", failed))
	}
	if len(removed) > 0 {
		parts = append(parts, "removed the worktrees of closed "+strings.Join(removed, ", "))
	}
	if len(kept) > 0 {
		parts = append(parts, "kept the worktrees of closed "+strings.Join(kept, ", "))
	}
	if len(parts) == 0 {
		a.done(fmt.Sprintf("%d open merge request(s), nothing new", len(open)))
		return
	}
	msg := fmt.Sprintf("%d open merge request(s): ", len(open)) + strings.Join(parts, " · ")
	if len(kept) > 0 {
		a.flash(msg)
		return
	}
	a.done(msg)
}
