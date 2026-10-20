package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/unibox"
)

// sandboxesView is the Sandboxes screen, laid out as Activity is: a band
// of cards on top - the sandboxes, and beside them the environment unibox
// works in - the tasks with their detail under it, and at the bottom the
// profiles and the log of what was asked of unibox. The pane is the tasks
// and the detail; the detail follows whichever panel has the focus.
type sandboxesView struct {
	app  *App
	pane *pane
	// frame holds root, or off while unibox is not there: the tab can be
	// gone to all the same, and says why it is empty.
	frame   *tview.Flex
	root    *tview.Flex
	off     *tview.Flex
	offText *tview.TextView
	offOn   bool

	band     *tview.Flex
	cards    *sandboxCards
	env      *tview.TextView
	lower    *tview.Flex
	profiles *tview.Table
	log      *tview.Table

	// tasks, boxes and profileRows are what the panels show, the filter
	// applied; profileRows' row r is the table's row r+1.
	tasks, boxes, profileRows []sandboxRow
	// logRows are the events of the log's rows, newest first.
	logRows []*uniboxEvent

	bandH, lowerH, bodyH int
	// autoDetail is a detail that opened by itself, for the room there
	// was; closedDetail one the user closed, which does not open again by
	// itself until they open it.
	autoDetail, closedDetail bool
	// current is the panel with the focus, which alone is lit, and which
	// the focus comes back to after a dialog.
	current tview.Primitive
	// profilesW is the profiles' inner width they were last laid out to.
	profilesW int
}

const (
	// sandboxesWide is the width from which the detail stands beside the
	// tasks and follows the focus by itself.
	sandboxesWide = 120
	// sandboxesTall is the height from which the cards have borders.
	sandboxesTall = 30
	// envWidth is the Environment panel's, its border included.
	envWidth = 40
	// sandboxCardWidth is a sandbox's card, its border included, and
	// compactCardWidth one folded to a line.
	sandboxCardWidth = 30
	compactCardWidth = 20
	// sandboxesLowerShare is the part of the rows under the band the
	// profiles and the log take; sandboxesLowerLeast their fewest lines,
	// borders included, and sandboxesListLeast the tasks' fewest.
	sandboxesLowerShare = 0.38
	sandboxesLowerLeast = 5
	sandboxesListLeast  = 6
	// sandboxesStackedLeast is the height from which a narrow screen puts
	// the detail under the tasks by itself.
	sandboxesStackedLeast = 20
)

func (a *App) newSandboxesPane() *pane {
	p := a.newPane("Tasks")
	p.stackBelow = sandboxesWide
	p.listWeight, p.detailWeight = 3, 2
	p.body.ResizeItem(p.kept, 0, p.listWeight)
	v := &sandboxesView{app: a, pane: p}
	a.sandboxes = v

	p.headline = func() string {
		o := a.ubx.overview
		working := 0
		for _, r := range v.tasks {
			if r.working() {
				working++
			}
		}
		var parts []string
		if working > 0 {
			parts = append(parts, tag(role("sandboxes.running"))+fmt.Sprintf("%d working", working)+tagEnd)
		}
		parts = append(parts, tag(colMuted)+fmt.Sprintf("%s · %s · %s",
			counted(len(o.Sandboxes), "sandbox", "sandboxes"), counted(len(v.tasks), "task", "tasks"),
			counted(len(o.Profiles), "profile", "profiles"))+tagEnd)
		if ver := o.Version.Version; ver != "" {
			parts = append(parts, tag(colMuted)+"unibox "+esc(ver)+tagEnd)
		}
		if a.ubx.read && !a.ubx.at.IsZero() {
			parts = append(parts, tag(colMuted)+"read "+humanAge(a.ubx.at)+tagEnd)
		}
		return strings.Join(parts, tag(colMuted)+" · "+tagEnd)
	}
	render := func(query string) {
		v.fill(query)
		a.drawSandboxTasks(p)
		v.setTitles()
		v.drawProfiles()
		v.drawEnv()
		v.drawLog()
		v.followDetail()
		p.updateHeader()
	}
	p.onQuery = render
	p.reload = func() {
		v.sync()
		render(p.query)
	}
	// Enter moves into the detail of the task, opening it where the
	// screen had no room for it by itself.
	p.onDetail = func(idx int, focus bool) {
		r, ok := v.taskAt(idx)
		if !ok {
			return
		}
		v.showDetail(r, focus)
	}
	p.onSelect = v.followDetail
	p.onOpen = func(ask bool) {
		r, ok := v.chosen()
		if !ok {
			return
		}
		if r.kind != rowProfile {
			a.flash("a sandbox and its tasks are in the VM - O on a profile opens the profile, s a shell in it")
			return
		}
		a.openProfileDir(r.profile, ask)
	}
	p.selection = func() (string, []uiAction) {
		r, ok := v.taskAt(p.selectedIndex())
		if !ok {
			return "", nil
		}
		return "Actions · " + sandboxRowName(r), a.sandboxRowActions(r)
	}
	p.screen = func() (string, []uiAction) { return "Sandboxes", a.sandboxesActions(p) }
	p.extraKeys = v.paneKeys
	p.focusElsewhere = func() tview.Primitive {
		if v.offOn {
			return v.offText
		}
		if v.current == p.table || v.current == p.detail {
			return nil
		}
		return v.current
	}
	p.focused = func(to tview.Primitive) {
		v.current = to
		v.lightBorders(to)
		v.followDetail()
	}
	p.table.SetTitle(panelTitle("Tasks", "t"))

	v.cards = &sandboxCards{Box: tview.NewBox(), v: v}
	box(v.cards.Box, "")
	v.cards.SetInputCapture(a.panelKeys(v, v.cards, func(ev *tcell.EventKey) bool {
		if v.cards.keys(ev) {
			v.followDetail()
			return true
		}
		_, acts := v.cards.selection()
		return runPanelKey(acts, ev)
	}, v.cards.selection))

	v.env = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	v.env.SetTextColor(colText)
	box(v.env.Box, "").SetBorderPadding(0, 0, 1, 1)
	v.env.SetInputCapture(a.panelKeys(v, v.env, func(ev *tcell.EventKey) bool {
		_, acts := v.envSelection()
		return runPanelKey(acts, ev)
	}, v.envSelection))

	v.band = tview.NewFlex().
		AddItem(v.cards, 0, 1, false).
		AddItem(v.env, envWidth, 0, false)

	v.profiles = tview.NewTable().SetSelectable(true, false).SetSeparator(' ').SetFixed(1, 0)
	v.profiles.SetSelectedStyle(styleSelected)
	box(v.profiles.Box, "")
	v.profiles.SetSelectionChangedFunc(func(int, int) { v.followDetail() })
	v.profiles.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		if w-2 != v.profilesW {
			v.profilesW = w - 2
			go a.tv.QueueUpdateDraw(v.drawProfiles)
		}
		return x + 1, y + 1, w - 2, h - 2
	})
	v.profiles.SetInputCapture(a.panelKeys(v, v.profiles, func(ev *tcell.EventKey) bool {
		_, acts := v.profileSelection()
		return runPanelKey(acts, ev)
	}, v.profileSelection))

	v.log = tview.NewTable().SetSelectable(true, false).SetSeparator(' ').SetFixed(1, 0)
	v.log.SetSelectedStyle(styleSelected)
	box(v.log.Box, "")
	v.log.SetInputCapture(a.panelKeys(v, v.log, func(ev *tcell.EventKey) bool {
		_, acts := v.logSelection()
		return runPanelKey(acts, ev)
	}, v.logSelection))

	v.lower = tview.NewFlex().
		AddItem(v.profiles, 0, 1, false).
		AddItem(v.log, 0, 1, false)

	v.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.filter, 1, 0, false).
		AddItem(v.band, 0, 0, false).
		AddItem(p.body, 0, 1, true).
		AddItem(v.lower, 0, 0, false).
		AddItem(p.headerRow, 1, 0, false)
	v.root.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		v.root.ResizeItem(p.headerRow, p.headerRow.fit(w), 0)
		// The environment gives the cards room on a narrow screen.
		v.band.ResizeItem(v.env, min(envWidth, max(24, w/3)), 0)
		v.layout(h)
		return x, y, w, h
	})

	v.offText = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetTextAlign(tview.AlignCenter)
	v.offText.SetTextColor(colText)
	v.offText.SetInputCapture(v.offKeys)
	v.off = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(tview.NewBox(), 0, 1, false).
			AddItem(v.offText, 0, 3, true).
			AddItem(tview.NewBox(), 0, 1, false), 6, 0, true).
		AddItem(tview.NewBox(), 0, 1, false)

	v.frame = tview.NewFlex().SetDirection(tview.FlexRow)
	v.frame.AddItem(v.root, 0, 1, true)
	p.root = v.frame
	return p
}

