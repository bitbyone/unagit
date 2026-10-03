package ui

import "github.com/rivo/tview"

// Form fields that appear in more than one place are built here, once, so they
// look and behave the same wherever they turn up. A select box or a checkbox
// made straight from tview is unstyled: its list paints text in the background's
// own colour, and a select takes typed letters into a hidden search field. Add
// the field here instead of configuring it again; a test refuses tview's own
// constructors anywhere else in this package.

// addSelect adds a select box to a form: chosen with the arrows and Enter, drawn
// like the selection band of the lists, and deaf to typed letters. Every select
// of a form is as wide as the widest: tview sizes each to its own longest
// option, and a column of selects then ends ragged.
func addSelect(form *tview.Form, label string, options []string, selected int) *tview.DropDown {
	form.AddDropDown(label, options, selected, nil)
	drop := styleDropDown(form.GetFormItemByLabel(label).(*tview.DropDown))
	fitSelects(form)
	return drop
}

// fitSelects makes every select of a form as wide as the widest, again after
// a select's options changed. Unsized, tview gives a select its longest
// option's width.
func fitSelects(form *tview.Form) {
	widest := 0
	var selects []*tview.DropDown
	for i := 0; i < form.GetFormItemCount(); i++ {
		if d, ok := form.GetFormItem(i).(*tview.DropDown); ok {
			selects = append(selects, d)
			d.SetFieldWidth(0)
			widest = max(widest, d.GetFieldWidth()+selectPadding)
		}
	}
	for _, d := range selects {
		d.SetFieldWidth(widest)
	}
}

// addTextArea adds a text area that fills what the form has left and opens on
// the first line of its text. tview puts the cursor after the last one, which
// scrolls a proposed description of several lines down to its end, the rest
// out of sight.
func addTextArea(form *tview.Form, label, text string, height int) *tview.TextArea {
	form.AddTextArea(label, text, 0, height, 0, nil)
	area := form.GetFormItemByLabel(label).(*tview.TextArea)
	area.SetText(text, false)
	return area
}

// addCheckbox adds a checkbox that can be seen when it is not ticked.
func addCheckbox(form *tview.Form, label string, checked bool) *tview.Checkbox {
	form.AddCheckbox(label, checked, nil)
	box := form.GetFormItemByLabel(label).(*tview.Checkbox)
	return box.SetCheckedString(tview.Escape("[x]")).SetUncheckedString(tview.Escape("[ ]"))
}
