package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
// Every change - a pipeline that began, passed, failed - is a toast in the
// corner, never a box: a failure the user is not looking at must not take
// the keyboard from what they are typing into. The tab counts what has not
// been seen, and a desktop notification goes out when no unagit is in
// front. What a watch reads is the lists' CI column too, so a watched row's
// mark moves without a refresh.

const (
	// watchLookEvery is how often the files are looked at, unless the App
	// says otherwise (App.watchLookEvery; tests make it short).
	watchLookEvery = time.Second
	// watchIdleEvery is how often a watch whose pipeline does not run is
	// asked about, to notice a push that starts a new one. A running one is
	// asked every ciAskEvery. It is short: a pipeline that starts is news.
	watchIdleEvery = 20 * time.Second
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
		return uiAction{name: "Stop Watching Pipelines", about: "Stop following its pipelines in the background; the Activity screen lets it go.", rank: 64,
			run: func() { a.setWatched(w, false) }}
	}
	return uiAction{name: "Watch Pipelines", about: "Follow its pipelines in the background, while unagit runs: the Activity screen lists them, and a failure or a success is said - with a desktop notification when unagit is not in front.", rank: 64,
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
		if err == nil {
			a.watchStore.AppendHistory(false, watchBegun(w, on))
		}
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

// watchBegun is the event of a watch begun or let go, for its history
// alone: it is the user's doing, not news.
func watchBegun(w watch.Watch, on bool) watch.Event {
	heading, line := "Watching", w.Subject()+" is watched"
	if !on {
		heading, line = "Not watched", w.Subject()+" is no longer watched"
	}
	return watch.Event{Key: w.Key(), What: w.Label(), Heading: heading, Line: line, Project: w.Project, Title: w.Title, Level: watch.Info}
}

// stopWatching lets several watches go at once.
func (a *App) stopWatching(keys []string, then func()) {
	var stopped []watch.Event
	for _, w := range a.watches {
		if slices.Contains(keys, w.Key()) {
			w.Title = a.watchTitle(w)
			stopped = append(stopped, watchBegun(w, false))
		}
	}
	go func() {
		watches, err := a.watchStore.Change(func(ws []watch.Watch) []watch.Watch {
			return slices.DeleteFunc(ws, func(w watch.Watch) bool { return slices.Contains(keys, w.Key()) })
		})
		if err == nil {
			a.watchStore.AppendHistory(false, stopped...)
		}
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
	a.redrawActivity()
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
	ctx     context.Context
	app     *App
	store   *watch.Store
	poller  *watch.Poller
	watches []watch.Watch
	snap    watch.Snapshot
	stamps  map[string]time.Time
	asked   map[string]time.Time
	askSeen map[string]uint64 // each instance's last ask, as the poller saw it
	// printsOff is, by server, until when its fingerprints are not asked
	// for: they failed, and its watches are read in full meanwhile.
	printsOff map[string]time.Time
	presence  watch.Presence
	askSeq    uint64
	// historyStamp is when the histories were last read.
	historyStamp time.Time
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
		asked: map[string]time.Time{}, askSeen: map[string]uint64{}, printsOff: map[string]time.Time{}}
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
	f.readHistory()
}

// activityLogKept is how many of the newest events of all the Activity
// screen holds: its log, and each thing's story in the detail.
const activityLogKept = 500

// readHistory hands the histories to the interface when any of them moved.
func (f *watchFollower) readHistory() {
	stamp := f.store.HistoryStamp()
	if stamp.IsZero() || stamp.Equal(f.historyStamp) {
		return
	}
	f.historyStamp = stamp
	events := f.store.AllHistory(activityLogKept)
	a := f.app
	a.tv.QueueUpdateDraw(func() { a.setActivityLog(events) })
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
	var inFront []string
	due := map[string]bool{}
	for _, p := range presences {
		snap.Seen = max(snap.Seen, p.Seen)
		anyFocused = anyFocused || p.Focused
		if p.Focused {
			inFront = append(inFront, p.ID)
		}
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
	results := f.read(ask, due)
	behind := f.readBehind(ask)
	var events []watch.Event
	var gone []string
	for i, w := range ask {
		f.asked[w.Key()] = time.Now()
		r := results[i]
		before, known := snap.States[w.Key()]
		after := r.state
		switch {
		case r.err != nil:
			after = before
			after.Error = r.err.Error()
			after.Read = time.Now()
			snap.States[w.Key()] = after
			continue
		case !r.paused.IsZero():
			// Short of its rate limit, the server is left alone; what was
			// read stands, and the row says why it is not read now.
			after = before
			after.Error = "the server's rate limit is nearly spent - asking again at " + r.paused.Format("15:04")
			snap.States[w.Key()] = after
			continue
		case r.same:
			after.Read, after.Error = time.Now(), ""
		}
		if d, ok := behind[w.Key()]; ok {
			after.Base, after.Behind = d.Base, d.Behind
		}
		evs := watchChanges(w, before, after, known, r.ended)
		after.Changed, after.Seq = before.Changed, before.Seq
		if after.Status != before.Status || after.Pipeline != before.Pipeline || !known || len(evs) > 0 {
			after.Changed = time.Now()
		}
		if len(evs) > 0 {
			after.Seq = snap.Seq + 1
		}
		snap.States[w.Key()] = after
		events = append(events, evs...)
		if r.ended != "" {
			gone = append(gone, w.Key())
		}
	}
	seq := snap.Seq
	snap.Add(events...)
	// Each thing's story keeps the news for longer than state.json does.
	f.store.AppendHistory(false, snap.After(seq)...)
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
	for _, e := range events {
		if anyFocused {
			a.logNotice(e, "not sent: unagit "+strings.Join(inFront, ", ")+" in front")
			continue
		}
		a.notifyWatch(f.ctx, e)
	}
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
	if a.currentTab() == pageActivity {
		a.markWatchesSeen()
	}
	if !same {
		a.shareWatchStates()
		a.redrawWatches(marks)
		a.watchCI()
	}
}

// shareWatchStates puts what the watches last read into the lists' CI
// column: a watched merge request's pipeline, a watched branch's. The lists
// are otherwise read on a refresh, and a watched row whose mark stood still
// while its pipeline ran was the point of watching it missed.
func (a *App) shareWatchStates() {
	branches := false
	for _, w := range a.watches {
		st, ok := a.watchSnap.States[w.Key()]
		if !ok || st.Read.IsZero() || st.Error != "" {
			continue
		}
		if w.IID > 0 {
			for i := range a.mrs {
				mr := &a.mrs[i]
				if mr.Instance == w.Instance && mr.IID == w.IID && a.projectPathOfMR(*mr) == w.Project && st.Status != "" {
					mr.Pipeline = st.Status
				}
			}
			continue
		}
		k := branchKey{w.Instance, w.Project, w.Branch}
		if a.branchStatus[k] == st.Status {
			continue
		}
		if a.branchStatus == nil {
			a.branchStatus = map[branchKey]string{}
		}
		if st.Status == "" {
			delete(a.branchStatus, k)
		} else {
			a.branchStatus[k] = st.Status
		}
		branches = true
	}
	if branches {
		a.saveBranchCI()
	}
}

// watchesRunning counts the watches whose pipeline is under way.
func (a *App) watchesRunning() int {
	n := 0
	for _, w := range a.watches {
		if ciStateOf(a.watchSnap.States[w.Key()].Status) == ciRunning {
			n++
		}
	}
	return n
}

// watchHeard is told what a list read of something's pipeline: when it is
// watched and the watch holds something else, the watch is read now rather
// than at its next turn, so the Activity screen and the toasts keep up.
func (a *App) watchHeard(w watch.Watch, status string) {
	if !a.isWatched(w) {
		return
	}
	if st, ok := a.watchSnap.States[w.Key()]; ok && st.Status != status {
		a.watchAsk([]string{w.Key()})
	}
}

// sayWatchEvents shows the news as toasts, the newest few; the rest are on
// the Activity screen.
func (a *App) sayWatchEvents(events []watch.Event) {
	if over := len(events) - toastsKept; over > 0 {
		a.showToast(sevInfo, fmt.Sprintf("%d more changes", over), "They are on the Activity screen ([4]).")
		events = events[over:]
	}
	for _, e := range events {
		heading, about := newsHeading(e)
		a.showToastAbout(levelSeverity(e.Level), heading, about, e.Line)
	}
}

// newsHeading is an event's heading, and the line of what it is about:
// the repository and the merge request's title. An event of an older
// unagit has no heading; its watch's name stands for one.
func newsHeading(e watch.Event) (heading, about string) {
	if e.Heading == "" {
		return e.What, e.Title
	}
	about = e.Project
	if e.Title != "" {
		about += " · " + e.Title
	}
	return e.Heading, about
}

// levelSeverity is an event's level as a message's severity.
func levelSeverity(l watch.Level) severity {
	switch l {
	case watch.Success:
		return sevSuccess
	case watch.Warning:
		return sevWarning
	case watch.Danger:
		return sevError
	}
	return sevInfo
}

// terminalInFront reports whether the user is looking at this unagit: its
// terminal has focus and no editor has it.
//
// A terminal that never says whether it has focus - or a multiplexer that
// keeps it to itself - is taken to be in front when its application is,
// asked of the system now and then; where even that cannot be told, it is,
// as unagit was just started there.
func (a *App) terminalInFront() bool {
	q := a.quiet
	if q == nil || q.suspended.Load() {
		return false
	}
	if q.focusKnown.Load() {
		return q.focused.Load()
	}
	a.frontMu.Lock()
	defer a.frontMu.Unlock()
	if time.Since(a.frontSeen.at) < frontAskEvery {
		return a.frontSeen.front
	}
	front := ""
	if a.frontApp != nil {
		front = a.frontApp()
	} else {
		front = notify.FrontApp(context.Background())
	}
	mine := notify.Detect(os.Getenv).App(os.Getenv)
	a.frontSeen.at = time.Now()
	a.frontSeen.front = front == "" || mine == "" || strings.EqualFold(front, mine)
	return a.frontSeen.front
}

// frontAskEvery is how often the system is asked which application is in
// front.
const frontAskEvery = 3 * time.Second

// notifyWatch sends a desktop notification of an event: through the
// terminal where it can show one, the system otherwise. It runs off the
// loop; the sequence is written on it, between two draws.
func (a *App) notifyWatch(ctx context.Context, e watch.Event) {
	heading, about := newsHeading(e)
	way, err := a.sendNotification(ctx, heading, noticeTitle(about), e.Line)
	switch {
	case err != nil:
		a.logNotice(e, "failed through the "+way+": "+err.Error())
	case way == "":
		a.logNotice(e, "not sent: notifications are off")
	default:
		a.logNotice(e, "sent through the "+way)
	}
}

// noticeLogMax is how large notify.log grows before its older half goes.
const noticeLogMax = 128 << 10

// logNotice notes in watch/notify.log what became of a change's desktop
// notification - sent, and which way, or why not - so one that did not
// come can be explained afterwards. It is this machine's, beside the
// watches.
func (a *App) logNotice(e watch.Event, what string) {
	path := filepath.Join(a.cfg.WatchDir(), "notify.log")
	line := fmt.Sprintf("%s  %s · %s  ->  %s\n", time.Now().Format(time.RFC3339), e.What, e.Line, what)
	a.noticeMu.Lock()
	defer a.noticeMu.Unlock()
	if old, err := os.ReadFile(path); err == nil && len(old) > noticeLogMax {
		keep := old[len(old)/2:]
		if i := bytes.IndexByte(keep, '\n'); i >= 0 {
			keep = keep[i+1:]
		}
		_ = os.WriteFile(path, keep, 0o600)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// sendNotification shows title and body as Settings › Integrations says.
func (a *App) sendNotification(ctx context.Context, title, subtitle, body string) (string, error) {
	mode := ""
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !a.onLoopWait(wait, func() { mode = a.cfg.Integrations.Notifications }) {
		return "", fmt.Errorf("unagit is closing")
	}
	return a.sendNotificationAs(mode, title, subtitle, body)
}

// noticeTitleMax is how much of a merge request's title a toast and a
// notification carry: enough to know which one, not the whole of it.
const noticeTitleMax = 64

// noticeTitle is a merge request's title cut to what a notice carries.
func noticeTitle(title string) string { return trunc(strings.TrimSpace(title), noticeTitleMax) }

// sendNotificationAs shows title and body through the way a mode chooses,
// and says which it took: "terminal", "system" or "" for none. It runs off
// the loop.
func (a *App) sendNotificationAs(mode, title, subtitle, body string) (string, error) {
	if a.notifier != nil {
		a.notifier(title, body)
		return "notifier", nil
	}
	q := a.quiet
	free := q != nil && !q.suspended.Load()
	away := q != nil && q.focusKnown.Load() && !q.focused.Load()
	term := notify.Detect(os.Getenv)
	toTerminal, toSystem := notify.Route(mode, term, free, away)
	if toTerminal {
		seq := term.SequenceAbout(title, subtitle, body)
		a.tv.QueueUpdate(func() {
			if q == nil || q.suspended.Load() {
				return
			}
			if tty, ok := q.Screen.Tty(); ok {
				tty.Write(seq)
			}
		})
		return "terminal", nil
	}
	if toSystem {
		return "system", notify.SystemNotify(title, subtitle, body)
	}
	return "", nil
}