// sync puts on the tab what it can show: the screen while unibox is on,
// the word that it is not otherwise.
func (v *sandboxesView) sync() {
	off := !v.app.uniboxOn()
	if off == v.offOn {
		if off {
			v.offText.SetText(v.app.sandboxesOffText())
		}
		return
	}
	v.offOn = off
	v.frame.Clear()
	if off {
		v.offText.SetText(v.app.sandboxesOffText())
		v.frame.AddItem(v.off, 0, 1, true)
	} else {
		v.frame.AddItem(v.root, 0, 1, true)
	}
	if v.app.currentTab() == pageSandboxes && !v.app.modalOpen() {
		v.focusAgain()
	}
}

// sandboxesOffText is the word in the middle of the tab without unibox:
// that there is nothing, why, and where it is changed.
func (a *App) sandboxesOffText() string {
	head := tag(role("sandboxes.problem")) + "[::b]Sandboxes are not available[::-]" + tagEnd + "\n\n"
	if on := a.cfg.Integrations.Unibox; on != nil && !*on {
		return head + "The unibox integration is turned off.\n" + tag(colMuted) + "Enter sets unibox up, step by step; Settings › Integrations turns it on too." + tagEnd
	}
	return head + "unibox is not installed - unagit runs it from PATH.\n" + tag(colMuted) + "Enter sets unibox up, step by step, from installing it." + tagEnd
}

// offKeys are the tab's keys while it only says unibox is not there: to
// Settings, to another tab, help, quit.
func (v *sandboxesView) offKeys(ev *tcell.EventKey) *tcell.EventKey {
	a := v.app
	if a.actionKeys(ev, func() (string, []uiAction) { return "", nil }, func() (string, []uiAction) {
		setup := a.setupAction()
		setup.keys, setup.rank = "Enter", 5
		integrations := a.integrationsAction()
		integrations.keys = ""
		return "Sandboxes", append([]uiAction{setup, integrations}, a.listActions(v.pane)[1:]...)
	}) {
		return nil
	}
	switch ev.Key() {
	case tcell.KeyEnter:
		a.showUniboxSetup()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			a.tv.Stop()
			return nil
		case '?':
			a.showHelp()
			return nil
		}
		if a.tabKey(ev.Rune()) {
			return nil
		}
	}
	return nil
}

