package ui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// A pipeline under way turns its mark in the CI column, and the lists,
// which are otherwise read only on a refresh, follow it: the merge
// requests the list shows whose pipeline is running, and the branches of
// Repositories and Worktrees whose is, are asked about again every so
// often, and only those, until each has ended. Then the watching stops,
// until a pipeline is seen running again.

// ciTurnEvery is how often the mark of a running pipeline turns.
const ciTurnEvery = 300 * time.Millisecond

// ciAskEvery is how often a running pipeline is asked about, unless the App
// says otherwise (App.ciAskEvery; tests make it short).
const ciAskEvery = 10 * time.Second

// runningMRs is the merge requests the list shows whose pipeline is under
// way. It runs on the event loop.
func (a *App) runningMRs() []forge.MergeRequest {
	var out []forge.MergeRequest
	for _, mr := range a.mrs {
		if ciStateOf(mr.Pipeline) == ciRunning &&
			a.passesFilters(mr.Instance, a.projectPathOfMR(mr)) && a.passesMRFilters(mr) {
			out = append(out, mr)
		}
	}
	return out
}

// runningBranches is the branches the lists show whose newest pipeline was
// last seen running; one the index still holds from an earlier branch of a
// clone is not followed. It runs on the event loop.
func (a *App) runningBranches() []branchKey {
	var out []branchKey
	for _, k := range append(a.repositoryCITargets(), a.worktreeCITargets()...) {
		if ciStateOf(a.branchStatus[k]) == ciRunning {
			out = append(out, k)
		}
	}
	return out
}

// watchCI starts the watching when a shown pipeline is running and it is
// not watched already. It runs on the event loop and is cheap to call.
func (a *App) watchCI() {
	if a.ciWatching || len(a.runningMRs()) == 0 && len(a.runningBranches()) == 0 && a.watchesRunning() == 0 && len(a.ciRetries) == 0 {
		return
	}
	a.ciWatching = true
	go a.followCI()
}

// followCI turns the marks and asks again about the running pipelines,
// until none is left.
func (a *App) followCI() {
	every := a.ciAskInterval()
	ticker := time.NewTicker(ciTurnEvery)
	defer ticker.Stop()
	asked := time.Now()
	asking := map[mrKey]bool{}
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok := a.onLoopWait(ctx, func() {
			// What the server stopped answering about is shown as unknown,
			// and asked about at its own times until its backoff ends.
			running := append(a.runningMRs(), a.retryingMRs()...)
			branches := append(a.runningBranches(), a.retryingBranches()...)
			watched := a.watchesRunning()
			if len(running) == 0 && len(branches) == 0 && watched == 0 {
				a.ciWatching = false
				a.drawTabs()
				return
			}
			if p := a.ciPane(); p != nil && p.reload != nil {
				p.reload()
			}
			if watched > 0 {
				a.drawTabs()
			}
			// What is watched is asked about by the watch, which hands it
			// to the lists (shareWatchStates); asking twice costs requests.
			running = slices.DeleteFunc(running, a.mrWatched)
			branches = slices.DeleteFunc(branches, func(k branchKey) bool { return a.branchWatched(k.instance, k.path, k.branch) })
			usual := time.Since(asked) >= every
			if usual {
				asked = time.Now()
			}
			branches = slices.DeleteFunc(branches, func(k branchKey) bool { return !a.ciDue(k, usual) })
			a.askBranchCI(branches, "")
			for _, mr := range running {
				key := keyOfMR(mr)
				client := a.client(mr.Instance)
				if client == nil || asking[key] || !a.ciDue(key, usual) {
					continue
				}
				asking[key] = true
				a.pollMR(client, mr, func() { delete(asking, key) })
			}
		})
		cancel()
		if !ok {
			return
		}
		// Asked for on the loop above, the turn is drawn here.
		stop := make(chan bool, 1)
		a.tv.QueueUpdateDraw(func() { stop <- !a.ciWatching })
		select {
		case done := <-stop:
			if done {
				return
			}
		case <-time.After(5 * time.Second):
			return
		}
	}
}

