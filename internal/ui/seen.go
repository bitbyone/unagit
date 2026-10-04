package ui

import (
	"fmt"
	"maps"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// What was marked as reviewed without a review on disk - a merge request
// read in the browser, or in Hunk and let go - is the head it had then, kept
// beside the indexes. It stands where a review's head would: the NEW column
// and the commit log count from it. A review checking the head out later
// replaces it, and a merge request that closes takes its mark along.

// seenKey names a merge request in the file.
func seenKey(mr forge.MergeRequest) string {
	return fmt.Sprintf("%s|%d|%d", mr.Instance, mr.ProjectID, mr.IID)
}

func (a *App) loadSeen() {
	if seen, err := index.Load[map[string]string](config.IndexPath("seen")); err == nil && seen != nil {
		a.seen = seen
	}
}

func (a *App) saveSeen() {
	if err := index.Save(config.IndexPath("seen"), a.seen); err != nil {
		a.errorf("cannot remember what was reviewed: %v", err)
	}
}

// markReviewed takes the merge request's head as seen, so NEW and the log's
// marks count only what is pushed after it.
func (a *App) markReviewed(mr forge.MergeRequest) {
	if mr.SHA == "" {
		a.flash("the head of this merge request is not known yet - r refreshes it")
		return
	}
	if a.seen == nil {
		a.seen = map[string]string{}
	}
	a.seen[seenKey(mr)] = mr.SHA
	a.saveSeen()
	a.loadMRFresh()
	a.done(fmt.Sprintf("!%d marked as reviewed at %s", mr.IID, shortSHA(mr.SHA)))
}

// forgetSeen drops a mark the review has caught up with.
func (a *App) forgetSeen(mr forge.MergeRequest) {
	if _, ok := a.seen[seenKey(mr)]; ok {
		delete(a.seen, seenKey(mr))
		a.saveSeen()
	}
}

// pruneSeen drops the marks of merge requests no longer open on the servers
// that were asked.
func (a *App) pruneSeen(asked map[string]bool, open []forge.MergeRequest) {
	keep := map[string]bool{}
	for _, mr := range open {
		keep[seenKey(mr)] = true
	}
	before := len(a.seen)
	maps.DeleteFunc(a.seen, func(key, _ string) bool {
		instance, _, _ := strings.Cut(key, "|")
		return asked[instance] && !keep[key]
	})
	if len(a.seen) != before {
		a.saveSeen()
	}
}
