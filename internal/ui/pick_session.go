package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/session"
)

// PickSession asks which open directory to go to, for unagit cd. It draws on
// the terminal itself - tcell talks to /dev/tty, not to standard output - so
// cd --print stays usable in a command substitution. It looks like the rest of
// unagit: the same theme, frame and hints as the passphrase dialog. The second
// value is false when nothing was chosen.
func PickSession(open []session.Record) (session.Record, bool, error) {
	return pickSession(nil, open)
}

// pickerStarted hands the tests the application, so they read the screen on
// its event loop rather than racing it.
var pickerStarted func(*tview.Application)

// pickSession is PickSession on a given screen; nil is the terminal.
func pickSession(screen tcell.Screen, open []session.Record) (session.Record, bool, error) {
	applyTheme()
	app := tview.NewApplication()
	if screen != nil {
		app.SetScreen(screen)
	}
	if pickerStarted != nil {
		pickerStarted(app)
	}

	list := tview.NewList().ShowSecondaryText(true)
	list.SetHighlightFullLine(true).
		SetMainTextColor(colText).
		SetSecondaryTextColor(colMuted).
		SetSelectedStyle(styleSelected)
	box(list.Box, "Open in an editor")
	hintPanel(list.Box, func() string { return "j/k move · Enter go there · Esc cancel" }, 1, 0, 2, 2)

	// Each entry is two lines fitted to the width: what it is, then where. A
	// line that runs past the frame would lose the part that tells two
	// entries apart - the title's end, the directory's name.
	width := 0
	fill := func(w int) {
		current := list.GetCurrentItem()
		list.Clear()
		for _, r := range open {
			main, where := sessionLines(r, w)
			list.AddItem(main, where, 0, nil)
		}
		list.SetCurrentItem(current)
	}
	const maxWidth = 120
	app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		sw, _ := s.Size()
		// The frame is as wide as the terminal, up to maxWidth, less its
		// border and padding.
		if inner := min(sw, maxWidth) - 6; inner != width {
			width = inner
			fill(inner)
		}
		return false
	})

	var chosen session.Record
	picked := false
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyEsc, ev.Rune() == 'q':
			app.Stop()
			return nil
		case ev.Key() == tcell.KeyEnter:
			if i := list.GetCurrentItem(); i >= 0 && i < len(open) {
				chosen, picked = open[i], true
			}
			app.Stop()
			return nil
		case ev.Rune() == 'j', ev.Key() == tcell.KeyCtrlN:
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case ev.Rune() == 'k', ev.Key() == tcell.KeyCtrlP:
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		case ev.Key() == tcell.KeyRune:
			// Letters are shortcuts in a tview List; none are used here.
			return nil
		}
		return ev
	})

	// Two lines an entry, the hint, the frame and its padding.
	height := 2*len(open) + 5
	root := modalFixed(list, maxWidth, height)
	if err := app.SetRoot(root, true).SetFocus(list).Run(); err != nil {
		return session.Record{}, false, err
	}
	return chosen, picked, nil
}

// sessionLines is an entry of the picker in width columns: the mode, the
// repository and merge request, and its title; below, the directory.
func sessionLines(r session.Record, width int) (string, string) {
	mode := fmt.Sprintf("%-*s", modeWidth, r.Mode)
	modeColour := colMuted
	switch r.Mode {
	case session.ModeReview:
		modeColour = colWarn
	case session.ModeBranch:
		modeColour = colOn
	}
	what := r.Project
	room := width - 1 - modeWidth - len([]rune(what))
	main := " " + tag(modeColour) + mode + tagEnd + tview.Escape(what)
	if r.IID > 0 {
		iid := fmt.Sprintf(" !%d", r.IID)
		main += tag(colAccent) + iid + tagEnd
		room -= len(iid)
	}
	if r.Title != "" && room > 12 {
		main += tag(colDim) + "   " + tview.Escape(ellipsis(r.Title, room-3)) + tagEnd
	}
	// The directory lines up under the repository.
	indent := 1 + modeWidth
	where := fmt.Sprintf("%*s", indent, "") + tview.Escape(shortPath(r.Dir, max(12, width-indent)))
	return main, where
}

// modeWidth fits the longest mode, "repository", and a gap.
const modeWidth = len(session.ModeRepository) + 2

// ellipsis cuts text to n characters, marking the cut.
func ellipsis(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:max(0, n-1)]) + "…"
	}
	return text
}
