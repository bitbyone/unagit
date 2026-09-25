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

// addCheckbox adds a checkbox that can be seen when it is not ticked.
func addCheckbox(form *tview.Form, label string, checked bool) *tview.Checkbox {
	form.AddCheckbox(label, checked, nil)
	box := form.GetFormItemByLabel(label).(*tview.Checkbox)
	return box.SetCheckedString(tview.Escape("[x]")).SetUncheckedString(tview.Escape("[ ]"))
}
