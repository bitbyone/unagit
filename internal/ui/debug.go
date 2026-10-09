package ui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/notify"
	"github.com/tobola/unagit/internal/watch"
)

// Settings › Debug is there only when unagit was started with --debug. It
// fires on request what otherwise comes when a server says so - a toast of
// each severity, a watched pipeline's news, a desktop notification by each
// way - since a pipeline that fails, or one waiting for a hand, cannot be
// had when it is wanted to see how it looks.

// sectionDebug is the last section, listed only with --debug.
const sectionDebug = sectionNotifications + 1

// debugSectionName is its name in the list.
const debugSectionName = "Debug"

// WithDebug lists Settings › Debug.
func (a *App) WithDebug(on bool) *App {
	a.debug = on
	return a
}

// debugTrigger is one row of the section: what it fires.
type debugTrigger struct {
	group, name string
	// levels are the severities of the news it fires, in their order.
	levels []watch.Level
	fire   func()
}

// debugView is the section: what unagit knows of the terminal, and the
// triggers.
type debugView struct {
	*tview.Flex
	app      *App
	state    *tview.TextView
	table    *tview.Table
	triggers []debugTrigger
}

func (s *settingsView) newDebugView() *debugView {
	a := s.app
	v := &debugView{Flex: tview.NewFlex().SetDirection(tview.FlexRow), app: a}
	v.state = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	v.state.SetTextColor(colText)
	v.table = tview.NewTable().SetSelectable(true, false).SetSeparator(' ')
	v.table.SetSelectedStyle(styleSelected)
	v.AddItem(v.state, 6, 0, false).AddItem(v.table, 0, 1, true)
	box(v.Box, debugSectionName).SetBorderPadding(0, 0, 1, 1)

	news := debugNewsCases()
	v.triggers = []debugTrigger{{"Toast", "six at once", nil, func() {
		var evs []watch.Event
		for i := range 6 {
			evs = append(evs, news[i%len(news)].events...)
		}
		a.sayWatchEvents(evs[:6])
	}}}
	for _, c := range news {
		v.triggers = append(v.triggers, debugTrigger{"Watched", c.name, eventLevels(c.events), func() { a.debugNews(c.events...) }})
	}
	failed := news[slices.IndexFunc(news, func(c debugNewsCase) bool { return c.name == "Pipeline failed" })]
	v.triggers = append(v.triggers,
		debugTrigger{"Watched", "Pipeline failed, in 4 s - switch away to see the desktop", eventLevels(failed.events), func() {
			a.afterDebugDelay("pipeline failed", func() { a.debugNews(failed.events...) })
		}},
		// Each after a while to switch to another program in: the way is
		// chosen when it is sent, from where the user then is.
		debugTrigger{"Desktop", "as Settings › Integrations chooses, in 4 s", nil, func() {
			a.testNotification(a.cfg.Integrations.Notifications, a.debugDelayOr())
		}},
		debugTrigger{"Desktop", "through the system, in 4 s", nil, func() { a.testNotification(notify.System, a.debugDelayOr()) }},
		debugTrigger{"Desktop", "through the terminal, in 4 s", nil, func() { a.testNotification(notify.Terminal, a.debugDelayOr()) }},
	)
	v.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		switch ev.Key() {
		case tcell.KeyEnter:
			v.fire()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			}
		}
		return ev
	})
	hintPanel(v.Box, func() string { return "Enter fire" }, 0, 0, 1, 1)
	v.fill(a)
	return v
}

// fire runs the trigger under the cursor and says again how the terminal
// stands.
func (v *debugView) fire() {
	row, _ := v.table.GetSelection()
	if row < 0 || row >= len(v.triggers) {
		return
	}
	v.triggers[row].fire()
	v.fill(v.app)
}

