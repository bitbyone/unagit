package ui

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/notify"
	"github.com/tobola/unagit/internal/watch"
)

// What the user watches is followed in the background while a unagit runs:
// a merge request's pipeline, whichever its head ran, or a branch's newest.
// Of several unagits open at once one polls - the one holding the poller's
// lock - and writes what it found to state.json; the others read that file
// when it moves. So the servers are asked once, and each change is a
// notification once, however many are open. The token is only ever in a
// running unagit's memory, so nothing follows anything once the last one
// exits; there is no background service.
//
// A change is a passing word on the status line, never a box: a failure the
// user is not looking at must not take the keyboard from what they are
// typing into. The tab counts what has not been seen, and a desktop
// notification goes out only when no unagit is in front.

const (
	// watchLookEvery is how often the files are looked at, unless the App
	// says otherwise (App.watchLookEvery; tests make it short).
	watchLookEvery = time.Second
	// watchIdleEvery is how often a watch whose pipeline does not run is
	// asked about, to notice a push that starts a new one. A running one is
	// asked every ciAskEvery.
	watchIdleEvery = 2 * time.Minute
)

// startWatching follows the watches from the unlock on: before it there is
// no token to ask the servers with. It runs once, on the loop.
func (a *App) startWatching() {
	if a.watchStore != nil {
		return
	}
	a.watchStore = watch.Open(a.cfg.WatchDir())
	a.watchID = watch.NewInstance()
	if ws, err := a.watchStore.Watches(); err == nil {
		a.watches = ws
	}
	stop := a.stopFollowing
	if stop == nil {
		stop = make(chan struct{})
	}
	a.followDone = make(chan struct{})
	go func() {
		defer close(a.followDone)
		a.followWatches(stop)
	}()
}

// pipelineWatch is a watch on a merge request's pipelines (iid) or a
// branch's.
func pipelineWatch(instance, project string, projectID, iid int, branch string) watch.Watch {
	return watch.Watch{Kind: watch.KindPipeline, Instance: instance, Project: project, ProjectID: projectID, IID: iid, Branch: branch}
}

// isWatched reports whether a watch on the same thing is kept.
func (a *App) isWatched(w watch.Watch) bool {
	key := w.Key()
	for _, have := range a.watches {
		if have.Key() == key {
			return true
		}
	}
	return false
}

// branchWatched reports whether a branch of a repository is watched.
func (a *App) branchWatched(instance, project, branch string) bool {
	if branch == "" || strings.HasPrefix(branch, "(") {
		return false
	}
	return a.isWatched(pipelineWatch(instance, project, 0, 0, branch))
}

// watchedMarks is the watched mark of a row, in the marks column beside
// what is open there.
func watchedMarks(watched bool) []openMark {
	if !watched {
		return nil
	}
	return []openMark{{glyphWatched, role("mark.watched")}}
}

// repositoryWatchBranch is the branch a repository's row stands for: the
// clone's, or the default branch before it is cloned, as J reads it.
func (a *App) repositoryWatchBranch(pr forge.Project) string {
	if branch := a.repositoryBranch(pr); branch != "" {
		return branch
	}
	return pr.DefaultBranch
}

func (a *App) projectWatched(pr forge.Project) bool {
	return a.branchWatched(pr.Instance, pr.PathWithNamespace, a.repositoryWatchBranch(pr))
}

func (a *App) mrWatched(mr forge.MergeRequest) bool {
	return a.isWatched(pipelineWatch(mr.Instance, a.projectPathOfMR(mr), 0, mr.IID, ""))
}

// worktreeWatched is a worktree's branch watched, or for a group any of
// its repositories' branches.
func (a *App) worktreeWatched(r worktreeRow) bool {
	if !r.grouped() {
		return a.branchWatched(r.Instance, r.Path, r.Branch)
	}
	for _, m := range r.Members {
		if a.branchWatched(m.Instance, m.Path, m.Branch) {
			return true
		}
	}
	return false
}

// watchUnseen counts the watches whose last change nobody has looked at.
func (a *App) watchUnseen() int {
	snap := a.watchSnap
	snap.Seen = max(snap.Seen, a.watchSeen.Load())
	return snap.Unseen(a.watches)
}

