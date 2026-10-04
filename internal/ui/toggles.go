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
}

type toggleKey struct {
	key  rune
	hint string
	run  func()
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
		keys := "j/k move · space/Enter " + t.verb
		for _, k := range t.keys {
			keys += fmt.Sprintf(" · %c %s", k.key, k.hint)
		}
		esc := "close"
		if t.escSays != "" {
			esc = t.escSays
		}
		keys += " · / search · Esc " + esc
		if filtering {
			keys = "type to search · ↑/↓ move · Enter " + t.verb + " · Esc list"
		}
		status := ""
		if t.status != nil {
			status = " · " + t.status()
		}
		footer.SetText(fmt.Sprintf(" %s%s%s%s", tag(colDim), keys, tagEnd, status))
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
			shown = append(shown, h.it)
			list.AddItem(h.it.Label, "", 0, nil)
		}
		if current > 0 && current < list.GetItemCount() {
			list.SetCurrentItem(current)
		}
		updateFooter()
	}
	rebuild("")
	input.SetChangedFunc(rebuild)

	toggle := func() {
		i := list.GetCurrentItem()
		if i < 0 || i >= len(shown) {
			return
		}
		t.toggle(shown[i])
		rebuild(input.GetText())
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
					k.run()
					rebuild(input.GetText())
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
	a.pages.AddPage(pageToggles, modalPct(frame, 70, 75), true, true)
	setMode(false)
}
