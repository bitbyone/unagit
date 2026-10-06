package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/session"
)

// PickSession asks which open directory to go to, for unagit cd. It draws on
// the terminal itself - tcell talks to /dev/tty, not to standard output - so
// cd --print stays usable in a command substitution. It looks like the rest of
// unagit: the same theme, frame and hints as the passphrase dialog. The second
// value is false when nothing was chosen.
func PickSession(open []session.Record) (session.Record, bool, error) {
	return pickRecords(nil, "Open in an editor", open)
}

// PickPlace is PickSession for unagit go: what is on disk rather than what
// is open.
func PickPlace(places []session.Record) (session.Record, bool, error) {
	return pickRecords(nil, "On disk", places)
}

// pickerStarted hands the tests the application, so they read the screen on
// its event loop rather than racing it.
var pickerStarted func(*tview.Application)

// pickSession is PickSession on a given screen; nil is the terminal.
func pickSession(screen tcell.Screen, open []session.Record) (session.Record, bool, error) {
	return pickRecords(screen, "Open in an editor", open)
}

// pickRecords is the picker both commands use. Like every list of unagit it
// opens on the list - j/k move, Enter goes there - and / starts a fuzzy
// filter, for the list of everything on disk that would not fit a screen.
func pickRecords(screen tcell.Screen, title string, records []session.Record) (session.Record, bool, error) {
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
	input := filterField(tview.NewInputField())
	filtering := false
	frame := tview.NewFlex().SetDirection(tview.FlexRow)
	box(frame.Box, title)
	hintPanel(frame.Box, func() string {
		return "Enter go there"
	}, 1, 0, 2, 2)

	// Each entry is two lines fitted to the width: what it is, then where. A
	// line that runs past the frame would lose the part that tells two
	// entries apart - the title's end, the directory's name.
	width := 0
	shown := records
	fill := func() {
		current := list.GetCurrentItem()
		list.Clear()
		for _, r := range shown {
			main, where := sessionLines(r, width)
			list.AddItem(main, where, 0, nil)
		}
		list.SetCurrentItem(current)
	}
	filter := func(query string) {
		shown = matchRecords(records, query)
		list.SetCurrentItem(0)
		fill()
	}
	input.SetChangedFunc(filter)
	layout := func() {
		frame.Clear()
		if filtering || input.GetText() != "" {
			frame.AddItem(input, 1, 0, filtering)
		}
		frame.AddItem(list, 0, 1, !filtering)
	}
	layout()

	const maxWidth = 120
	app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		sw, _ := s.Size()
		// The frame is as wide as the terminal, up to maxWidth, less its
		// border and padding.
		if inner := min(sw, maxWidth) - 6; inner != width {
			width = inner
			fill()
		}
		return false
	})

	var chosen session.Record
	picked := false
	pick := func() {
		if i := list.GetCurrentItem(); i >= 0 && i < len(shown) {
			chosen, picked = shown[i], true
		}
		app.Stop()
	}
	setFiltering := func(on bool) {
		filtering = on
		layout()
		if on {
			app.SetFocus(input)
		} else {
			app.SetFocus(list)
		}
	}
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			setFiltering(false)
			return nil
		case tcell.KeyEnter:
			pick()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn:
			if h := list.InputHandler(); h != nil {
				h(ev, func(tview.Primitive) {})
			}
			return nil
		}
		return ev
	})
	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch {
		case ev.Key() == tcell.KeyEsc, ev.Rune() == 'q':
			app.Stop()
			return nil
		case ev.Key() == tcell.KeyEnter:
			pick()
			return nil
		case ev.Rune() == '/':
			setFiltering(true)
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

	// Two lines an entry, the filter, the hint, the frame and its padding.
	height := 2*len(records) + 6
	root := modalFixed(frame, maxWidth, height)
	if err := app.SetRoot(root, true).SetFocus(list).Run(); err != nil {
		return session.Record{}, false, err
	}
	return chosen, picked, nil
}

// matchRecords keeps the records the query fuzzily matches, best first;
// an empty query keeps them all in their order.
func matchRecords(records []session.Record, query string) []session.Record {
	if strings.TrimSpace(query) == "" {
		return records
	}
	type hit struct {
		r     session.Record
		score int
	}
	var hits []hit
	for _, r := range records {
		if score, ok := fuzzy.Match(query, recordSearch(r)); ok {
			hits = append(hits, hit{r, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]session.Record, len(hits))
	for i, h := range hits {
		out[i] = h.r
	}
	return out
}

// recordSearch is what a search is matched against: the repository, the
// merge request, its title or branch, the kind of place and its folder.
func recordSearch(r session.Record) string {
	text := r.Project + " " + r.Mode + " " + r.Title + " " + filepath.Base(r.Dir)
	if r.IID > 0 {
		text += fmt.Sprintf(" !%d", r.IID)
	}
	return text
}

// MatchPlaces narrows the places to those that contain every word of the
// query, ignoring case. The command line is stricter than the list's /:
// there a loose match is seen and passed over, here it would be entered.
func MatchPlaces(places []session.Record, query string) []session.Record {
	words := strings.Fields(strings.ToLower(query))
	var out []session.Record
	for _, r := range places {
		hay := strings.ToLower(recordSearch(r) + " " + r.Server)
		all := true
		for _, w := range words {
			all = all && strings.Contains(hay, w)
		}
		if all {
			out = append(out, r)
		}
	}
	return out
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