// watchPipelinesAction is Watch Pipelines on something, or Stop Watching
// Pipelines once it is. It has no key: watching is chosen now and then,
// not every day.
func (a *App) watchPipelinesAction(w watch.Watch) uiAction {
	if a.isWatched(w) {
		return uiAction{name: "Stop Watching Pipelines", about: "Stop following its pipelines in the background; the Watched tab lets it go.", rank: 64,
			run: func() { a.setWatched(w, false) }}
	}
	return uiAction{name: "Watch Pipelines", about: "Follow its pipelines in the background, while unagit runs: the Watched tab lists them, and a failure or a success is said - with a desktop notification when unagit is not in front.", rank: 64,
		run: func() { a.setWatched(w, true) }}
}

// watchMRAction is Watch Pipelines on a merge request.
func (a *App) watchMRAction(mr forge.MergeRequest) uiAction {
	w := pipelineWatch(mr.Instance, a.projectPathOfMR(mr), mr.ProjectID, mr.IID, "")
	w.Title = mr.Title
	return a.watchPipelinesAction(w)
}

// watchWorktreeAction is Watch Pipelines on a worktree's branch; a group
// asks which of its repositories first, as J does.
func (a *App) watchWorktreeAction(r worktreeRow) uiAction {
	if !r.grouped() {
		return a.watchPipelinesAction(a.worktreeWatch(r))
	}
	return uiAction{name: "Watch Pipelines…", about: "Choose a repository of the group whose branch's pipelines to follow in the background, or to stop following.", rank: 64,
		when: func() bool { return len(r.Members) > 0 }, run: func() {
			items := make([]pickItem, len(r.Members))
			for i, m := range r.Members {
				mark := " "
				if a.worktreeWatched(m) {
					mark = glyphWatched
				}
				items[i] = pickItem{Label: esc(mark + " " + m.Path), Sub: esc(m.Branch), Data: i}
			}
			a.showPicker("Watch the pipelines of which repository · "+r.Path, items, func(it pickItem) {
				m := r.Members[it.Data.(int)]
				w := a.worktreeWatch(m)
				a.setWatched(w, !a.isWatched(w))
			})
		}}
}

// worktreeWatch is the watch on a worktree's branch.
func (a *App) worktreeWatch(r worktreeRow) watch.Watch {
	pr := a.worktreeProject(r)
	return pipelineWatch(r.Instance, r.Path, pr.ID, 0, r.Branch)
}

// setWatched starts or stops a watch, in watches.json under its lock, and
// has it read at once.
func (a *App) setWatched(w watch.Watch, on bool) {
	if a.watchStore == nil {
		a.flash("watching starts once the tokens are unlocked")
		return
	}
	w.Kind = watch.KindPipeline
	w.Since = time.Now()
	key := w.Key()
	go func() {
		watches, err := a.watchStore.Change(func(ws []watch.Watch) []watch.Watch {
			ws = slices.DeleteFunc(ws, func(have watch.Watch) bool { return have.Key() == key })
			if on {
				ws = append(ws, w)
			}
			return ws
		})
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			a.setWatches(watches)
			if on {
				a.done("watching the pipelines of " + w.Label())
			} else {
				a.done("stopped watching " + w.Label())
			}
		})
		a.watchAsk(nil)
	}()
}

// stopWatching lets several watches go at once.
func (a *App) stopWatching(keys []string, then func()) {
	go func() {
		watches, err := a.watchStore.Change(func(ws []watch.Watch) []watch.Watch {
			return slices.DeleteFunc(ws, func(w watch.Watch) bool { return slices.Contains(keys, w.Key()) })
		})
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			a.setWatches(watches)
			then()
		})
	}()
}

// setWatches puts what is watched in place and draws what shows it. It
// runs on the loop.
func (a *App) setWatches(watches []watch.Watch) {
	if reflect.DeepEqual(watches, a.watches) {
		return
	}
	a.watches = watches
	a.redrawWatches(true)
}

// redrawWatches draws the Watched screen and the tab again, and the lists'
// marks when what is watched changed.
func (a *App) redrawWatches(marks bool) {
	if a.watchedPane != nil {
		a.watchedPane.reload()
	}
	a.drawTabs()
	if marks {
		a.projectsPane.reload()
		a.mrsPane.reload()
		a.worktreesPane.reload()
	}
}