// integrationsAction opens Settings › Integrations, where unibox is
// turned on.
func (a *App) integrationsAction() uiAction {
	return uiAction{name: "Open Integrations", about: "Settings › Integrations, where unibox is turned on or off.", keys: "Enter", rank: 10,
		run: func() {
			a.switchTab(pageSettings)
			a.settings.selectSection(sectionIntegrations)
		}}
}

// layout shares the screen's height: the band its rows, the profiles and
// the log their share, the tasks and the detail the rest - never fewer
// than sandboxesListLeast, for which the cards fold to a line first.
func (v *sandboxesView) layout(h int) {
	rows := h - 2
	band := 6
	if h < sandboxesTall {
		band = 3
	}
	lower := 0
	if rows-band >= sandboxesListLeast+sandboxesLowerLeast {
		lower = max(sandboxesLowerLeast, int(float64(rows-band)*sandboxesLowerShare+0.5))
		if rows-band-lower < sandboxesListLeast {
			band = min(band, 3)
			lower = max(sandboxesLowerLeast, rows-band-sandboxesListLeast)
		}
	}
	v.bodyH = rows - band - lower
	switch room := v.roomForDetail(); {
	case !room && v.autoDetail && v.pane.detailShown:
		v.autoDetail = false
		go v.app.tv.QueueUpdateDraw(v.pane.hideDetail)
	case room && !v.pane.detailShown && !v.closedDetail:
		go v.app.tv.QueueUpdateDraw(v.followDetail)
	}
	if band != v.bandH {
		v.bandH = band
		v.root.ResizeItem(v.band, band, 0)
		v.cards.compact = band < 6
		v.drawEnv()
	}
	if lower != v.lowerH {
		v.lowerH = lower
		v.root.ResizeItem(v.lower, lower, 0)
	}
}

// roomForDetail reports whether the detail opens by itself.
func (v *sandboxesView) roomForDetail() bool {
	return v.app.tabsWidth >= sandboxesWide || v.bodyH >= sandboxesStackedLeast
}

// fill sorts what unibox said into the panels, keeping what the filter
// finds: a task by its name, its sandbox, agent, repositories, branches.
func (v *sandboxesView) fill(query string) {
	v.tasks, v.boxes, v.profileRows = v.tasks[:0], v.boxes[:0], v.profileRows[:0]
	if v.app.sandboxesProblem() != "" {
		return
	}
	found := func(r sandboxRow) bool {
		if strings.TrimSpace(query) == "" {
			return true
		}
		_, ok := fuzzy.Match(query, sandboxHay(r))
		return ok
	}
	for _, r := range v.app.sandboxRows {
		if !found(r) {
			continue
		}
		switch r.kind {
		case rowTask:
			v.tasks = append(v.tasks, r)
		case rowSandbox:
			v.boxes = append(v.boxes, r)
		default:
			v.profileRows = append(v.profileRows, r)
		}
	}
}

func (v *sandboxesView) taskAt(i int) (sandboxRow, bool) {
	if i < 0 || i >= len(v.tasks) {
		return sandboxRow{}, false
	}
	return v.tasks[i], true
}

func (v *sandboxesView) profileAt() (sandboxRow, bool) {
	row, _ := v.profiles.GetSelection()
	if row < 1 || row > len(v.profileRows) {
		return sandboxRow{}, false
	}
	return v.profileRows[row-1], true
}

// chosen is the row of the panel with the focus: the card, the profile or
// the task under the cursor.
func (v *sandboxesView) chosen() (sandboxRow, bool) {
	switch v.current {
	case v.cards:
		return v.cards.chosen()
	case v.profiles:
		return v.profileAt()
	case v.env, v.log:
		return sandboxRow{}, false
	}
	return v.taskAt(v.pane.selectedIndex())
}

// setTitles names the panels: again on every draw, since the icons are
// the theme's and the terminal's.
func (v *sandboxesView) setTitles() {
	v.pane.table.SetTitle(panelTitle("Tasks", "t"))
	v.cards.SetTitle(panelTitle("Sandboxes", "B"))
	v.env.SetTitle(panelTitle("Environment", "E"))
	v.profiles.SetTitle(panelTitle("Profiles", "P"))
	v.log.SetTitle(panelTitle("Log", "L"))
}

