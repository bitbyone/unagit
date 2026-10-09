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
	p.extraKeys = v.keys

	v.cards = newCardsView(a)
	v.cards.SetInputCapture(v.panelKeys(func(ev *tcell.EventKey) bool { return v.cards.keys(ev) }))
	v.watching = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	v.watching.SetTextColor(colText)
	box(v.watching.Box, "Watching").SetBorderPadding(0, 0, 1, 1)
	v.watching.SetInputCapture(v.panelKeys(func(ev *tcell.EventKey) bool {
		if ev.Key() == tcell.KeyEnter {
			a.showWatches()
			return true
		}
		return false
	}))
	v.band = tview.NewFlex().
		AddItem(v.cards, 0, 1, false).
		AddItem(v.watching, watchingWidth, 0, false)

	v.log = tview.NewTable().SetSelectable(true, false).SetSeparator(' ')
	v.log.SetSelectedStyle(styleSelected)
	box(v.log.Box, "Log")
	v.log.SetInputCapture(v.panelKeys(func(ev *tcell.EventKey) bool {
		switch {
		case ev.Key() == tcell.KeyEnter:
			v.chooseLogRow(true)
			return true
		case ev.Key() == tcell.KeyRune && ev.Rune() == 'z':
			a.showActivityLog()
			return true
		}
		return false
	}))
	v.log.SetSelectionChangedFunc(func(row, _ int) {
		if v.log.HasFocus() {
			v.chooseLogRow(false)
		}
	})

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

