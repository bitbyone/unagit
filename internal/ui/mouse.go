package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The mouse does what the keys do, never something of its own: a click puts
// the cursor on a row, a double click is Enter, the right button opens the
// action picker of what it is on (the selection's, as . does), Ctrl- or
// Alt-click marks a row as space does, Shift-click marks from the cursor to
// it, and a drag over a list's rows marks them all. The tabs are clicked to,
// the wheel scrolls, and buttons, fields and selects take a click as tview
// gives it them.
//
// tview's own lists do more on a click - a List and a TreeView run their
// Enter on it - so unagit answers the mouse itself over every list, and
// hands tview the rest. Nothing beneath a dialog is reached through it, and
// a click never leaves the keys with something that has no use for them.

// mouseState is what the mouse is in the middle of.
type mouseState struct {
	// captured is what tview asked to have the mouse's next events, a text
	// area being drawn over, say.
	captured tview.Primitive
	// drag is a press on a list's row that may become a drag over rows.
	drag *rowDrag
	// clickX, clickY and clickAt are where and when the last click was.
	// unagit tells a double click itself: tview counts any two clicks in
	// quick succession as one, wherever they were and whichever button.
	clickX, clickY int
	clickAt        time.Time
	// scrollBy is the wheel's turn not yet applied, over the list at
	// scrollX, scrollY; scrollQueued says its frame is on the way.
	scrollBy, scrollX, scrollY int
	scrollQueued               bool
}

// rowDrag marks the rows a drag passes over: from the row it began on to
// the row under the pointer, on top of what was marked before it.
type rowDrag struct {
	pane   *pane
	anchor int
	before map[int]bool
	moved  bool
}

// mouseHandled is what onMouse hands tview for an event it answered: an
// action no widget knows. Handing back no event at all would make tview drop
// the rest of what the same event means - the press after a move.
const mouseHandled tview.MouseAction = -1

// mouseActionsKey is what a right click presses: the selection's action
// picker opens on it whatever keys the picker is bound to, and nothing
// else answers it.
var mouseActionsKey = tcell.NewEventKey(tcell.KeyF64, 0, tcell.ModNone)

// mouseScreenActionsKey is what a right click on a tab presses: the
// screen's action picker, as : opens it.
var mouseScreenActionsKey = tcell.NewEventKey(tcell.KeyF63, 0, tcell.ModNone)

// setRoot is what the application draws, kept for the mouse to look into.
func (a *App) setRoot(root tview.Primitive) {
	a.root = root
	a.tv.SetRoot(root, true)
}

// applyMouse takes the mouse or leaves it to the terminal, as chosen.
func (a *App) applyMouse() {
	a.tv.EnableMouse(a.cfg.MouseOn())
	a.mouse = mouseState{}
}