// paneKeys are the screen's keys over the tasks and the detail, heard
// before the pane's own: Tab round the panels, and j or k past the end of
// what is in a panel on to the panel beyond it.
func (v *sandboxesView) paneKeys(ev *tcell.EventKey) bool {
	if v.cycleKeys(ev) {
		return true
	}
	p := v.pane
	if p.detail.HasFocus() {
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
	if ev.Key() == tcell.KeyEsc && p.query == "" && p.detailShown {
		v.closedDetail = true
		return false
	}
	first, last := selectableEnds(p.table)
	row, _ := p.table.GetSelection()
	empty := first < 0
	switch {
	case isKey(ev, 'j', tcell.KeyDown) && (empty || row >= last):
		return v.move(p.table, 'j')
	case isKey(ev, 'k', tcell.KeyUp) && (empty || row <= first):
		return v.move(p.table, 'k')
	case isKey(ev, 'l', tcell.KeyRight) && !p.detailShown:
		return v.move(p.table, 'l')
	}
	return false
}

func (v *sandboxesView) cycleKeys(ev *tcell.EventKey) bool {
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

func (v *sandboxesView) home() *pane { return v.pane }

// move goes from a panel to the one beside it, h j k l as they lie on the
// screen: the cards and the environment above, the tasks and the detail
// side by side, the profiles and the log below.
func (v *sandboxesView) move(from tview.Primitive, dir rune) bool {
	p := v.pane
	lower := v.lowerH > 0
	var to tview.Primitive
	switch from {
	case v.cards:
		switch dir {
		case 'j':
			to = p.table
		case 'l':
			to = v.env
		}
	case v.env:
		switch {
		case dir == 'h':
			to = v.cards
		case dir == 'j' && p.detailShown && p.bodyDirection == tview.FlexColumn:
			to = p.detail
		case dir == 'j':
			to = p.table
		}
	case p.table:
		switch {
		case dir == 'k':
			to = v.cards
		case dir == 'j' && p.detailShown && p.bodyDirection == tview.FlexRow:
			to = p.detail
		case dir == 'j' && lower:
			to = v.profiles
		case dir == 'l' && p.detailShown:
			to = p.detail
		}
	case p.detail:
		switch {
		case dir == 'h' || dir == 'k' && p.bodyDirection == tview.FlexRow:
			to = p.table
		case dir == 'k':
			to = v.env
		case dir == 'j' && lower:
			to = v.log
		}
	case v.profiles:
		switch dir {
		case 'k':
			to = p.table
		case 'l':
			to = v.log
		}
	case v.log:
		switch {
		case dir == 'h':
			to = v.profiles
		case dir == 'k' && p.detailShown:
			to = p.detail
		case dir == 'k':
			to = p.table
		}
	}
	if to == nil {
		return false
	}
	v.focus(to)
	return true
}

// panels are what Tab goes round, in order, those on screen only.
func (v *sandboxesView) panels() []tview.Primitive {
	p := v.pane
	out := []tview.Primitive{v.cards, v.env, p.table}
	if p.detailShown {
		out = append(out, p.detail)
	}
	if v.lowerH > 0 {
		out = append(out, v.profiles, v.log)
	}
	return out
}

func (v *sandboxesView) cycle(by int) {
	panels := v.panels()
	at := 0
	for i, pr := range panels {
		if pr.HasFocus() {
			at = i
		}
	}
	v.focus(panels[(at+by+len(panels))%len(panels)])
}

// focus puts the focus on a panel and lights its border alone; the detail
// turns to what the panel has chosen.
func (v *sandboxesView) focus(to tview.Primitive) {
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
	v.lightBorders(to)
	v.drawLog()
	v.drawProfiles()
	v.followDetail()
	p.updateHeader()
}

func (v *sandboxesView) lightBorders(to tview.Primitive) {
	focusBox(v.cards.Box, to == v.cards)
	focusBox(v.env.Box, to == v.env)
	focusBox(v.profiles.Box, to == v.profiles)
	focusBox(v.log.Box, to == v.log)
}

// focusAgain gives the focus back to the panel that had it, lit - the
// tasks on the first visit.
func (v *sandboxesView) focusAgain() {
	if v.offOn {
		v.app.tv.SetFocus(v.offText)
		return
	}
	to := v.current
	if to == nil || to == v.pane.detail && !v.pane.detailShown {
		to = v.pane.table
	}
	v.focus(to)
}

// detailFor is what the detail shows for the panel with the focus: a
// row's configuration, or the environment's.
func (v *sandboxesView) detailFor() (title string, r sandboxRow, ok, env bool) {
	switch v.current {
	case v.env:
		return "Environment", sandboxRow{}, false, true
	case v.log, v.pane.detail:
		if v.pane.detailShown {
			return "", sandboxRow{}, false, false
		}
	}
	r, ok = v.chosen()
	if !ok {
		return "Details", r, false, false
	}
	return sandboxRowName(r), r, true, false
}

// followDetail keeps the detail on what the focus has chosen, where there
// is room for it or it was asked for.
func (v *sandboxesView) followDetail() {
	p := v.pane
	if v.app.sandboxesProblem() != "" {
		if p.detailShown {
			p.stopDebounce()
			p.body.RemoveItem(p.detail)
			p.detailShown, p.detailFocused, p.detailFor = false, false, -1
		}
		return
	}
	if v.current == p.detail || v.current == v.log && p.detailShown {
		return
	}
	if !p.detailShown && (!v.roomForDetail() || v.closedDetail) {
		return
	}
	if !p.detailShown {
		v.autoDetail = true
	}
	title, r, ok, env := v.detailFor()
	switch {
	case env:
		v.showEnvDetail(false)
	case ok:
		v.showDetail(r, false)
	default:
		v.openDetail(title, func(int) string { return tag(colMuted) + "Nothing chosen." + tagEnd })
	}
}

// openDetail puts the detail beside the tasks, or under them, without
// moving the focus.
func (v *sandboxesView) openDetail(title string, render func(width int) string) {
	p := v.pane
	p.detailFor = p.selectedIndex()
	if !p.detailShown {
		p.body.AddItem(p.detail, 0, p.detailWeight, false)
		p.detailShown = true
	}
	icon := ""
	if ic := columnIcons["Details"]; ic != "" {
		icon = ic + " "
	}
	p.setDetailFunc(icon+esc(trunc(title, 56)), render)
}

// showDetail fills the detail with a row's configuration, reading what the
// overview does not hold the first time the row is looked at.
func (v *sandboxesView) showDetail(r sandboxRow, focus bool) {
	a := v.app
	render := func(int) string { return a.sandboxDetailText(r) }
	v.openDetail(sandboxRowName(r), render)
	if focus {
		v.autoDetail, v.closedDetail = false, false
		v.focus(v.pane.detail)
	}
	a.readSandboxDetail(r, func() {
		if now, ok := v.chosen(); (ok && now.key() == r.key() || v.current == v.pane.detail) && v.pane.detailShown {
			v.pane.setDetailFunc(v.pane.detail.GetTitle(), func(int) string { return a.sandboxDetailText(r) })
		}
	})
}

// showEnvDetail fills the detail with the environment: unibox's settings,
// its runtimes, the secrets by name and the values.
func (v *sandboxesView) showEnvDetail(focus bool) {
	v.openDetail("Environment", func(int) string { return v.app.environmentDetailText() })
	if focus {
		v.autoDetail, v.closedDetail = false, false
		v.focus(v.pane.detail)
	}
}

// openChosenDetail is Show Details of a panel: into the detail of what it
// has chosen, opening it where it was not open.
func (v *sandboxesView) openChosenDetail() {
	if v.current == v.env {
		v.showEnvDetail(true)
		return
	}
	if r, ok := v.chosen(); ok {
		v.showDetail(r, true)
	}
}

// ------------------------------------------------------------- the cards

// sandboxCards draws the sandboxes as cards side by side, as Activity
// draws the editors open: the state and the name in the border, the
// profile and the runtime, then the resources and the tasks.
type sandboxCards struct {
	*tview.Box
	v *sandboxesView
	// at is the card chosen; compact draws each on one line.
	at      int
	compact bool
}

func (c *sandboxCards) Draw(screen tcell.Screen) {
	c.Box.DrawForSubclass(screen, c)
	x, y, w, h := c.GetInnerRect()
	boxes := c.v.boxes
	if len(boxes) == 0 {
		text := "No sandbox yet - c on a profile makes one, n starts a task in a new one."
		if problem := c.v.app.sandboxesProblem(); problem != "" {
			text = "Nothing to show."
		}
		tview.Print(screen, tag(colMuted)+esc(text)+tagEnd, x+1, y+max(0, (h-1)/2), w-2, tview.AlignLeft, colMuted)
		return
	}
	x, w = x+1, w-1
	width := c.width()
	shown := len(boxes)
	more := false
	if shown*(width+1)-1 > w-1 {
		shown, more = max(0, (w-1-moreWidth)/(width+1)), true
	}
	c.at = min(c.at, len(boxes)-1)
	focused := c.HasFocus()
	for i := range shown {
		c.drawCard(screen, boxes[i], x+i*(width+1), y, h, focused && i == c.at)
	}
	if more {
		cx := x + shown*(width+1)
		rest := min(moreWidth, x+w-cx)
		drawCardFrame(screen, cx, y, rest, h, "", focused && c.at >= shown, c.compact)
		row := y
		if !c.compact {
			row = y + 1
		}
		tview.Print(screen, "["+colMuted.String()+":"+colCard.String()+"]"+fmt.Sprintf("+%d more", len(boxes)-shown), cx+1, row, rest-2, tview.AlignCenter, colMuted)
	}
}

// width is a card's, its border included.
func (c *sandboxCards) width() int {
	if c.compact {
		return compactCardWidth
	}
	return sandboxCardWidth
}

// sandboxResources are a sandbox's processors, memory and tasks, each with
// its icon where the terminal has them and its word where it has not.
func (a *App) sandboxResources(s sandboxUse, on string) string {
	var parts []string
	item := func(icon, value, word string) {
		if value == "" {
			return
		}
		if icon != "" {
			parts = append(parts, "["+role("sandboxes.resource").String()+on+"]"+icon+"["+colText.String()+on+"] "+esc(value))
			return
		}
		parts = append(parts, "["+colText.String()+on+"]"+esc(value)+"["+colMuted.String()+on+"]"+word)
	}
	item(glyphCPU, s.cpus, " cpu")
	item(glyphMemory, s.memory, "")
	if s.tasks == 0 {
		parts = append(parts, "["+colMuted.String()+on+"]no tasks")
	} else {
		item(glyphTask, fmt.Sprint(s.tasks), plural(s.tasks, " task", " tasks"))
	}
	text := strings.Join(parts, "  ")
	if s.working > 0 {
		text += "  [" + role("sandboxes.running").String() + on + "]" + spinnerGlyph(a.spinFrame) + fmt.Sprintf(" %d", s.working)
	}
	return text
}

// sandboxUse is what a card says of a sandbox: its processors and memory,
// its tasks and how many of them an agent works on.
type sandboxUse struct {
	cpus, memory   string
	tasks, working int
}

func useOf(r sandboxRow) sandboxUse {
	s := r.sandbox
	out := sandboxUse{cpus: s.Settings.CPUs, memory: firstOf(s.Memory, s.Settings.Memory), tasks: len(s.Tasks)}
	for _, t := range s.Tasks {
		if _, ok := s.RunOf(t.ID); ok {
			out.working++
		}
	}
	return out
}

// sandboxStateMark is a sandbox's state as a glyph and its role.
func sandboxStateMark(state string) (string, string) {
	switch state {
	case "running":
		return glyphDot, "sandboxes.running"
	case "stopped":
		return glyphRing, "sandboxes.stopped"
	case "missing", "unavailable":
		return glyphCross, "sandboxes.problem"
	}
	return glyphRing, "sandboxes.stopped"
}

func (c *sandboxCards) drawCard(screen tcell.Screen, r sandboxRow, x, y, h int, chosen bool) {
	a := c.v.app
	s := r.sandbox
	on := ":" + colCard.String()
	glyph, stateRole := sandboxStateMark(s.State)
	context := a.ubx.overview.Context.Set.Sandbox == s.Name
	nameInk := colText
	if chosen {
		nameInk = colBorderFocus
	}
	title := "[" + role(stateRole).String() + on + "]" + glyph + " [" + nameInk.String() + on + ":b]" + esc(trunc(s.Name, sandboxCardWidth-18)) + "[-:-:-]"
	if context {
		title += "[" + colMuted.String() + on + "] · context"
	}
	if c.compact {
		// The state and the name, and how many tasks it has.
		drawCardFrame(screen, x, y, compactCardWidth, h, "", chosen, true)
		use := useOf(r)
		count := counted(use.tasks, "task", "tasks")
		if glyphTask != "" {
			count = glyphTask + " " + fmt.Sprint(use.tasks)
		}
		text := "[" + role(stateRole).String() + on + "]" + glyph + " [" + nameInk.String() + on + ":b]" + esc(trunc(s.Name, compactCardWidth-6-cells(count))) + "[-:-:-]"
		tview.Print(screen, text, x+1, y, compactCardWidth-2, tview.AlignLeft, colText)
		tview.Print(screen, "["+colMuted.String()+on+"]"+esc(count), x+1, y, compactCardWidth-2, tview.AlignRight, colMuted)
		return
	}
	resources := a.sandboxResources(useOf(r), on)
	lines := drawCardFrame(screen, x, y, sandboxCardWidth, h, title, chosen, false)
	inner := sandboxCardWidth - 4
	where := esc(trunc(s.Profile+" · "+s.Runtime, inner-cells(s.State)-1))
	tview.Print(screen, "["+colMuted.String()+on+"]"+where, x+2, y+1, inner, tview.AlignLeft, colMuted)
	tview.Print(screen, "["+role(stateRole).String()+on+"]"+esc(s.State), x+2, y+1, inner, tview.AlignRight, colMuted)
	if lines >= 4 {
		tview.Print(screen, resources, x+2, y+2, inner, tview.AlignLeft, colText)
	}
}

// keys move between the cards; past the first or the last they are the
// panels' to go on with.
func (c *sandboxCards) keys(ev *tcell.EventKey) bool {
	switch {
	case isKey(ev, 'h', tcell.KeyLeft):
		if c.at == 0 {
			return false
		}
		c.at--
		return true
	case isKey(ev, 'l', tcell.KeyRight):
		if c.at >= len(c.v.boxes)-1 {
			return false
		}
		c.at++
		return true
	}
	return false
}

func (c *sandboxCards) chosen() (sandboxRow, bool) {
	if c.at < 0 || c.at >= len(c.v.boxes) {
		return sandboxRow{}, false
	}
	return c.v.boxes[c.at], true
}

func (c *sandboxCards) selection() (string, []uiAction) {
	r, ok := c.chosen()
	if !ok {
		return "Sandboxes", nil
	}
	return "Actions · " + sandboxRowName(r), c.v.app.sandboxRowActions(r)
}

// ------------------------------------------------------- the environment

// drawEnv fills the Environment panel: where the profiles are, the
// context, the runtimes, what is kept for the profiles - each on a line,
// or all on one where the band is folded.
func (v *sandboxesView) drawEnv() {
	a := v.app
	o := a.ubx.overview
	if !a.ubx.read {
		v.env.SetText(tag(colMuted) + "reading…" + tagEnd)
		return
	}
	icon := func(name string) string {
		if ic := columnIcons[name]; ic != "" {
			return tag(colMuted) + ic + tagEnd + " "
		}
		return ""
	}
	repo := tag(role("sandboxes.problem")) + "no environments repository" + tagEnd
	if o.Environments != "" {
		repo = esc(tildePath(o.Environments))
	}
	context := tag(colMuted) + "no context" + tagEnd
	if set := o.Context.Set; set.Sandbox != "" || set.Profile != "" {
		context = tag(colAccent) + glyphDot + tagEnd + " " + esc(strings.Trim(set.Sandbox+" · "+firstOf(set.Profile, o.Context.Profile), " ·"))
	}
	var runtimes []string
	for _, rt := range o.Runtimes {
		word := rt.Name
		switch {
		case !rt.Available || !rt.Enabled:
			word = tag(colMuted) + esc(rt.Name+" off") + tagEnd
		case rt.Default:
			word = "[::b]" + esc(rt.Name) + "[::-]"
		default:
			word = esc(word)
		}
		runtimes = append(runtimes, word)
	}
	values := len(o.Values.Shared)
	for _, vs := range o.Values.Profiles {
		values += len(vs)
	}
	kept := counted(len(o.Secrets.Names), "secret", "secrets") + " · " + counted(values, "value", "values")
	if len(o.Unread) > 0 {
		kept += " · " + tag(role("sandboxes.problem")) + "not all read" + tagEnd
	}
	if v.cards.compact {
		v.env.SetText(repo + tag(colMuted) + " · " + tagEnd + context)
		return
	}
	v.env.SetText(strings.Join([]string{
		icon("PATH") + repo,
		context,
		tag(colMuted) + "runtimes " + tagEnd + strings.Join(runtimes, tag(colMuted)+" · "+tagEnd),
		tag(colMuted) + esc(kept) + tagEnd,
	}, "\n"))
}

// envSelection is what the Environment panel can do; the secrets, the
// values and unibox's settings are the screen's, from any panel.
func (v *sandboxesView) envSelection() (string, []uiAction) {
	a := v.app
	return "Environment", []uiAction{
		{name: "Show Details", about: "Move into the detail: unibox's settings, its runtimes, the secrets by name, your values.",
			keys: "Enter", rank: 5, run: v.openChosenDetail},
		{name: "Set Environments Repository…", about: "Choose which of your clones holds the profiles; unibox config set environments keeps it.",
			keys: "e", rank: 10, run: a.chooseEnvironments},
		{name: "Open Environments Repository", about: "Open the repository of the profiles in the default editor.",
			keys: "O", rank: 20, when: func() bool { return a.ubx.overview.Environments != "" }, run: a.openEnvironments},
		{name: "Clear Context", about: "Forget the sandbox and profile unibox commands work in when not told.",
			keys: "c", rank: 30, when: a.contextSet, run: func() { a.uniboxDo("Clearing the context", "context", "clear") }},
	}
}

func (a *App) contextSet() bool {
	c := a.ubx.overview.Context.Set
	return c.Sandbox != "" || c.Profile != ""
}

// ---------------------------------------------------------- the profiles

// drawProfiles lists the profiles: the context marked, each one's images,
// its agents and how many sandboxes are made from it.
func (v *sandboxesView) drawProfiles() {
	a := v.app
	t := v.profiles
	row, _ := t.GetSelection()
	t.Clear()
	width := max(20, v.profilesW)
	var names, states, agentsW []int
	for _, r := range v.profileRows {
		names = append(names, cells(r.profile.Name))
		state, _ := a.sandboxState(r)
		states = append(states, cells(state))
		agentsW = append(agentsW, cells(profileAgentsText(r.profile)))
	}
	boxesW := headingWidth("BOXES")
	nameCol := flexColumn("PROFILE", names, 8, 2)
	stateCol := flexColumn("IMAGES", states, 6, 1)
	agentCol := flexColumn("AGENTS", agentsW, 6, 1)
	agentCol.drop = 1
	spare := layoutColumns(width-1, fixedColumn(2), nameCol, stateCol, agentCol, fixedColumn(boxesW))
	head := role("sandboxes.header")
	header := []field{
		{width: 2},
		{text: "PROFILE", width: nameCol.width, colour: head},
		{text: "IMAGES", width: stateCol.width, colour: head},
		{text: "AGENTS", width: agentCol.width, colour: head},
	}
	if spare > 1 {
		header = append(header, field{width: spare - 1})
	}
	header = append(header, field{text: "BOXES", width: boxesW, colour: head, right: true})
	t.SetCell(0, 0, tview.NewTableCell(rowText(withHeadingIcons(header))).SetSelectable(false).SetExpansion(1))
	current := a.ubx.overview.Context.Set.Profile
	for i, r := range v.profileRows {
		mark := field{width: 2}
		if r.profile.Name == current {
			mark = field{text: " " + glyphDot, width: 2, colour: colAccent}
		}
		state, stateRole := a.sandboxState(r)
		n := 0
		for _, s := range a.ubx.overview.Sandboxes {
			if s.Profile == r.profile.Name {
				n++
			}
		}
		cells := []field{
			mark,
			{text: r.profile.Name, width: nameCol.width, colour: role("sandboxes.profile")},
			{text: state, width: stateCol.width, colour: role(stateRole)},
			{text: profileAgentsText(r.profile), width: agentCol.width, colour: role("sandboxes.agent")},
		}
		if spare > 1 {
			cells = append(cells, field{width: spare - 1})
		}
		cells = append(cells, field{text: fmt.Sprint(n), width: boxesW, colour: colMuted, right: true})
		t.SetCell(i+1, 0, tview.NewTableCell(rowText(cells)).SetExpansion(1))
	}
	if len(v.profileRows) == 0 {
		t.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+"No profiles."+tagEnd).SetSelectable(false))
	}
	t.Select(min(max(row, 1), max(1, t.GetRowCount()-1)), 0)
}

