package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
)

// ------------------------------------------------------------------ help

// --------------------------------------------------------------- confirm

// confirm shows a yes/no dialog for something destructive.
func (a *App) confirm(title, body string, warnings []string, onYes func()) {
	a.confirmWith(title, body, "Delete", warnings, onYes)
}

// confirmWith shows a yes/no dialog whose accepting button says what it does.
func (a *App) confirmWith(title, body, accept string, warnings []string, onYes func()) {
	text := body
	if len(warnings) > 0 {
		text += "\n\n" + tag(colBad) + "Careful:" + tagEnd + "\n"
		for _, w := range warnings {
			text += "  " + tag(colBad) + "!" + tagEnd + " " + tview.Escape(w) + "\n"
		}
	}
	keys := buttonKeys([]string{"Cancel", accept})
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Cancel", accept}).
		SetDoneFunc(func(i int, label string) {
			a.closeModal(pageConfirm)
			if i == 1 {
				onYes()
			}
		})
	modal.SetTextColor(colText)
	modal.SetButtonActivatedStyle(styleSelected)
	box(modal.Box, title)
	modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc {
			a.closeModal(pageConfirm)
			return nil
		}
		if ev.Key() != tcell.KeyRune || ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			return ev
		}
		key := unicode.ToLower(ev.Rune())
		switch {
		case key == keys[0] || key == 'n' || key == 'q':
			a.closeModal(pageConfirm)
			return nil
		case key == keys[1] || key == 'y':
			a.closeModal(pageConfirm)
			onYes()
			return nil
		}
		return ev
	})

	hint := fmt.Sprintf("c cancel · %c %s · Esc back", keys[1], strings.ToLower(accept))
	a.pages.AddPage(pageConfirm, modalFull(&confirmationHint{Modal: modal, hint: hint}), true, true)
	a.tv.SetFocus(modal)
}

// ---------------------------------------------------------------- picker

type pickItem struct {
	Label string
	Sub   string
	Data  any
}

// showPicker opens a fuzzy-filtered single choice list.
//
// Like the main lists it has two modes: typing filters, Esc leaves the input so
// j/k drive the selection, and a second Esc closes the modal.
func (a *App) showPicker(title string, items []pickItem, onSelect func(pickItem)) {
	a.showPickerActions(title, items, onSelect, nil, nil)
}

// showPickerActions is showPicker with two extra, optional keys available
// while browsing (not filtering): 'n' calls onNew instead of picking
// anything, and 'd' calls onDelete with the highlighted item instead of
// onSelect. Both dismiss the picker first, the same order choose() uses, so
// whatever dialog they open lands cleanly on the page underneath. Either may
// be nil, in which case its key does nothing - the callers that only need a
// plain choice list are unaffected.
func (a *App) showPickerActions(title string, items []pickItem, onSelect func(pickItem), onNew func(), onDelete func(pickItem)) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetMainTextColor(colText)
	list.SetSelectedStyle(styleSelected)

	input := filterField(tview.NewInputField())

	footer := tview.NewTextView().SetDynamicColors(true)

	shown := make([]pickItem, 0, len(items))
	rebuild := func(query string) {
		list.Clear()
		shown = shown[:0]
		type hit struct {
			it    pickItem
			score int
		}
		var hits []hit
		for _, it := range items {
			score, ok := fuzzy.Match(query, it.Label+" "+it.Sub)
			if !ok {
				continue
			}
			hits = append(hits, hit{it, score})
		}
		if strings.TrimSpace(query) != "" {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		}
		for _, h := range hits {
			label := h.it.Label
			if h.it.Sub != "" {
				label += "   " + tag(colDim) + h.it.Sub + tagEnd
			}
			shown = append(shown, h.it)
			list.AddItem(label, "", 0, nil)
		}
	}
	rebuild("")
	input.SetChangedFunc(rebuild)

	dismiss := func() { a.closeModal(pagePicker) }
	choose := func() {
		i := list.GetCurrentItem()
		if i < 0 || i >= len(shown) {
			return
		}
		it := shown[i]
		dismiss()
		onSelect(it)
	}

	setMode := func(filtering bool) {
		if filtering {
			footer.SetText(" " + tag(colWarn) + "FILTER" + tagEnd + tag(colDim) +
				"   type to narrow · Esc to the list · Enter select" + tagEnd)
			a.tv.SetFocus(input)
			return
		}
		hint := "   j/k move · / filter · Enter select"
		if onNew != nil {
			hint += " · n new"
		}
		if onDelete != nil {
			hint += " · d delete"
		}
		footer.SetText(" " + tag(colMuted) + "NORMAL" + tagEnd + tag(colDim) + hint + " · Esc close" + tagEnd)
		a.tv.SetFocus(list)
	}

	move := func(delta int) {
		if n := list.GetItemCount(); n > 0 {
			next := list.GetCurrentItem() + delta
			list.SetCurrentItem(max(0, min(next, n-1)))
		}
	}

	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			setMode(false)
			return nil
		case tcell.KeyEnter:
			choose()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn:
			if h := list.InputHandler(); h != nil {
				h(ev, func(tview.Primitive) {})
			}
			return nil
		case tcell.KeyCtrlN:
			move(1)
			return nil
		case tcell.KeyCtrlP:
			move(-1)
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
			choose()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case '/':
				setMode(true)
				return nil
			case 'j':
				move(1)
				return nil
			case 'k':
				move(-1)
				return nil
			case 'g':
				list.SetCurrentItem(0)
				return nil
			case 'G':
				list.SetCurrentItem(list.GetItemCount() - 1)
				return nil
			case 'q':
				dismiss()
				return nil
			case 'n':
				if onNew != nil {
					dismiss()
					onNew()
				}
				return nil
			case 'd':
				if onDelete != nil {
					if i := list.GetCurrentItem(); i >= 0 && i < len(shown) {
						it := shown[i]
						dismiss()
						onDelete(it)
					}
				}
				return nil
			}
			// Runes are shortcuts in tview's List; nothing here uses them.
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false).
		AddItem(footer, 1, 0, false)
	box(flex.Box, title)

	fitFooter(flex, footer, 1)
	a.pages.AddPage(pagePicker, modalPct(flex, 70, 70), true, true)
	setMode(true)
}