// fill draws the section again.
func (v *debugView) fill(a *App) {
	term := notify.Detect(os.Getenv)
	yesNo := func(b bool) string {
		if b {
			return tag(colOn) + "yes" + tagEnd
		}
		return tag(colWarn) + "no" + tagEnd
	}
	name := term.Name
	if name == "" {
		name = "not known"
	}
	if term.Muxer != "" {
		name += " in " + term.Muxer
	}
	if term.Tmux {
		name += " under tmux"
	}
	known := a.quiet != nil && a.quiet.focusKnown.Load()
	lines := []string{
		tag(colMuted) + "terminal      " + tagEnd + esc(name) + tag(colMuted) + " · sequence " + tagEnd + yesNo(term.Protocol != notify.None),
		tag(colMuted) + "says focus    " + tagEnd + yesNo(known),
		tag(colMuted) + "in front      " + tagEnd + yesNo(a.terminalInFront()),
		tag(colMuted) + "desktop       " + tagEnd + esc(notificationModeName(a.cfg.Integrations.Notifications)) +
			tag(colMuted) + " · now through " + tagEnd + esc(a.notificationWay()),
		tag(colMuted) + "Watched news is a toast here, and on the desktop when no unagit is in front." + tagEnd,
	}
	v.state.SetText(strings.Join(lines, "\n"))
	row, _ := v.table.GetSelection()
	v.table.Clear()
	for i, t := range v.triggers {
		v.table.SetCell(i, 0, tview.NewTableCell(tag(colMuted)+esc(t.group)+tagEnd).SetSelectable(true))
		var levels []string
		for _, l := range t.levels {
			levels = append(levels, tag(toastRole(levelSeverity(l), "border"))+string(l)+tagEnd)
		}
		v.table.SetCell(i, 1, tview.NewTableCell(strings.Join(levels, tag(colMuted)+" + "+tagEnd)))
		v.table.SetCell(i, 2, tview.NewTableCell(esc(t.name)).SetTextColor(colText).SetExpansion(1))
	}
	v.table.Select(max(row, 0), 0)
}

// debugNews is watched changes as they arrive: toasts here, and desktop
// notifications when this unagit is not in front - the poller asks every
// instance, this asks this one alone.
func (a *App) debugNews(events ...watch.Event) {
	a.sayWatchEvents(events)
	if a.terminalInFront() {
		return
	}
	for _, e := range events {
		go a.notifyWatch(context.Background(), e)
	}
}

// eventLevels are the severities of events, in their order.
func eventLevels(events []watch.Event) []watch.Level {
	var out []watch.Level
	for _, e := range events {
		out = append(out, e.Level)
	}
	return out
}

// debugNewsCase is a situation a watch tells of, and what it says of it.
type debugNewsCase struct {
	name   string
	events []watch.Event
}

// debugNewsCases is every situation a watch tells of, each as the poller
// would say it: made by the same watchChanges out of two readings, so
// what Debug shows is what will come.
func debugNewsCases() []debugNewsCase {
	mr := watch.Watch{Project: "acme/gateway", IID: 334, Title: "Rate limiting for the public API, with a token bucket per client"}
	branch := watch.Watch{Project: "acme/gateway", Branch: "feature/token-bucket"}
	was := watch.State{
		Title: mr.Title, Known: true, Head: "1a2b3c4d5e6f", Commits: 4, Comments: 2, Approvers: []string{"jdoe"},
		Pipeline: 8120, Status: "success", SHA: "1a2b3c4d5e6f", User: "Jane Doe",
		HeadBy: "Jane Doe", HeadTitle: "Count the tokens per client, not per route",
	}
	onBranch := watch.State{Pipeline: 8120, Status: "success", SHA: "1a2b3c4d5e6f", User: "Jane Doe", Base: "main", Behind: 1}
	pushed := func(st watch.State, commits int) watch.State {
		st.Head, st.Commits = "9f8e7d6c5b4a", st.Commits+commits
		st.HeadBy, st.HeadTitle = "John Smith", "Refill the bucket lazily on the next request instead of on a timer"
		return st
	}
	pipeline := func(st watch.State, status, failed string) watch.State {
		st.Pipeline, st.Status, st.SHA, st.Failed, st.User = 8121, status, "9f8e7d6c5b4a", failed, "John Smith"
		return st
	}
	running := pipeline(was, "running", "")
	jobs := func(st watch.State, again bool, deploy, by string) watch.State {
		st.Again = again
		st.Jobs = []watch.JobState{{Name: "test:unit", Status: "success"}, {Name: "deploy:staging", Status: deploy, User: by}}
		return st
	}
	done := jobs(pipeline(was, "success", ""), false, "manual", "")
	byHand := jobs(pipeline(was, "running", ""), true, "running", "Jane Doe")
	with := func(st watch.State, change func(*watch.State)) watch.State {
		st.Approvers = slices.Clone(st.Approvers)
		change(&st)
		return st
	}
	cases := []struct {
		name          string
		w             watch.Watch
		before, after watch.State
		ended         string
	}{
		{"Pipeline running", mr, was, running, ""},
		{"Pipeline passed", mr, running, pipeline(was, "success", ""), ""},
		{"Pipeline failed", mr, running, pipeline(was, "failed", "test:unit"), ""},
		{"Pipeline cancelled", branch, pipeline(onBranch, "running", ""), pipeline(onBranch, "canceled", ""), ""},
		{"Manual job waiting", branch, pipeline(onBranch, "running", ""), pipeline(onBranch, "manual", ""), ""},
		{"Pipeline running on a branch", branch, onBranch, pipeline(onBranch, "running", ""), ""},
		{"Manual job started", mr, done, byHand, ""},
		{"Job passed, run by hand", mr, byHand, jobs(pipeline(was, "success", ""), true, "success", "Jane Doe"), ""},
		{"Job failed, run by hand", mr, byHand, jobs(pipeline(was, "failed", "deploy:staging"), true, "failed", "Jane Doe"), ""},
		{"New commit in MR", mr, was, pushed(was, 1), ""},
		{"New commits in MR", mr, was, pushed(was, 3), ""},
		{"A push: its commit and its pipeline", mr, was, pipeline(pushed(was, 1), "running", ""), ""},
		{"MR force-pushed", mr, was, pushed(was, 0), ""},
		{"New comment in MR", mr, was, with(was, func(s *watch.State) { s.Comments++ }), ""},
		{"New comments in MR", mr, was, with(was, func(s *watch.State) { s.Comments += 3 }), ""},
		{"MR approved", mr, was, with(was, func(s *watch.State) { s.Approvers = append(s.Approvers, "msmith") }), ""},
		{"Approval withdrawn", mr, was, with(was, func(s *watch.State) { s.Approvers = nil }), ""},
		{"Base moved on", branch, onBranch, with(onBranch, func(s *watch.State) { s.Behind = 4 }), ""},
		{"MR merged", mr, was, was, "merge request merged"},
		{"MR closed", mr, was, was, "merge request closed"},
		{"Branch deleted", branch, onBranch, onBranch, "branch deleted on origin"},
	}
	out := make([]debugNewsCase, 0, len(cases))
	for _, c := range cases {
		events := watchChanges(c.w, c.before, c.after, true, c.ended)
		for i := range events {
			events[i].Key = "debug"
		}
		out = append(out, debugNewsCase{c.name, events})
	}
	return out
}