// profileAgentsText is a profile's agents by unagit's names for them.
func profileAgentsText(p unibox.Profile) string {
	var names []string
	for _, id := range p.Agents {
		names = append(names, agentLabel(id))
	}
	return strings.Join(names, ", ")
}

func (v *sandboxesView) profileSelection() (string, []uiAction) {
	r, ok := v.profileAt()
	if !ok {
		return "Profiles", nil
	}
	return "Actions · " + sandboxRowName(r), v.app.sandboxRowActions(r)
}

// --------------------------------------------------------------- the log

// uniboxEvent is one thing asked of unibox from unagit, for the log: what
// it was, the command, how it went and what unibox said - every line of
// it, for a command whose output is worth reading.
type uniboxEvent struct {
	at      time.Time
	title   string
	command string
	level   string
	said    string
	output  []string
}

// The levels of an event.
const (
	eventUnderWay = "under way"
	eventDone     = "done"
	eventRefused  = "refused"
	eventFailed   = "failed"
	eventHere     = "terminal"
)

// sandboxLogKept is how many events the log keeps.
const sandboxLogKept = 200

// logUnibox writes down that a command was started, on the event loop.
func (a *App) logUnibox(title string, args ...string) *uniboxEvent {
	e := &uniboxEvent{at: time.Now(), title: title, command: "unibox " + strings.Join(args, " "), level: eventUnderWay}
	a.ubxLog = append(a.ubxLog, e)
	if len(a.ubxLog) > sandboxLogKept {
		a.ubxLog = a.ubxLog[len(a.ubxLog)-sandboxLogKept:]
	}
	if a.sandboxes != nil {
		a.sandboxes.drawLog()
	}
	return e
}

