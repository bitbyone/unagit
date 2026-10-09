package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
)

// toggleItem is one row of a multiple choice.
type toggleItem struct {
	// Label is drawn as it is, marked up; Search is what / matches against.
	Label  string
	Search string
	Data   any
}

// toggleSeparator is the Data of a line between two parts of a list - what
// is chosen above it, the rest below. The cursor passes over it, and a
// filter leaves it out.
type toggleSeparator struct{}

// separatorItem is that line.
func separatorItem() toggleItem {
	return toggleItem{Label: tag(colBorder) + strings.Repeat("─", 200) + tagEnd, Data: toggleSeparator{}}
}

func isSeparator(it toggleItem) bool { _, ok := it.Data.(toggleSeparator); return ok }

// toggles describes a multiple choice: a list in which space or Enter turns
// the row under the cursor on or off and the dialog stays open, until Esc.
type toggles struct {
	title string
	// items lists the rows as they are now; it is asked again after every
	// toggle, so a row can show its new state.
	items  func() []toggleItem
	toggle func(toggleItem)
	// verb says what space does, as in "space/Enter hide/show".
	verb string
	// status, when set, follows the keys in the footer.
	status func() string
	// keys are the extra keys of the list, each with its hint.
	keys []toggleKey
	// closed, when set, runs once the dialog is gone: for a choice that is
	// made all at once rather than toggle by toggle. escSays is then what the
	// footer says Esc does, instead of close.
	closed  func()
	escSays string
	// cursor, when set, is the row the cursor goes to once the dialog
	// opens and after each toggle, while nothing is filtered; -1 leaves it
	// where it is.
	cursor func() int
	// pack sizes the dialog to its rows, as a packed picker is, rather than
	// to most of the screen: a few tags are not a screenful.
	pack bool
}

type toggleKey struct {
	key  rune
	hint string
	run  func()
	// onItem, when set instead of run, is given the row under the cursor.
	onItem func(toggleItem)
}

// showToggles opens a multiple choice. Like every picker it opens on the list
// - j/k move, / starts the filter - so typed letters never vanish into a
// search; here Enter and space toggle instead of choosing, and Esc closes.
func (a *App) showToggles(t toggles) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true).
		SetMainTextColor(colText).
		SetSelectedStyle(styleSelected)

	input := filterField(tview.NewInputField())
	footer := tview.NewTextView().SetDynamicColors(true)
	filtering := false
	updateFooter := func() {
		keys := "space/Enter " + t.verb
		for _, k := range t.keys {
			keys += fmt.Sprintf(" · %c %s", k.key, k.hint)
		}
		// Esc is said only when it does more than close.
		if t.escSays != "" {
			keys += " · Esc " + t.escSays
		}
		if filtering {
			keys = "Enter " + t.verb
		}
		status := ""
		if t.status != nil {
			status = " · " + t.status()
		}
		footer.SetText(" " + litHint(keys) + status)
	}

	var shown []toggleItem
	rebuild := func(query string) {
		current := list.GetCurrentItem()
		list.Clear()
		shown = shown[:0]
		type hit struct {
			it    toggleItem
			score int
		}
		var hits []hit
		for _, it := range t.items() {
			score, ok := fuzzy.Match(query, it.Search)
			if !ok {
				continue
			}
			hits = append(hits, hit{it, score})
		}
		if strings.TrimSpace(query) != "" {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		}
		for _, h := range hits {
			if isSeparator(h.it) && strings.TrimSpace(query) != "" {
				continue
			}
			shown = append(shown, h.it)
			list.AddItem(h.it.Label, "", 0, nil)
		}
		if current > 0 && current < list.GetItemCount() {
			list.SetCurrentItem(current)
		}
		updateFooter()
	}
	// place puts the cursor where the list wants it, while unfiltered.
	place := func() {
		if t.cursor == nil || strings.TrimSpace(input.GetText()) != "" {
			return
		}
		if i := t.cursor(); i >= 0 && i < list.GetItemCount() {
			list.SetCurrentItem(i)
		}
	}
	// The cursor never rests on a separator: it goes on past it the way it
	// was moving.
	last := 0
	list.SetChangedFunc(func(i int, _, _ string, _ rune) {
		if i >= 0 && i < len(shown) && isSeparator(shown[i]) {
			next := i + 1
			if i < last || next >= len(shown) {
				next = i - 1
			}
			if next >= 0 && next < len(shown) {
				list.SetCurrentItem(next)
				return
			}
		}
		last = i
	})
	rebuild("")
	place()
	input.SetChangedFunc(rebuild)

	toggle := func() {
		i := list.GetCurrentItem()
		if i < 0 || i >= len(shown) || isSeparator(shown[i]) {
			return
		}
		t.toggle(shown[i])
		rebuild(input.GetText())
		place()
	}
	dismiss := func() {
		a.closeModal(pageToggles)
		if t.closed != nil {
			t.closed()
		}
	}
	move := func(delta int) {
		if n := list.GetItemCount(); n > 0 {
			list.SetCurrentItem(max(0, min(list.GetCurrentItem()+delta, n-1)))
		}
	}
	setMode := func(active bool) {
		filtering = active
		updateFooter()
		if filtering {
			a.tv.SetFocus(input)
			return
		}
		a.tv.SetFocus(list)
	}

	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			setMode(false)
			return nil
		case tcell.KeyEnter:
			toggle()
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
		switch ev.Key() {
		case tcell.KeyEsc:
			dismiss()
			return nil
		case tcell.KeyEnter:
			toggle()
			return nil
		case tcell.KeyRune:
			for _, k := range t.keys {
				if ev.Rune() == k.key {
					if k.onItem != nil {
						if i := list.GetCurrentItem(); i >= 0 && i < len(shown) && !isSeparator(shown[i]) {
							k.onItem(shown[i])
						}
					} else {
						k.run()
					}
					rebuild(input.GetText())
					place()
					return nil
				}
			}
			switch ev.Rune() {
			case ' ':
				toggle()
			case '/':
				setMode(true)
			case 'j':
				move(1)
			case 'k':
				move(-1)
			case 'g':
				list.SetCurrentItem(0)
			case 'G':
				list.SetCurrentItem(list.GetItemCount() - 1)
			case 'q':
				dismiss()
			}
			// Runes are shortcuts in tview's List; nothing here uses them.
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)
	box(flex.Box, t.title)

	fitFooter(flex, footer, 1)
	frame := &pickerFrame{Flex: flex, target: func() tview.Primitive {
		if filtering {
			return input
		}
		return list
	}}
	var page tview.Primitive = modalPct(frame, 70, 75)
	if t.pack {
		// As wide as the longest row, the title or the footer, up to what a
		// packed picker takes, and as tall as the rows and the footer.
		inner := max(tview.TaggedStringWidth(t.title)+4, tview.TaggedStringWidth(footer.GetText(false))+1, 40)
		for _, it := range shown {
			if !isSeparator(it) {
				inner = max(inner, tview.TaggedStringWidth(it.Label)+1)
			}
		}
		inner = min(inner, 76)
		footerLines := len(tview.WordWrap(footer.GetText(true), inner))
		page = modalFixed(frame, inner+2, 2+1+max(1, len(shown))+footerLines)
	}
	a.pages.AddPage(pageToggles, page, true, true)
	setMode(false)
}
