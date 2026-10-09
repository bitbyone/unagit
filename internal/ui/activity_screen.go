package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/watch"
)

// activityView is what the Activity screen has besides its list (a pane,
// with its detail): the band of the editors open and what is watched, and
// the log under the list.
type activityView struct {
	app      *App
	pane     *pane
	root     *tview.Flex
	band     *tview.Flex
	cards    *cardsView
	watching *tview.TextView
	log      *tview.Table
	// logKeys is the key of the thing each row of the log is about, ""
	// for the line of the last visit.
	logKeys []string
	// bandH and logH are the heights last laid out.
	bandH, logH int
	// autoDetail is a detail that opened by itself, for the room there was.
	autoDetail bool
	// current is the panel with the focus, which alone is lit, and which
	// the focus comes back to after a dialog.
	current tview.Primitive
}

const (
	// activityWide is the width from which the detail stands beside the
	// list without being asked for.
	activityWide = 120
	// activityTall is the height from which the cards have borders.
	activityTall = 30
	// watchingWidth is the Watching panel's, its border included.
	watchingWidth = 30
	// activityLogShare is the part of the rows under the band the log
	// takes; activityLogLeast its fewest lines of events, activityListLeast
	// the fewest rows the list and the detail keep.
	activityLogShare  = 0.3
	activityLogLeast  = 3
	activityListLeast = 8
)

func (a *App) newActivityPane() *pane {
	p := a.newPane("Activity")
	p.markable = true
	p.stackBelow = activityWide
	// The list's rows want more room than the detail's lines.
	p.listWeight, p.detailWeight = 3, 2
	p.body.ResizeItem(p.kept, 0, p.listWeight)
	v := &activityView{app: a, pane: p}
	a.activity = v
	var filtered []int

	p.headline = func() string {
		need, under := 0, 0
		for _, it := range a.activityRows {
			switch it.section {
			case sectionNeedsYou:
				need++
			case sectionUnderWay:
				under++
			}
		}
		var parts []string
		if need > 0 {
			parts = append(parts, tag(role("activity.needs"))+fmt.Sprintf("%d need you", need)+tagEnd)
		}
		if under > 0 {
			parts = append(parts, tag(role("activity.under_way"))+fmt.Sprintf("%d under way", under)+tagEnd)
		}
		parts = append(parts, tag(colMuted)+fmt.Sprintf("%d watched · %d %s", len(a.watches), len(a.agentRows), plural(len(a.agentRows), "agent", "agents"))+tagEnd)
		if n := a.watchUnseen(); n > 0 {
			parts = append(parts, tag(role("activity.new"))+fmt.Sprintf("%d new", n)+tagEnd)
		}
		var read time.Time
		for _, st := range a.watchSnap.States {
			if st.Read.After(read) {
				read = st.Read
			}
		}
		if !read.IsZero() {
			parts = append(parts, tag(colMuted)+"read "+humanAge(read)+tagEnd)
		}
		if poller := a.watchSnap.Poller; poller != "" && !a.watchPolling.Load() {
			pid, _, _ := strings.Cut(poller, "-")
			parts = append(parts, tag(colMuted)+"followed by unagit "+pid+tagEnd)
		}
		switch {
		case a.agentsError != "":
			parts = append(parts, tag(colWarn)+esc(a.agentsError)+tagEnd)
		case len(a.agentRows) > 0 && a.herdr() == nil:
			parts = append(parts, tag(colMuted)+"herdr is off: where agents run, not what they do"+tagEnd)
		}
		return strings.Join(parts, tag(colMuted)+" · "+tagEnd)
	}
	render := func(query string) {
		a.activityRows = a.activityItems()
		filtered = filterActivity(a, a.activityRows, query)
		a.drawActivity(p, a.activityRows, filtered)
		v.setTitles()
		v.drawBand()
		v.drawLog()
		v.followDetail()
		p.updateHeader()
	}
	p.onQuery = render
	p.reload = func() { render(p.query) }

	// Enter does what the row is for; the detail follows the cursor by
	// itself, and d or l go into it.
	p.onDetail = func(idx int, focus bool) {
		it, ok := a.activityItemAt(idx)
		if !ok {
			return
		}
		if focus {
			if it.kind == activityAgent {
				a.goToAgent(it.agent)
			} else {
				a.showWatchedPipeline(it.watch)
			}
			return
		}
		v.showDetail(it, false)
	}
	p.onOpen = func(ask bool) {
		it, ok := a.activityItemAt(p.selectedIndex())
		if !ok || it.kind != activityAgent {
			return
		}
		a.withEditor(ask, func(ed *editors.Editor) { a.openNow(it.agent.Dir, a.agentRecord(it.agent), ed) })
	}
	p.selection = func() (string, []uiAction) {
		if len(p.marks) > 0 {
			var ws []watch.Watch
			for idx := range p.marks {
				if it, ok := a.activityItemAt(idx); ok && it.kind == activityWatch {
					ws = append(ws, it.watch)
				}
			}
			return fmt.Sprintf("Actions · %d marked", len(ws)), []uiAction{
				{name: "Stop Watching", about: "Let every marked watch go; their rows leave the list and their marks the other lists.", keys: "x", rank: 10,
					when: func() bool { return len(ws) > 0 }, run: func() { a.unwatch(p, ws) }},
			}
		}
		it, ok := a.activityItemAt(p.selectedIndex())
		if !ok {
			return "", nil
		}
		if it.kind == activityAgent {
			return "Actions · " + agentName(it.agent.Kind), a.agentActionsOf(p, it.agent)
		}
		return "Actions · " + it.watch.Label(), a.watchedActions(p, it.watch)
	}
	p.screen = func() (string, []uiAction) { return "Activity", a.activityScreenActions(p) }
	p.extraKeys = v.paneKeys
	p.focusElsewhere = func() tview.Primitive {
		if v.current == p.table || v.current == p.detail {
			return nil
		}
		return v.current
	}
	p.focused = func(to tview.Primitive) {
		v.current = to
		focusBox(v.log.Box, false)
		focusBox(v.cards.Box, false)
		focusBox(v.watching.Box, false)
		v.drawLog()
	}
	p.onSelect = v.lightLog
	p.table.SetTitle(panelTitle("Activity", "a"))

	v.cards = newCardsView(a)
	v.cards.SetInputCapture(v.panelKeys(v.cards, func(ev *tcell.EventKey) bool {
		if v.cards.keys(ev) {
			return true
		}
		_, acts := v.cards.selection()
		return runPanelKey(acts, ev)
	}, v.cards.selection))
	v.watching = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	v.watching.SetTextColor(colText)
	box(v.watching.Box, "").SetBorderPadding(0, 0, 1, 1)
	v.watching.SetTitle(panelTitle("Watching", "W"))
	watchingActs := func() (string, []uiAction) {
		return "Watching", []uiAction{{name: "Watches…", about: "Everything watched in one list: go to one, or stop watching it.", keys: "Enter", rank: 10, run: a.showWatches}}
	}
	v.watching.SetInputCapture(v.panelKeys(v.watching, func(ev *tcell.EventKey) bool {
		_, acts := watchingActs()
		return runPanelKey(acts, ev)
	}, watchingActs))

	v.band = tview.NewFlex().
		AddItem(v.cards, 0, 1, false).
		AddItem(v.watching, watchingWidth, 0, false)

	v.log = tview.NewTable().SetSelectable(true, false).SetSeparator(' ').SetFixed(1, 0)
	v.log.SetSelectedStyle(styleSelected)
	box(v.log.Box, "")
	v.log.SetTitle(panelTitle("Log", "L"))
	logActs := func() (string, []uiAction) {
		return "Log", []uiAction{
			{name: "Go to Its Row", about: "Put the list's cursor on what the event is about, and go there.", keys: "Enter", rank: 10, run: func() { v.chooseLogRow() }},
			{name: "Show Log", about: "The log in front, over the screen: every event at full length.", keys: "z", rank: 20, run: a.showActivityLog},
		}
	}
	v.log.SetInputCapture(v.panelKeys(v.log, func(ev *tcell.EventKey) bool {
		_, acts := logActs()
		return runPanelKey(acts, ev)
	}, logActs))

	v.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.filter, 1, 0, false).
		AddItem(v.band, 0, 0, false).
		AddItem(p.body, 0, 1, true).
		AddItem(v.log, 0, 0, false).
		AddItem(p.headerRow, 1, 0, false)
	v.root.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		v.root.ResizeItem(p.headerRow, p.headerRow.fit(w), 0)
		v.layout(h)
		return x, y, w, h
	})
	p.root = v.root
	return p
}

