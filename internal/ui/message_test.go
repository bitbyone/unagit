package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestADialogsMessagesComeUpOverIt: a warning from a dialog is a box over the
// dialog, not a line in the status bar under it; Esc closes the box and
// nothing more. A note goes with the next key, which still does its work.
func TestADialogsMessagesComeUpOverIt(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "enter the new branch")
	if got := onLoop(a, func() string { return a.transient }); strings.Contains(got, "new branch") {
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

	// A note is a passing word in the dialog's bottom edge, at its right,
	// and asks for nothing: c still cancels the dialog.
	changeOnLoop(a, func() { a.note("something was done") })
	waitFor(t, a, sc, "something was done")
	if onLoop(a, func() bool { return a.pages.HasPage(pageMessage) }) {
		t.Error("a note over a dialog opened a box")
	}
	frame := onLoop(a, func() rect { x, y, w, h := form.GetRect(); return rect{x, y, w, h} })
	if row := lineOf(a.screenText(sc), "something was done"); row != frame.y+frame.h-1 {
		t.Errorf("the note is on row %d, not in the dialog's bottom edge (row %d):\n%s", row, frame.y+frame.h-1, a.screenText(sc))
	}
	assertLegible(t, a, sc, "a note in a dialog's edge")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Grouped worktree")
	waitGone(t, a, sc, "something was done")
	if onLoop(a, func() bool { return a.modalOpen() }) {
		t.Error("a dialog or the note is still open")
	}

	// With nothing in front, a note is the status line's, at its right.
	changeOnLoop(a, func() { a.note("on the main screen") })
	waitFor(t, a, sc, "on the main screen")
	if onLoop(a, func() bool { return a.modalOpen() }) {
		t.Error("a note on the main screen opened a box")
	}
	if line := lineAt(a.screenText(sc), "on the main screen"); !strings.Contains(line, "? help") ||
		strings.Index(line, "on the main screen") < strings.Index(line, "repositories") {
		t.Errorf("the note is not at the right of the status line: %q", line)
	}
	// A warning asks for attention, there too: a box until Esc.
	changeOnLoop(a, func() { a.flash("look here") })
	waitFor(t, a, sc, "look here")
	if !onLoop(a, func() bool { return a.modalOpen() }) {
		t.Error("a warning on the main screen did not come up in a box")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "look here")
}

// closeMessage closes the warning or error in front, which holds the keys
// until Esc, and waits until it is gone.
func closeMessage(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	open := func() bool { return onLoop(a, func() bool { return a.pages.HasPage(pageMessage) }) }
	deadline := time.Now().Add(patience)
	for !open() {
		if time.Now().After(deadline) {
			t.Fatalf("no message to close:\n%s", a.screenText(sc))
		}
		time.Sleep(10 * time.Millisecond)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	for open() {
		if time.Now().After(deadline) {
			t.Fatalf("the message does not close:\n%s", a.screenText(sc))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// messageText is the text of the message box in front, "" without one.
func messageText(a *App) string {
	return onLoop(a, func() string {
		box, ok := a.pages.GetPage(pageMessage).(*modalBox)
		if !ok {
			return ""
		}
		if flex, ok := box.content.(*tview.Flex); ok && flex.GetItemCount() > 1 {
			if text, ok := flex.GetItem(1).(*tview.TextView); ok {
				return text.GetText(true)
			}
		}
		return ""
	})
}

// TestModalsDimEachCellOnce: a dialog dims the whole terminal, the tabs
// too; a message over it dims the dialog it stands on and leaves the rest
// as it was - dimming everything twice made the screen look inverted - so
// every cell is dimmed once at most and the box in front is the bright one.
func TestModalsDimEachCellOnce(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	tabs := func() tcell.Style {
		text := a.screenText(sc)
		row := lineOf(text, "[1] Repositories")
		col := len([]rune(strings.Split(text, "\n")[row][:strings.Index(strings.Split(text, "\n")[row], "[1] Repositories")])) + 1
		return cellStyleAt(a, sc, col, row)
	}
	bright := tabs()
	markBoth(t, a, sc)
	if got := tabs(); got == bright {
		t.Error("the tabs are not dimmed under a dialog")
	}
	text := a.screenText(sc)
	listRow := lineOf(text, "acme/billing")
	listCol := len([]rune(strings.Split(text, "\n")[listRow][:strings.Index(strings.Split(text, "\n")[listRow], "acme/billing")])) + 1
	formRow := lineOf(text, "Folder name")
	formCol := len([]rune(strings.Split(text, "\n")[formRow][:strings.Index(strings.Split(text, "\n")[formRow], "Folder name")])) + 1
	list, form, tab := cellStyleAt(a, sc, listCol, listRow), cellStyleAt(a, sc, formCol, formRow), tabs()

	changeOnLoop(a, func() { a.flash("over the dialog") })
	waitFor(t, a, sc, "over the dialog")
	if got := cellStyleAt(a, sc, listCol, listRow); got != list {
		t.Errorf("the list behind went from %v to %v", list, got)
	}
	if got := tabs(); got != tab {
		t.Errorf("the tabs went from %v to %v", tab, got)
	}
	fg, _, _ := form.Decompose()
	if got, _, _ := cellStyleAt(a, sc, formCol, formRow).Decompose(); got.Hex() != onLoop(a, func() tcell.Color { return darken(fg, colDimmedText) }).Hex() {
		t.Errorf("the dialog under the message is %v, not its own %v dimmed once", got, fg)
	}
	assertLegible(t, a, sc, "a message over a dialog")
}

// TestAMessageSaysWhatKindItIs: the box is filled like a confirmation, and
// its first line names the severity in the severity's colour; a success or
// a note goes with the next key, a warning or an error waits for Esc.
func TestAMessageSaysWhatKindItIs(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	markBoth(t, a, sc)
	for _, c := range []struct {
		say      func()
		heading  string
		colour   tcell.Color
		closing  string
		severity string
	}{
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
