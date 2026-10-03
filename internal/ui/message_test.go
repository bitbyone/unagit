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

// TestAMessageOverADialogDimsNothingMore: the screen behind is dimmed once,
// by the dialog; the message over it leaves both as they were - dimming twice
// made the screen look inverted.
func TestAMessageOverADialogDimsNothingMore(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	markBoth(t, a, sc)
	text := a.screenText(sc)
	listRow := lineOf(text, "acme/billing")
	listCol := len([]rune(strings.Split(text, "\n")[listRow][:strings.Index(strings.Split(text, "\n")[listRow], "acme/billing")])) + 1
	formRow := lineOf(text, "Folder name")
	formCol := len([]rune(strings.Split(text, "\n")[formRow][:strings.Index(strings.Split(text, "\n")[formRow], "Folder name")])) + 1
	list, form := cellStyleAt(a, sc, listCol, listRow), cellStyleAt(a, sc, formCol, formRow)

	changeOnLoop(a, func() { a.flash("over the dialog") })
	waitFor(t, a, sc, "over the dialog")
	if got := cellStyleAt(a, sc, listCol, listRow); got != list {
		t.Errorf("the list behind went from %v to %v", list, got)
	}
	if got := cellStyleAt(a, sc, formCol, formRow); got != form {
		t.Errorf("the dialog under the message went from %v to %v", form, got)
	}
}

// TestAMessageSaysWhatKindItIs: the box is filled like a confirmation, and
// its first line names the severity in the severity's colour; a success or
// a note goes with the next key, a warning or an error waits for Esc.
func TestAMessageSaysWhatKindItIs(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	markBoth(t, a, sc)
	for _, c := range []struct {
		say      func()
		heading  string
		colour   tcell.Color
		closing  string
		severity string
	}{
		{func() { a.done("deleted it") }, "✓ Success", colOn, "any key closes", "success"},
		{func() { a.note("looking") }, "i Info", colAccent, "any key closes", "info"},
		{func() { a.flash("not that way") }, "! Warning", colWarn, "Esc close", "warning"},
		{func() { a.errorf("it broke") }, "✗ Error", colBad, "Esc close", "error"},
	} {
		changeOnLoop(a, c.say)
		waitFor(t, a, sc, c.heading)
		text := a.screenText(sc)
		row := lineOf(text, c.heading)
		col := len([]rune(strings.Split(text, "\n")[row][:strings.Index(strings.Split(text, "\n")[row], c.heading)]))
		if _, style := cellAt(a, sc, col+2, row); func() bool {
			fg, bg, _ := style.Decompose()
			return fg.Hex() != c.colour.Hex() || bg.Hex() != colSurface.Hex()
		}() {
			fg, bg, _ := style.Decompose()
			t.Errorf("%s: heading drawn %v on %v", c.severity, fg, bg)
		}
		if _, style := cellAt(a, sc, col-1, row); func() bool { _, bg, _ := style.Decompose(); return bg.Hex() != colSurface.Hex() }() {
			t.Errorf("%s: the box is not filled", c.severity)
		}
		if lineOf(text, c.closing) != row+2 {
			t.Errorf("%s: the message is not between its heading and %q:\n%s", c.severity, c.closing, text)
		}
		assertLegible(t, a, sc, "a "+c.severity+" message")
		// The next message replaces this one.
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "✗ Error")
}