// layout shares the screen's height: the band its rows when there is
// something to show in it, the log its share, the list and the detail the
// rest - never fewer than activityListLeast, for which the cards fold to a
// line and then the log gives way to its least.
func (v *activityView) layout(h int) {
	a := v.app
	rows := h - 2
	band := 0
	if len(a.editorsOpen) > 0 || len(a.watches) > 0 {
		band = 6
		if h < activityTall {
			band = 3
		}
	}
	logH := 0
	if rows-band >= activityListLeast+activityLogLeast+2 {
		logH = max(activityLogLeast+2, int(float64(rows-band)*activityLogShare+0.5))
		if rows-band-logH < activityListLeast {
			band = min(band, 3)
			logH = max(activityLogLeast+2, rows-band-activityListLeast)
		}
	}
	if w := v.app.tabsWidth; w > 0 && w < activityWide && v.autoDetail && v.pane.detailShown {
		// The detail opened by itself where there was room for it; where
		// there is none, it waits for Enter.
		v.autoDetail = false
		go v.app.tv.QueueUpdateDraw(v.pane.hideDetail)
	}
	if band != v.bandH {
		v.bandH = band
		v.root.ResizeItem(v.band, band, 0)
		v.cards.compact = band < 6
		v.drawBand()
	}
	if logH != v.logH {
		v.logH = logH
		v.root.ResizeItem(v.log, logH, 0)
	}
}

// runPanelKey is runKey with Enter too: runKey leaves Enter to the widget,
// and a panel's Enter is its first action.
func runPanelKey(acts []uiAction, ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyEnter && ev.Modifiers() == tcell.ModNone {
		for _, act := range acts {
			if act.keys == "Enter" && (act.when == nil || act.when()) {
				act.run()
				return true
			}
		}
	}
	return runKey(acts, ev)
}

// panelTitle is a panel's icon, where the terminal has a Nerd Font, its
// name, and the key that goes to it, in brackets and quieter than the
// name: it is there to be found, not read.
func panelTitle(name, key string) string {
	title := " "
	if icon := columnIcons[name]; icon != "" {
		title += icon + " "
	}
	if name != "" {
		title += name + " "
	}
	return title + tag(role("activity.panel_key")) + esc("["+key+"]") + tagEnd + " "
}

