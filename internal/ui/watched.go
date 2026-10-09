package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/watch"
)

// The Watched screen lists everything watched, in sections by kind - the
// pipelines, for now - a change nobody has looked at marked until the
// screen is opened. Enter opens the pipeline's jobs, x lets watches go.

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

func (a *App) newWatchedPane() *pane {
	p := a.newPane("Watched")
	p.markable = true
	var rows []watch.Watch
	var filtered []int

	p.headline = func() string {
		text := fmt.Sprintf("%s%d/%d watched", tag(colMuted), len(filtered), len(a.watches))
		if n := a.watchUnseen(); n > 0 {
			text += tagEnd + " · " + tag(role("watched.unseen")) + fmt.Sprintf("%d new", n) + tagEnd + tag(colMuted)
		}
		var read time.Time
		for _, st := range a.watchSnap.States {
			if st.Read.After(read) {
				read = st.Read
			}
		}
		if !read.IsZero() {
			text += " · read " + humanAge(read)
		}
		if poller := a.watchSnap.Poller; poller != "" && !a.watchPolling.Load() {
			pid, _, _ := strings.Cut(poller, "-")
			text += " · followed by unagit " + pid
		}
		return text + tagEnd
	}
	render := func(query string) {
		rows = a.watchedRows()
		filtered = filterWatches(a, rows, query)
		a.drawWatched(p, rows, filtered)
		p.updateHeader()
	}
	p.onQuery = render
	p.reload = func() { render(p.query) }

	at := func(idx int) (watch.Watch, bool) {
		if idx < 0 || idx >= len(rows) {
			return watch.Watch{}, false
		}
		return rows[idx], true
	}
	p.onDetail = func(idx int, focus bool) {
		if w, ok := at(idx); ok {
			a.showWatchedPipeline(w)
		}
	}
	marked := func() []watch.Watch {
		var out []watch.Watch
		for idx := range p.marks {
			if w, ok := at(idx); ok {
				out = append(out, w)
			}
		}
		return out
	}
	p.selection = func() (string, []uiAction) {
		if len(p.marks) > 0 {
			ws := marked()
			return fmt.Sprintf("Actions · %d marked", len(ws)), []uiAction{
				{name: "Stop Watching", about: "Let every marked watch go; their rows leave the list and their marks the other lists.", keys: "x", rank: 10,
					run: func() { a.unwatch(p, ws) }},
			}
		}
		w, ok := at(p.selectedIndex())
		if !ok {
			return "", nil
		}
		return "Actions · " + w.Label(), a.watchedActions(p, w)
	}
	p.screen = func() (string, []uiAction) { return "Watched", a.watchedScreenActions(p) }
	return p
}

