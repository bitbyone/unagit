package ui

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// The CI column of Repositories and Worktrees is the newest pipeline of the
// branch the row shows - the branch's own, even when a merge request is open
// from it: that one's pipeline is in the merge request list, and the two
// columns say two things rather than one twice. It is asked for on a
// refresh and kept in an index of its own, so the column is there at start
// as the lists are. A repository not cloned has no branch of the user's and
// is not asked about; a grouped worktree holds several repositories and
// says nothing here.

// branchKey names a branch of a repository on a server.
type branchKey struct{ instance, path, branch string }

// branchCIStatus is the status of a branch's newest pipeline, "" when there
// is none or it was never read.
func (a *App) branchCIStatus(instance, path, branch string) string {
	if branch == "" || branch == "(detached)" {
		return ""
	}
	return a.branchStatus[branchKey{instance, path, branch}]
}

// repositoryBranch is the clone's branch, "" before it is cloned.
func (a *App) repositoryBranch(pr forge.Project) string {
	if info := a.diskOf(pr.Instance, pr.PathWithNamespace); info.Cloned {
		return info.Branch
	}
	return ""
}

// repositoryCI is the CI column of a repository's row.
func (a *App) repositoryCI(pr forge.Project) string {
	return a.branchCIStatus(pr.Instance, pr.PathWithNamespace, a.repositoryBranch(pr))
}

// worktreeCI is the CI column of a worktree's row; nothing for a group.
func (a *App) worktreeCI(r worktreeRow) string {
	if r.grouped() {
		return ""
	}
	return a.branchCIStatus(r.Instance, r.Path, r.Branch)
}

// repositoryCITargets is the branches of the clones the repository list
// shows.
func (a *App) repositoryCITargets() []branchKey {
	var out []branchKey
	for _, i := range a.filterProjects(a.projects, "") {
		pr := a.projects[i]
		if branch := a.repositoryBranch(pr); branch != "" {
			out = append(out, branchKey{pr.Instance, pr.PathWithNamespace, branch})
		}
	}
	return out
}

// worktreeCITargets is the branches of the worktrees the list shows, the
// groups left out.
func (a *App) worktreeCITargets() []branchKey {
	var out []branchKey
	for _, r := range a.worktrees {
		if !r.grouped() && !a.cfg.Filters.IsHidden(r.Instance, r.Path) {
			out = append(out, branchKey{r.Instance, r.Path, r.Branch})
		}
	}
	return out
}

// askBranchCI reads the newest pipeline of those branches, in the
// background, several at a time, and leaves out the ones being asked about
// already. A non-empty title
// shows it as a job. What cannot be read keeps what was last read: it is a
// mark in a column, not something to fail on.
func (a *App) askBranchCI(keys []branchKey, title string) {
	type question struct {
		key     branchKey
		project forge.Project
		client  forge.Provider
	}
	if a.ciAsking == nil {
		a.ciAsking = map[branchKey]bool{}
	}
	var qs []question
	for _, k := range keys {
		if k.branch == "" || k.branch == "(detached)" || a.ciAsking[k] {
			continue
		}
		client := a.client(k.instance)
		if client == nil {
			continue
		}
		a.ciAsking[k] = true
		pr := a.worktreeProject(worktreeRow{Instance: k.instance, Path: k.path})
		qs = append(qs, question{k, pr, client})
	}
	if len(qs) == 0 {
		return
	}
	var job *bgJob
	if title != "" {
		job = a.startJob(title)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		status := make([]string, len(qs))
		read := make([]bool, len(qs))
		sem := make(chan struct{}, extrasFanOut)
		var wg sync.WaitGroup
		var finished atomic.Int32
		for i, q := range qs {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() {
					<-sem
					if job != nil {
						a.jobProgress(job, fmt.Sprintf("%d/%d", finished.Add(1), len(qs)))
					}
					wg.Done()
				}()
				p, err := q.client.LatestPipeline(ctx, q.project, q.key.branch)
				if err != nil {
					return
				}
				read[i] = true
				if p != nil {
					status[i] = p.Status
				}
			}()
		}
		wg.Wait()
		a.tv.QueueUpdateDraw(func() {
			if job != nil {
				a.endJob(job)
			}
			if a.branchStatus == nil {
				a.branchStatus = map[branchKey]string{}
			}
			for i, q := range qs {
				delete(a.ciAsking, q.key)
				switch {
				case !read[i]:
				case status[i] == "":
					delete(a.branchStatus, q.key)
				default:
					a.branchStatus[q.key] = status[i]
				}
			}
			a.saveBranchCI()
			a.projectsPane.reload()
			a.worktreesPane.reload()
			a.watchCI()
		})
	}()
}

// loadBranchCI reads the pipelines last read of the branches.
func (a *App) loadBranchCI() {
	idx, err := index.Load[index.Pipelines](a.cfg.IndexPath("pipelines"))
	if err != nil {
		return
	}
	a.branchStatus = make(map[branchKey]string, len(idx.Items))
	for _, p := range idx.Items {
		a.branchStatus[branchKey{p.Instance, p.Project, p.Branch}] = p.Status
	}
}

// saveBranchCI writes them down for the next start.
func (a *App) saveBranchCI() {
	items := make([]index.BranchPipeline, 0, len(a.branchStatus))
	for k, status := range a.branchStatus {
		items = append(items, index.BranchPipeline{Instance: k.instance, Project: k.path, Branch: k.branch, Status: status})
	}
	// In a steady order, so the file changes only when a status does.
	sort.Slice(items, func(i, j int) bool {
		l, r := items[i], items[j]
		if l.Instance != r.Instance {
			return l.Instance < r.Instance
		}
		if l.Project != r.Project {
			return l.Project < r.Project
		}
		return l.Branch < r.Branch
	})
	_ = index.Save(a.cfg.IndexPath("pipelines"), index.Pipelines{UpdatedAt: time.Now(), Items: items})
}