// onMouse answers every mouse event, in place of tview's own routing.
func (a *App) onMouse(ev *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if ev == nil || a.root == nil {
		return ev, action
	}
	before := a.tv.GetFocus()
	defer func() {
		a.afterMouse(before)
		// tview draws after an event only when one of its widgets says it
		// took it, and none does here: unagit answered it. Without a draw
		// of its own, the cursor and the marks a click moved showed only
		// when something else drew, seconds later. The pointer's mere
		// moving changes nothing and draws nothing.
		switch action {
		case tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseScrollLeft, tview.MouseScrollRight:
			// A wheel - a trackpad more so - sends dozens of these a
			// second; drawn after each, they queued up behind one another
			// and a key or a click after them waited seconds.
			a.drawSoon()
		case tview.MouseMove:
			if a.mouse.drag != nil || a.mouse.captured != nil {
				a.drawSoon()
			}
		default:
			a.tv.ForceDraw()
		}
	}()
	x, y := ev.Position()
	switch action {
	case tview.MouseLeftClick, tview.MouseLeftDoubleClick:
		// When the terminal sent it, not when the loop got to it: a click
		// kept waiting behind a draw is not the first half of a double one.
		now := ev.When()
		if x == a.mouse.clickX && y == a.mouse.clickY && now.Sub(a.mouse.clickAt) <= tview.DoubleClickInterval {
			action = tview.MouseLeftDoubleClick
			a.mouse.clickAt = time.Time{}
		} else {
			action = tview.MouseLeftClick
			a.mouse.clickX, a.mouse.clickY, a.mouse.clickAt = x, y, now
		}
	case tview.MouseRightClick, tview.MouseRightDoubleClick:
		action = tview.MouseRightClick
		a.mouse.clickAt = time.Time{}
	case tview.MouseMiddleClick, tview.MouseMiddleDoubleClick:
		action = tview.MouseMiddleClick
		a.mouse.clickAt = time.Time{}
	}

	if drag := a.mouse.drag; drag != nil {
		switch {
		case action == tview.MouseMove && ev.Buttons()&tcell.ButtonPrimary != 0:
			a.dragTo(drag, y)
			return ev, mouseHandled
		case action == tview.MouseLeftUp:
			a.mouse.drag = nil
			if drag.moved {
				return ev, mouseHandled
			}
		}
	}
	if action == tview.MouseMove {
		// Nothing follows the pointer.
		if a.mouse.captured != nil {
			a.dispatchMouse(ev, action)
		}
		return ev, mouseHandled
	}
	if a.mouseOnTabs(x, y, action) {
		return ev, mouseHandled
	}
	if top := a.mouseTop(); top != a.root && a.mouse.captured == nil {
		if _, beside := primitiveAt(top, x, y).(*modalBox); beside || !inRect(top, x, y) {
			// Beside the dialog in front: nothing beneath it is reached,
			// and a click there closes it, as Esc does.
			if action == tview.MouseLeftClick || action == tview.MouseLeftDoubleClick {
				a.closeFrontByMouse()
			}
			return ev, mouseHandled
		}
	}
	if mark := a.closeMarkAt(x, y); mark != nil && a.mouse.captured == nil {
		if action == tview.MouseLeftClick || action == tview.MouseLeftDoubleClick {
			mark.close()
		}
		return ev, mouseHandled
	}
	if a.mouse.captured == nil {
		if rows := a.rowsAt(x, y); rows != nil {
			a.mouseOnRows(rows, ev, action)
			return ev, mouseHandled
		}
	}
	a.dispatchMouse(ev, action)
	if action == tview.MouseLeftDoubleClick && a.textPanelAt(x, y) {
		// A panel that is a few lines of text - Watching, the environment
		// - has no row to double click: the whole panel is its row, and
		// Enter its action.
		a.pressKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	}
	if action == tview.MouseRightClick {
		// A right click on a field or a button takes the keys there - it
		// presses nothing - then opens the actions of where it landed.
		a.dispatchMouse(ev, tview.MouseLeftDown)
		a.pressKey(mouseActionsKey)
	}
	return ev, mouseHandled
}

