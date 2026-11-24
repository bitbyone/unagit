package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/board"
	"github.com/tobola/unagit/internal/fuzzy"
)

// The Board (plans/board.md) is the user's tasks as cards in three columns
// - to do, in progress, done - in spaces of their own. A card is the
// comments' card (bubble.go); a task's detail is the conversation's dialog
// (board_detail.go). A task done goes off the board after the time set in
// Settings, and History shows every one.

const (
	pageBoard  = "board"
	scopeBoard = "board"
	// boardMinColumn is the width under which the columns are shown one at
	// a time, h and l going from one to the next.
	boardMinColumn = 30
	// boardHistory is the column History is drawn as, after the three.
	boardHistory = 3
	// boardLook is how often the board looks for what another unagit
	// wrote, and for a task done long enough to go.
	boardLook = 2 * time.Second
)

type boardView struct {
	app    *App
	store  *board.Store
	root   *tview.Flex
	filter *tview.InputField
	header *tview.TextView
	line   *statusLine
	canvas *boardCanvas
	frames [4]*tview.Box

	spaces []board.Space
	space  board.Space
	// tasks are the space's as read; cols what the columns show of them,
	// the filter applied and the tasks done long enough gone; hist every
	// task done, the newest first.
	tasks   []board.Task
	cols    [3][]board.Task
	hist    []board.Task
	history bool
	// col is the column the cursor is in, at the card it is on in each
	// column (History's last), top the first card drawn in each.
	col     int
	at, top [4]int
	query   string
	// filtering is while the filter line has the keys.
	filtering bool
	stamp     time.Time
	filled    time.Time
	problem   string
	// hits are where the cards were last drawn, for the mouse; laid the
	// cards laid out, by all that makes them.
	hits []boardHit
	laid map[laidKey]string
}

// boardHit is a card on screen.
type boardHit struct{ col, i, x, y, w, h int }

// boardCanvas draws the columns and takes the keys.
type boardCanvas struct {
	*tview.Box
	v *boardView
}

func (a *App) newBoardView() *boardView {
	v := &boardView{app: a, store: board.Open(a.cfg.BoardDir()), laid: map[laidKey]string{}}
	v.header = tview.NewTextView().SetDynamicColors(true)
	help := tview.NewTextView().SetDynamicColors(true).SetText(litHint("? help")).SetTextAlign(tview.AlignRight)
	v.line = newStatusLine(v.header, help)
	v.filter = filterField(tview.NewInputField())
	v.filter.SetChangedFunc(func(text string) {
		v.query = text
		v.fill()
	})
	v.filter.SetInputCapture(v.filterKeys)
	v.canvas = &boardCanvas{Box: tview.NewBox(), v: v}
	v.canvas.SetInputCapture(v.keys)
	for i := range v.frames {
		v.frames[i] = box(tview.NewBox(), "")
	}
	v.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.filter, 1, 0, false).
		AddItem(v.canvas, 0, 1, true).
		AddItem(v.line, 1, 0, false)
	v.line.holdIn(v.root)
	a.onRepaint(v.canvas, func() { clear(v.laid) })
	return v
}

// reload reads the spaces and the space's tasks again, the cursor staying
// on the task it was on.
func (v *boardView) reload() {
	a := v.app
	keep := ""
	if t, ok := v.chosen(); ok {
		keep = t.ID
	}
	spaces, err := v.store.Spaces()
	if err != nil {
		v.problem = err.Error()
		return
	}
	v.spaces = spaces
	v.space = spaces[0]
	for _, sp := range spaces {
		if sp.ID == a.cfg.State.BoardSpace {
			v.space = sp
		}
	}
	v.stamp = v.store.Stamp(v.space.ID)
	tasks, err := v.store.Tasks(v.space.ID)
	v.problem = ""
	if err != nil {
		v.problem = err.Error()
	}
	v.tasks = tasks
	v.fill()
	if keep != "" {
		v.focusTask(keep)
	}
}

// fill sorts the tasks into the columns, keeping what the filter finds.
func (v *boardView) fill() {
	after, err := board.ParseAfter(v.app.cfg.Board.HideDoneAfter)
	if err != nil {
		after = 24 * time.Hour
	}
	now := time.Now()
	v.filled = now
	v.cols = [3][]board.Task{}
	v.hist = nil
	for _, t := range v.tasks {
		if q := strings.TrimSpace(v.query); q != "" {
			if _, ok := fuzzy.Match(q, taskHay(t)); !ok {
				continue
			}
		}
		if t.State == board.Done {
			v.hist = append(v.hist, t)
		}
		if t.HiddenAt(now, after) {
			continue
		}
		v.cols[slices.Index(board.States, t.State)] = append(v.cols[slices.Index(board.States, t.State)], t)
	}
	slices.SortStableFunc(v.hist, func(x, y board.Task) int { return y.Done.Compare(x.Done) })
	v.updateHeader()
}

