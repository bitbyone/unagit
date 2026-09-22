package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

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
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Cancel", accept}).
		SetDoneFunc(func(i int, label string) {
			a.closeModal(pageConfirm)
			if label == accept {
				onYes()
			}
		})
	modal.SetTextColor(colText)
	modal.SetButtonBackgroundColor(tcell.ColorDefault)
	modal.SetButtonTextColor(colText)
	box(modal.Box, title)
	modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Rune() {
		case 'y', 'Y':
			a.closeModal(pageConfirm)
			onYes()
			return nil
		case 'n', 'N', 'q':
			a.closeModal(pageConfirm)
			return nil
		}
		if ev.Key() == tcell.KeyEsc {
			a.closeModal(pageConfirm)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageConfirm, modalFull(modal), true, true)
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
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetMainTextColor(colText)
	list.SetSelectedStyle(styleSelected)

	input := tview.NewInputField().
		SetLabel(" / ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(colText).
		SetLabelColor(colAccent)

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
		footer.SetText(" " + tag(colMuted) + "NORMAL" + tagEnd + tag(colDim) +
			"   j/k move · / filter · Enter select · Esc close" + tagEnd)
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
