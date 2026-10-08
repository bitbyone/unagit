package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// keptMarkup is a stretch of a row that keeps its own colours when the row is
// painted over: where it starts in the row, its markup as drawn on the
// selection band, and, for a row with a band of its own (a marked one), its
// markup as drawn on that.
type keptMarkup struct {
	x      int
	markup string
	width  int
	banded string
}

// rowBand is the background a row of a list has of its own: none, a
// marked row's, or that of a row with something open in it. The cursor's
// band is over either, and a mark's over what is open.
type rowBand int

const (
	bandNone rowBand = iota
	bandMarked
	bandOpen
)

// paint gives a row's cell its background.
func (b rowBand) paint(cell *tview.TableCell) {
	switch b {
	case bandMarked:
		cell.SetBackgroundColor(colMarked).SetSelectedStyle(styleMarkedSelected)
	case bandOpen:
		cell.SetBackgroundColor(colOpen)
	}
}

// keptTable is a table whose painted rows keep some of their colours. tview
// paints a cell's background over everything in it - the selected row in the
// selection's ink and band, a row with a background of its own in that -
// which turns a tag's pill into plain text between two coloured ends; the
// stretches kept here are drawn again over the band once the table has drawn.
// Any row given a background must keep its pills here, or they lose their
// fill.
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
	selected, _ := k.GetSelection()
	x, y, _, h := k.GetInnerRect()
	offset, _ := k.GetOffset()
	for row, marks := range k.kept {
		if row < k.fixed+offset {
			continue
		}
		line := y + row - offset
		if line >= y+h {
			continue
		}
		for _, m := range marks {
			markup := m.banded
			if row == selected {
				markup = m.markup
			}
			if markup != "" {
				tview.Print(screen, markup, x+m.x, line, m.width, tview.AlignLeft, colText)
			}
		}
	}
}
