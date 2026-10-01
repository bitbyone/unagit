package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// keptMarkup is a stretch of a row that keeps its own colours when the row is
// selected: where it starts in the row, and its markup as drawn on the band.
type keptMarkup struct {
	x      int
	markup string
	width  int
}

// keptTable is a table whose selected row keeps some of its colours. tview
// paints the whole selected row in the selection's ink and band, which turned
// a tag's pill into plain text between two coloured ends; the stretches kept
// here are drawn again over the band once the table has drawn.
type keptTable struct {
	*tview.Table
	fixed int
	kept  map[int][]keptMarkup
}

func newKeptTable(t *tview.Table, fixed int) *keptTable {
	return &keptTable{Table: t, fixed: fixed, kept: map[int][]keptMarkup{}}
}

// reset forgets every row's stretches, for a table about to be refilled.
func (k *keptTable) reset() { k.kept = map[int][]keptMarkup{} }

// keep adds a stretch to a row, at x cells into it.
func (k *keptTable) keep(row int, m keptMarkup) {
	if m.width > 0 {
		k.kept[row] = append(k.kept[row], m)
	}
}

func (k *keptTable) Draw(screen tcell.Screen) {
	k.Table.Draw(screen)
	row, _ := k.GetSelection()
	marks := k.kept[row]
	if len(marks) == 0 {
		return
	}
	x, y, _, h := k.GetInnerRect()
	offset, _ := k.GetOffset()
	if row < k.fixed+offset {
		return
	}
	line := y + k.fixed + row - k.fixed - offset
	if line >= y+h {
		return
	}
	for _, m := range marks {
		tview.Print(screen, m.markup, x+m.x, line, m.width, tview.AlignLeft, colText)
	}
}