// ciPane is the list in front when it has a CI column, nil otherwise.
func (a *App) ciPane() *pane {
	switch a.currentTab() {
	case pageMRs:
		return a.mrsPane
	case pageProjects:
		return a.projectsPane
	case pageWorktrees:
		return a.worktreesPane
	case pageActivity:
		return a.activityPane
	}
	return nil
}

// turnWhile draws again, every turn of a running mark, what has one: a
// dialog listing jobs or pipelines, a running job's log. It stops when the
// dialog is gone, and while nothing shown runs it draws nothing. open,
// running and redraw run on the event loop.
func (a *App) turnWhile(open, running func() bool, redraw func()) {
	ticker := time.NewTicker(ciTurnEvery)
	defer ticker.Stop()
	for range ticker.C {
		state := make(chan [2]bool, 1)
		a.tv.QueueUpdate(func() { state <- [2]bool{open(), open() && running()} })
		select {
		case s := <-state:
			if !s[0] {
				return
			}
			if s[1] {
				a.tv.QueueUpdateDraw(func() {
					if open() {
						redraw()
					}
				})
			}
		case <-time.After(5 * time.Second):
			return
		}
	}
}

// ciUnknown is the state of a pipeline the server stopped answering
// about: drawn as a question mark, and no longer taken for running.
const ciUnknown = "unknown"

// ciRetryAfter are the waits after each failure in a row to ask about a
// running pipeline, in units of the asking interval: twice more soon, then
// longer and longer. Past the last the pipeline is not asked about again;
// a refresh asks. ciUnknownAfter is the failures in a row after which it
// is shown as unknown.
var ciRetryAfter = []int{1, 1, 2, 3, 5, 8}

const ciUnknownAfter = 3

// ciRetry is a pipeline the server did not answer about: how many times in
// a row, and when it is asked next.
type ciRetry struct {
	failures int
	next     time.Time
}

// ciDue reports whether something the polling follows may be asked about
// now: at its own time once the server has failed it, at the usual one
// otherwise.
func (a *App) ciDue(key any, usual bool) bool {
	if r, ok := a.ciRetries[key]; ok {
		return !time.Now().Before(r.next)
	}
	return usual
}

// ciOutcome counts what the polling learnt of one pipeline. An answer ends
// its backoff. A failure is said on the status line - never in front, it
// is the background's - and asked again later, shown as unknown from the
// third in a row, and given up after the last wait; what is given up the
// polling leaves to a refresh. It runs on the event loop.
func (a *App) ciOutcome(key any, answered bool, what string, unknown func()) {
	if answered {
		delete(a.ciRetries, key)
		return
	}
	if a.ciRetries == nil {
		a.ciRetries = map[any]*ciRetry{}
	}
	r := a.ciRetries[key]
	if r == nil {
		r = &ciRetry{}
		a.ciRetries[key] = r
	}
	r.failures++
	if r.failures == ciUnknownAfter {
		unknown()
	}
	if r.failures > len(ciRetryAfter) {
		delete(a.ciRetries, key)
		a.note(what + ": the server does not answer - its pipeline is not asked about again until r refreshes it")
		return
	}
	wait := time.Duration(ciRetryAfter[r.failures-1]) * a.ciAskInterval()
	r.next = time.Now().Add(wait)
	a.note(fmt.Sprintf("%s: the server does not answer - asking again in %s", what, wait.Round(time.Second)))
}

// ciAskInterval is how often a running pipeline is asked about.
func (a *App) ciAskInterval() time.Duration {
	if a.ciAskEvery != 0 {
		return a.ciAskEvery
	}
	return ciAskEvery
}

// retryingMRs are the merge requests shown as unknown that the backoff
// still asks about.
func (a *App) retryingMRs() []forge.MergeRequest {
	var out []forge.MergeRequest
	for _, mr := range a.mrs {
		if _, ok := a.ciRetries[keyOfMR(mr)]; ok && mr.Pipeline == ciUnknown {
			out = append(out, mr)
		}
	}
	return out
}

// retryingBranches is the same of the branches.
func (a *App) retryingBranches() []branchKey {
	var out []branchKey
	for key := range a.ciRetries {
		if k, ok := key.(branchKey); ok && a.branchStatus[k] == ciUnknown {
			out = append(out, k)
		}
	}
	return out
}
