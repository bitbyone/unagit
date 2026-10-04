package ui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/session"
)

// Places lists everything unagit has on disk - clones, the worktrees of merge
// requests, other worktrees and grouped ones - for unagit go. It reads the
// indexes and the disk only: no passphrase, no server, nothing drawn.
//
// Each is a session.Record, so the picker unagit cd uses can show it: the
// mode says what kind of place it is, the title the branch or the merge
// request's title.
func Places(cfg *config.Config) []session.Record {
	a := &App{cfg: cfg}
	a.loadIndexes()
	if a.chezmoiOn() {
		if checkout, err := findChezmoiCheckout(); err == nil {
			a.chezmoi.Store(&chezmoiState{checkout: checkout})
			a.pairChezmoi()
		}
	}
	disk, worktrees := a.scanDisk(false)

	titles := map[placeKey]string{}
	for _, mr := range a.mrs {
		titles[placeKey{mr.Instance, a.projectPathOfMR(mr), mr.IID}] = mr.Title
	}
	record := func(instance, project, mode, dir string, iid int, title string) session.Record {
		return session.Record{Instance: instance, Server: a.instanceLabel(instance), Project: project,
			Mode: mode, Dir: dir, IID: iid, Title: title}
	}

	var out []session.Record
	for key, info := range disk {
		if info.Cloned {
			out = append(out, record(key.Instance, key.Path, session.ModeRepository,
				a.projectDir(key.Instance, key.Path), 0, info.Branch))
		}
		for iid, d := range info.MRs {
			title := titles[placeKey{key.Instance, key.Path, iid}]
			if d.ReviewDir != "" {
				out = append(out, record(key.Instance, key.Path, session.ModeReview, d.ReviewDir, iid, title))
			}
			if d.BranchDir != "" {
				out = append(out, record(key.Instance, key.Path, session.ModeBranch, d.BranchDir, iid, title))
			}
		}
	}
	for _, w := range worktrees {
		if isGroupRow(w) {
			var names []string
			for _, m := range w.Members {
				names = append(names, filepath.Base(m.Dir))
			}
			out = append(out, record(w.Instance, w.Path, session.ModeGroup, w.Dir, 0, strings.Join(names, " · ")))
			continue
		}
		out = append(out, record(w.Instance, w.Path, session.ModeBranch, w.Dir, 0, w.Branch))
	}

	// The repository first, then what hangs off it, the merge requests in
	// order; within that a review before the branch.
	rank := map[string]int{session.ModeRepository: 0, session.ModeReview: 1, session.ModeBranch: 2, session.ModeGroup: 3}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if x.Project != y.Project {
			return x.Project < y.Project
		}
		if x.IID != y.IID {
			return x.IID < y.IID
		}
		if rank[x.Mode] != rank[y.Mode] {
			return rank[x.Mode] < rank[y.Mode]
		}
		return x.Dir < y.Dir
	})
	return out
}

// placeKey names a merge request by the repository's path, which is what
// the disk knows it by.
type placeKey struct {
	instance, path string
	iid            int
}

// isGroupRow tells a grouped worktree's row from a plain worktree's: groupRow
// gives it a list of members, empty or not.
func isGroupRow(w worktreeRow) bool { return w.Members != nil }