// setTitles names the panels: again on every draw, since the icons are
// the theme's and the terminal's.
func (v *activityView) setTitles() {
	v.pane.table.SetTitle(panelTitle("Activity", "a"))
	v.watching.SetTitle(panelTitle("Watching", "W"))
	v.log.SetTitle(panelTitle("Log", "L"))
	v.cards.SetTitle(panelTitle("Open", "e"))
}

// paneKeys are the screen's keys over the list and the detail, heard
// before the pane's own: Tab round the panels, and j or k past the end of
// what is in a panel on to the panel beyond it.
func (v *activityView) paneKeys(ev *tcell.EventKey) bool {
	if v.cycleKeys(ev) {
		return true
	}
	p := v.pane
	if p.detail.HasFocus() {
		if ev.Key() == tcell.KeyEnter && ev.Modifiers() == tcell.ModNone {
			// Enter in the detail does what it does on the row.
			p.onDetail(p.selectedIndex(), true)
			return true
		}
		row, _ := p.detail.GetScrollOffset()
		_, _, _, height := p.detail.GetInnerRect()
		switch {
		case isKey(ev, 'j', tcell.KeyDown) && row+height >= p.detail.GetWrappedLineCount():
			return v.move(p.detail, 'j')
		case isKey(ev, 'k', tcell.KeyUp) && row == 0:
			return v.move(p.detail, 'k')
		}
		return false
	}
	first, last := selectableEnds(p.table)
	row, _ := p.table.GetSelection()
	switch {
	case isKey(ev, 'j', tcell.KeyDown) && row >= last:
		return v.move(p.table, 'j')
	case isKey(ev, 'k', tcell.KeyUp) && row <= first:
		return v.move(p.table, 'k')
	case isKey(ev, 'l', tcell.KeyRight) && !p.detailShown:
		return v.move(p.table, 'l')
	}
	return false
}

// isKey reports whether ev is the letter or the arrow.
func isKey(ev *tcell.EventKey, r rune, k tcell.Key) bool {
	return ev.Key() == k || ev.Key() == tcell.KeyRune && ev.Rune() == r && ev.Modifiers() == tcell.ModNone
}

// selectableEnds are a table's first and last rows that can be chosen.
func selectableEnds(t *tview.Table) (first, last int) {
	first, last = -1, -1
	for row := 0; row < t.GetRowCount(); row++ {
		cell := t.GetCell(row, 0)
		if cell == nil || cell.NotSelectable {
			continue
		}
		if first < 0 {
			first = row
		}
		last = row
	}
	return first, last
}

// cycleKeys go round the panels with Tab.
func (v *activityView) cycleKeys(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyTab:
		v.cycle(1)
		return true
	case tcell.KeyBacktab:
		v.cycle(-1)
		return true
	}
	return false
}

// move goes from a panel to the one beside it in a direction, h j k l as
// they lie on the screen: the band above, the list and the detail side by
// side, the log below. It reports whether there was one.
func (v *activityView) move(from tview.Primitive, dir rune) bool {
	p := v.pane
	band := v.bandH > 0
	cards := band && len(v.app.editorsOpen) > 0
	log := v.logH > 0
	var to tview.Primitive
	switch from {
	case p.table:
		switch {
		case dir == 'j' && log:
			to = v.log
		case dir == 'k' && cards:
			to = v.cards
		case dir == 'k' && band:
			to = v.watching
		case dir == 'l' && p.detailShown:
			to = p.detail
		}
	case p.detail:
		switch {
		case dir == 'j' && log:
			to = v.log
		case dir == 'k' && band:
			to = v.watching
		case dir == 'h':
			to = p.table
		}
	case v.log:
		if dir == 'k' {
			to = p.table
		}
	case v.cards:
		switch {
		case dir == 'j':
			to = p.table
		case dir == 'l':
			to = v.watching
		}
	case v.watching:
		switch {
		case dir == 'h' && cards:
			to = v.cards
		case dir == 'j' && p.detailShown:
			to = p.detail
		case dir == 'j':
			to = p.table
		}
	}
	if to == nil {
		return false
	}
	v.focus(to)
	return true
}

// panelKeys are the keys of a panel that is not the pane's: its own first,
// then going on to the panel beyond what it holds, Tab round, Esc back to
// the list, its actions (Alt-Enter) and the screen's.
func (v *activityView) panelKeys(self tview.Primitive, own func(*tcell.EventKey) bool, selection func() (string, []uiAction)) func(*tcell.EventKey) *tcell.EventKey {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		a := v.app
		if own(ev) || v.cycleKeys(ev) {
			return nil
		}
		if a.actionKeys(ev, selection, v.pane.screen) {
			return nil
		}
		if t, ok := self.(*tview.Table); ok {
			first, last := selectableEnds(t)
			row, _ := t.GetSelection()
			if isKey(ev, 'k', tcell.KeyUp) && row <= first && v.move(self, 'k') ||
				isKey(ev, 'j', tcell.KeyDown) && row >= last && v.move(self, 'j') {
				return nil
			}
		} else {
			for _, dir := range []struct {
				r rune
				k tcell.Key
			}{{'h', tcell.KeyLeft}, {'j', tcell.KeyDown}, {'k', tcell.KeyUp}, {'l', tcell.KeyRight}} {
				if isKey(ev, dir.r, dir.k) {
					v.move(self, dir.r)
					return nil
				}
			}
		}
		switch ev.Key() {
		case tcell.KeyEsc:
			v.focus(v.pane.table)
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'q':
				a.tv.Stop()
				return nil
			case '?':
				a.showHelp()
				return nil
			case 'j', 'k', 'g', 'G':
				return ev
			}
			if a.tabKey(ev.Rune()) {
				return nil
			}
		}
		return ev
	}
}

