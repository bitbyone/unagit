package ui

import (
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
	width         int // inner width of the table, for column layout
	detailSeq     int // guards against a stale async detail arriving late
	detailShown   bool
	detailFocused bool
	query         string

	onQuery  func(string)                          // rebuild rows for a new query
	onKey    func(*tcell.EventKey) *tcell.EventKey // extra NORMAL mode commands
	onEnter  func()                                // Enter: load the detail column
	onOpen   func()                                // Ctrl-O: clone/update and open the editor
	headline func() string                         // header text
	reload   func()                                // rebuild rows from the current data
}

func (a *App) newPane(title string) *pane {
	p := &pane{app: a}

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
		AddItem(p.header, 1, 0, false).
		AddItem(p.filter, 1, 0, false).
		AddItem(p.body, 0, 1, true)

	p.filter.SetInputCapture(p.filterKeys)
	p.table.SetInputCapture(p.tableKeys)
	p.detail.SetInputCapture(p.detailKeys)
	p.table.SetSelectedFunc(func(int, int) {
		if p.onEnter != nil {
			p.onEnter()
		}
	})
	return p
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
		if p.onEnter != nil {
			p.onEnter()
		}
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
	return ev
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