// taskHay is what the filter finds a task by.
func taskHay(t board.Task) string {
	parts := []string{t.Title, t.Body, t.Colour}
	for _, l := range t.Links {
		parts = append(parts, l.Name(), l.URL)
	}
	for _, c := range t.Comments {
		parts = append(parts, c.Body)
	}
	return strings.Join(parts, " ")
}

// column is the column the cursor is in: one of the three, or History.
func (v *boardView) column() int {
	if v.history {
		return boardHistory
	}
	return v.col
}

func (v *boardView) list(col int) []board.Task {
	if col == boardHistory {
		return v.hist
	}
	return v.cols[col]
}

// chosen is the task under the cursor.
func (v *boardView) chosen() (board.Task, bool) {
	col := v.column()
	tasks := v.list(col)
	if len(tasks) == 0 {
		return board.Task{}, false
	}
	return tasks[max(0, min(v.at[col], len(tasks)-1))], true
}

// focusTask puts the cursor on a task, in whichever column it is.
func (v *boardView) focusTask(id string) {
	for col := range 4 {
		if v.history != (col == boardHistory) {
			continue
		}
		for i, t := range v.list(col) {
			if t.ID == id {
				if col != boardHistory {
					v.col = col
				}
				v.at[col] = i
				return
			}
		}
	}
}

func (v *boardView) updateHeader() {
	mode := tag(colMuted) + "NORMAL" + tagEnd
	switch {
	case v.filtering:
		mode = tag(colWarn) + "FILTER" + tagEnd
	case v.history:
		mode = tag(colAccent) + "HISTORY" + tagEnd
	}
	parts := []string{tag(colAccent) + "[::b]" + esc(v.space.Name) + "[::-]" + tagEnd}
	for i, state := range board.States {
		parts = append(parts, tag(colMuted)+fmt.Sprintf("%d %s", len(v.cols[i]), strings.ToLower(board.StateName(state)))+tagEnd)
	}
	if len(v.spaces) > 1 {
		parts = append(parts, tag(colMuted)+counted(len(v.spaces), "space", "spaces")+tagEnd)
	}
	v.line.setRight(v.app.rightLine())
	v.header.SetText(" " + mode + "  " + strings.Join(parts, tag(colMuted)+" · "+tagEnd))
}

// ------------------------------------------------------------------ drawing

func (c *boardCanvas) Draw(screen tcell.Screen) {
	c.Box.DrawForSubclass(screen, c)
	v := c.v
	x, y, w, h := c.GetInnerRect()
	v.hits = v.hits[:0]
	if v.problem != "" {
		tview.Print(screen, tag(colBad)+esc(v.problem)+tagEnd, x+1, y+h/2, w-2, tview.AlignCenter, colBad)
		return
	}
	shown := []int{0, 1, 2}
	switch {
	case v.history:
		shown = []int{boardHistory}
	case w < 3*boardMinColumn+2:
		shown = []int{v.col}
	}
	width := (w - (len(shown) - 1)) / len(shown)
	for i, col := range shown {
		cx := x + i*(width+1)
		cw := width
		if i == len(shown)-1 {
			cw = x + w - cx
		}
		v.drawColumn(screen, col, cx, y, cw, h)
	}
}