// end writes down how the command went, on the event loop.
func (e *uniboxEvent) end(a *App, said string, err error, refused bool) {
	switch {
	case refused:
		e.level, e.said = eventRefused, err.Error()
	case err != nil:
		e.level, e.said = eventFailed, err.Error()
	default:
		e.level, e.said = eventDone, said
	}
	if a.sandboxes != nil {
		a.sandboxes.drawLog()
	}
}

func eventMark(level string) (string, string) {
	switch level {
	case eventUnderWay:
		return spinnerGlyph(0), "sandboxes.running"
	case eventDone:
		return glyphCheck, "sandboxes.running"
	case eventRefused:
		return glyphRing, "sandboxes.problem"
	case eventFailed:
		return glyphCross, "sandboxes.failed"
	}
	return glyphDot, "sandboxes.stopped"
}

// drawLog fills the log, newest first, under the names of its columns.
func (v *sandboxesView) drawLog() {
	a := v.app
	t := v.log
	row, _ := t.GetSelection()
	t.Clear()
	v.logRows = v.logRows[:0]
	for i := len(a.ubxLog) - 1; i >= 0; i-- {
		v.logRows = append(v.logRows, a.ubxLog[i])
	}
	_, _, width, _ := t.GetInnerRect()
	whenW, whatW := headingWidth("WHEN"), headingWidth("WHAT")
	for _, e := range v.logRows {
		whenW = max(whenW, cells(eventTime(e.at)))
		whatW = max(whatW, min(28, cells(e.title)))
	}
	head := role("activity.label")
	detailsW := max(8, width-whenW-whatW-6)
	t.SetCell(0, 0, tview.NewTableCell(rowText(withHeadingIcons([]field{
		{width: 1},
		{text: "WHEN", width: whenW, colour: head},
		{width: 1},
		{text: "WHAT", width: whatW, colour: head},
		{text: "DETAILS", width: detailsW, colour: head},
	}))).SetSelectable(false).SetExpansion(1))
	for i, e := range v.logRows {
		glyph, colour := eventMark(e.level)
		if e.level == eventUnderWay {
			glyph = spinnerGlyph(a.spinFrame)
		}
		line := e.said
		if line == "" {
			line = e.level
		}
		t.SetCell(i+1, 0, tview.NewTableCell(rowText([]field{
			{width: 1},
			{text: eventTime(e.at), width: whenW, colour: role("activity.when")},
			{text: glyph, width: 1, colour: role(colour)},
			{text: e.title, width: whatW, colour: role("activity.what")},
			{text: line, width: detailsW, colour: role("activity.about")},
		})).SetExpansion(1))
	}
	if len(v.logRows) == 0 {
		t.SetCell(1, 0, tview.NewTableCell(" "+tag(colMuted)+"Nothing asked of unibox yet: what is done here comes here, with what unibox said."+tagEnd).SetSelectable(false))
	}
	if !t.HasFocus() {
		t.SetOffset(0, 0)
		t.SetSelectable(false, false)
		row = 1
	} else {
		t.SetSelectable(true, false)
	}
	t.Select(min(max(row, 1), max(1, t.GetRowCount()-1)), 0)
}

