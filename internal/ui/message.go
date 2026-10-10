package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// severity is what kind of thing a message says, and so how it is headed
// and coloured.
type severity int

const (
	sevInfo severity = iota
	sevSuccess
	sevWarning
	sevError
)

func (s severity) colour() tcell.Color {
	switch s {
	case sevSuccess:
		return colOn
	case sevWarning:
		return colWarn
	case sevError:
		return colBad
	}
	return colAccent
}

// heading is the first line of a message box: a mark and a word, in the
// severity's colour, so what kind of message it is reads before the message.
func (s severity) heading() string {
	switch s {
	case sevSuccess:
		return glyphCheck + " Success"
	case sevWarning:
		return "! Warning"
	case sevError:
		return glyphCross + " Error"
	}
	return "i Info"
}

// showMessage puts a warning or an error over whatever is in front, in a
// small box of its own, filled like a confirmation so it stands off what is
// under it. It stays until Esc (or Enter) closes it, back to what was there,
// and holds every other key, so that nothing typed in the meantime lands
// anywhere unseen. A second message replaces the first. A note or a success
// never comes here: it is a passing word at the right of the status line or
// of the dialog's bottom edge (say).
func (a *App) showMessage(msg string, sev severity) {
	if a.pages.HasPage(pageMessage) {
		a.pages.RemovePage(pageMessage)
	}
	heading := tview.NewTextView().SetDynamicColors(true).
		SetText(tag(sev.colour()) + "[::b]" + sev.heading() + "[::-]" + tagEnd)
	text := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	text.SetText(tag(colText) + tview.Escape(msg) + tagEnd)
	footer := tview.NewTextView().SetTextColor(colMuted).SetText("Esc close")
	for _, v := range []*tview.TextView{heading, text, footer} {
		v.SetBackgroundColor(colSurface)
	}
	block := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(heading, 1, 0, false).
		AddItem(text, 0, 1, true).
		AddItem(footer, 1, 0, false)
	box(block.Box, "").SetBorderPadding(0, 0, 2, 2)
	block.SetBorderColor(colBorderFocus)
	block.SetBackgroundColor(colSurface)
	// A Flex draws nothing of its own; the dialog under it would show
	// through the padding.
	fill := tcell.StyleDefault.Background(colSurface)
	block.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		for row := y + 1; row < y+h-1; row++ {
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, row, ' ', nil, fill)
			}
		}
		return x + 3, y + 1, max(0, w-6), max(0, h-2)
	})
	text.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyCtrlC:
			return ev
		case ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter:
			a.closeModal(pageMessage)
			// Said over a bare screen after a list closed to act: back to
			// the list, as a refusal should leave it (back.go).
			if back := a.peekBack(); back != nil && !a.modalOpen() {
				back()
			}
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