// watchAsk has the watches read now: those keys, or every one for nil. In
// the polling instance that is the next turn; another asks the poller
// through its presence file.
func (a *App) watchAsk(keys []string) {
	a.watchAskMu.Lock()
	a.watchAskSeq++
	a.watchAskKeys = keys
	a.watchAskMu.Unlock()
	select {
	case a.watchNow <- struct{}{}:
	default:
	}
}

// markWatchesSeen takes every change as seen: the Watched screen is in
// front. The poller folds it into state.json, so every instance's count
// goes with it.
func (a *App) markWatchesSeen() {
	if a.watchSnap.Seq > a.watchSeen.Load() {
		a.watchSeen.Store(a.watchSnap.Seq)
		a.drawTabs()
		a.watchAsk([]string{})
	}
}

// watchFollower is the goroutine's side of the watches: what it last read
// of the files, and, while it polls, when it last asked about each.
type watchFollower struct {
	// ctx ends when the interface does, so nothing waits on a loop that is
	// gone.
	ctx      context.Context
	app      *App
	store    *watch.Store
	poller   *watch.Poller
	watches  []watch.Watch
	snap     watch.Snapshot
	stamps   map[string]time.Time
	asked    map[string]time.Time
	askSeen  map[string]uint64 // each instance's last ask, as the poller saw it
	presence watch.Presence
	askSeq   uint64
	// delivered is what the interface was last handed, so a quiet turn
	// draws nothing.
	delivered struct {
		watches []watch.Watch
		snap    watch.Snapshot
		once    bool
	}
}

// followWatches runs for as long as the interface does, from the unlock.
func (a *App) followWatches(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stop
		cancel()
	}()
	f := &watchFollower{ctx: ctx, app: a, store: a.watchStore, stamps: map[string]time.Time{},
		asked: map[string]time.Time{}, askSeen: map[string]uint64{}}
	f.presence = watch.Presence{ID: a.watchID, PID: os.Getpid()}
	defer func() {
		f.poller.Release()
		f.store.Leave(a.watchID)
	}()
	every := a.watchLookEvery
	if every == 0 {
		every = watchLookEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		f.turn()
		select {
		case <-stop:
			return
		case <-ticker.C:
		case <-a.watchNow:
		}
	}
}

// turn is one look: take the poller's place if it is free, write this
// instance's presence, then poll or read what the poller wrote.
func (f *watchFollower) turn() {
	a := f.app
	if f.poller == nil {
		if p, ok, err := f.store.TryPoll(); err == nil && ok {
			f.poller = p
			a.watchPolling.Store(true)
			// Carry on from what the last poller left.
			f.snap, _ = f.store.State()
		}
	}
	watchesMoved := f.moved("watches.json")
	if watchesMoved {
		if ws, err := f.store.Watches(); err == nil {
			f.watches = ws
		}
	}
	f.present()
	if f.poller != nil {
		f.poll()
	} else if f.moved("state.json") {
		if snap, err := f.store.State(); err == nil {
			f.snap = snap
		}
	}
	f.deliver()
}

// moved reports whether a file was written since the last look.
func (f *watchFollower) moved(name string) bool {
	stamp := f.store.Stamp(name)
	if stamp.Equal(f.stamps[name]) {
		return false
	}
	f.stamps[name] = stamp
	return true
}

// present writes what this instance says of itself when it changed: whether
// its terminal is in front, how far its user has looked, and what it asks
// the poller to read.
func (f *watchFollower) present() {
	a := f.app
	p := f.presence
	p.Focused = a.terminalInFront()
	p.Seen = a.watchSeen.Load()
	a.watchAskMu.Lock()
	if a.watchAskSeq != f.askSeq {
		f.askSeq = a.watchAskSeq
		p.AskSeq, p.Ask = a.watchAskSeq, a.watchAskKeys
	}
	a.watchAskMu.Unlock()
	if reflect.DeepEqual(p, f.presence) && f.store.Stamp("present/"+p.ID) != (time.Time{}) {
		return
	}
	f.presence = p
	f.store.SetPresence(p)
}