// testNotification sends a desktop notification the way a mode chooses,
// after a while to switch away in - a terminal shows none for its window
// in front - and says which way it went.
func (a *App) testNotification(mode string, after time.Duration) {
	if after > 0 {
		a.done(fmt.Sprintf("in %d s: switch to another program", int(after.Round(time.Second).Seconds())))
	}
	go func() {
		select {
		case <-time.After(after):
		case <-a.stopFollowing:
			return
		}
		way, err := a.sendNotificationAs(mode, "Test notification", "Rate limiting for the public API", fmt.Sprintf("A notification through %s.", notificationModeName(mode)))
		a.tv.QueueUpdateDraw(func() {
			switch {
			case err != nil:
				a.errorf("%v", err)
			case way == "":
				a.flash("nothing was sent: " + notificationModeName(mode) + " has no way here")
			case way == "terminal":
				a.done("sent through the terminal, which shows it only while its window is not in front")
			default:
				a.done("sent through the " + way)
			}
		})
	}()
}

// notificationWay is the way a notification would go now, as Settings
// chooses.
func (a *App) notificationWay() string {
	q := a.quiet
	free := q != nil && !q.suspended.Load()
	away := q != nil && q.focusKnown.Load() && !q.focused.Load()
	term, system := notify.Route(a.cfg.Integrations.Notifications, notify.Detect(os.Getenv), free, away)
	switch {
	case term:
		return "the terminal"
	case system:
		return "the system"
	}
	return "nothing"
}

// debugDelay is how long Debug waits before what it fires on the desktop:
// long enough to switch to another program, short enough to wait for.
const debugDelay = 4 * time.Second

// debugDelayOr is debugDelay, or what a test set instead.
func (a *App) debugDelayOr() time.Duration {
	if a.debugWait > 0 {
		return a.debugWait
	}
	return debugDelay
}

// afterDebugDelay runs fire on the loop after debugDelay, saying first
// what is coming.
func (a *App) afterDebugDelay(what string, fire func()) {
	after := a.debugDelayOr()
	a.done(fmt.Sprintf("in %d s: %s - switch to another program", int(after.Round(time.Second).Seconds()), what))
	go func() {
		select {
		case <-time.After(after):
			a.tv.QueueUpdateDraw(fire)
		case <-a.stopFollowing:
		}
	}()
}