// drawColumn draws a column's frame, its name and count on it, and as many
// of its cards as fit, the one under the cursor among them.
func (v *boardView) drawColumn(screen tcell.Screen, col, x, y, w, h int) {
	tasks := v.list(col)
	name := "History"
	if col != boardHistory {
		name = board.StateName(board.States[col])
	}
	f := v.frames[col]
	f.SetRect(x, y, w, h)
	current := col == v.column()
	border := colBorder
	if current && v.canvas.HasFocus() {
		border = colBorderFocus
	}
	f.SetBorderColor(border).SetTitle(fmt.Sprintf(" %s %s ", name, tag(colMuted)+fmt.Sprint(len(tasks))+tagEnd))
	f.Draw(screen)
	ix, iy, iw, ih := f.GetInnerRect()
	cx, cw := ix+1, iw-2
	if cw < minBubbleWidth || ih < 2 {
		return
	}
	if len(tasks) == 0 {
		tview.Print(screen, tag(colMuted)+esc(v.emptyWords(col))+tagEnd, cx, iy, cw, tview.AlignLeft, colMuted)
		return
	}
	at := max(0, min(v.at[col], len(tasks)-1))
	v.at[col] = at
	cards := make([][]string, len(tasks))
	for i, t := range tasks {
		cards[i] = strings.Split(v.card(t, cw, current && i == at), "\n")
	}
	// Full cards stand a row apart, to be told one from the next.
	gap := 0
	if v.full() {
		gap = 1
	}
	// The cursor's card shows whole when it can: the first drawn moves
	// down to it, or up.
	top := min(v.top[col], at)
	for top < at {
		rows := (at - top) * gap
		for _, c := range cards[top : at+1] {
			rows += len(c)
		}
		if rows <= ih {
			break
		}
		top++
	}
	v.top[col] = top
	row, shown := iy, top
	for i := top; i < len(tasks); i++ {
		if i > top {
			row += gap
		}
		if row >= iy+ih {
			break
		}
		from := row
		for _, l := range cards[i] {
			if row >= iy+ih {
				break
			}
			tview.Print(screen, l, cx, row, cw, tview.AlignLeft, colText)
			row++
		}
		v.hits = append(v.hits, boardHit{col: col, i: i, x: cx, y: from, w: cw, h: row - from})
		shown = i + 1
	}
	// What is below the frame, a card cut short by it counted with it.
	rest := len(tasks) - shown
	if shown > top && row >= iy+ih && len(cards[shown-1]) > row-v.hits[len(v.hits)-1].y {
		rest++
	}
	if rest > 0 {
		tview.Print(screen, tag(colMuted)+fmt.Sprintf(" %d more ", rest)+tagEnd, x+1, y+h-1, w-3, tview.AlignRight, colMuted)
	}
}

// emptyWords is what an empty column says.
func (v *boardView) emptyWords(col int) string {
	switch {
	case v.query != "":
		return "Nothing found."
	case col == boardHistory:
		return "Nothing done yet."
	case len(v.tasks) == 0 && col == 0:
		return "No task yet - n writes one."
	case col == 2:
		return "Nothing done lately - y shows History."
	}
	return "Nothing here."
}

// card is a task drawn as a card width wide: its title on the frame, then
// - compact - what it holds as counts on the frame, or - full - its
// description, links and latest comment in it.
func (v *boardView) card(t board.Task, width int, chosen bool) string {
	b := v.bubbleOf(t, chosen)
	key := laidKey{b, width}
	if text, ok := v.laid[key]; ok {
		return text
	}
	if len(v.laid) > 500 {
		clear(v.laid)
	}
	text := renderBubble(b, width)
	v.laid[key] = text
	return text
}

func (v *boardView) bubbleOf(t board.Task, chosen bool) bubble {
	frame, ink := role("board."+t.State), colText
	var fill tcell.Color
	if t.Colour != "" {
		c := role("board.card." + t.Colour)
		frame, ink = c, c
		fill = mix(role("comments.card"), c, 0.14)
	}
	if t.State == board.Done {
		frame = mix(frame, role("comments.card"), 0.45)
		ink = colMuted
	}
	var meta []string
	if n := len(t.Comments); n > 0 {
		meta = append(meta, glyphComment+fmt.Sprint(n))
	}
	if n := len(t.Links); n > 0 {
		meta = append(meta, glyphLinkOr()+fmt.Sprint(n))
	}
	b := bubble{Name: t.Title, Icon: boardGlyph(t.State), Colour: ink, Frame: frame, Fill: fill, Selected: chosen}
	if len(meta) > 0 {
		b.Meta = tag(role("comments.meta")) + esc(strings.Join(meta, " ")) + tagEnd
	}
	if v.full() {
		var body []string
		if d := strings.TrimSpace(t.Body); d != "" {
			lines := strings.Split(d, "\n")
			if len(lines) > 4 {
				lines = append(lines[:4], "…")
			}
			body = append(body, strings.Join(lines, "\n"))
		}
		for _, l := range t.Links {
			body = append(body, "↗ "+l.Name())
		}
		if n := len(t.Comments); n > 0 {
			first, _, _ := strings.Cut(strings.TrimSpace(t.Comments[n-1].Body), "\n")
			body = append(body, "> "+first)
		}
		b.Body = strings.Join(body, "\n\n")
	}
	if t.State == board.Done && !t.Done.IsZero() {
		b.Where = "done " + humanAge(t.Done)
	}
	return b
}

// full is whether the cards are drawn in full.
func (v *boardView) full() bool { return v.app.cfg.State.BoardFull }

// boardGlyph marks a card by its state.
func boardGlyph(state string) string {
	switch state {
	case board.Doing:
		return glyphDot
	case board.Done:
		return glyphCheck
	}
	return "○"
}

