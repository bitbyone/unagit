package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A block list is a column of framed blocks, one of them lit, for a view that
// is several things side by side - the repositories of a grouped worktree -
// rather than one long text. Each block has a frame of its own, so the eye
// finds where one thing ends and the next begins, and the lit one has the
// focused border, so the keys' target is never in doubt.

// textBlock is one framed block: a title on its top border and its rows.
type textBlock struct {
	title string
	rows  []string // tview markup, cut at the frame
}

// blockList draws blocks under each other and keeps the lit one in sight.
// Until it has blocks it says what it is waiting for, in the middle.
type blockList struct {
	*tview.Box
	blocks  []textBlock
	lit     int
	offset  int
	waiting string
}

func newBlockList() *blockList { return &blockList{Box: tview.NewBox()} }

// labelWidth is the column the values of kvRow start at.
const labelWidth = 13

// kvRow is a row of a block: a dim label, and the value in a column of its own.
func kvRow(label, value string) string {
	return fmt.Sprintf("%s%-*s%s%s", tag(colDim), labelWidth, label, tagEnd, value)
}

// moreRow is a row under a kvRow, its value in the same column.
func moreRow(value string) string { return strings.Repeat(" ", labelWidth) + value }

func (b *blockList) Draw(screen tcell.Screen) {
	b.Box.DrawForSubclass(screen, b)
	x, y, w, h := b.GetInnerRect()
	if w < 6 || h < 1 {
		return
	}
	if len(b.blocks) == 0 {
		tview.Print(screen, b.waiting, x, y+h/3, w, tview.AlignCenter, colMuted)
		return
	}
	// Where each block starts, one blank row between two.
	starts := make([]int, len(b.blocks))
	line := 0
	for i, bl := range b.blocks {
		starts[i] = line
		line += len(bl.rows) + 3
	}
	lit := min(max(b.lit, 0), len(b.blocks)-1)
	top, bottom := starts[lit], starts[lit]+len(b.blocks[lit].rows)+2
	switch {
	case top < b.offset:
		b.offset = top
	case bottom > b.offset+h:
		b.offset = min(top, bottom-h)
	}
	b.offset = max(0, min(b.offset, line-1-h))

	for i, bl := range b.blocks {
		border, title := colBorder, colMuted
		if i == lit {
			border, title = colBorderFocus, colBorderFocus
		}
		row := y + starts[i] - b.offset
		put := func(dy int, draw func(int)) {
			if at := row + dy; at >= y && at < y+h {
				draw(at)
			}
		}
		edge := tcell.StyleDefault.Foreground(border)
		put(0, func(at int) {
			screen.SetContent(x, at, '╭', nil, edge)
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, at, '─', nil, edge)
			}
			screen.SetContent(x+w-1, at, '╮', nil, edge)
			name := bl.title
			if i == lit {
				name = "[::b]" + name + "[::-]"
			}
			tview.Print(screen, " "+name+" ", x+2, at, w-4, tview.AlignLeft, title)
		})
		for j, text := range bl.rows {
			put(j+1, func(at int) {
				screen.SetContent(x, at, '│', nil, edge)
				screen.SetContent(x+w-1, at, '│', nil, edge)
				tview.Print(screen, text, x+2, at, w-4, tview.AlignLeft, colText)
			})
		}
		put(len(bl.rows)+1, func(at int) {
			screen.SetContent(x, at, '╰', nil, edge)
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, at, '─', nil, edge)
			}
			screen.SetContent(x+w-1, at, '╯', nil, edge)
		})
	}
}
