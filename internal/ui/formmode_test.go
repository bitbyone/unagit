package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestFormsHaveANormalAndAnInsertMode: a form with a text field first opens
// typing into it; Esc stops typing, j and k then move from field to field -
// the one with the focus painted in an accent of its own - i types again, and
// a button is its letter.
func TestFormsHaveANormalAndAnInsertMode(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	pressed := ""
	form := onLoop(a, func() *tview.Form {
		f := tview.NewForm()
		styleForm(f)
		f.AddInputField("First", "", 0, nil, nil)
		f.AddInputField("Second", "", 0, nil, nil)
		f.AddButton("Save", func() { pressed = "save" })
		f.AddButton("Cancel", func() { a.closeModal(pageForm) })
		a.showFormModal("Modes", f, 8)
		return f
	})
	a.tv.QueueUpdateDraw(func() {})
	text := func(label string) string {
		return onLoop(a, func() string { return form.GetFormItemByLabel(label).(*tview.InputField).GetText() })
	}
	focusBackground := func(label string) tcell.Color {
		r := onLoop(a, func() rect {
			x, y, w, h := form.GetFormItemByLabel(label).GetRect()
			return rect{x, y, w, h}
		})
		_, style := cellAt(a, sc, r.x+r.w-2, r.y)
		_, bg, _ := style.Decompose()
		return bg
	}

	waitFor(t, a, sc, "Esc stop typing")
	typeRunes(sc, "abc")
	waitFor(t, a, sc, "abc")
	if bg := focusBackground("First"); bg != colFieldTyping {
		t.Errorf("the field typed into is not marked: %v (surface %v)", bg, colSurface)
	}

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "i type")
	typeRunes(sc, "xy")
	if got := text("First"); got != "abc" {
		t.Errorf("NORMAL typed into the field: %q", got)
	}
	typeRunes(sc, "j")
	waitFor(t, a, sc, "i type")
	if bg := focusBackground("Second"); bg != colFieldFocus {
		t.Errorf("the field j moved to is not marked: %v\n%s", bg, a.screenText(sc))
	}
	if bg := focusBackground("First"); bg == colFieldFocus || bg == colFieldTyping {
		t.Error("the field left behind is still marked")
	}
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Esc stop typing")
	typeRunes(sc, "de")
	waitFor(t, a, sc, "de")
	if got := text("Second"); got != "de" {
		t.Errorf("i did not type into the second field: %q", got)
	}

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "i type")
	typeRunes(sc, "s")
	if onLoop(a, func() string { return pressed }) != "save" {
		t.Error("s did not press Save in NORMAL")
	}
	if !strings.Contains(a.screenText(sc), "Save") {
		t.Errorf("the button lost its name:\n%s", a.screenText(sc))
	}
	assertLegible(t, a, sc, "a form in NORMAL")
}
