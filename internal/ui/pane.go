package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// pane is a table with a fuzzy filter line on top and a detail column that
// slides in on the right.
//
// It has two modes, like vim: in NORMAL mode single letters are commands, in
// FILTER mode (entered with "/") typing narrows the list while the arrow keys
// and Enter still drive the table.
type pane struct {
	app    *App
	root   *tview.Flex
	body   *tview.Flex
	table  *tview.Table
	filter *tview.InputField
	header *tview.TextView
	detail *tview.TextView

	filtering     bool
	width         int    // inner width of the table, for column layout
	lastQuery     string // the query the rows were last drawn for
	detailSeq     int    // guards against a stale async detail arriving late
	detailShown   bool
	detailFocused bool
	query         string

	onQuery  func(string)                          // rebuild rows for a new query
	onKey    func(*tcell.EventKey) *tcell.EventKey // extra NORMAL mode commands
	onDetail func(idx int, focus bool)             // fill the detail column for a row
	onOpen   func()                                // Ctrl-O: clone/update and open the editor
	headline func() string                         // header text
	reload   func()                                // rebuild rows from the current data

	// detailFor is the data index the detail column currently shows, and
	// debounce delays following the cursor so holding j does not fire a
	// request per row.
	detailFor int
	debounce  *time.Timer
	debounceN int
}

// detailDebounce is how long the selection has to settle before the detail
// column follows it.
const detailDebounce = 300 * time.Millisecond

func (a *App) newPane(title string) *pane {
	p := &pane{app: a, detailFor: -1}

	p.header = tview.NewTextView().SetDynamicColors(true)

	p.filter = tview.NewInputField().
		SetLabel(" / ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(colText).
		SetLabelColor(colAccent)
	p.filter.SetChangedFunc(func(text string) {
		p.query = text
		if p.onQuery != nil {
			p.onQuery(text)
		}
	})

	p.table = tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetSeparator(' ')
	p.table.SetSelectedStyle(styleSelected)
	box(p.table.Box, title)
	// Track the usable width so the rows can be laid out to fit. The handler
	// is given the outer rect and has to return the content rect, which for a
	// bordered box without padding is one cell in on every side.
	p.table.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		if inner := w - 2; inner != p.width {
			p.width = inner
			go p.app.tv.QueueUpdateDraw(func() {
				if p.reload != nil {
					p.reload()
				}
			})
		}
		return x + 1, y + 1, w - 2, h - 2
	})

	p.detail = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)
	p.detail.SetTextColor(colText)
	box(p.detail.Box, "Details").SetBorderPadding(0, 0, 1, 1)

	p.body = tview.NewFlex().AddItem(p.table, 0, 1, true)

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.filter, 1, 0, false).
		AddItem(p.body, 0, 1, true).
		AddItem(p.header, 1, 0, false)

	p.filter.SetInputCapture(p.filterKeys)
	p.table.SetInputCapture(p.tableKeys)
	p.detail.SetInputCapture(p.detailKeys)
	p.table.SetSelectedFunc(func(int, int) { p.enter() })
	p.table.SetSelectionChangedFunc(func(int, int) { p.followSelection() })
	return p
}

// enter loads the detail column for the selected row and moves into it.
func (p *pane) enter() {
	i := p.selectedIndex()
	if i < 0 || p.onDetail == nil {
		return
	}
	p.stopDebounce()
	p.detailFor = i
	p.onDetail(i, true)
}

// followSelection keeps an open detail column in step with the cursor. It does
// nothing while the column is closed, and waits for the cursor to settle so
// that scrolling through a list does not fire a request per row.
func (p *pane) followSelection() {
	if !p.detailShown || p.onDetail == nil {
		return
	}
	if i := p.selectedIndex(); i < 0 || i == p.detailFor {
		return
	}
	p.stopDebounce()
	p.debounceN++
	seq := p.debounceN
	p.debounce = time.AfterFunc(detailDebounce, func() {
		p.app.tv.QueueUpdateDraw(func() {
			// A newer move, a closed column or an Enter in the meantime wins.
			if seq != p.debounceN || !p.detailShown {
				return
			}
			i := p.selectedIndex()
			if i < 0 || i == p.detailFor {
				return
			}
			p.detailFor = i
			p.onDetail(i, false)
		})
	})
}

