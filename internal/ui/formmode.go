package ui

import (
	"regexp"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Every form has two modes, as the lists have NORMAL and FILTER. In NORMAL the
// keys move: j/k (or Tab) from field to field and over the buttons, i or Enter
// starts typing into a text field, a button's letter presses it, and Esc
// leaves the form. In INSERT the keys type and Esc goes back to NORMAL; a
// button has its lit letter and no other key, so pressing one while typing is
// Esc and the letter. A form that starts with a text field opens in INSERT,
// since that field is what one came to fill in.
//
// The mode is kept per form, on the event loop, by the app the form is in.

type formMode struct{ insert bool }

// navigationKeys are what NORMAL moves and edits with; no button takes them.
const navigationKeys = "hjkli"

// editable reports whether a form item is typed into.
func editable(item tview.FormItem) bool {
	switch item.(type) {
	case *tview.InputField, *tview.TextArea:
		return true
	}
	return false
}

// focusable reports whether the form can stop on an item: the notes between
// fields cannot, and Form.SetFocus on one deadlocks tview.
func focusable(item tview.FormItem) bool {
	_, note := item.(*tview.TextView)
	return !note
}

// formFocus is where the form's focus is, counting the buttons after the items.
func formFocus(form *tview.Form) int {
	item, button := form.GetFocusedItemIndex()
	if item >= 0 {
		return item
	}
	if button >= 0 {
		return form.GetFormItemCount() + button
	}
	return -1
}

// focusedItem is the form item with the focus, or nil on a button.
func focusedItem(form *tview.Form) tview.FormItem {
	if item, _ := form.GetFocusedItemIndex(); item >= 0 {
		return form.GetFormItem(item)
	}
	return nil
}

// moveFocus steps to the next or previous place the form can stop at, around
// the end, and reports whether that is a text field.
func (a *App) moveFocus(form *tview.Form, step int) bool {
	items, total := form.GetFormItemCount(), form.GetFormItemCount()+form.GetButtonCount()
	if total == 0 {
		return false
	}
	at := formFocus(form)
	for range total {
		at = (at + step + total) % total
		if at >= items || focusable(form.GetFormItem(at)) {
			break
		}
	}
	form.SetFocus(at)
	a.tv.SetFocus(form)
	return at < items && editable(form.GetFormItem(at))
}

// bindFormButtons gives a form its two modes and its button letters.
func (a *App) bindFormButtons(form *tview.Form) {
	labelButtons(form)
	mode := &formMode{}
	if a.formModes == nil {
		a.formModes = map[*tview.Form]*formMode{}
	}
	a.formModes[form] = mode
	if item := focusedItem(form); item != nil {
		mode.insert = editable(item)
	} else if form.GetFormItemCount() > 0 && editable(form.GetFormItem(0)) {
		mode.insert = true
	}
	press := func(r rune) bool {
		for i, key := range buttonKeys(formButtonLabels(form)) {
			if unicode.ToLower(r) == key {
				form.GetButton(i).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
				return true
			}
		}
		return false
	}
	previous := form.GetInputCapture()
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		item := focusedItem(form)
		// Only a text field is typed into; whatever brought the focus to a
		// button or a select, it is in NORMAL there.
		if item == nil || !editable(item) {
			mode.insert = false
		}
		// An open select's list has the keys; j and k move in it. Esc only
		// closes the list: tview hands it to the select rather than to its
		// list, and the select reports it to the form as finished, which the
		// form takes for cancel - the whole dialog went. The list, which has
		// the keyboard while open, aborts the choice and nothing more.
		if drop, ok := item.(*tview.DropDown); ok && drop.IsOpen() {
			if list, ok := a.tv.GetFocus().(*tview.List); ok && ev.Key() == tcell.KeyEsc {
				list.InputHandler()(ev, func(p tview.Primitive) { a.tv.SetFocus(p) })
				return nil
			}
			return ev
		}
		if mode.insert {
			_, line := item.(*tview.InputField)
			switch {
			case ev.Key() == tcell.KeyEsc:
				mode.insert = false
				return nil
			case ev.Key() == tcell.KeyTab:
				mode.insert = a.moveFocus(form, 1)
				return nil
			case ev.Key() == tcell.KeyBacktab:
				mode.insert = a.moveFocus(form, -1)
				return nil
			case line && (ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyDown):
				mode.insert = a.moveFocus(form, 1)
				return nil
			case line && ev.Key() == tcell.KeyUp:
				mode.insert = a.moveFocus(form, -1)
				return nil
			}
		} else {
			switch ev.Key() {
			case tcell.KeyTab, tcell.KeyDown:
				a.moveFocus(form, 1)
				return nil
			case tcell.KeyBacktab, tcell.KeyUp:
				a.moveFocus(form, -1)
				return nil
			case tcell.KeyEnter:
				if item != nil && editable(item) {
					mode.insert = true
					return nil
				}
			case tcell.KeyRune:
				if ev.Modifiers()&tcell.ModCtrl != 0 {
					break
				}
				switch ev.Rune() {
				case 'j', 'l':
					a.moveFocus(form, 1)
					return nil
				case 'k', 'h':
					a.moveFocus(form, -1)
					return nil
				case 'i':
					if item != nil && editable(item) {
						mode.insert = true
					}
					return nil
				}
				if press(ev.Rune()) {
					return nil
				}
				// A key of the form's own, like ? for help, still works.
				if previous != nil {
					if ev = previous(ev); ev == nil {
						return nil
					}
				}
				// Nothing is typed in NORMAL.
				if item != nil && editable(item) {
					return nil
				}
				return ev
			}
		}
		if previous != nil {
			return previous(ev)
		}
		return ev
	})
}