// glyphLinkOr is the mark of a task's links.
func glyphLinkOr() string { return "↗" }

// ------------------------------------------------------------------ keys

func (v *boardView) keys(ev *tcell.EventKey) *tcell.EventKey {
	a := v.app
	col := v.column()
	switch {
	case isKey(ev, 'j', tcell.KeyDown):
		v.at[col] = min(v.at[col]+1, max(0, len(v.list(col))-1))
		return nil
	case isKey(ev, 'k', tcell.KeyUp):
		v.at[col] = max(v.at[col]-1, 0)
		return nil
	case isKey(ev, 'g', tcell.KeyHome):
		v.at[col] = 0
		return nil
	case isKey(ev, 'G', tcell.KeyEnd):
		v.at[col] = max(0, len(v.list(col))-1)
		return nil
	case isKey(ev, 'h', tcell.KeyLeft):
		if !v.history {
			v.col = max(0, v.col-1)
		}
		return nil
	case isKey(ev, 'l', tcell.KeyRight):
		if !v.history {
			v.col = min(2, v.col+1)
		}
		return nil
	case ev.Key() == tcell.KeyEnter:
		if t, ok := v.chosen(); ok {
			v.openTask(t, false)
		}
		return nil
	case ev.Key() == tcell.KeyEsc:
		switch {
		case v.query != "":
			v.filter.SetText("")
		case v.history:
			v.toggleHistory()
		}
		return nil
	}
	if a.actionKeys(ev, scopeBoard, v.selection, v.screen) {
		return nil
	}
	if ev.Key() == tcell.KeyRune {
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
	return ev
}

// filterKeys are the filter line's: Esc and Enter go back to the cards,
// the arrows move on them while typing.
func (v *boardView) filterKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyEnter:
		v.filtering = false
		v.app.tv.SetFocus(v.canvas)
		v.updateHeader()
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		v.keys(ev)
		return nil
	}
	return ev
}

func (v *boardView) startFilter() {
	v.filtering = true
	v.app.tv.SetFocus(v.filter)
	v.updateHeader()
}

// MouseHandler chooses the card clicked; a double click opens it, the
// right button lists what can be done with it, the wheel moves the cursor.
func (c *boardCanvas) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return func(action tview.MouseAction, ev *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		v := c.v
		x, y := ev.Position()
		if !c.InRect(x, y) {
			return false, nil
		}
		setFocus(c)
		switch action {
		case tview.MouseScrollDown, tview.MouseScrollUp:
			key := tcell.KeyDown
			if action == tview.MouseScrollUp {
				key = tcell.KeyUp
			}
			v.keys(tcell.NewEventKey(key, 0, tcell.ModNone))
			return true, nil
		}
		hit := -1
		for i, h := range v.hits {
			if x >= h.x && x < h.x+h.w && y >= h.y && y < h.y+h.h {
				hit = i
			}
		}
		if hit < 0 {
			// A click in a column's empty part goes to the column.
			for col, f := range v.frames {
				if col != boardHistory && !v.history && f.InRect(x, y) && action == tview.MouseLeftDown {
					v.col = col
				}
			}
			return true, nil
		}
		h := v.hits[hit]
		switch action {
		case tview.MouseLeftDown, tview.MouseRightClick:
			if h.col != boardHistory {
				v.col = h.col
			}
			v.at[h.col] = h.i
			if action == tview.MouseRightClick {
				if title, acts := v.selection(); acts != nil {
					v.app.showActions(title, acts)
				}
			}
			return true, nil
		case tview.MouseLeftDoubleClick:
			v.keys(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			return true, nil
		}
		return true, nil
	}
}

// ------------------------------------------------------------------ actions

func (v *boardView) selection() (string, []uiAction) {
	t, ok := v.chosen()
	if !ok {
		return "", nil
	}
	return "Actions · " + trunc(t.Title, 48), v.taskActions(t)
}

func (v *boardView) screen() (string, []uiAction) {
	return "Board", v.screenActions()
}