func (p *pane) stopDebounce() {
	p.debounceN++
	if p.debounce != nil {
		p.debounce.Stop()
		p.debounce = nil
	}
}

// focusTarget is the primitive that should receive focus when the tab is shown.
func (p *pane) focusTarget() tview.Primitive {
	if p.detailShown && p.detailFocused {
		return p.detail
	}
	if p.filtering {
		return p.filter
	}
	return p.table
}

// ------------------------------------------------------------------ focus

func (p *pane) focusTable() {
	p.filtering = false
	p.detailFocused = false
	focusBox(p.table.Box, true)
	focusBox(p.detail.Box, false)
	p.app.tv.SetFocus(p.table)
	p.updateHeader()
}

func (p *pane) focusDetail() {
	if !p.detailShown {
		return
	}
	p.filtering = false
	p.detailFocused = true
	focusBox(p.table.Box, false)
	focusBox(p.detail.Box, true)
	p.app.tv.SetFocus(p.detail)
	p.updateHeader()
}

func (p *pane) startFilter() {
	p.filtering = true
	p.app.tv.SetFocus(p.filter)
	p.updateHeader()
}

func (p *pane) clearFilter() {
	p.filter.SetText("")
	p.query = ""
	p.focusTable()
}

// ----------------------------------------------------------- detail column

// openDetail reveals the right hand column, taking the focus only when asked:
// following the cursor must leave the focus in the list.
func (p *pane) openDetail(title, text string, focus bool) {
	if focus || !p.detailShown {
		p.showDetail(title, text)
		if !focus {
			p.focusTable()
		}
		return
	}
	p.setDetail(title, text)
}

// showDetail reveals the right hand column and moves focus into it.
func (p *pane) showDetail(title, text string) {
	if !p.detailShown {
		p.body.AddItem(p.detail, 0, 1, false)
		p.detailShown = true
	}
	p.detail.SetTitle(" " + title + " ")
	p.detail.SetText(text)
	p.detail.ScrollToBeginning()
	p.focusDetail()
}

// setDetail replaces the text without touching focus, for async updates.
func (p *pane) setDetail(title, text string) {
	if !p.detailShown {
		return
	}
	p.detail.SetTitle(" " + title + " ")
	p.detail.SetText(text)
	p.detail.ScrollToBeginning()
}

func (p *pane) hideDetail() {
	if !p.detailShown {
		return
	}
	p.stopDebounce()
	p.detailFor = -1
	p.body.RemoveItem(p.detail)
	p.detailShown = false
	p.focusTable()
}

// ------------------------------------------------------------------ header

func (p *pane) updateHeader() {
	text := ""
	if p.headline != nil {
		text = p.headline()
	}
	mode := tag(colMuted) + "NORMAL" + tagEnd
	switch {
	case p.filtering:
		mode = tag(colWarn) + "FILTER" + tagEnd
	case p.detailFocused:
		mode = tag(colAccent) + "DETAIL" + tagEnd
	}
	p.header.SetText(" " + mode + "  " + text)
}

// ------------------------------------------------------------------- keys

// filterKeys forwards navigation keys to the table while typing.
func (p *pane) filterKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEsc:
		// Leave filter mode but keep the narrowed list, so the commands can
		// act on what was just filtered. A second Esc clears the query.
		p.focusTable()
		return nil
	case tcell.KeyEnter:
		p.focusTable()
		p.enter()
		return nil
	case tcell.KeyCtrlO:
		if p.onOpen != nil {
			p.onOpen()
		}
		return nil
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		p.forwardToTable(ev)
		return nil
	case tcell.KeyCtrlN:
		p.forwardToTable(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
		return nil
	case tcell.KeyCtrlP:
		p.forwardToTable(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
		return nil
	}
	return ev
}

func (p *pane) forwardToTable(ev *tcell.EventKey) {
	if handler := p.table.InputHandler(); handler != nil {
		handler(ev, func(tview.Primitive) {})
	}
}