// focusedForm is the form with the focus and its mode, or nil.
func (a *App) focusedForm() (*tview.Form, *formMode) {
	for form, mode := range a.formModes {
		if form.HasFocus() {
			return form, mode
		}
	}
	return nil, nil
}

// markFocusedField paints the field the focus is on in an accent of its own,
// once the form is drawn: tview gives every field the same colours on every
// draw, focused or not, so nothing else would say where the focus is - least
// of all in NORMAL, where no cursor blinks. The colour also tells the modes
// apart. A button marks itself already.
func (a *App) markFocusedField(screen tcell.Screen) {
	form, mode := a.focusedForm()
	if form == nil {
		return
	}
	if !mode.insert {
		// Nothing would be typed where it blinks.
		screen.HideCursor()
	}
	item := focusedItem(form)
	if item == nil {
		return
	}
	accent := colFieldFocus
	if mode.insert {
		accent = colFieldTyping
	}
	x, y, w, h := item.GetRect()
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			r, comb, style, _ := screen.GetContent(col, row)
			if _, bg, _ := style.Decompose(); bg == colSurface {
				screen.SetContent(col, row, r, comb, style.Background(accent))
			}
		}
	}
}

// forgetForm drops the mode of a form that is gone.
func (a *App) forgetForm(form *tview.Form) { delete(a.formModes, form) }

// labelButtons marks each button's letter in its label, so the key is seen
// where the button is. It is coloured rather than bracketed: brackets widen
// every button, and a row that no longer fits its dialog wraps past the frame.
// The name stays what the code knows it by: buttonName reads it back.
func labelButtons(form *tview.Form) {
	labels := formButtonLabels(form)
	keys := buttonKeys(labels)
	for i, label := range labels {
		form.GetButton(i).SetLabel(markKey(label, keys[i]))
	}
}

// keyOpen and keyClose wrap the letter that presses a button.
func keyOpen() string { return tag(colKey) + "[::b]" }

const keyClose = "[::-]" + "[-]"

// keyMarkup is what markKey puts around a letter, in any theme's colour: a
// label marked before the theme changed still reads by its name.
var keyMarkup = regexp.MustCompile(`\[[^\[\]]*\]\[::b\]|\[::-\]\[-\]`)

// markKey colours the letter that presses a button. A key the label lacks - a
// digit - is only in the hint.
func markKey(label string, key rune) string {
	runes := []rune(label)
	for i, r := range runes {
		if unicode.ToLower(r) == key {
			return string(runes[:i]) + keyOpen() + string(r) + keyClose + string(runes[i+1:])
		}
	}
	return label
}

// buttonName is a button's label without the colour markKey added.
func buttonName(label string) string {
	return keyMarkup.ReplaceAllString(label, "")
}