// closeFrontByMouse does with the dialog in front what Esc does - goes
// back to what it was opened from - as many times as it takes for that
// dialog to go: in a field or a filter, the first Esc only stops typing.
// The passphrase is not left that way.
func (a *App) closeFrontByMouse() {
	name, front := a.pages.GetFrontPage()
	if name == pageUnlock {
		return
	}
	for range 3 {
		a.pressKey(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
		if now, p := a.pages.GetFrontPage(); now != name || p != front {
			return
		}
	}
}

// pressKey handles a key at once, the way tview hands it on: the
// application's capture, then whatever has the focus. Never through
// QueueEvent: that waits for room in the very queue the event loop, which
// runs this, is the one to empty - a hand moving the mouse fills it, and
// unagit stopped.
func (a *App) pressKey(ev *tcell.EventKey) {
	if capture := a.tv.GetInputCapture(); capture != nil {
		if ev = capture(ev); ev == nil {
			return
		}
	}
	if a.root != nil && a.root.HasFocus() {
		if handler := a.root.InputHandler(); handler != nil {
			handler(ev, func(p tview.Primitive) { a.tv.SetFocus(p) })
		}
	}
}

// mouseTop is what the mouse can reach: the dialog in front, or all.
func (a *App) mouseTop() tview.Primitive {
	if a.pages != nil && a.modalOpen() {
		if _, front := a.pages.GetFrontPage(); front != nil {
			return front
		}
	}
	return a.root
}

// dispatchMouse hands an event to tview's widgets, as tview would.
func (a *App) dispatchMouse(ev *tcell.EventMouse, action tview.MouseAction) {
	target := a.mouse.captured
	if target == nil {
		target = a.mouseTop()
	}
	handler := target.MouseHandler()
	if handler == nil {
		return
	}
	x, y := ev.Position()
	_, a.mouse.captured = handler(action, ev, func(p tview.Primitive) {
		// A widget of unagit's own - the cards, the worktree's blocks -
		// takes a click with the Box it is built on, which then asks for
		// the focus itself: a Box nobody knows, the widget lit as if it had
		// the keys and its screen none the wiser. The widget drawn there
		// is what gets it.
		if _, bare := p.(*tview.Box); bare {
			if under := primitiveAt(a.mouseTop(), x, y); under != nil {
				p = under
			}
		}
		a.tv.SetFocus(p)
	})
}

// mouseOnTabs switches to the tab clicked on the bar, while no dialog is in
// front: what a dialog is in the middle of is not left by a click beside it.
// The right button opens that tab's screen actions, as : does.
func (a *App) mouseOnTabs(x, y int, action tview.MouseAction) bool {
	if a.tabs == nil || !a.tabs.InRect(x, y) {
		return false
	}
	if (action == tview.MouseLeftClick || action == tview.MouseRightClick) && !a.modalOpen() {
		if page := a.tabAt(x); page != "" {
			a.switchTab(page)
			if action == tview.MouseRightClick {
				a.pressKey(mouseScreenActionsKey)
			}
		}
	}
	return true
}

// afterMouse keeps the screens' own idea of where the keys are in step with
// where a click put them: a pane's list, detail or filter, Settings' halves,
// a form's mode. What has no use for the keys - a note, a status line - does
// not keep them.
func (a *App) afterMouse(before tview.Primitive) {
	now := a.tv.GetFocus()
	if now == before {
		return
	}
	if !takesKeys(now) {
		a.tv.SetFocus(before)
		return
	}
	if a.focusPanel(now) {
		return
	}
	for _, p := range a.mainPanes() {
		switch now {
		case p.detail:
			if !p.detailFocused {
				p.focusDetail()
			}
		case p.filter:
			if !p.filtering {
				p.startFilter()
			}
		case p.table:
			if p.filtering || p.detailFocused {
				p.focusTable()
			}
		}
	}
	if form, mode := a.focusedForm(); form != nil {
		item := focusedItem(form)
		mode.insert = item != nil && editable(item)
	}
	if s := a.settings; s != nil && a.currentTab() == pageSettings && !a.modalOpen() {
		s.contentFocused = !s.list.HasFocus()
		s.paintFocus()
	}
	if !a.modalOpen() {
		a.forgetReturn()
	}
}

// takesKeys reports whether a click may leave the keys with something:
// whatever is typed into, pressed or moved in, or answers keys of its own.
func takesKeys(p tview.Primitive) bool {
	switch v := p.(type) {
	case nil:
		return false
	case *tview.InputField, *tview.TextArea, *tview.Button, *tview.Checkbox, *tview.DropDown,
		*tview.Table, *tview.List, *tview.TreeView, *tview.Form,
		*keptTable, *bandedList:
		return true
	case interface {
		GetInputCapture() func(*tcell.EventKey) *tcell.EventKey
	}:
		return v.GetInputCapture() != nil
	}
	return false
}

// panelled is a screen of panels around a pane - Activity, Sandboxes -
// whose focus lights the one border and turns the screen to that panel.
type panelled interface {
	panels() []tview.Primitive
	focus(tview.Primitive)
}

// panelledScreens are the screens made of panels.
func (a *App) panelledScreens() []panelled {
	var screens []panelled
	if a.activity != nil {
		screens = append(screens, a.activity)
	}
	if a.sandboxes != nil {
		screens = append(screens, a.sandboxes)
	}
	return screens
}

// focusPanel gives the keys to a panel of such a screen the way its keys
// do, and reports whether it was one: tview's focus alone left the panel
// before it lit and the screen still turned to it.
func (a *App) focusPanel(to tview.Primitive) bool {
	for _, v := range a.panelledScreens() {
		for _, panel := range v.panels() {
			if panel == to {
				v.focus(to)
				return true
			}
		}
	}
	return false
}

// textPanelAt says whether a panel of text is under the pointer: one of a
// screen's panels that is neither a list nor the detail column, whose text
// is about what is chosen elsewhere.
func (a *App) textPanelAt(x, y int) bool {
	tv, ok := primitiveAt(a.mouseTop(), x, y).(*tview.TextView)
	if !ok {
		return false
	}
	for _, p := range a.mainPanes() {
		if tv == p.detail {
			return false
		}
	}
	for _, v := range a.panelledScreens() {
		for _, panel := range v.panels() {
			if panel == tv {
				return true
			}
		}
	}
	return false
}

// mainPanes are the lists of the main screens.
func (a *App) mainPanes() []*pane {
	var out []*pane
	for _, p := range []*pane{a.projectsPane, a.mrsPane, a.worktreesPane, a.activityPane, a.sandboxesPane} {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

// paneOf is the main screen's pane a table is the list of, or nil.
func (a *App) paneOf(t *tview.Table) *pane {
	for _, p := range a.mainPanes() {
		if p.table == t {
			return p
		}
	}
	return nil
}

// ---------------------------------------------------------------- the rows

// rowsUnder is a list as the mouse uses it, whatever widget draws it: the
// row under a point, the cursor, and what takes the keys.
type rowsUnder struct {
	focus  tview.Primitive
	table  *tview.Table
	at     func(x, y int) int
	cursor func() int
	move   func(row int)
	// step moves the cursor to the next row that can have it, up or down.
	step func(dir int)
	// edge is the row beyond the top (-1) or the bottom (1) of what is
	// drawn, for a drag that goes past it; -1 when there is none.
	edge func(side int) int
}

// rowsAt is the list under the point, or nil when it is not one; what a
// dialog covers is not under it.
func (a *App) rowsAt(x, y int) *rowsUnder {
	switch v := primitiveAt(a.mouseTop(), x, y).(type) {
	case *keptTable:
		return tableRows(v.Table)
	case *tview.Table:
		return tableRows(v)
	case *bandedList:
		return listRows(v.List)
	case *tview.List:
		return listRows(v)
	case *tview.TreeView:
		return treeRows(v)
	case *integrationsView:
		return v.rows()
	}
	return nil
}

// primitiveAt is the innermost widget drawn at a point: through the
// layouts, the front page that covers it, a dialog's frame and a form.
func primitiveAt(p tview.Primitive, x, y int) tview.Primitive {
	if p == nil || !inRect(p, x, y) {
		return nil
	}
	switch v := p.(type) {
	case *tview.Pages:
		names := v.GetPageNames(true)
		for i := len(names) - 1; i >= 0; i-- {
			page := v.GetPage(names[i])
			if inRect(page, x, y) {
				return primitiveAt(page, x, y)
			}
		}
		return nil
	case *modalBox:
		if inner := primitiveAt(v.content, x, y); inner != nil {
			return inner
		}
		// Beside the dialog: nothing, not what is under it.
		return v
	case *tview.Form:
		for i := range v.GetFormItemCount() {
			if item := v.GetFormItem(i); inRect(item, x, y) {
				return primitiveAt(item, x, y)
			}
		}
		for i := range v.GetButtonCount() {
			if b := v.GetButton(i); inRect(b, x, y) {
				return b
			}
		}
		return v
	case *tview.Frame:
		if inner := primitiveAt(v.GetPrimitive(), x, y); inner != nil {
			return inner
		}
		return v
	case interface {
		GetItemCount() int
		GetItem(int) tview.Primitive
	}:
		for i := range v.GetItemCount() {
			if inner := primitiveAt(v.GetItem(i), x, y); inner != nil {
				return inner
			}
		}
		return p
	}
	return p
}

func inRect(p tview.Primitive, x, y int) bool {
	if p == nil {
		return false
	}
	rx, ry, w, h := p.GetRect()
	return x >= rx && x < rx+w && y >= ry && y < ry+h
}

func tableRows(t *tview.Table) *rowsUnder {
	selectable := func(row int) bool {
		if row < 0 || row >= t.GetRowCount() {
			return false
		}
		cell := t.GetCell(row, 0)
		return cell != nil && !cell.NotSelectable
	}
	return &rowsUnder{
		focus: t,
		table: t,
		at: func(x, y int) int {
			row, _ := t.CellAt(x, y)
			if !selectable(row) {
				return -1
			}
			return row
		},
		cursor: func() int { row, _ := t.GetSelection(); return row },
		move: func(row int) {
			_, column := t.GetSelection()
			t.Select(row, max(column, 0))
		},
		step: func(dir int) {
			row, column := t.GetSelection()
			for row += dir; row >= 0 && row < t.GetRowCount(); row += dir {
				if selectable(row) {
					t.Select(row, max(column, 0))
					return
				}
			}
		},
		edge: func(side int) int {
			// The first and the last row drawn, a heading kept on top
			// passed over, and the row beyond.
			x, top, _, h := t.GetInnerRect()
			first, _ := t.CellAt(x, top)
			if !selectable(first) {
				first, _ = t.CellAt(x, top+1)
			}
			last, _ := t.CellAt(x, top+h-1)
			row := first - 1
			if side > 0 {
				row = last + 1
			}
			for ; row >= 0 && row < t.GetRowCount(); row += side {
				if selectable(row) {
					return row
				}
			}
			return -1
		},
	}
}

func listRows(l *tview.List) *rowsUnder {
	return &rowsUnder{
		focus: l,
		at: func(x, y int) int {
			_, top, _, h := l.GetInnerRect()
			if y < top || y >= top+h {
				return -1
			}
			offset, _ := l.GetOffset()
			if i := offset + y - top; i < l.GetItemCount() {
				return i
			}
			return -1
		},
		cursor: l.GetCurrentItem,
		move:   func(i int) { l.SetCurrentItem(i) },
		step: func(dir int) {
			if i := l.GetCurrentItem() + dir; i >= 0 && i < l.GetItemCount() {
				l.SetCurrentItem(i)
			}
		},
		edge: func(int) int { return -1 },
	}
}

// treeRows reads the rows as the tree last drew them: its nodes in order,
// those of a folded node left out, the root too where the tree hides it.
func treeRows(t *tview.TreeView) *rowsUnder {
	visible := func() []*tview.TreeNode {
		var all []*tview.TreeNode
		if root := t.GetRoot(); root != nil {
			root.Walk(func(node, _ *tview.TreeNode) bool {
				all = append(all, node)
				return node.IsExpanded()
			})
		}
		if skip := len(all) - t.GetRowCount(); skip > 0 && skip <= len(all) {
			all = all[skip:]
		}
		return all
	}
	return &rowsUnder{
		focus: t,
		at: func(x, y int) int {
			_, top, _, h := t.GetInnerRect()
			if y < top || y >= top+h {
				return -1
			}
			i := t.GetScrollOffset() + y - top
			if nodes := visible(); i < len(nodes) && nodes[i].GetReference() != nil {
				return i
			}
			return -1
		},
		cursor: func() int {
			for i, node := range visible() {
				if node == t.GetCurrentNode() {
					return i
				}
			}
			return -1
		},
		move: func(i int) {
			if nodes := visible(); i >= 0 && i < len(nodes) {
				t.SetCurrentNode(nodes[i])
			}
		},
		step: func(dir int) {
			nodes := visible()
			at := -1
			for i, node := range nodes {
				if node == t.GetCurrentNode() {
					at = i
				}
			}
			for i := at + dir; i >= 0 && i < len(nodes); i += dir {
				if nodes[i].GetReference() != nil {
					t.SetCurrentNode(nodes[i])
					return
				}
			}
		},
		edge: func(int) int { return -1 },
	}
}

// mouseOnRows is the mouse on a list: the cursor goes where it is pressed,
// Enter and the actions follow, marks are made.
func (a *App) mouseOnRows(rows *rowsUnder, ev *tcell.EventMouse, action tview.MouseAction) {
	x, y := ev.Position()
	switch action {
	case tview.MouseScrollUp, tview.MouseScrollDown:
		// The wheel moves the cursor, as j and k do: tview scrolls a
		// list's view alone, and a List puts it back to the cursor at
		// once, so a picker did not scroll at all.
		dir := 1
		if action == tview.MouseScrollUp {
			dir = -1
		}
		a.queueScroll(x, y, dir)
		return
	case tview.MouseScrollLeft, tview.MouseScrollRight:
		return
	}
	row := rows.at(x, y)
	var p *pane
	if rows.table != nil {
		p = a.paneOf(rows.table)
	}
	switch action {
	case tview.MouseLeftDown, tview.MouseRightDown:
		a.focusRows(rows, p)
		// A panel may draw its rows anew as it takes the keys.
		row = rows.at(x, y)
		if row < 0 {
			return
		}
		mods := ev.Modifiers()
		if action == tview.MouseLeftDown && p != nil && p.markable && mods&(tcell.ModShift|tcell.ModCtrl|tcell.ModAlt) == 0 {
			a.mouse.drag = &rowDrag{pane: p, anchor: row, before: copyMarks(p.marks)}
		}
		if mods&tcell.ModShift != 0 && action == tview.MouseLeftDown && p != nil && p.markable {
			if from := rows.cursor(); from > 0 {
				p.marks = copyMarks(p.marks)
				markRows(p, from, row, true)
			}
		}
		if action == tview.MouseLeftDown && mods&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			// On the press, not the click: a second click soon after the
			// first is a double click, and would mark nothing.
			switch {
			case p != nil && p.markable:
				p.marks = copyMarks(p.marks)
				markRows(p, row, row, !p.marks[rowIndex(p, row)])
			case p == nil:
				// A picker's marks are its own: its space marks the row
				// and moves on, as it does from the keys.
				rows.move(row)
				a.pressKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
				return
			}
		}
		rows.move(row)
		if p != nil {
			p.chosen = true
		}
	case tview.MouseLeftDoubleClick:
		if row >= 0 && ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt|tcell.ModShift) == 0 {
			a.pressKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
		}
	case tview.MouseRightClick:
		if row >= 0 {
			a.pressKey(mouseActionsKey)
		}
	}
}

// queueScroll adds a notch of the wheel to what the next frame applies: a
// trackpad sends dozens a second, and moving the cursor for each - a picker
// explaining every action it passed - fell seconds behind, Esc waiting at
// the back. Ups and downs in between cancel out. Everything here runs on
// the event loop, the frame's update too.
func (a *App) queueScroll(x, y, dir int) {
	a.mouse.scrollBy += dir
	a.mouse.scrollX, a.mouse.scrollY = x, y
	if a.mouse.scrollQueued {
		return
	}
	a.mouse.scrollQueued = true
	time.AfterFunc(16*time.Millisecond, func() {
		a.tv.QueueUpdateDraw(func() {
			by, x, y := a.mouse.scrollBy, a.mouse.scrollX, a.mouse.scrollY
			a.mouse.scrollBy, a.mouse.scrollQueued = 0, false
			// Read again: what was under the pointer may have closed.
			rows := a.rowsAt(x, y)
			if rows == nil || by == 0 {
				return
			}
			dir := 1
			if by < 0 {
				dir, by = -1, -by
			}
			for range by {
				a.scrollRows(rows, dir)
			}
		})
	})
}

// scrollRows moves a list's cursor a row: by its own keys when it, or the
// dialog it is in, has them - a picker passes over its rules that way, and
// moves its list from the filter too - and on its own when it is a panel
// beside the one with the keys, which keeps them.
func (a *App) scrollRows(rows *rowsUnder, dir int) {
	if rows.focus.HasFocus() || a.modalOpen() {
		key := tcell.KeyDown
		if dir < 0 {
			key = tcell.KeyUp
		}
		a.pressKey(tcell.NewEventKey(key, 0, tcell.ModNone))
		return
	}
	rows.step(dir)
}

// drawSoon draws once the frame's worth of events is in: as many as come
// in the meantime are drawn by the one draw.
func (a *App) drawSoon() {
	if a.drawPending.Swap(true) {
		return
	}
	time.AfterFunc(16*time.Millisecond, func() {
		a.tv.QueueUpdateDraw(func() { a.drawPending.Store(false) })
	})
}

// focusRows gives a list the keys, by the way its screen would.
func (a *App) focusRows(rows *rowsUnder, p *pane) {
	if a.tv.GetFocus() != rows.focus && a.focusPanel(rows.focus) {
		return
	}
	switch {
	case p != nil:
		if !p.table.HasFocus() || p.filtering || p.detailFocused {
			p.focusTable()
		}
	case !rows.focus.HasFocus():
		a.tv.SetFocus(rows.focus)
	}
}

// dragTo marks the rows from where the drag began to the one under the
// pointer, scrolling a row on when it goes past the list's edge.
func (a *App) dragTo(drag *rowDrag, y int) {
	p := drag.pane
	rows := tableRows(p.table)
	x, top, w, h := p.table.GetInnerRect()
	row := rows.at(x+w/2, y)
	switch {
	case row >= 0:
	case y <= top:
		row = rows.edge(-1)
	case y >= top+h-1:
		row = rows.edge(1)
	}
	if row < 0 || row == rows.cursor() && drag.moved {
		return
	}
	if row == drag.anchor && !drag.moved {
		return
	}
	drag.moved = true
	p.marks = copyMarks(drag.before)
	markRows(p, drag.anchor, row, true)
	rows.move(row)
}

// markRows marks or unmarks the rows of a pane from one to another, and
// draws the list again.
func markRows(p *pane, from, to int, on bool) {
	if from > to {
		from, to = to, from
	}
	for row := from; row <= to; row++ {
		i := rowIndex(p, row)
		if i < 0 {
			continue
		}
		if on {
			if p.marks == nil {
				p.marks = map[int]bool{}
			}
			p.marks[i] = true
		} else {
			delete(p.marks, i)
		}
	}
	if len(p.marks) == 0 {
		p.marks = nil
	}
	if p.reload != nil {
		p.reload()
	}
	p.updateHeader()
}

// rowIndex is the data index of a pane's row, or -1 for a heading.
func rowIndex(p *pane, row int) int {
	if row < 1 || row >= p.table.GetRowCount() {
		return -1
	}
	if i, ok := p.table.GetCell(row, 0).GetReference().(int); ok {
		return i
	}
	return -1
}

func copyMarks(m map[int]bool) map[int]bool {
	if len(m) == 0 {
		return nil
	}
	out := make(map[int]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ------------------------------------------------------------ close marks

// closeMark is a close mark drawn in a panel's corner: where, and what a
// click on it does - what Esc does there.
type closeMark struct {
	x0, x1, y int
	close     func()
}

// drawCloseMarks puts a close mark in the top right corner of every detail
// column on the screen in front, on its border, for the mouse: the keys
// close it with Esc and need no mark. None is drawn under a dialog, where
// it would stand undimmed on the dimmed screen and could not be clicked.
func (a *App) drawCloseMarks(screen tcell.Screen) {
	a.closeMarks = a.closeMarks[:0]
	if a.pages == nil || a.modalOpen() || !a.cfg.MouseOn() || glyphClose == "" {
		return
	}
	_, front := a.pages.GetFrontPage()
	for _, p := range a.mainPanes() {
		if !p.detailShown || front != p.root {
			continue
		}
		x, y, w, h := p.detail.GetRect()
		// Measured and drawn as tview draws text, so a mark of two cells
		// in this terminal still ends at the corner and leaves it whole.
		mark := " " + glyphClose + " "
		width := tview.TaggedStringWidth(tview.Escape(mark))
		if w < width+8 || h < 2 {
			continue
		}
		at := x + w - 1 - width
		tview.Print(screen, tview.Escape(mark), at, y, width, tview.AlignLeft, role("panel.close"))
		pane := p
		a.closeMarks = append(a.closeMarks, closeMark{at, at + width, y, pane.hideDetail})
	}
}

// closeMarkAt is the close mark at a cell, or nil.
func (a *App) closeMarkAt(x, y int) *closeMark {
	if a.modalOpen() {
		return nil
	}
	for i := range a.closeMarks {
		if m := &a.closeMarks[i]; y == m.y && x >= m.x0 && x < m.x1 {
			return m
		}
	}
	return nil
}