// tableKeys implements NORMAL mode.
func (p *pane) tableKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyCtrlO:
		if p.onOpen != nil {
			p.onOpen()
		}
		return nil
	case tcell.KeyEsc:
		switch {
		case p.query != "":
			p.clearFilter()
			if p.onQuery != nil {
				p.onQuery("")
			}
		case p.detailShown:
			p.hideDetail()
		}
		return nil
	case tcell.KeyRight:
		if p.detailShown {
			p.focusDetail()
			return nil
		}
	case tcell.KeyRune:
		switch ev.Rune() {
		case '/':
			p.startFilter()
			return nil
		case 'q':
			p.app.tv.Stop()
			return nil
		case '?':
			p.app.showHelp()
			return nil
		case 'j':
			p.forwardToTable(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
			return nil
		case 'k':
			p.forwardToTable(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
			return nil
		case 'l':
			if p.detailShown {
				p.focusDetail()
				return nil
			}
		case 'g':
			p.table.Select(1, 0)
			return nil
		case 'G':
			p.table.Select(p.table.GetRowCount()-1, 0)
			return nil
		}
		if p.app.tabKey(ev.Rune()) {
			return nil
		}
	}
	if p.onKey != nil {
		return p.onKey(ev)
	}
	return ev
}

// detailKeys handles the right hand column. Scrolling itself (j/k/g/G/Ctrl-F/
// Ctrl-B/arrows) is already implemented by tview's TextView.
func (p *pane) detailKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyLeft:
		p.focusTable()
		return nil
	case tcell.KeyCtrlO:
		if p.onOpen != nil {
			p.onOpen()
		}
		return nil
	case tcell.KeyCtrlR:
		if p.onKey != nil {
			return p.onKey(ev)
		}
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'h':
			p.focusTable()
			return nil
		case 'q':
			p.app.tv.Stop()
			return nil
		case '?':
			p.app.showHelp()
			return nil
		case '/':
			p.startFilter()
			return nil
		}
		if p.app.tabKey(ev.Rune()) {
			return nil
		}
	}
	// The row commands work while reading the detail too.
	if p.onKey != nil {
		return p.onKey(ev)
	}
	return ev
}

// selectRow puts the cursor back where it was after the rows were rebuilt.
//
// A redraw happens for all sorts of reasons - a resize, the detail column
// opening beside the table, a refresh - and none of them should move the
// cursor. A new filter should, though: there the best match is what you want
// to land on.
func (p *pane) selectRow(previous, first int) {
	if first <= 0 {
		p.lastQuery = p.query
		return
	}
	if previous >= 0 && p.query == p.lastQuery {
		for row := 1; row < p.table.GetRowCount(); row++ {
			cell := p.table.GetCell(row, 0)
			if cell == nil {
				continue
			}
			if i, ok := cell.GetReference().(int); ok && i == previous {
				p.lastQuery = p.query
				p.table.Select(row, 0)
				return
			}
		}
	}
	p.lastQuery = p.query
	p.table.Select(first, 0)
}

// selectedIndex maps the highlighted table row onto the underlying data slice.
// It returns -1 when the list is empty.
func (p *pane) selectedIndex() int {
	row, _ := p.table.GetSelection()
	if row < 1 || row >= p.table.GetRowCount() {
		return -1
	}
	ref := p.table.GetCell(row, 0).GetReference()
	if i, ok := ref.(int); ok {
		return i
	}
	return -1
}

// setHeaders writes the table's header row. A trailing filler column makes the
// selection band span the whole row instead of stopping after the last word.
func (p *pane) setHeaders(titles ...string) {
	for c, t := range titles {
		cell := tview.NewTableCell(t).
			SetTextColor(colDim).
			SetSelectable(false)
		p.table.SetCell(0, c, cell)
	}
	p.table.SetCell(0, len(titles), tview.NewTableCell("").SetSelectable(false).SetExpansion(1))
}

// fill adds the trailing filler cell to a data row.
func (p *pane) fill(row, column int) {
	p.table.SetCell(row, column, tview.NewTableCell("").SetExpansion(1))
}

// contentWidth is the width available for the rows, once the border is gone.
func (p *pane) contentWidth() int {
	if p.width > 0 {
		return p.width
	}
	return 80
}

// trunc shortens s to at most n cells, marking the cut with an ellipsis.
func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