// taskActions are what can be done with a task on the board.
func (v *boardView) taskActions(t board.Task) []uiAction {
	state := slices.Index(board.States, t.State)
	done := t.State == board.Done
	acts := []uiAction{
		{name: "Open Task", about: "The task in full: its description, links, comments and story.", keys: "Enter", rank: 5,
			run: func() { v.openTask(t, false) }},
		{name: "Comment…", about: "Write a comment on the task: a note of what was tried, what is left.", keys: "c", rank: 10,
			run: func() { v.openTask(t, true) }},
		{name: "Edit Task…", about: "Change the task's title and description.", keys: "e", rank: 12,
			run: func() { v.editTask(&t, t.State, nil) }},
		{name: "Move Right", about: "To the next column: to do, in progress, done.", keys: "L", rank: 20,
			when: func() bool { return state < 2 }, run: func() { v.setState(t, board.States[min(state+1, 2)]) }},
		{name: "Move Left", about: "Back to the column before.", keys: "H", rank: 21,
			when: func() bool { return state > 0 && !v.history }, run: func() { v.setState(t, board.States[max(state-1, 0)]) }},
		{name: "Move Down", about: "Lower in its column; the order is what comes first.", keys: "J", rank: 22,
			when: func() bool { return !v.history }, run: func() { v.reorder(t, 1) }},
		{name: "Move Up", about: "Higher in its column.", keys: "K", rank: 23,
			when: func() bool { return !v.history }, run: func() { v.reorder(t, -1) }},
		{name: "Mark Done", about: "Move it to Done; it leaves the board after the time set in Settings, History keeps it.", keys: "x", rank: 25,
			when: func() bool { return !done }, run: func() { v.setState(t, board.Done) }},
		{name: "Open Again", about: "Take a task done back to the column of things to do.", keys: "x", rank: 26,
			when: func() bool { return done }, run: func() { v.setState(t, board.Todo) }},
		{name: "Open in Browser", about: "The task's link; a list when it has several.", keys: "w", rank: 30,
			when: func() bool { return len(t.Links) > 0 }, run: func() { v.openLink(t) }},
		{name: "Link URL…", about: "Add a link to the task: a ticket, a document, a page.", keys: "i", rank: 32,
			run: func() { v.linkURL(t, nil) }},
		{name: "Colour…", about: "Give the card a colour to make it stand out, or take it away; it means nothing else.", keys: "#", rank: 40,
			run: func() { v.pickColour(t) }},
		{name: "Move to Space…", about: "Take the task to another space.", keys: "", rank: 50,
			when: func() bool { return len(v.spaces) > 1 }, run: func() { v.moveToSpace(t) }},
		{name: "Delete Task…", about: "Delete the task, its comments and its story, after asking.", keys: "d", rank: 90,
			run: func() { v.deleteTask(t) }},
	}
	return acts
}

// screenActions are what the board can do, whatever is chosen.
func (v *boardView) screenActions() []uiAction {
	a := v.app
	acts := []uiAction{
		{name: "New Task…", about: "Write a task into the column the cursor is in.", keys: "n", rank: 10,
			run: func() {
				state := board.Todo
				if !v.history {
					state = board.States[v.col]
				}
				v.editTask(nil, state, nil)
			}},
		{name: "Spaces…", about: "Switch to another space, or make, rename or delete one.", keys: "p", rank: 20, run: v.showSpaces},
		{name: "Next Space", about: "The space after this one.", keys: "]", rank: 22, when: func() bool { return len(v.spaces) > 1 },
			run: func() { v.cycleSpace(1) }},
		{name: "Previous Space", about: "The space before this one.", keys: "[", rank: 23, when: func() bool { return len(v.spaces) > 1 },
			run: func() { v.cycleSpace(-1) }},
		{name: "Toggle Card Size", about: "The cards compact - the title and counts - or in full: the description, the links and the latest comment.", keys: "v", rank: 30,
			run: func() {
				a.cfg.State.BoardFull = !a.cfg.State.BoardFull
				a.saveState()
			}},
		{name: "Toggle History", about: "Every task done, the newest first, or the board again.", keys: "y", rank: 32, run: v.toggleHistory},
		{name: "Filter", about: "Narrow the cards by typing: a title, a description, a link, a comment.", keys: "/", rank: 300, run: v.startFilter},
		{name: "Read Again", about: "Read the space's tasks from disk again; another unagit's changes come by themselves too.", keys: "R", rank: 310, run: v.reload},
	}
	return append(acts, a.listActions(nil)[1:]...)
}

func (v *boardView) toggleHistory() {
	v.history = !v.history
	v.updateHeader()
}

// change changes a task as it is on disk, then shows the board again with
// the cursor on it.
func (v *boardView) change(t board.Task, what func(*board.Task)) {
	if _, err := v.store.Change(t.Space, t.ID, func(m *board.Task) error { what(m); return nil }); err != nil {
		v.app.errorf("%v", err)
	}
	v.reload()
	v.focusTask(t.ID)
}

// topRank is a rank above every task of a state.
func (v *boardView) topRank(state string) string {
	first := ""
	for _, t := range v.tasks {
		if t.State == state && (first == "" || t.Rank < first) {
			first = t.Rank
		}
	}
	return board.Between("", first)
}