// poll asks the servers about every watch that is due, writes what changed
// and says it.
func (f *watchFollower) poll() {
	a := f.app
	snap := f.snap
	snap.States = cloneStates(snap.States)
	snap.Poller = a.watchID
	presences := f.store.Presences()
	anyFocused := false
	due := map[string]bool{}
	for _, p := range presences {
		snap.Seen = max(snap.Seen, p.Seen)
		anyFocused = anyFocused || p.Focused
		if p.AskSeq > f.askSeen[p.ID] {
			f.askSeen[p.ID] = p.AskSeq
			if p.Ask == nil {
				for _, w := range f.watches {
					due[w.Key()] = true
				}
			}
			for _, k := range p.Ask {
				due[k] = true
			}
		}
	}
	snap.Seen = min(snap.Seen, snap.Seq)
	// A watch let go takes its state with it.
	keys := map[string]bool{}
	for _, w := range f.watches {
		keys[w.Key()] = true
	}
	for k := range snap.States {
		if !keys[k] {
			delete(snap.States, k)
		}
	}
	running := a.ciAskEvery
	if running == 0 {
		running = ciAskEvery
	}
	idle := a.watchIdleEvery
	if idle == 0 {
		idle = watchIdleEvery
	}
	var ask []watch.Watch
	for _, w := range f.watches {
		st, read := snap.States[w.Key()]
		every := idle
		if st.Error == "" && ciStateOf(st.Status) == ciRunning {
			every = running
		}
		if !read || due[w.Key()] || time.Since(f.asked[w.Key()]) >= every {
			ask = append(ask, w)
		}
	}
	results := f.read(ask)
	var events []watch.Event
	var gone []string
	for i, w := range ask {
		f.asked[w.Key()] = time.Now()
		r := results[i]
		before, known := snap.States[w.Key()]
		after := r.state
		if r.err != nil {
			after = before
			after.Error = r.err.Error()
			after.Read = time.Now()
			snap.States[w.Key()] = after
			continue
		}
		evs := pipelineChanges(w, before, after, known, r.ended)
		after.Changed, after.Seq = before.Changed, before.Seq
		if after.Status != before.Status || after.Pipeline != before.Pipeline || !known {
			after.Changed = time.Now()
		}
		if loud(evs) {
			after.Seq = snap.Seq + 1
		}
		snap.States[w.Key()] = after
		events = append(events, evs...)
		if r.ended != "" {
			gone = append(gone, w.Key())
		}
	}
	snap.Add(events...)
	if len(gone) > 0 {
		if ws, err := f.store.Change(func(ws []watch.Watch) []watch.Watch {
			return slices.DeleteFunc(ws, func(w watch.Watch) bool { return slices.Contains(gone, w.Key()) })
		}); err == nil {
			f.watches = ws
		}
	}
	if !reflect.DeepEqual(snap, f.snap) {
		if err := f.store.WriteState(snap); err == nil {
			f.snap = snap
			f.moved("state.json")
		}
	}
	if !anyFocused {
		for _, e := range events {
			if e.News {
				a.notifyWatch(f.ctx, e)
			}
		}
	}
}

// loud reports whether events are news to count on the tab and notify.
func loud(events []watch.Event) bool {
	return slices.ContainsFunc(events, func(e watch.Event) bool { return e.News })
}