// panels are what Tab goes round, in order, those on screen only.
func (v *activityView) panels() []tview.Primitive {
	p := v.pane
	out := []tview.Primitive{p.table}
	if p.detailShown {
		out = append(out, p.detail)
	}
	if v.logH > 0 {
		out = append(out, v.log)
	}
	if v.bandH > 0 {
		if len(v.app.editorsOpen) > 0 {
			out = append(out, v.cards)
		}
		out = append(out, v.watching)
	}
	return out
}

// cycle moves the focus to the next panel, or the one before.
func (v *activityView) cycle(by int) {
	panels := v.panels()
	at := 0
	for i, pr := range panels {
		if pr.HasFocus() {
			at = i
		}
	}
	v.focus(panels[(at+by+len(panels))%len(panels)])
}

// focus puts the focus on a panel and lights its border alone.
func (v *activityView) focus(to tview.Primitive) {
	p := v.pane
	switch to {
	case p.table:
		p.focusTable()
	case p.detail:
		p.focusDetail()
	default:
		focusBox(p.table.Box, false)
		focusBox(p.detail.Box, false)
		p.detailFocused = false
		p.filtering = false
		v.app.tv.SetFocus(to)
	}
	v.current = to
	focusBox(v.log.Box, to == v.log)
	focusBox(v.cards.Box, to == v.cards)
	focusBox(v.watching.Box, to == v.watching)
	v.drawLog()
	p.updateHeader()
}

// followDetail keeps the detail beside the list where there is room, on
// the row under the cursor.
func (v *activityView) followDetail() {
	p := v.pane
	it, ok := v.app.activityItemAt(p.selectedIndex())
	switch {
	case !ok && p.detailShown:
		p.setDetail("Details", tag(colMuted)+"Nothing chosen."+tagEnd)
	case !ok:
	case p.detailShown:
		v.showDetail(it, false)
	case v.app.tabsWidth >= activityWide:
		v.showDetail(it, false)
		v.autoDetail = true
	}
}

// showDetail fills the detail with a row's state and story.
func (v *activityView) showDetail(it activityItem, focus bool) {
	p := v.pane
	a := v.app
	_, what := a.itemWhat(it)
	title := what
	if about := a.itemAbout(it); about != "" && it.kind == activityWatch {
		title += " · " + about
	}
	p.detailFor = p.selectedIndex()
	if !p.detailShown {
		// Opened without moving the focus: the screen may be drawn again
		// while it is not in front, and must not take the keys then.
		p.body.AddItem(p.detail, 0, p.detailWeight, false)
		p.detailShown = true
	}
	icon := ""
	if ic := columnIcons["Details"]; ic != "" {
		icon = ic + " "
	}
	p.setDetailFunc(icon+esc(trunc(title, 56))+" "+tag(role("activity.panel_key"))+esc("[d]")+tagEnd, func(width int) string { return a.activityDetail(it, width) })
	if focus {
		v.focus(p.detail)
	}
}

// drawBand fills the cards and the Watching panel.
func (v *activityView) drawBand() {
	a := v.app
	counts := a.watchCounts()
	var b strings.Builder
	switch {
	case len(counts) == 0:
		b.WriteString(tag(colMuted) + "Nothing watched." + tagEnd)
	case v.cards.compact:
		b.WriteString(tag(role("activity.watching")) + glyphWatched + tagEnd + " " + tag(role("activity.what")) + esc(strings.Join(a.watchCountsShort(), " · ")) + tagEnd)
	default:
		for _, c := range counts {
			b.WriteString(tag(role("activity.watching")) + glyphWatched + tagEnd + " " + esc(c) + "\n")
		}
	}
	if !v.cards.compact {
		for range 2 - min(2, len(counts)) {
			b.WriteString("\n")
		}
		b.WriteString(tag(role("activity.key")) + "W" + tagEnd + tag(colMuted) + " all of them…" + tagEnd)
	}
	v.watching.SetText(b.String())
}

