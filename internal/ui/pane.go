package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// pane is a table with a fuzzy filter line on top.
//
// It has two modes, like vim: in NORMAL mode single letters are commands, in
// FILTER mode (entered with "/") typing narrows the list while the arrow keys
// and Enter still drive the table.
type pane struct {
	app    *App
	root   *tview.Flex
	table  *tview.Table
	filter *tview.InputField
	header *tview.TextView

	filtering bool
	query     string

	onQuery  func(string)                          // rebuild rows for a new query
	onKey    func(*tcell.EventKey) *tcell.EventKey // NORMAL mode commands
	onEnter  func()                                // Enter on a row
	headline func() string                         // header text
	reload   func()                                // rebuild rows from the current data
}

func (a *App) newPane(title string) *pane {
	p := &pane{app: a}

	p.header = tview.NewTextView().SetDynamicColors(true)

	p.filter = tview.NewInputField().
		SetLabel("/ ").
		SetFieldBackgroundColor(tcell.ColorDefault)
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
	p.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkCyan).Foreground(tcell.ColorWhite).Bold(true))
	p.table.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)

	p.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.header, 1, 0, false).
		AddItem(p.filter, 1, 0, false).
		AddItem(p.table, 0, 1, true)

	p.filter.SetInputCapture(p.filterKeys)
	p.table.SetInputCapture(p.tableKeys)
	p.table.SetSelectedFunc(func(int, int) {
		if p.onEnter != nil {
			p.onEnter()
		}
	})
	return p
}

// focusTable leaves filter mode without clearing the query.
func (p *pane) focusTable() {
	p.filtering = false
	p.app.tv.SetFocus(p.table)
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

func (p *pane) updateHeader() {
	text := ""
	if p.headline != nil {
		text = p.headline()
	}
	mode := "[darkcyan]NORMAL[-]"
	if p.filtering {
		mode = "[yellow]FILTER[-]"
	}
	p.header.SetText(" " + mode + "  " + text)
}

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
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		p.forwardToTable(ev)
		return nil
	case tcell.KeyCtrlN:
		p.forwardToTable(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
		return nil
	case tcell.KeyCtrlP:
		p.forwardToTable(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
		return nil
	case tcell.KeyTab, tcell.KeyBacktab:
		p.focusTable()
		return ev
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
	case tcell.KeyEsc:
		if p.query != "" {
			p.clearFilter()
			if p.onQuery != nil {
				p.onQuery("")
			}
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
		case 'g':
			p.table.Select(1, 0)
			return nil
		case 'G':
			p.table.Select(p.table.GetRowCount()-1, 0)
			return nil
		}
	}
	if p.onKey != nil {
		return p.onKey(ev)
	}
	return ev
}

// selectedIndex maps the highlighted table row onto the filtered data slice.
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

// setHeaders writes the table's header row.
func (p *pane) setHeaders(titles ...string) {
	for c, t := range titles {
		cell := tview.NewTableCell(t).
			SetTextColor(tcell.ColorGray).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold)
		if c == len(titles)-1 {
			cell.SetExpansion(1)
		}
		p.table.SetCell(0, c, cell)
	}
}