func cloneStates(in map[string]watch.State) map[string]watch.State {
	out := make(map[string]watch.State, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// deliver hands what the follower now knows to the interface, which draws
// and says what is new to it.
func (f *watchFollower) deliver() {
	a := f.app
	d := &f.delivered
	if d.once && reflect.DeepEqual(d.watches, f.watches) && reflect.DeepEqual(d.snap, f.snap) {
		return
	}
	watches := slices.Clone(f.watches)
	snap := f.snap
	snap.States = cloneStates(snap.States)
	snap.Events = slices.Clone(snap.Events)
	d.watches, d.snap, d.once = watches, snap, true
	a.tv.QueueUpdateDraw(func() { a.applyWatchState(watches, snap) })
}

// applyWatchState is the interface's side of a turn. The first one after
// the start says nothing: what happened before this instance looked is not
// news to it.
func (a *App) applyWatchState(watches []watch.Watch, snap watch.Snapshot) {
	marks := !reflect.DeepEqual(watches, a.watches)
	same := !marks && reflect.DeepEqual(snap, a.watchSnap)
	a.watches, a.watchSnap = watches, snap
	if !a.watchStarted {
		a.watchStarted = true
		a.watchShown = snap.Seq
		a.watchSeen.Store(max(a.watchSeen.Load(), snap.Seen))
	}
	if events := snap.After(a.watchShown); len(events) > 0 {
		a.watchShown = snap.Seq
		a.sayWatchEvents(events)
	}
	if a.currentTab() == pageWatched {
		a.markWatchesSeen()
	}
	if !same {
		a.redrawWatches(marks)
	}
}

// sayWatchEvents puts the news on the status line: the newest, and how many
// more came with it.
func (a *App) sayWatchEvents(events []watch.Event) {
	e := events[len(events)-1]
	msg := e.What + " · " + e.Line
	if n := len(events) - 1; n > 0 {
		msg += fmt.Sprintf(" · and %d more on the Watched tab", n)
	}
	if e.Good {
		a.done(msg)
		return
	}
	a.note(msg)
}

// watchReading is what one watch's read came to.
type watchReading struct {
	state watch.State
	// ended is "merged" or "closed" for a merge request no longer open.
	ended string
	err   error
}

// read asks about the watches, several at a time as a refresh does.
func (f *watchFollower) read(ws []watch.Watch) []watchReading {
	out := make([]watchReading, len(ws))
	if len(ws) == 0 {
		return out
	}
	a := f.app
	ctx, cancel := context.WithTimeout(f.ctx, time.Minute)
	defer cancel()
	sem := make(chan struct{}, extrasFanOut)
	var wg sync.WaitGroup
	for i, w := range ws {
		client := a.watchClient(ctx, w.Instance)
		if client == nil {
			out[i].err = fmt.Errorf("%s has no token - set one in %s", w.Instance, settingsTab)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			out[i] = readPipelineWatch(ctx, client, w, f.snap.States[w.Key()])
		}()
	}
	wg.Wait()
	return out
}

// watchClient is the server's client, read on the loop: the clients change
// when Settings does.
func (a *App) watchClient(ctx context.Context, instance string) forge.Provider {
	ch := make(chan forge.Provider, 1)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !a.onLoopWait(ctx, func() { ch <- a.client(instance) }) {
		return nil
	}
	return <-ch
}

// readPipelineWatch reads a watch's newest pipeline: a merge request's -
// whatever its head ran, so a push moves the watch on - or a branch's. The
// jobs are read only when a pipeline has newly failed, to name the job.
func readPipelineWatch(ctx context.Context, client forge.Provider, w watch.Watch, before watch.State) watchReading {
	st := watch.State{Read: time.Now()}
	var pipe *forge.Pipeline
	var err error
	var mr forge.MergeRequest
	pr := forge.Project{ID: w.ProjectID, PathWithNamespace: w.Project, Instance: w.Instance}
	if w.IID > 0 {
		mr = forge.MergeRequest{IID: w.IID, ProjectID: w.ProjectID, TargetProjectID: w.ProjectID, ProjectPath: w.Project, Instance: w.Instance}
		det, err := client.MergeRequestDetail(ctx, mr)
		if err != nil {
			return watchReading{err: err}
		}
		mr = withDetail(mr, det)
		st.Title, st.URL = det.Title, det.WebURL
		switch state := strings.ToLower(det.State); state {
		case "merged", "closed":
			st.Status, st.Pipeline, st.SHA = before.Status, before.Pipeline, before.SHA
			return watchReading{state: st, ended: state}
		}
		pipe, err = client.MergeRequestPipeline(ctx, mr)
	} else {
		pipe, err = client.LatestPipeline(ctx, pr, w.Branch)
	}
	if err != nil {
		return watchReading{err: err}
	}
	if pipe == nil {
		return watchReading{state: st}
	}
	st.Pipeline, st.Status, st.SHA, st.WebURL = pipe.ID, pipe.Status, pipe.SHA, pipe.WebURL
	st.Started = pipe.StartedAt
	if st.Started.IsZero() {
		st.Started = pipe.CreatedAt
	}
	if pipe.User != nil {
		st.User = pipe.User.Username
	}
	if ciStateOf(st.Status) == ciFailed {
		st.Failed = before.Failed
		if before.Pipeline != st.Pipeline || before.Status != st.Status || before.SHA != st.SHA {
			var jobs []forge.Job
			if w.IID > 0 {
				_, jobs, _ = client.PipelineJobs(ctx, mr)
			} else {
				_, jobs, _ = client.BranchPipelineJobs(ctx, pr, w.Branch)
			}
			st.Failed = firstFailed(jobs)
		}
	}
	return watchReading{state: st}
}

// firstFailed is the first job that failed and was not run again.
func firstFailed(jobs []forge.Job) string {
	for _, j := range jobs {
		if !j.Retried && ciStateOf(j.Status) == ciFailed {
			return j.Name
		}
	}
	return ""
}

// pipelineChanges is what changed between two readings, as events. The
// first reading is never news - only a change from what was last held.
func pipelineChanges(w watch.Watch, before, after watch.State, known bool, ended string) []watch.Event {
	ev := func(line string) watch.Event { return watch.Event{Key: w.Key(), What: w.Label(), Line: line} }
	if ended != "" {
		e := ev("merge request " + ended + " · no longer watched")
		e.News, e.Good = true, ended == "merged"
		return []watch.Event{e}
	}
	if !known || after.Status == "" {
		return nil
	}
	fresh := after.Pipeline != before.Pipeline || after.SHA != before.SHA
	if !fresh && after.Status == before.Status {
		return nil
	}
	var out []watch.Event
	if fresh && w.IID > 0 && before.SHA != "" && after.SHA != before.SHA {
		out = append(out, ev("new head "+shortSHA(after.SHA)))
	}
	switch after.Status {
	case "manual":
		e := ev("pipeline waits for a manual job")
		e.News = true
		out = append(out, e)
	case "canceled", "cancelled":
		e := ev("pipeline cancelled")
		e.News, e.Bad = true, true
		out = append(out, e)
	default:
		switch ciStateOf(after.Status) {
		case ciPassed:
			e := ev("pipeline passed")
			e.News, e.Good = true, true
			out = append(out, e)
		case ciFailed:
			line := "pipeline failed"
			if after.Failed != "" {
				line += " · " + after.Failed
			}
			e := ev(line)
			e.News, e.Bad = true, true
			out = append(out, e)
		case ciRunning:
			if fresh || ciStateOf(before.Status) != ciRunning {
				out = append(out, ev("pipeline started"))
			}
		}
	}
	return out
}

// terminalInFront reports whether the user is looking at this unagit: its
// terminal has focus and no editor has it.
func (a *App) terminalInFront() bool {
	q := a.quiet
	return q != nil && q.focused.Load() && !q.suspended.Load()
}

// notifyWatch sends a desktop notification of an event: through the
// terminal where it can show one, the system otherwise. It runs off the
// loop; the sequence is written on it, between two draws.
func (a *App) notifyWatch(ctx context.Context, e watch.Event) {
	if a.notifier != nil {
		a.notifier("unagit · "+e.What, e.Line)
		return
	}
	a.sendNotification(ctx, "unagit · "+e.What, e.Line)
}

// sendNotification shows title and body as Settings › Integrations says.
func (a *App) sendNotification(ctx context.Context, title, body string) {
	mode := ""
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !a.onLoopWait(ctx, func() { mode = a.cfg.Integrations.Notifications }) {
		return
	}
	q := a.quiet
	free := q != nil && !q.suspended.Load()
	term := notify.Detect(os.Getenv)
	toTerminal, toSystem := notify.Route(mode, term, free)
	if toTerminal {
		seq := term.Sequence(title, body)
		a.tv.QueueUpdate(func() {
			if q == nil || q.suspended.Load() {
				return
			}
			if tty, ok := q.Screen.Tty(); ok {
				tty.Write(seq)
			}
		})
		return
	}
	if toSystem {
		notify.SystemNotify(title, body)
	}
}
