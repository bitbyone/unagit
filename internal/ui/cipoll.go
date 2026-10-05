package ui

import (
	"context"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// A pipeline under way turns its mark in the CI column, and the list,
// which is otherwise read only on a refresh, follows it: the merge
// requests the list shows whose pipeline is running are asked about again
// every so often, and only those, until each has ended. Then the watching
// stops, until a pipeline is seen running again.

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

// watchCI starts the watching when a shown pipeline is running and it is
// not watched already. It runs on the event loop and is cheap to call.
func (a *App) watchCI() {
	if a.ciWatching || len(a.runningMRs()) == 0 {
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
			running := a.runningMRs()
			if len(running) == 0 {
				a.ciWatching = false
				return
			}
			ciTurn++
			if a.currentTab() == pageMRs && a.mrsPane != nil && a.mrsPane.reload != nil {
				a.mrsPane.reload()
			}
			if time.Since(asked) < every {
				return
			}
			asked = time.Now()
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
