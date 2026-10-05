package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// r refreshes the row under the cursor - asks about one merge request, fetches
// one clone or one worktree - and R the whole list. Waiting for a pipeline or
// for a fix to be pushed is about one row, and the whole list takes a while.

// refreshMRRow asks the server about one merge request: its state and head,
// and what a full refresh reads besides - pipeline, approvals, threads. One
// merged or closed since leaves the list, its worktrees tidied as R would.
func (a *App) refreshMRRow(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(mr.Instance))
		return
	}
	job := a.startJob(fmt.Sprintf("refreshing !%d", mr.IID))
	a.fetchMR(client, mr, true, func() { a.endJob(job) })
}

// refetchMR is refreshMRRow without a word on screen, for an action that has
// just said what it did and wants the row to follow.
func (a *App) refetchMR(client forge.Provider, mr forge.MergeRequest) {
	a.fetchMR(client, mr, false, func() {})
}

// fetchMR asks about one merge request and puts the answer in the list; say
// adds that it is up to date once that is known. ended runs on the event
// loop once the answer is in, or the question failed.
func (a *App) fetchMR(client forge.Provider, mr forge.MergeRequest, say bool, ended func()) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		det, err := client.MergeRequestDetail(ctx, mr)
		if err != nil {
			a.tv.QueueUpdateDraw(func() {
				ended()
				a.errorf("!%d: %v", mr.IID, err)
			})
			return
		}
		fresh := []forge.MergeRequest{withDetail(mr, det)}
		mrExtras(ctx, fresh, []int{0}, map[string]forge.Provider{mr.Instance: client}, nil)
		a.tv.QueueUpdateDraw(func() {
			ended()
			a.applyMRRefresh(fresh[0], say)
		})
	}()
}

// applyMRRefresh puts one merge request as the server now has it into the
// list, or takes it out when it is no longer open.
func (a *App) applyMRRefresh(fresh forge.MergeRequest, say bool) {
	if state := strings.ToLower(fresh.State); state != "" && state != "opened" && state != "open" {
		kept := a.mrs[:0]
		for _, m := range a.mrs {
			if keyOfMR(m) != keyOfMR(fresh) {
				kept = append(kept, m)
			}
		}
		a.mrs = kept
		// The marks point into the list as it was.
		a.mrsPane.marks = nil
		a.saveMRIndex()
		a.forgetSeen(fresh)
		a.mrsPane.reload()
		key := projectKey{fresh.Instance, a.projectPathOfMR(fresh)}
		a.tidy(map[projectKey][]int{key: {fresh.IID}}, func(removed, held []string) {
			a.refreshDisk()
			switch {
			case len(held) > 0:
				a.flash(fmt.Sprintf("!%d is %s · kept its worktrees: %s", fresh.IID, state, strings.Join(held, ", ")))
			case len(removed) > 0:
				a.done(fmt.Sprintf("!%d is %s · its worktrees are removed", fresh.IID, state))
			default:
				a.done(fmt.Sprintf("!%d is %s, and leaves the list", fresh.IID, state))
			}
		})
		return
	}
	a.applyMRUpdate(fresh, true, false)
	if say {
		a.afterFresh = func() { a.done(fmt.Sprintf("!%d is up to date", fresh.IID)) }
	}
	a.refreshDisk()
}

// saveMRIndex writes the merge request index as the list now has it, the
// time of the last full refresh kept.
func (a *App) saveMRIndex() {
	_ = index.Save(a.cfg.IndexPath("mrs"), index.MergeRequests{
		Version: index.Version, UpdatedAt: a.mrsUpdated, Items: a.mrs, Me: a.me})
}

// refreshProjectRow fetches one clone and reads again where it stands.
func (a *App) refreshProjectRow(pr forge.Project) {
	key := projectKey{pr.Instance, pr.PathWithNamespace}
	if !a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.note(pr.PathWithNamespace + " is not cloned - nothing to fetch; R refreshes the list")
		return
	}
	dir := a.projectDir(pr.Instance, pr.PathWithNamespace)
	git := a.newManager(pr.Instance, pr.PathWithNamespace, nil).Git()
	a.addFetching(1)
	a.reloadProjectsHeader()
	go func() {
		err := git.Fetch(dir)
		a.tv.QueueUpdateDraw(func() {
			a.addFetching(-1)
			if a.fetchFailed == nil {
				a.fetchFailed = map[projectKey]string{}
			}
			if err != nil {
				a.fetchFailed[key] = firstLine(err.Error())
				a.errorf("fetching %s: %s", pr.PathWithNamespace, firstLine(err.Error()))
			} else {
				delete(a.fetchFailed, key)
				a.done("fetched " + pr.PathWithNamespace)
			}
			a.refreshDisk()
			a.projectsPane.reload()
		})
	}()
}

// refreshWorktreeRow fetches the repository of one worktree - each of a
// group's - and brings in the comments of its merge request.
func (a *App) refreshWorktreeRow(r worktreeRow) {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	type fetch struct {
		dir  string
		name string
		git  interface{ Fetch(string) error }
	}
	var fetches []fetch
	for _, m := range members {
		fetches = append(fetches, fetch{m.Dir, m.Path, a.newManager(m.Instance, m.Path, nil).Git()})
	}
	a.addFetching(len(fetches))
	a.reloadWorktreesHeader()
	go func() {
		var failed []string
		for _, f := range fetches {
			if err := f.git.Fetch(f.dir); err != nil {
				failed = append(failed, f.name+": "+firstLine(err.Error()))
			}
		}
		a.tv.QueueUpdateDraw(func() {
			a.addFetching(-len(fetches))
			a.reloadWorktreesHeader()
			finish := func() {
				a.refreshDisk()
				if len(failed) > 0 {
					a.errorf("fetching %s", strings.Join(failed, "; "))
					return
				}
				a.done("fetched " + r.Path)
			}
			if !a.cfg.Integrations.Incomm {
				finish()
				return
			}
			a.syncWorktreeComments([]worktreeRow{r}, finish)
		})
	}()
}
