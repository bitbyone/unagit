package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// showMessage puts a message over the dialog in front, in a small box of its
// own. A warning or an error stays until Esc (or Enter) closes it, back to the
// dialog as it was, and holds every other key, so that nothing typed in the
// meantime lands in the dialog unseen. A note - what was done, what is under
// way - asks for nothing: the next key closes it and still does what it does
// in the dialog. A second message replaces the first.
func (a *App) showMessage(msg string, colour tcell.Color) {
	if a.pages.HasPage(pageMessage) {
		a.pages.RemovePage(pageMessage)
	}
	text := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	text.SetText(tag(colour) + tview.Escape(msg) + tagEnd)
	note := colour == colMuted
	closing := "Esc close"
	if note {
		closing = "any key closes"
	}
	footer := tview.NewTextView().SetTextColor(colDim).SetText(closing)
	block := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(text, 0, 1, true).
		AddItem(footer, 1, 0, false)
	box(block.Box, "").SetBorderPadding(1, 0, 2, 2)
	block.SetBorderColor(colBorderFocus)
	// A Flex draws nothing of its own; the dialog under it would show
	// through the padding.
	block.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		for row := y + 1; row < y+h-1; row++ {
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, row, ' ', nil, tcell.StyleDefault)
			}
		}
		return x + 3, y + 2, max(0, w-6), max(0, h-3)
	})
	text.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyCtrlC:
			return ev
		case ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter:
			a.closeModal(pageMessage)
		case note:
			a.closeModal(pageMessage)
			// In through the pages, as a key comes, so that the dialog's own
			// handling - a form's modes - has it before the widget does.
			a.pages.InputHandler()(ev, func(p tview.Primitive) { a.tv.SetFocus(p) })
		}
		return nil
	})
	// As wide as the message wants, up to a comfortable line; as tall as it
	// wraps to there.
	width := min(max(len([]rune(msg))+6, 30), 72)
	lines := len(tview.WordWrap(msg, width-6))
	a.pages.AddPage(pageMessage, modalFixed(block, width, lines+4), true, true)
	a.tv.SetFocus(text)
}