func (v *boardView) setState(t board.Task, state string) {
	rank := v.topRank(state)
	v.change(t, func(m *board.Task) {
		m.SetState(state, time.Now())
		m.Rank = rank
	})
	if state != board.Done || !v.history {
		v.focusTask(t.ID)
	}
	v.app.done(board.StateName(state) + ": " + t.Title)
}

// reorder moves a task one place up or down its column.
func (v *boardView) reorder(t board.Task, by int) {
	tasks := v.list(v.col)
	i := slices.IndexFunc(tasks, func(o board.Task) bool { return o.ID == t.ID })
	j := i + by
	if i < 0 || j < 0 || j >= len(tasks) {
		return
	}
	var lo, hi string
	if by < 0 {
		hi = tasks[j].Rank
		if j > 0 {
			lo = tasks[j-1].Rank
		}
	} else {
		lo = tasks[j].Rank
		if j+1 < len(tasks) {
			hi = tasks[j+1].Rank
		}
	}
	if hi != "" && lo >= hi {
		hi = ""
	}
	rank := board.Between(lo, hi)
	v.change(t, func(m *board.Task) { m.Rank = rank })
}

// editTask writes a new task into a state's column (t nil), or changes
// one's title and description; then, when set, runs once it is saved.
func (v *boardView) editTask(t *board.Task, state string, then func()) {
	a := v.app
	title, body, heading := "", "", "New Task · "+v.space.Name
	if t != nil {
		title, body, heading = t.Title, t.Body, "Edit Task"
	}
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Title", title, 0, nil, nil)
	area := addTextArea(form, "Description", body, 9)
	setPlaceholder(area, "What it is, what done means…")
	form.AddTextView("", "Markdown is understood. Ctrl-S saves, Esc closes without saving.", 0, 1, true, false)
	save := func() {
		name := strings.TrimSpace(form.GetFormItemByLabel("Title").(*tview.InputField).GetText())
		text := strings.TrimSpace(area.GetText())
		if name == "" {
			a.flash("a task needs a title")
			return
		}
		a.closeModal(pageForm)
		now := time.Now()
		if t != nil {
			if name == t.Title && text == t.Body {
				return
			}
			v.change(*t, func(m *board.Task) {
				m.Title, m.Body = name, text
				m.Tell(now, "edited")
			})
			if then != nil {
				then()
			}
			return
		}
		task := board.Task{ID: board.NewID(), Space: v.space.ID, Title: name, Body: text, State: state,
			Rank: v.topRank(state), Created: now, Moved: now}
		task.Tell(now, "created")
		if err := v.store.Add(task); err != nil {
			a.errorf("%v", err)
			return
		}
		v.history = false
		v.reload()
		v.focusTask(task.ID)
		a.done("New task: " + name)
	}
	form.AddButton("Save", save)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS {
			save()
			return nil
		}
		return ev
	})
	a.showFormModal(heading, form, 16)
}

// openLink opens the task's link, or lists them to choose one.
func (v *boardView) openLink(t board.Task) {
	a := v.app
	if len(t.Links) == 1 {
		a.openWeb(t.Links[0].URL)
		return
	}
	items := make([]pickItem, len(t.Links))
	for i, l := range t.Links {
		items[i] = pickItem{Label: esc(l.Name()), Sub: esc(l.URL), Data: l}
	}
	a.showPickerWith("Links · "+t.Title, items, pickerOptions{pack: true, enterHint: "open"}, func(it pickItem) {
		a.openWeb(it.Data.(board.Link).URL)
	})
}

