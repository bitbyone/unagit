package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestADialogsMessagesComeUpOverIt: a warning from a dialog is a box over the
// dialog, not a line in the status bar under it; Esc closes the box and
// nothing more. A note goes with the next key, which still does its work.
func TestADialogsMessagesComeUpOverIt(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "enter the new branch")
	if got := onLoop(a, func() string { return a.projectsPane.statusMessage }); strings.Contains(got, "new branch") {
		t.Errorf("the warning went to the status bar too: %q", got)
	}
	assertLegible(t, a, sc, "a message over a dialog")
	typeRunes(sc, "xyz") // held, not typed into the dialog
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "enter the new branch")
	if !onLoop(a, func() bool { name, _ := a.pages.GetFrontPage(); return name == pageForm }) {
		t.Fatal("Esc closed more than the message")
	}
	if got := onLoop(a, func() string {
		return form.GetFormItemByLabel(labelGroupBranch).(interface{ GetText() string }).GetText()
	}); got != "" {
		t.Errorf("keys pressed at the message reached the dialog: %q", got)
	}

	changeOnLoop(a, func() { a.note("something was done") })
	waitFor(t, a, sc, "any key closes")
	typeRunes(sc, "c") // closes the note, and cancels the dialog
	waitGone(t, a, sc, "Grouped worktree")
	if onLoop(a, func() bool { return a.modalOpen() }) {
		t.Error("a dialog or the note is still open")
	}

	// With nothing in front, a message is the status bar's.
	changeOnLoop(a, func() { a.flash("on the main screen") })
	waitFor(t, a, sc, "on the main screen")
	if onLoop(a, func() bool { return a.modalOpen() }) {
		t.Error("a message on the main screen opened a box")
	}
}