func (v *sandboxesView) logEvent() (*uniboxEvent, bool) {
	row, _ := v.log.GetSelection()
	if row < 1 || row > len(v.logRows) {
		return nil, false
	}
	return v.logRows[row-1], true
}

func (v *sandboxesView) logSelection() (string, []uiAction) {
	a := v.app
	return "Log", []uiAction{
		{name: "Show Event", about: "The command, how it went and everything unibox said, in front of the screen.",
			keys: "Enter", rank: 10, when: func() bool { _, ok := v.logEvent(); return ok },
			run: func() {
				if e, ok := v.logEvent(); ok {
					a.showUniboxEvent(e)
				}
			}},
		{name: "Copy Command", about: "Put the event's unibox command on the clipboard, to run it again from a terminal.",
			keys: "y", rank: 20, when: func() bool { _, ok := v.logEvent(); return ok },
			run: func() {
				if e, ok := v.logEvent(); ok {
					a.yank("Command", e.command)
				}
			}},
	}
}

// showUniboxEvent shows an event at full length.
func (a *App) showUniboxEvent(e *uniboxEvent) {
	d := &detailBuf{}
	d.title(e.title)
	d.sub(e.at.Format("Jan 2 15:04:05") + " · " + e.level)
	d.blank()
	d.kv("Command", esc(e.command))
	if e.said != "" {
		d.kv("unibox said", esc(e.said))
	}
	if len(e.output) > 0 {
		d.section("Output")
		for _, line := range e.output {
			d.raw(tag(colMuted) + outputLine(line) + tagEnd + "\n")
		}
	}
	a.showSandboxText(e.title, d.String())
}