// keys are the screen's own keys over the list and the detail: Tab goes
// round the panels.
func (v *activityView) keys(ev *tcell.EventKey) bool {
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

// panelKeys are the keys of the panels that are not the pane's: own first,
// then Tab round, Esc back to the list, and the screen's actions.
func (v *activityView) panelKeys(own func(*tcell.EventKey) bool) func(*tcell.EventKey) *tcell.EventKey {
	return func(ev *tcell.EventKey) *tcell.EventKey {
		a := v.app
		if own(ev) || v.keys(ev) {
			return nil
		}
		if opensPicker(ev) && v.pane.actionKeys(ev) {
			return nil
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
			case 'j', 'k', 'h', 'l', 'g', 'G':
				return ev
			}
			if a.tabKey(ev.Rune()) {
				return nil
			}
		}
		// The screen's actions, not the row's: the list's cursor is not
		// where the eye is.
		if _, acts := v.pane.screen(); runKey(joinActions(acts, a.globalActions()), ev) {
			return nil
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
	p.setDetailFunc(trunc(title, 60), func(width int) string { return a.activityDetail(it, width) })
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
// visit after what came since; the rows of the thing chosen in the list
// lit.
func (v *activityView) drawLog() {
	a := v.app
	t := v.log
	row, _ := t.GetSelection()
	t.Clear()
	v.logKeys = v.logKeys[:0]
	chosen := ""
	if it, ok := a.activityItemAt(v.pane.selectedIndex()); ok && !v.log.HasFocus() {
		chosen = it.key()
	}
	_, _, width, _ := t.GetInnerRect()
	newOnes := 0
	for _, e := range a.activityLog {
		if a.activityUnseen(e) {
			newOnes++
		}
	}
	whatW, headW := 0, 0
	for _, e := range a.activityLog {
		whatW = max(whatW, min(28, cells(e.What)))
		if e.Heading != "" {
			headW = max(headW, min(24, cells(e.Heading)))
		}
	}
	r := 0
	for i, e := range a.activityLog {
		if newOnes > 0 && i == newOnes {
			t.SetCell(r, 0, tview.NewTableCell(a.visitLine(width-1)).SetSelectable(false).SetExpansion(1))
			v.logKeys = append(v.logKeys, "")
			r++
		}
		glyph, colour := levelGlyph(e)
		mark := " "
		if a.activityUnseen(e) {
			mark = tag(role("activity.new")) + glyphNew + tagEnd
		}
		text := mark + " " + tag(role("activity.when")) + esc(fmt.Sprintf("%-10s", eventTime(e.At))) + tagEnd +
			tag(role(colour)) + esc(glyph) + tagEnd + "  " +
			tag(role("activity.what")) + esc(padTo(trunc(e.What, 28), whatW)) + tagEnd + "  " +
			"[::b]" + esc(padTo(trunc(eventHeading(e), 24), headW)) + "[::-]"
		if e.Heading != "" {
			text += tag(role("activity.about")) + "  " + esc(e.Line) + tagEnd
		}
		cell := tview.NewTableCell(text).SetExpansion(1)
		if chosen != "" && e.Key == chosen {
			cell.SetBackgroundColor(role("activity.lit"))
		}
		t.SetCell(r, 0, cell)
		v.logKeys = append(v.logKeys, e.Key)
		r++
	}
	if len(a.activityLog) == 0 {
		t.SetCell(0, 0, tview.NewTableCell(" "+tag(colMuted)+"Nothing has happened yet: what the watches and the agents do comes here."+tagEnd).SetSelectable(false))
		v.logKeys = append(v.logKeys, "")
	}
	if row >= t.GetRowCount() {
		row = t.GetRowCount() - 1
	}
	if row >= 0 && row < len(v.logKeys) && v.logKeys[row] == "" {
		row++
	}
	if !t.HasFocus() {
		// Read, not walked: the newest at the top, no cursor.
		row = 0
		t.SetOffset(0, 0)
		t.SetSelectedStyle(baseStyle())
	} else {
		t.SetSelectedStyle(styleSelected)
	}
	t.Select(max(row, 0), 0)
}

// chooseLogRow puts the list on the thing of the log's row, and with go
// the focus too.
func (v *activityView) chooseLogRow(goThere bool) {
	row, _ := v.log.GetSelection()
	if row < 0 || row >= len(v.logKeys) || v.logKeys[row] == "" {
		return
	}
	v.app.selectActivityKey(v.logKeys[row])
	if goThere {
		v.focus(v.pane.table)
	}
}

// cardsView draws the editors open as cards, side by side.
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
	box(c.Box, "Open")
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
	shown, more := c.fit(w-1, len(recs))
	c.at = min(c.at, len(recs)-1)
	focused := c.HasFocus()
	// A cell in from the panel's border, a cell between cards.
	x, w = x+1, w-1
	for i := range shown {
		c.drawCard(screen, recs[i], x+i*(cardWidth+1), y, h, focused && i == c.at)
	}
	if more {
		cx := x + shown*(cardWidth+1)
		fill := tcell.StyleDefault.Background(role("activity.card")).Foreground(role("activity.card_text"))
		width := min(8, x+w-cx)
		lines := min(h, 4)
		if c.compact {
			lines = 1
		}
		for row := y; row < y+lines; row++ {
			for col := cx; col < cx+width; col++ {
				screen.SetContent(col, row, ' ', nil, fill)
			}
		}
		label := fmt.Sprintf("+%d", len(recs)-shown)
		if focused && c.at >= shown {
			label = "[::b]" + label
		}
		tview.Print(screen, "["+role("activity.card_text").String()+":"+role("activity.card").String()+"]"+label, cx, y+lines/2, width, tview.AlignCenter, role("activity.card_text"))
	}
}

// drawCard draws one card at x, y: on its own background, with a border a
// shade stronger, the editor and the repository, then where and how long.
func (c *cardsView) drawCard(screen tcell.Screen, r session.Record, x, y, h int, chosen bool) {
	fillColour, ink, line := role("activity.card"), role("activity.card_text"), role("activity.card_border")
	if chosen {
		fillColour, line = colSelection(), role("activity.card_chosen")
	}
	fill := tcell.StyleDefault.Background(fillColour).Foreground(ink)
	on := ":" + fillColour.String()
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
	if c.compact {
		for col := x; col < x+cardWidth; col++ {
			screen.SetContent(col, y, ' ', nil, fill)
		}
		text := "[" + role("activity.card_icon").String() + on + "]" + esc(glyph) + "[" + ink.String() + on + ":b] " + esc(trunc(project, cardWidth-12)) +
			"[" + role("activity.card_muted").String() + on + ":-] " + esc(age)
		tview.Print(screen, text, x+1, y, cardWidth-2, tview.AlignLeft, ink)
		return
	}
	lines := min(h, 4)
	for row := y; row < y+lines; row++ {
		for col := x; col < x+cardWidth; col++ {
			screen.SetContent(col, row, ' ', nil, fill)
		}
	}
	border := fill.Foreground(line)
	for col := x + 1; col < x+cardWidth-1; col++ {
		screen.SetContent(col, y, tview.Borders.Horizontal, nil, border)
		screen.SetContent(col, y+lines-1, tview.Borders.Horizontal, nil, border)
	}
	for row := y + 1; row < y+lines-1; row++ {
		screen.SetContent(x, row, tview.Borders.Vertical, nil, border)
		screen.SetContent(x+cardWidth-1, row, tview.Borders.Vertical, nil, border)
	}
	screen.SetContent(x, y, tview.Borders.TopLeft, nil, border)
	screen.SetContent(x+cardWidth-1, y, tview.Borders.TopRight, nil, border)
	screen.SetContent(x, y+lines-1, tview.Borders.BottomLeft, nil, border)
	screen.SetContent(x+cardWidth-1, y+lines-1, tview.Borders.BottomRight, nil, border)
	inner := cardWidth - 4
	name := editorShortName(r.Editor)
	first := "[" + role("activity.card_icon").String() + on + "]" + esc(glyph) + " " + esc(name) +
		"[" + ink.String() + on + ":b]  " + esc(trunc(project, max(4, inner-cells(name)-4))) + "[-:-:-]"
	tview.Print(screen, first, x+2, y+1, inner, tview.AlignLeft, ink)
	if lines >= 4 {
		second := "[" + role("activity.card_muted").String() + on + "]" + esc(trunc(where, max(4, inner-cells(age)-1)))
		tview.Print(screen, second, x+2, y+2, inner, tview.AlignLeft, ink)
		tview.Print(screen, "["+role("activity.card_muted").String()+on+"]"+esc(age), x+2, y+2, inner, tview.AlignRight, ink)
	}
}

// keys move between the cards and go to the one chosen.
func (c *cardsView) keys(ev *tcell.EventKey) bool {
	recs := c.app.editorsOpen
	switch {
	case ev.Key() == tcell.KeyLeft || ev.Key() == tcell.KeyRune && ev.Rune() == 'h':
		c.at = max(0, c.at-1)
		return true
	case ev.Key() == tcell.KeyRight || ev.Key() == tcell.KeyRune && ev.Rune() == 'l':
		c.at = min(len(recs)-1, c.at+1)
		return true
	case ev.Key() == tcell.KeyEnter:
		if c.at >= 0 && c.at < len(recs) {
			_, _, w, _ := c.GetInnerRect()
			if shown, more := c.fit(w, len(recs)); more && c.at >= shown {
				c.app.showRunningEditors()
				return true
			}
			c.app.goToEditor(recs[c.at])
		}
		return true
	}
	return false
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

// colSelection is the selection's background.
func colSelection() tcell.Color {
	_, bg, _ := styleSelected.Decompose()
	return bg
}

// activityScreenActions are what the Activity screen itself can do.
func (a *App) activityScreenActions(p *pane) []uiAction {
	v := a.activity
	acts := []uiAction{
		{name: "Refresh All", about: "Ask the servers about every watch, and herdr about every agent, now.", keys: "R", rank: 10,
			run: func() { a.watchAsk(nil); a.agentsNowAsk() }},
		{name: "Show Log", about: "The log in front, over the screen: every event at full length, to filter and to go to.", keys: "z", rank: 20, run: a.showActivityLog},
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
		a.note("nothing watched: w on a merge request or a branch, or Watch Pipelines in its actions")
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