func filterWatches(a *App, rows []watch.Watch, query string) []int {
	var hits []scored
	for i, w := range rows {
		st := a.watchSnap.States[w.Key()]
		hay := strings.Join([]string{w.Label(), a.watchTitle(w), st.Status, st.Failed, st.User}, " ")
		score, ok := fuzzy.Match(query, hay)
		if !ok {
			continue
		}
		hits = append(hits, scored{idx: i, score: score})
	}
	if strings.TrimSpace(query) != "" {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
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

// latestNews is, by watch, what was last said of it.
func latestNews(snap watch.Snapshot) map[string]string {
	out := map[string]string{}
	for _, e := range snap.Events {
		out[e.Key] = e.Line
	}
	return out
}

func (a *App) drawWatched(p *pane, rows []watch.Watch, filtered []int) {
	previous := p.selectedIndex()
	p.table.Clear()
	const markW, ciW = 1, 1
	var whats, titles, pipes, bys, lasts []int
	news := latestNews(a.watchSnap)
	changedW := headingWidth("CHANGED")
	for _, idx := range filtered {
		w := rows[idx]
		st := a.watchSnap.States[w.Key()]
		if !st.Changed.IsZero() {
			changedW = max(changedW, cells(humanAge(st.Changed)))
		}
		whats = append(whats, cells(w.Label()))
		titles = append(titles, cells(a.watchTitle(w)))
		pipes = append(pipes, cells(watchPipelineWords(st)))
		bys = append(bys, cells(st.User))
		lasts = append(lasts, cells(news[w.Key()]))
	}
	whatCol := flexColumn("WHAT", whats, 12, 1.5)
	titleCol := flexColumn("TITLE", titles, 10, 2)
	pipeCol := flexColumn("PIPELINE", pipes, 8, 1.2)
	byCol := flexColumn("BY", bys, 4, 0.6)
	lastCol := flexColumn("LATEST", lasts, 10, 1.2)
	changedCol := fixedColumn(changedW)
	titleCol.drop, byCol.drop, lastCol.drop = 3, 1, 2
	spare := layoutColumns(p.contentWidth()-1, fixedColumn(markW), fixedColumn(ciW), whatCol, pipeCol, lastCol, byCol, changedCol, titleCol)
	if titleCol.shown() {
		titleCol.width += spare
	} else {
		whatCol.width += spare
	}
	head := role("watched.header")
	header := []field{{width: markW}, {width: ciW},
		{text: "WHAT", width: whatCol.width, colour: head},
		{text: "PIPELINE", width: pipeCol.width, colour: head}}
	if lastCol.shown() {
		header = append(header, field{text: "LATEST", width: lastCol.width, colour: head})
	}
	if byCol.shown() {
		header = append(header, field{text: "BY", width: byCol.width, colour: head})
	}
	header = append(header, field{text: "CHANGED", width: changedCol.width, colour: head})
	if titleCol.shown() {
		header = append(header, field{text: "TITLE", width: titleCol.width, colour: head})
	}
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(withHeadingIcons(header))).SetSelectable(false).SetExpansion(1))

	for row, idx := range filtered {
		w := rows[idx]
		st := a.watchSnap.States[w.Key()]
		mark := field{width: markW}
		if a.watchUnseenRow(w) {
			mark = field{text: glyphDot, width: markW, colour: role("watched.unseen")}
		}
		ci, colour := ciMark(st.Status)
		pipeColour := stateColour(st.Status)
		if st.Error != "" {
			pipeColour = role("watched.error")
		}
		changed := ""
		if !st.Changed.IsZero() {
			changed = humanAge(st.Changed)
		}
		cells := []field{mark, {text: ci, width: ciW, colour: colour},
			{icon: a.forgeIcon(w.Instance), text: w.Label(), width: whatCol.width, colour: role("watched.what"), shorten: shortenRepo},
			{text: watchPipelineWords(st), width: pipeCol.width, colour: pipeColour}}
		if lastCol.shown() {
			cells = append(cells, field{text: news[w.Key()], width: lastCol.width, colour: role("watched.latest")})
		}
		if byCol.shown() {
			cells = append(cells, field{text: st.User, width: byCol.width, colour: role("watched.by")})
		}
		cells = append(cells, field{text: changed, width: changedCol.width, colour: role("watched.changed")})
		if titleCol.shown() {
			cells = append(cells, field{text: a.watchTitle(w), width: titleCol.width, colour: role("column.name")})
		}
		cell := tview.NewTableCell(rowText(cells)).SetReference(idx).SetExpansion(1)
		if p.marks[idx] {
			bandMarked.paint(cell)
		}
		p.table.SetCell(row+1, 0, cell)
	}
	if len(filtered) == 0 {
		hint := "Nothing watched. Watch Pipelines - in the actions of a repository, a merge request or a worktree (Alt-Enter) - follows its pipelines here."
		if len(rows) > 0 {
			hint = "No watch matches the filter."
		}
		p.table.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+esc(hint)+tagEnd).SetSelectable(false).SetExpansion(1))
	}
	first := 0
	if len(filtered) > 0 {
		first = 1
	}
	p.selectRow(previous, first)
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

// watchedScreenActions are what the Watched screen itself can do.
func (a *App) watchedScreenActions(p *pane) []uiAction {
	acts := []uiAction{
		{name: "Refresh All", about: "Ask the servers about every watch now.", keys: "R", rank: 10, run: func() { a.watchAsk(nil) }},
		{name: "Stop Watching All…", about: "Let every watch go, after asking.", keys: "", rank: 60, when: func() bool { return len(a.watches) > 0 },
			run: func() {
				ws := append([]watch.Watch(nil), a.watches...)
				a.confirmWith("Stop Watching All", fmt.Sprintf("Stop watching all %d?\n\nNothing follows their pipelines any more until you watch them again.", len(ws)), "Stop", nil, func() {
					a.unwatch(p, ws)
				})
			}},
	}
	return append(acts, a.listActions(p)...)
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
