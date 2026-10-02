package ui

import "github.com/rivo/tview"

// Form fields that appear in more than one place are built here, once, so they
// look and behave the same wherever they turn up. A select box or a checkbox
// made straight from tview is unstyled: its list paints text in the background's
// own colour, and a select takes typed letters into a hidden search field. Add
// the field here instead of configuring it again; a test refuses tview's own
// constructors anywhere else in this package.

// addSelect adds a select box to a form: chosen with the arrows and Enter, drawn
// like the selection band of the lists, and deaf to typed letters.
func addSelect(form *tview.Form, label string, options []string, selected int) *tview.DropDown {
	form.AddDropDown(label, options, selected, nil)
	return styleDropDown(form.GetFormItemByLabel(label).(*tview.DropDown))
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