// drawLog fills the log: every event, newest first, the line of the last
// visit after what came since, under the names of its columns.
func (v *activityView) drawLog() {
	a := v.app
	t := v.log
	row, _ := t.GetSelection()
	t.Clear()
	v.logKeys = v.logKeys[:0]
	_, _, width, _ := t.GetInnerRect()
	newOnes := 0
	for _, e := range a.activityLog {
		if a.activityUnseen(e) {
			newOnes++
		}
	}
	whenW, whatW, headW := headingWidth("WHEN"), headingWidth("WHAT"), headingWidth("WHAT HAPPENED")
	for _, e := range a.activityLog {
		whenW = max(whenW, cells(eventTime(e.At)))
		whatW = max(whatW, min(28, cells(e.What)))
		if e.Heading != "" {
			headW = max(headW, min(24, cells(e.Heading)))
		}
	}
	head := role("activity.label")
	header := []field{{width: 1}, {text: "WHEN", width: whenW, colour: head}, {width: 1},
		{text: "WHAT", width: whatW, colour: head}, {text: "WHAT HAPPENED", width: headW, colour: head},
		{text: "DETAILS", width: max(8, width-whenW-whatW-headW-8), colour: head}}
	t.SetCell(0, 0, tview.NewTableCell(rowText(withHeadingIcons(header))).SetSelectable(false).SetExpansion(1))
	v.logKeys = append(v.logKeys, "")
	r := 1
	for i, e := range a.activityLog {
		if newOnes > 0 && i == newOnes {
			t.SetCell(r, 0, tview.NewTableCell(a.visitLine(width-1)).SetSelectable(false).SetExpansion(1))
			v.logKeys = append(v.logKeys, "")
			r++
		}
		glyph, colour := levelGlyph(e)
		mark := field{width: 1}
		if a.activityUnseen(e) {
			mark = field{text: glyphNew, width: 1, colour: role("activity.new")}
		}
		heading := field{text: eventHeading(e), width: headW, colour: role("activity.what")}
		cells := []field{mark,
			{text: eventTime(e.At), width: whenW, colour: role("activity.when")},
			{text: glyph, width: 1, colour: role(colour)},
			{text: e.What, width: whatW, colour: role("activity.what"), shorten: shortenRepo},
			heading}
		if e.Heading != "" {
			cells = append(cells, field{text: e.Line, width: max(8, width-whenW-whatW-headW-8), colour: role("activity.about")})
		}
		t.SetCell(r, 0, tview.NewTableCell(rowText(cells)).SetExpansion(1).SetReference(e.Key))
		v.logKeys = append(v.logKeys, e.Key)
		r++
	}
	if len(a.activityLog) == 0 {
		t.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+"Nothing has happened yet: what the watches and the agents do comes here."+tagEnd).SetSelectable(false))
		v.logKeys = append(v.logKeys, "")
	}
	if !t.HasFocus() {
		// Read, not walked: the newest at the top, no cursor.
		t.SetOffset(0, 0)
		t.SetSelectable(false, false)
		row = 1
	} else {
		t.SetSelectable(true, false)
	}
	row = min(max(row, 1), t.GetRowCount()-1)
	if row < len(v.logKeys) && v.logKeys[row] == "" && row+1 < t.GetRowCount() {
		row++
	}
	t.Select(row, 0)
	v.lightLog()
}

// lightLog lights, faintly, the log's rows of the thing under the list's
// cursor. It is only a look at each row's key, so it follows the cursor at
// once.
func (v *activityView) lightLog() {
	chosen := ""
	if it, ok := v.app.activityItemAt(v.pane.selectedIndex()); ok {
		chosen = it.key()
	}
	lit := role("activity.lit")
	for row := 1; row < v.log.GetRowCount() && row < len(v.logKeys); row++ {
		cell := v.log.GetCell(row, 0)
		if cell == nil || v.logKeys[row] == "" {
			continue
		}
		if chosen != "" && v.logKeys[row] == chosen {
			cell.SetBackgroundColor(lit)
		} else {
			cell.SetBackgroundColor(colBackground)
		}
	}
}

// chooseLogRow puts the list on the thing of the log's row, and the focus
// with it.
func (v *activityView) chooseLogRow() {
	row, _ := v.log.GetSelection()
	if row < 0 || row >= len(v.logKeys) || v.logKeys[row] == "" {
		return
	}
	v.app.selectActivityKey(v.logKeys[row])
	v.focus(v.pane.table)
}

// cardsView draws the editors open as cards, side by side, as Settings ›
// Integrations draws its cards: each a box of the card background with
// the editor's name in its border, the one chosen bordered in the focus
// colour while the panel has the focus.
type cardsView struct {
	*tview.Box
	app *App
	// at is the card chosen; compact draws each on one line, without a
	// border.
	at      int
	compact bool
}

// cardWidth is a card's, its border included.
const cardWidth = 28

func newCardsView(a *App) *cardsView {
	c := &cardsView{Box: tview.NewBox(), app: a}
	box(c.Box, "")
	c.SetTitle(panelTitle("Open", "e"))
	return c
}

// editorNames are the editors' names on a card.
var editorNames = map[string]string{
	editors.Nvim: "nvim", editors.Idea: "IDEA", editors.Code: "Code", editors.Zed: "Zed", editors.Custom: "editor",
}

func editorShortName(id string) string {
	if name, ok := editorNames[id]; ok {
		return name
	}
	return id
}

// fit is how many cards are drawn in a width, and whether the last place
// is the count of the rest.
func (c *cardsView) fit(width, n int) (shown int, more bool) {
	room := max(0, (width+1)/(cardWidth+1))
	if n <= room {
		return n, false
	}
	return max(0, room-1), true
}

func (c *cardsView) Draw(screen tcell.Screen) {
	c.Box.DrawForSubclass(screen, c)
	x, y, w, h := c.GetInnerRect()
	recs := c.app.editorsOpen
	if len(recs) == 0 {
		tview.Print(screen, tag(colMuted)+"No editor open."+tagEnd, x+1, y, w-2, tview.AlignLeft, colMuted)
		return
	}
	// A cell in from the panel's border, a cell between cards.
	x, w = x+1, w-1
	shown, more := c.fit(w-1, len(recs))
	c.at = min(c.at, len(recs)-1)
	focused := c.HasFocus()
	for i := range shown {
		c.drawCard(screen, recs[i], x+i*(cardWidth+1), y, h, focused && i == c.at)
	}
	if more {
		cx := x + shown*(cardWidth+1)
		label := fmt.Sprintf("+%d more", len(recs)-shown)
		c.drawFrame(screen, cx, y, min(12, x+w-cx), h, "", focused && c.at >= shown)
		row := y
		if !c.compact {
			row = y + 1
		}
		tview.Print(screen, "["+colMuted.String()+":"+colCard.String()+"]"+label, cx+1, row, min(12, x+w-cx)-2, tview.AlignCenter, colMuted)
	}
}