// ------------------------------------------------------------- formatting

// humanAge renders a timestamp as a coarse relative age.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}

// Reserve c for Cancel even when an earlier action also starts with c.
func buttonKeys(labels []string) []rune {
	keys := make([]rune, len(labels))
	used := map[rune]bool{}
	for i, label := range labels {
		if label == "Cancel" {
			keys[i] = 'c'
			used['c'] = true
		}
	}
	for i, label := range labels {
		if keys[i] != 0 {
			continue
		}
		for _, key := range strings.ToLower(label) + "1234567890" {
			if !used[key] && (unicode.IsLetter(key) || unicode.IsDigit(key)) {
				keys[i] = key
				used[key] = true
				break
			}
		}
	}
	return keys
}

func formButtonLabels(form *tview.Form) []string {
	labels := make([]string, form.GetButtonCount())
	for i := range labels {
		labels[i] = form.GetButton(i).GetLabel()
	}
	return labels
}

func formButtonHint(form *tview.Form) string {
	labels := formButtonLabels(form)
	keys := buttonKeys(labels)
	hints := make([]string, 0, len(labels)+1)
	for i, label := range labels {
		shortcut := fmt.Sprintf("Alt-%c", keys[i])
		if _, focused := form.GetFocusedItemIndex(); focused >= 0 {
			shortcut = string(keys[i])
		}
		if label == "Send" {
			shortcut += "/Ctrl-S"
		}
		hints = append(hints, shortcut+" "+strings.ToLower(label))
	}
	return strings.Join(append(hints, "Esc back"), " · ")
}

// A modal leaves one empty line beneath its buttons. Draw there after its
// internal frame, which would otherwise erase the hint.
type confirmationHint struct {
	*tview.Modal
	hint string
}

func (m *confirmationHint) Draw(screen tcell.Screen) {
	m.Modal.Draw(screen)
	x, y, w, h := m.GetRect()
	tview.Print(screen, m.hint, x+2, y+h-2, max(0, w-4), tview.AlignCenter, colDim)
}

// Forms keep their hints outside the scrollable fields and button row.
func hintForm(form *tview.Form) {
	hintPanel(form.Box, func() string { return formButtonHint(form) }, 1, 1, 2, 2)
}

// Reserve space inside the border so scrolling content cannot overwrite hints.
func hintPanel(panel *tview.Box, hint func() string, top, bottom, left, right int) {
	panel.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		width := max(0, w-2-left-right)
		lines := tview.WordWrap(hint(), max(1, width))
		for i, line := range lines {
			tview.Print(screen, line, x+1+left, y+h-1-len(lines)+i, width, tview.AlignLeft, colDim)
		}
		return x + 1 + left, y + 1 + top, width, max(0, h-2-top-bottom-len(lines))
	})
}

// Plain letters belong to the focused field. Alt shortcuts also work while
// editing, and button activation goes through tview so disabled buttons stay inert.
func bindFormButtons(form *tview.Form) {
	previous := form.GetInputCapture()
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModCtrl == 0 {
			_, focusedButton := form.GetFocusedItemIndex()
			if ev.Modifiers()&tcell.ModAlt != 0 || focusedButton >= 0 {
				for i, key := range buttonKeys(formButtonLabels(form)) {
					if unicode.ToLower(ev.Rune()) == key {
						form.GetButton(i).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
						return nil
					}
				}
			}
		}
		if previous != nil {
			return previous(ev)
		}
		return ev
	})
}

// Hints wrap with their block instead of disappearing off the terminal edge.
func fitFooter(block *tview.Flex, footer *tview.TextView, inset int) {
	block.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		width := max(1, w-2*inset)
		lines := len(tview.WordWrap(footer.GetText(false), width))
		block.ResizeItem(footer, max(1, lines), 0)
		return x + inset, y + inset, max(0, w-2*inset), max(0, h-2*inset)
	})
}
