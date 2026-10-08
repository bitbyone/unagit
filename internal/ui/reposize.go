package ui

import (
	"path/filepath"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/workspace"
)

// A repository on disk is more than its clone: every worktree of it - a
// merge request's, a review's, a branch's, one in a grouped folder - is a
// folder of its own beside it, sharing the clone's objects. Repositories'
// SIZE is all of it together, measured behind the interface when the disk
// is first looked at and again as the last step of a refresh - but only
// while the list shows it, in its SIZE column or its order: measuring every
// clone costs a stutter while moving through the list, for nothing when
// nobody looks.

// repoDirs is the folders a repository takes: the clone, and each worktree
// that is not inside a folder already counted.
func repoDirs(clone string) []string {
	dirs := []string{clone}
	for _, dir := range gitx.New("", nil).WorktreeDirs(clone) {
		inside := false
		for _, counted := range dirs {
			if dir == counted || strings.HasPrefix(dir, counted+string(filepath.Separator)) {
				inside = true
				break
			}
		}
		if !inside {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// repoUsage is what a repository takes on disk, everything of it counted.
func repoUsage(clone string) int64 {
	var total int64
	for _, dir := range repoDirs(clone) {
		total += workspace.DiskUsage(dir)
	}
	return total
}

// loadRepoSizes measures the cloned repositories not yet measured - every
// one again when again - two at a time, as one job on the status line.
func (a *App) loadRepoSizes(again bool) {
	if a.repoSize == nil {
		a.repoSize, a.repoSizing = map[projectKey]int64{}, map[projectKey]bool{}
	}
	if !a.wantsSizes(config.ListRepositories) {
		return
	}
	type job struct {
		key projectKey
		dir string
	}
	var jobs []job
	for key, info := range a.disk {
		if !info.Cloned {
			continue
		}
		if _, known := a.repoSize[key]; a.repoSizing[key] || known && !again {
			continue
		}
		a.repoSizing[key] = true
		jobs = append(jobs, job{key, a.projectDir(key.Instance, key.Path)})
	}
	if len(jobs) == 0 {
		return
	}
	measuring := a.startJob("measuring repositories")
	left := len(jobs)
	go func() {
		// The disk, not the processor, is what they wait for.
		sem := make(chan struct{}, 2)
		for _, j := range jobs {
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				n := repoUsage(j.dir)
				a.tv.QueueUpdateDraw(func() {
					a.repoSize[j.key] = n
					delete(a.repoSizing, j.key)
					if left--; left == 0 {
						a.endJob(measuring)
					}
					if a.projectsPane != nil && a.projectsPane.reload != nil {
						a.projectsPane.reload()
					}
				})
			}()
		}
	}()
}

// wantsSizes says whether a list shows what its rows take on disk, in its
// SIZE column or its order.
func (a *App) wantsSizes(list string) bool {
	return !a.hidesColumn(list, "size") || a.order(list) == config.SortSize
}

// repoSizeWords is the SIZE column of a repository: what it takes on disk,
// "…" while it is being measured, "" when it is not cloned.
func (a *App) repoSizeWords(key projectKey) string {
	if !a.diskOf(key.Instance, key.Path).Cloned {
		return ""
	}
	n, known := a.repoSize[key]
	if !known {
		return "…"
	}
	return humanBytes(n)
}

// repoSizeRange is the least and the most a measured repository takes.
func (a *App) repoSizeRange() (least, most int64) {
	for _, n := range a.repoSize {
		if n <= 0 {
			continue
		}
		if least == 0 || n < least {
			least = n
		}
		most = max(most, n)
	}
	return least, most
}