// drawFrame fills a card's box and draws its border with a title, as a
// bordered box draws its own.
func (c *cardsView) drawFrame(screen tcell.Screen, x, y, width, h int, title string, chosen bool) int {
	fill := baseStyle().Background(colCard).Foreground(colText)
	lines := min(h, 4)
	if c.compact {
		lines = 1
	}
	for row := y; row < y+lines; row++ {
		for col := x; col < x+width; col++ {
			screen.SetContent(col, row, ' ', nil, fill)
		}
	}
	if c.compact {
		return lines
	}
	line := colBorder
	if chosen {
		line = colBorderFocus
	}
	border := fill.Foreground(line)
	for col := x + 1; col < x+width-1; col++ {
		screen.SetContent(col, y, tview.Borders.Horizontal, nil, border)
		screen.SetContent(col, y+lines-1, tview.Borders.Horizontal, nil, border)
	}
	for row := y + 1; row < y+lines-1; row++ {
		screen.SetContent(x, row, tview.Borders.Vertical, nil, border)
		screen.SetContent(x+width-1, row, tview.Borders.Vertical, nil, border)
	}
	screen.SetContent(x, y, tview.Borders.TopLeft, nil, border)
	screen.SetContent(x+width-1, y, tview.Borders.TopRight, nil, border)
	screen.SetContent(x, y+lines-1, tview.Borders.BottomLeft, nil, border)
	screen.SetContent(x+width-1, y+lines-1, tview.Borders.BottomRight, nil, border)
	if title != "" {
		titleColour := colTitle
		if chosen {
			titleColour = colBorderFocus
		}
		tview.Print(screen, "["+titleColour.String()+":"+colCard.String()+"] "+title+" ", x+1, y, width-2, tview.AlignLeft, titleColour)
	}
	return lines
}

// drawCard draws one card at x, y: the editor in its border, the
// repository, then where it is open and for how long.
func (c *cardsView) drawCard(screen tcell.Screen, r session.Record, x, y, h int, chosen bool) {
	on := ":" + colCard.String()
	glyph := editorGlyph(r.Editor)
	project := r.Project
	if project == "" {
		project = tildePath(r.Dir)
	}
	where := editorContext(r)
	if where == "" {
		where = tildePath(r.Dir)
	}
	age := humanAge(r.Since)
	icon := "[" + role("activity.card_icon").String() + on + "]" + esc(glyph) + " " + esc(editorShortName(r.Editor))
	if c.compact {
		c.drawFrame(screen, x, y, cardWidth, h, "", chosen)
		ink := colText
		if chosen {
			ink = colBorderFocus
		}
		text := icon + "[" + ink.String() + on + ":b] " + esc(trunc(project, cardWidth-16)) +
			"[" + colMuted.String() + on + ":-] " + esc(age)
		tview.Print(screen, text, x+1, y, cardWidth-2, tview.AlignLeft, colText)
		return
	}
	lines := c.drawFrame(screen, x, y, cardWidth, h, icon, chosen)
	inner := cardWidth - 4
	tview.Print(screen, "["+colText.String()+on+":b]"+esc(trunc(project, inner))+"[-:-:-]", x+2, y+1, inner, tview.AlignLeft, colText)
	if lines >= 4 {
		tview.Print(screen, "["+colMuted.String()+on+"]"+esc(trunc(where, max(4, inner-cells(age)-1))), x+2, y+2, inner, tview.AlignLeft, colMuted)
		tview.Print(screen, "["+colMuted.String()+on+"]"+esc(age), x+2, y+2, inner, tview.AlignRight, colMuted)
	}
}

// keys move between the cards; past the first or the last they are the
// panels' to go on with.
func (c *cardsView) keys(ev *tcell.EventKey) bool {
	recs := c.app.editorsOpen
	switch {
	case isKey(ev, 'h', tcell.KeyLeft):
		if c.at == 0 {
			return false
		}
		c.at--
		return true
	case isKey(ev, 'l', tcell.KeyRight):
		if c.at >= len(recs)-1 {
			return false
		}
		c.at++
		return true
	}
	return false
}

// chosen is the editor of the card chosen, false on the count of the rest.
func (c *cardsView) chosen() (session.Record, bool) {
	recs := c.app.editorsOpen
	_, _, w, _ := c.GetInnerRect()
	if shown, more := c.fit(w-2, len(recs)); more && c.at >= shown {
		return session.Record{}, false
	}
	if c.at < 0 || c.at >= len(recs) {
		return session.Record{}, false
	}
	return recs[c.at], true
}