// linkURL adds a link to a task: its address, and a name for it; then,
// when set, runs once it is linked.
func (v *boardView) linkURL(t board.Task, then func()) {
	a := v.app
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("URL", "", 0, nil, nil)
	form.AddInputField("Name", "", 0, nil, nil)
	form.AddTextView("", "The name is optional; without one the address is shown.", 0, 1, true, false)
	form.AddButton("Link", func() {
		url := strings.TrimSpace(form.GetFormItemByLabel("URL").(*tview.InputField).GetText())
		name := strings.TrimSpace(form.GetFormItemByLabel("Name").(*tview.InputField).GetText())
		if !strings.Contains(url, "://") {
			a.flash("an address starts with its scheme: https://…")
			return
		}
		a.closeModal(pageForm)
		v.addLink(t, board.Link{Kind: board.KindURL, Key: url, URL: url, Label: name})
		if then != nil {
			then()
		}
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Link · "+t.Title, form, 76, 7)
}

func (v *boardView) addLink(t board.Task, l board.Link) {
	now := time.Now()
	l.Added = now
	v.change(t, func(m *board.Task) {
		m.Links = append(m.Links, l)
		m.Tell(now, "linked "+l.Name())
	})
	v.app.done("Linked " + l.Name())
}

// pickColour gives a card a colour, or takes it away.
func (v *boardView) pickColour(t board.Task) {
	items := []pickItem{{Label: "none", Sub: "the state's frame", Data: ""}}
	start := 0
	for i, c := range board.Colours {
		swatch := tag(role("board.card."+c)) + "████" + tagEnd
		items = append(items, pickItem{Label: swatch + " " + c, Data: c})
		if c == t.Colour {
			start = i + 1
		}
	}
	v.app.showPickerWith("Colour · "+t.Title, items, pickerOptions{start: start, pack: true, enterHint: "colour"}, func(it pickItem) {
		colour := it.Data.(string)
		v.change(t, func(m *board.Task) { m.Colour = colour })
	})
}

func (v *boardView) deleteTask(t board.Task) {
	a := v.app
	a.confirmWith("Delete Task", fmt.Sprintf("Delete [::b]%s[::-]?\n\nIts comments and its story go with it.", esc(t.Title)), "Delete", nil, func() {
		if err := v.store.Delete(t.Space, t.ID); err != nil {
			a.errorf("%v", err)
			return
		}
		v.reload()
		a.done("Deleted " + t.Title)
	})
}

// ------------------------------------------------------------------ spaces

// useSpace opens a space and remembers it.
func (v *boardView) useSpace(id string) {
	v.app.cfg.State.BoardSpace = id
	v.app.saveState()
	v.at, v.top = [4]int{}, [4]int{}
	v.reload()
}

func (v *boardView) cycleSpace(by int) {
	i := slices.IndexFunc(v.spaces, func(s board.Space) bool { return s.ID == v.space.ID })
	n := len(v.spaces)
	v.useSpace(v.spaces[((i+by)%n+n)%n].ID)
	v.app.note(v.space.Name)
}

// showSpaces lists the spaces: Enter opens one, n makes one, r renames,
// d deletes, m moves the task under the cursor there.
func (v *boardView) showSpaces() {
	a := v.app
	items := make([]pickItem, len(v.spaces))
	start := 0
	for i, sp := range v.spaces {
		count := ""
		if tasks, err := v.store.Tasks(sp.ID); err == nil {
			count = counted(len(tasks), "task", "tasks")
		}
		items[i] = pickItem{Label: esc(sp.Name), Sub: count, Data: sp}
		if sp.ID == v.space.ID {
			start = i
		}
	}
	chosen, hasTask := v.chosen()
	keys := []pickKey{
		{keys: "n", hint: "new", name: "New Space…", about: "Make a space: a board of its own.", run: func(pickItem) { v.newSpace() }},
		{keys: "r", hint: "rename", name: "Rename Space…", about: "Give the space another name.", run: func(it pickItem) { v.renameSpace(it.Data.(board.Space)) }},
		{keys: "d", hint: "delete", name: "Delete Space…", about: "Delete the space, its tasks moved to another or deleted with it, after asking.",
			run: func(it pickItem) { v.deleteSpace(it.Data.(board.Space)) }},
		{keys: "m", hint: "move task here", name: "Move Task Here", about: "Take the task under the cursor on the board to this space.",
			when: func(it pickItem) bool { return hasTask && it.Data.(board.Space).ID != v.space.ID },
			run: func(it pickItem) {
				if hasTask {
					v.moveTask(chosen, it.Data.(board.Space))
				}
			}},
	}
	a.showPickerWith("Spaces", items, pickerOptions{start: start, pack: true, enterHint: "open", keys: keys}, func(it pickItem) {
		v.useSpace(it.Data.(board.Space).ID)
	})
}

func (v *boardView) newSpace() {
	v.app.askText("New Space", "Name", "", "Make", v.checkSpaceName(""), func(name string) {
		sp := board.Space{ID: board.NewID(), Name: name}
		if _, err := v.store.ChangeSpaces(func(spaces []board.Space) ([]board.Space, error) {
			return append(spaces, sp), nil
		}); err != nil {
			v.app.errorf("%v", err)
			return
		}
		v.useSpace(sp.ID)
		v.app.done("New space: " + name)
	})
}

func (v *boardView) renameSpace(sp board.Space) {
	v.app.askText("Rename Space", "Name", sp.Name, "Rename", v.checkSpaceName(sp.ID), func(name string) {
		if _, err := v.store.ChangeSpaces(func(spaces []board.Space) ([]board.Space, error) {
			for i := range spaces {
				if spaces[i].ID == sp.ID {
					spaces[i].Name = name
				}
			}
			// The default space is written down once it is renamed.
			if !slices.ContainsFunc(spaces, func(s board.Space) bool { return s.ID == sp.ID }) {
				spaces = append(spaces, board.Space{ID: sp.ID, Name: name})
			}
			return spaces, nil
		}); err != nil {
			v.app.errorf("%v", err)
			return
		}
		v.reload()
	})
}

// checkSpaceName refuses an empty name and one another space has.
func (v *boardView) checkSpaceName(self string) func(string) string {
	return func(name string) string {
		if name == "" {
			return "a space needs a name"
		}
		for _, sp := range v.spaces {
			if sp.ID != self && strings.EqualFold(sp.Name, name) {
				return "there is a space of that name"
			}
		}
		return ""
	}
}

// deleteSpace takes a space away after asking where its tasks go.
func (v *boardView) deleteSpace(sp board.Space) {
	a := v.app
	if len(v.spaces) == 1 {
		a.flash("the last space cannot go - rename it instead")
		return
	}
	remove := func(to string) {
		if err := v.store.RemoveSpace(sp.ID, to); err != nil {
			a.errorf("%v", err)
			return
		}
		if v.space.ID == sp.ID {
			a.cfg.State.BoardSpace = to
			a.saveState()
		}
		v.reload()
		a.done("Deleted the space " + sp.Name)
	}
	tasks, _ := v.store.Tasks(sp.ID)
	if len(tasks) == 0 {
		a.confirmWith("Delete Space", fmt.Sprintf("Delete the space [::b]%s[::-]? It has no tasks.", esc(sp.Name)), "Delete", nil, func() { remove("") })
		return
	}
	var items []pickItem
	for _, other := range v.spaces {
		if other.ID != sp.ID {
			items = append(items, pickItem{Label: "Move them to " + esc(other.Name), Data: other.ID})
		}
	}
	items = append(items, pickItem{Label: tag(colBad) + "Delete them with it" + tagEnd, Data: ""})
	a.showPickerWith(fmt.Sprintf("Delete %s · its %s", sp.Name, counted(len(tasks), "task", "tasks")), items, pickerOptions{pack: true, enterHint: "delete the space"}, func(it pickItem) {
		to := it.Data.(string)
		if to != "" {
			remove(to)
			return
		}
		a.confirmWith("Delete Space", fmt.Sprintf("Delete [::b]%s[::-] and its %s?", esc(sp.Name), counted(len(tasks), "task", "tasks")), "Delete", nil, func() { remove("") })
	})
}

// moveToSpace takes a task to another space, chosen from a list.
func (v *boardView) moveToSpace(t board.Task) {
	var items []pickItem
	for _, sp := range v.spaces {
		if sp.ID != t.Space {
			items = append(items, pickItem{Label: esc(sp.Name), Data: sp})
		}
	}
	v.app.showPickerWith("Move to Space · "+t.Title, items, pickerOptions{pack: true, enterHint: "move"}, func(it pickItem) {
		v.moveTask(t, it.Data.(board.Space))
	})
}

func (v *boardView) moveTask(t board.Task, to board.Space) {
	there, err := v.store.Tasks(to.ID)
	if err != nil {
		v.app.errorf("%v", err)
		return
	}
	first := ""
	for _, o := range there {
		if o.State == t.State && (first == "" || o.Rank < first) {
			first = o.Rank
		}
	}
	now := time.Now()
	if _, err := v.store.Change(t.Space, t.ID, func(m *board.Task) error {
		m.Space, m.Rank = to.ID, board.Between("", first)
		m.Tell(now, "moved from "+v.space.Name)
		return nil
	}); err != nil {
		v.app.errorf("%v", err)
		return
	}
	v.reload()
	v.app.done(fmt.Sprintf("Moved to %s: %s", to.Name, t.Title))
}

// ------------------------------------------------------------------ looking

// watchBoard reads the space again when another unagit wrote it, and
// takes off the board a task done long enough, while unagit runs.
func (a *App) watchBoard(stop <-chan struct{}) {
	tick := time.NewTicker(boardLook)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		a.tv.QueueUpdate(func() {
			v := a.board
			if v == nil || a.currentTab() != pageBoard {
				return
			}
			switch {
			case !v.store.Stamp(v.space.ID).Equal(v.stamp):
				v.reload()
			case time.Since(v.filled) > time.Minute:
				v.fill()
			default:
				return
			}
			go a.tv.QueueUpdateDraw(func() {})
		})
	}
}