// ----------------------------------------------------------- the hints

// hint is the keys of the panel with the focus: what can be done there,
// never how to move.
func (v *sandboxesView) hint() (tview.Primitive, string) {
	p := v.pane
	join := func(parts ...string) string { return strings.Join(parts, " · ") }
	switch {
	case v.offOn:
		return nil, ""
	case p.table.HasFocus():
		if _, ok := v.taskAt(p.selectedIndex()); !ok {
			return p.table, ""
		}
		return p.table, join("o resume", "a with agent", "s shell", "d remove", "Tab panels")
	case p.detail.HasFocus():
		return p.detail, ""
	case v.cards.HasFocus():
		if _, ok := v.cards.chosen(); !ok {
			return v.cards, ""
		}
		return v.cards, join("n new task", "s shell", "p ports", "x stop")
	case v.env.HasFocus():
		return v.env, join("K secrets", "V values", "U settings")
	case v.profiles.HasFocus():
		return v.profiles, join("n task", "c sandbox", "b build", "v services")
	case v.log.HasFocus():
		return v.log, join("Enter show", "y copy command")
	}
	return nil, ""
}

// drawPanelHint writes a panel's keys into its bottom border, after
// everything is drawn: a panel as low as a band of cards has no line to
// spare for them.
func drawPanelHint(screen tcell.Screen, panel tview.Primitive, text string) {
	if panel == nil || text == "" {
		return
	}
	x, y, w, h := panel.GetRect()
	if w < 12 || h < 2 {
		return
	}
	text = " " + litHint(text) + " "
	tview.Print(screen, "["+":"+colBackground.String()+"]"+text, x+2, y+h-1, w-4, tview.AlignLeft, colDim)
}

func (a *App) drawSandboxesHint(screen tcell.Screen) {
	if a.sandboxes == nil || a.currentTab() != pageSandboxes || a.modalOpen() {
		return
	}
	panel, text := a.sandboxes.hint()
	drawPanelHint(screen, panel, text)
}