// selection is what can be done with the card chosen: what Running
// Editors… offers for an editor.
func (c *cardsView) selection() (string, []uiAction) {
	a := c.app
	r, ok := c.chosen()
	if !ok {
		return "Open editors", []uiAction{a.runningEditorsAction("Enter")}
	}
	nvim := r.Socket != "" || r.Pane != ""
	return "Actions · " + editorShortName(r.Editor) + " · " + r.Label(), []uiAction{
		{name: "Attach to Editor", about: "Return to this editor: its pane, or this terminal for a Neovim put aside.", keys: "Enter", rank: 10,
			run: func() { a.goToEditor(r) }},
		{name: "Attach In…", about: "Return to this Neovim in a tab, split or window, chosen from where it can go.", keys: "a", rank: 20,
			when: func() bool { return nvim }, run: func() { a.attachWhere(r) }},
		{name: "Close Editor", about: "Close Neovim; with unsaved changes, attach and ask there.", keys: "x", rank: 30,
			when: func() bool { return r.Socket != "" }, run: func() {
				a.closeRunningEditor(r, func(session.Record) { a.refreshOpenEditors() })
			}},
		a.runningEditorsAction("E"),
	}
}

// goToEditor brings an open editor forward: a Neovim is attached, in its
// pane or here; a window editor is in its own window, which unagit cannot
// raise.
func (a *App) goToEditor(r session.Record) {
	if r.Socket != "" || r.Pane != "" {
		a.attachEditor(r)
		return
	}
	a.note(editorShortName(r.Editor) + " has a window of its own - go there")
}

// activityScreenActions are what the Activity screen itself can do.
func (a *App) activityScreenActions(p *pane) []uiAction {
	v := a.activity
	acts := []uiAction{
		{name: "Refresh All", about: "Ask the servers about every watch, and herdr about every agent, now.", keys: "R", rank: 10,
			run: func() { a.watchAsk(nil); a.agentsNowAsk() }},
		{name: "Show Log", about: "The log in front, over the screen: every event at full length, to filter and to go to.", keys: "z", rank: 20, run: a.showActivityLog},
		{name: "Go to List", about: "Move to the list of the watches and the agents.", keys: "a", rank: 28, run: func() { v.focus(p.table) }},
		{name: "Go to Open Editors", about: "Move to the cards of the editors open: h and l between them, Enter goes to one.", keys: "e", rank: 30,
			when: func() bool { return v.bandH > 0 && len(a.editorsOpen) > 0 }, run: func() { v.focus(v.cards) }},
		{name: "Go to Detail", about: "Move into the detail of the row under the cursor, to read its whole story; where the screen is narrow, open it under the list.", keys: "d", rank: 32,
			run: func() {
				if p.detailShown {
					v.focus(p.detail)
					return
				}
				if it, ok := a.activityItemAt(p.selectedIndex()); ok {
					v.autoDetail = false
					v.showDetail(it, true)
				}
			}},
		{name: "Go to Log", about: "Move into the log: a row of it chooses its thing in the list.", keys: "L", rank: 34,
			when: func() bool { return v.logH > 0 }, run: func() { v.focus(v.log) }},
		{name: "Stop Watching All…", about: "Let every watch go, after asking.", keys: "", rank: 60, when: func() bool { return len(a.watches) > 0 },
			run: func() { a.stopWatchingAll(p) }},
		a.runningAgentsAction("Alt-A"),
	}
	return append(acts, a.listActions(p)...)
}

// stopWatchingAll lets every watch go, after asking.
func (a *App) stopWatchingAll(p *pane) {
	ws := append([]watch.Watch(nil), a.watches...)
	a.confirmWith("Stop Watching All", fmt.Sprintf("Stop watching all %d?\n\nNothing follows their pipelines any more until you watch them again.", len(ws)), "Stop", nil, func() {
		a.unwatch(p, ws)
	})
}

