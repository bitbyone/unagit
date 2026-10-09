package ui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/watch"
)

// What the Activity screen does with a watch (activity.go): its words, its
// actions, going to its pipeline or its row in the lists.

// watchedRows is what the screen lists, the newest change first.
func (a *App) watchedRows() []watch.Watch {
	rows := append([]watch.Watch(nil), a.watches...)
	changed := func(w watch.Watch) time.Time {
		if st, ok := a.watchSnap.States[w.Key()]; ok && !st.Changed.IsZero() {
			return st.Changed
		}
		return w.Since
	}
	sort.SliceStable(rows, func(i, j int) bool { return changed(rows[i]).After(changed(rows[j])) })
	return rows
}

// watchUnseenRow reports whether a watch changed since the screen was last
// looked at: opening it counts the changes as seen on the tab, and the rows
// keep their mark for the visit, so it can be told what they were.
func (a *App) watchUnseenRow(w watch.Watch) bool {
	st, ok := a.watchSnap.States[w.Key()]
	return ok && st.Seq > a.watchLooked
}

// watchTitle is what a row says of the thing: the merge request's title as
// last read, or as it was when the watch began.
func (a *App) watchTitle(w watch.Watch) string {
	if st := a.watchSnap.States[w.Key()]; st.Title != "" {
		return st.Title
	}
	return w.Title
}

// watchPipelineWords is the PIPELINE column: its status, the job it failed
// on, and how long ago it began.
func watchPipelineWords(st watch.State) string {
	switch {
	case st.Error != "" && st.Status == "":
		return "unread"
	case st.Status == "":
		if st.Read.IsZero() {
			return "reading…"
		}
		return "none"
	}
	words := st.Status
	if st.Failed != "" {
		words += " · " + st.Failed
	}
	if !st.Started.IsZero() {
		words += " " + humanAge(st.Started)
	}
	if st.Behind > 0 {
		words += fmt.Sprintf(" · %d behind %s", st.Behind, st.Base)
	}
	return words
}

// watchedActions are what can be done with one watch.
func (a *App) watchedActions(p *pane, w watch.Watch) []uiAction {
	st := a.watchSnap.States[w.Key()]
	goTo := uiAction{name: "Go to Merge Request", about: "Switch to Merge requests with the cursor on the merge request watched.", keys: "m", rank: 30,
		run: func() { a.goToWatched(w) }}
	if w.IID == 0 {
		goTo.name, goTo.about = "Go to Repository", "Switch to Repositories with the cursor on the repository of the branch watched."
	}
	return []uiAction{
		{name: "Show Pipeline…", about: "The jobs of the pipeline watched, the first that failed under the cursor: read its log, retry it, open it.", keys: "Enter", rank: 5, run: p.enter},
		{name: "Stop Watching", about: "Let the watch go: its row leaves the list, its mark the other lists.", keys: "x", rank: 10,
			run: func() { a.unwatch(p, []watch.Watch{w}) }},
		{name: "Open Pipeline in Browser", about: "The pipeline's page on the forge.", keys: "w", rank: 20,
			when: func() bool { return st.WebURL != "" || st.URL != "" }, run: func() {
				if st.WebURL != "" {
					a.openWeb(st.WebURL)
				} else {
					a.openWeb(st.URL)
				}
			}},
		goTo,
		{name: "Refresh", about: "Ask the server about this watch now, rather than at its next turn.", keys: "r", rank: 40,
			run: func() { a.watchAsk([]string{w.Key()}) }},
	}
}

// unwatch lets watches go, after no question - a watch costs nothing to
// start again.
func (a *App) unwatch(p *pane, ws []watch.Watch) {
	keys := make([]string, len(ws))
	for i, w := range ws {
		keys[i] = w.Key()
	}
	a.stopWatching(keys, func() {
		p.clearMarks()
		if len(ws) == 1 {
			a.done("stopped watching " + ws[0].Label())
			return
		}
		a.done(fmt.Sprintf("stopped watching %d", len(ws)))
	})
}

// watchMR is the merge request a watch is on, as the list has it when it
// is there.
func (a *App) watchMR(w watch.Watch) forge.MergeRequest {
	for _, mr := range a.mrs {
		if mr.Instance == w.Instance && mr.IID == w.IID && a.projectPathOfMR(mr) == w.Project {
			return mr
		}
	}
	st := a.watchSnap.States[w.Key()]
	return forge.MergeRequest{IID: w.IID, ProjectID: w.ProjectID, TargetProjectID: w.ProjectID, ProjectPath: w.Project,
		Instance: w.Instance, SHA: st.SHA, Title: a.watchTitle(w), WebURL: st.URL}
}

// showWatchedPipeline opens the jobs of what a watch follows.
func (a *App) showWatchedPipeline(w watch.Watch) {
	if w.IID > 0 {
		a.showMRPipeline(a.watchMR(w), 0)
		return
	}
	target, err := a.branchCI(w.Instance, w.Project, w.Branch)
	if err != nil {
		pr := forge.Project{ID: w.ProjectID, PathWithNamespace: w.Project, Instance: w.Instance}
		target = ciTarget{instance: w.Instance, project: pr, label: w.Label(), watch: &w,
			load: func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error) {
				return client.BranchPipelineJobs(ctx, pr, w.Branch)
			}}
	}
	a.showPipeline(target, 0)
}

// goToWatched shows what a watch is on in its list.
func (a *App) goToWatched(w watch.Watch) {
	if w.IID > 0 {
		a.showMRAt(a.watchMR(w))
		return
	}
	a.switchTab(pageProjects)
	a.selectProject(projectKey{w.Instance, w.Project})
}
