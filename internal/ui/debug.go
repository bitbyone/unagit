package ui

import (
	"context"
	"fmt"
	"os"
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
	fire        func()
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

	event := func(what, line string, level watch.Level) watch.Event {
		return watch.Event{Key: "debug", What: what, Line: line, Level: level}
	}
	mr, branch := "acme/gateway !7", "acme/gateway main"
	news := []watch.Event{
		event(mr, "pipeline started", watch.Info),
		event(mr, "new head 1a2b3c4d · pipeline started", watch.Info),
		event(mr, "pipeline passed", watch.Success),
		event(mr, "pipeline failed · unit tests", watch.Danger),
		event(branch, "pipeline cancelled", watch.Warning),
		event(branch, "pipeline waits for a manual job", watch.Warning),
		event(mr, "merge request merged · no longer watched", watch.Success),
		event(mr, "merge request closed · no longer watched", watch.Warning),
	}
	toast := func(sev severity, word string) debugTrigger {
		return debugTrigger{"Toast", word, func() {
			a.showToast(sev, "Debug · "+word, "A toast of this severity, as news from the background shows.")
		}}
	}
	v.triggers = []debugTrigger{
		toast(sevInfo, "info"),
		toast(sevSuccess, "success"),
		toast(sevWarning, "warning"),
		toast(sevError, "danger"),
		{"Toast", "six at once", func() {
			var evs []watch.Event
			for i := range 6 {
				evs = append(evs, news[i%len(news)])
			}
			a.sayWatchEvents(evs)
		}},
	}
	for _, e := range news {
		v.triggers = append(v.triggers, debugTrigger{"Watched", e.Line, func() { a.debugNews(e) }})
	}
	v.triggers = append(v.triggers,
		debugTrigger{"Watched", "pipeline failed, in 10 s - switch away to see the desktop", func() {
			a.done("in 10 s: pipeline failed")
			e := news[3]
			go func() {
				select {
				case <-time.After(10 * time.Second):
					a.tv.QueueUpdateDraw(func() { a.debugNews(e) })
				case <-a.stopFollowing:
				}
			}()
		}},
		debugTrigger{"Desktop", "as Settings › Integrations chooses", func() { a.testNotification(a.cfg.Integrations.Notifications, 0) }},
		debugTrigger{"Desktop", "as Settings chooses, in 10 s - switch away", func() { a.testNotification(a.cfg.Integrations.Notifications, 10*time.Second) }},
		debugTrigger{"Desktop", "through the terminal, in 10 s - switch away", func() { a.testNotification(notify.Terminal, 10*time.Second) }},
		debugTrigger{"Desktop", "through the system", func() { a.testNotification(notify.System, 0) }},
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
		v.table.SetCell(i, 1, tview.NewTableCell(esc(t.name)).SetTextColor(colText).SetExpansion(1))
	}
	v.table.Select(max(row, 0), 0)
}

// debugNews is a watched change as it arrives: a toast here, and a desktop
// notification when this unagit is not in front - the poller asks every
// instance, this asks this one alone.
func (a *App) debugNews(e watch.Event) {
	a.sayWatchEvents([]watch.Event{e})
	if a.terminalInFront() {
		return
	}
	go a.notifyWatch(context.Background(), e)
}

// testNotification sends a desktop notification the way a mode chooses,
// after a while to switch away in - a terminal shows none for its window
// in front - and says which way it went.
func (a *App) testNotification(mode string, after time.Duration) {
	if after > 0 {
		a.done(fmt.Sprintf("in %d s: switch to another window", int(after.Seconds())))
	}
	go func() {
		select {
		case <-time.After(after):
		case <-a.stopFollowing:
			return
		}
		way, err := a.sendNotificationAs(mode, "unagit · test", fmt.Sprintf("A notification through %s.", notificationModeName(mode)))
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