// showActivityLog opens the log in front: every event at full length, the
// newest first; Enter goes to its thing in the list.
func (a *App) showActivityLog() {
	if len(a.activityLog) == 0 {
		a.note("nothing has happened yet")
		return
	}
	items := make([]pickItem, len(a.activityLog))
	table := make([][]string, len(a.activityLog))
	for i, e := range a.activityLog {
		glyph, colour := levelGlyph(e)
		mark := " "
		if a.activityUnseen(e) {
			mark = tag(role("activity.new")) + glyphNew + tagEnd
		}
		table[i] = []string{mark, tag(role("activity.when")) + esc(eventTime(e.At)) + tagEnd, tag(role(colour)) + esc(glyph) + tagEnd,
			tag(role("activity.what")) + esc(e.What) + tagEnd, "[::b]" + esc(eventHeading(e)) + "[::-]", esc(e.Line)}
		about := e.Line
		if e.Project != "" {
			about += " · " + e.Project
		}
		if e.Title != "" {
			about += " · " + e.Title
		}
		items[i] = pickItem{About: about, Data: e}
	}
	header, labels := pickTable([]string{"", "WHEN", "", "WHAT", "WHAT HAPPENED", ""}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	a.showPickerWith("Log", items, pickerOptions{wide: true, explain: true, header: header, enterHint: "go to",
		enterName: "Go to Its Row", enterAbout: "Close the log and put the list's cursor on what the event is about."}, func(it pickItem) {
		e := it.Data.(watch.Event)
		if a.currentTab() != pageActivity {
			a.switchTab(pageActivity)
		}
		a.selectActivityKey(e.Key)
		a.activity.focus(a.activityPane.table)
	})
}

// showWatches lists everything watched, to go to one or stop watching it.
func (a *App) showWatches() {
	if len(a.watches) == 0 {
		a.note("nothing watched: Watch Merge Request or Watch Branch in the actions of a row (Alt-Enter)")
		return
	}
	var picker *livePicker
	itemsOf := func() ([]pickItem, string) {
		items := make([]pickItem, len(a.watches))
		table := make([][]string, len(a.watches))
		for i, w := range a.watches {
			st := a.watchSnap.States[w.Key()]
			kind := "branch"
			if w.IID > 0 {
				kind = "merge request"
			}
			glyph, colour := ciMark(st.Status)
			table[i] = []string{tag(colMuted) + kind + tagEnd, tag(role("activity.what")) + esc(w.Label()) + tagEnd,
				tag(colour) + glyph + tagEnd + " " + tag(stateColour2(st.Status)) + esc(watchPipelineWords(st)) + tagEnd,
				tag(colMuted) + humanAge(w.Since) + tagEnd, esc(trunc(a.watchTitle(w), 50))}
			items[i] = pickItem{About: strings.TrimSpace(a.watchTitle(w) + " · watched since " + w.Since.Format("Jan 2 15:04")), Data: w}
		}
		header, labels := pickTable([]string{"KIND", "WHAT", "STATE", "SINCE", "TITLE"}, table)
		for i := range items {
			items[i].Label = labels[i]
		}
		return items, header
	}
	refresh := func() {
		if picker == nil || !picker.open() {
			return
		}
		items, header := itemsOf()
		picker.setHeader(header)
		picker.set("Watches", items)
	}
	items, header := itemsOf()
	picker = a.showPickerWith("Watches", items, pickerOptions{wide: true, explain: true, header: header, enterHint: "go to",
		enterName: "Go to Its Row", enterAbout: "Close the list and put the Activity screen's cursor on the watch.",
		keys: []pickKey{
			{keys: "x", hint: "stop", name: "Stop Watching", about: "Let the watch go; the list stays open for the next.", stay: true,
				run: func(it pickItem) {
					w := it.Data.(watch.Watch)
					a.stopWatching([]string{w.Key()}, func() {
						a.done("stopped watching " + w.Label())
						refresh()
					})
				}},
			{keys: "X", hint: "stop all", name: "Stop Watching All…", about: "Let every watch go, after asking.",
				run: func(pickItem) { a.stopWatchingAll(a.activityPane) }},
			a.browserKey("o", "browser", "Open in Browser", "The merge request's page, or the pipeline's, on the forge; the list stays open.",
				func(it pickItem) string {
					st := a.watchSnap.States[it.Data.(watch.Watch).Key()]
					if st.URL != "" {
						return st.URL
					}
					return st.WebURL
				}),
		},
	}, func(it pickItem) {
		if a.currentTab() != pageActivity {
			a.switchTab(pageActivity)
		}
		a.selectActivityKey(it.Data.(watch.Watch).Key())
	})
}

// visitActivity is the screen opened: what came since the last visit is
// set apart for this one, the visit is noted for the next, the watches'
// changes count as seen on the tab, and the agents are asked at once.
func (a *App) visitActivity() {
	a.activitySince = a.cfg.State.ActivityVisit
	a.cfg.State.ActivityVisit = time.Now()
	a.saveState()
	a.watchLooked = max(a.watchSnap.Seen, a.watchSeen.Load())
	a.markWatchesSeen()
	a.agentsNowAsk()
	a.activityPane.reload()
}

// activityHints are the keys of each panel, for the one with the focus:
// what can be done there, never how to move.
func (v *activityView) hint() (tview.Primitive, string) {
	p := v.pane
	key := func(k, what string) string {
		return tag(role("activity.key")) + k + tagEnd + " " + tag(colDim) + what + tagEnd
	}
	join := func(parts ...string) string { return strings.Join(parts, tag(colDim)+" · "+tagEnd) }
	switch {
	case p.table.HasFocus():
		return p.table, join(key("Enter", "open"), key("x", "stop or close"), key("z", "log in front"), key("Tab", "panels"))
	case p.detail.HasFocus():
		return p.detail, join(key("Enter", "open"), key("x", "stop or close"), key("w", "browser"))
	case v.log.HasFocus():
		return v.log, join(key("Enter", "go to its row"), key("z", "in front"))
	case v.cards.HasFocus():
		if _, ok := v.cards.chosen(); !ok {
			return v.cards, key("Enter", "every editor open")
		}
		return v.cards, join(key("Enter", "attach"), key("a", "attach in…"), key("x", "close"), key("E", "all"))
	case v.watching.HasFocus():
		return v.watching, key("Enter", "every watch")
	}
	return nil, ""
}

// drawActivityHint writes the keys of the Activity panel with the focus
// into its bottom border, after everything is drawn: a panel as low as
// the band of cards has no line to spare for them.
func (a *App) drawActivityHint(screen tcell.Screen) {
	if a.activity == nil || a.currentTab() != pageActivity || a.modalOpen() {
		return
	}
	panel, text := a.activity.hint()
	if panel == nil {
		return
	}
	x, y, w, h := panel.GetRect()
	if w < 12 || h < 2 {
		return
	}
	text = " " + text + " "
	tview.Print(screen, "["+":"+colBackground.String()+"]"+text, x+2, y+h-1, w-4, tview.AlignLeft, colDim)
}

// focusAgain gives the focus back to the panel that had it, lit - the
// list on the first visit. Setting the focus alone left every border
// unlit.
func (v *activityView) focusAgain() {
	to := v.current
	if to == nil || to == v.pane.detail && !v.pane.detailShown {
		to = v.pane.table
	}
	v.focus(to)
}
