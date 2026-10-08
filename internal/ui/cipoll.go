package ui

import (
	"context"
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
const ciAskEvery = 15 * time.Second

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
	if a.ciWatching || len(a.runningMRs()) == 0 && len(a.runningBranches()) == 0 && a.watchesRunning() == 0 {
		return
	}
	a.ciWatching = true
	go a.followCI()
}

// followCI turns the marks and asks again about the running pipelines,
// until none is left.
func (a *App) followCI() {
	every := a.ciAskEvery
	if every == 0 {
		every = ciAskEvery
	}
	ticker := time.NewTicker(ciTurnEvery)
	defer ticker.Stop()
	asked := time.Now()
	asking := map[mrKey]bool{}
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok := a.onLoopWait(ctx, func() {
			running, branches := a.runningMRs(), a.runningBranches()
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
			if time.Since(asked) < every {
				return
			}
			asked = time.Now()
			a.askBranchCI(branches, "")
			for _, mr := range running {
				client := a.client(mr.Instance)
				if client == nil || asking[keyOfMR(mr)] {
					continue
				}
				asking[keyOfMR(mr)] = true
				key := keyOfMR(mr)
				a.fetchMR(client, mr, false, func() { delete(asking, key) })
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
	case pageWatched:
		return a.watchedPane
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
