package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// eventually waits for a state read on the event loop.
func eventually(t *testing.T, a *App, what string, holds func() bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if onLoop(a, holds) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not come", what)
}

// at is the cell where a text is on screen.
func at(t *testing.T, a *App, sc tcell.SimulationScreen, text string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(a.screenText(sc), "\n") {
		if x := strings.Index(line, text); x >= 0 {
			return len([]rune(line[:x])), y
		}
	}
	t.Fatalf("%q is not on screen", text)
	return 0, 0
}

// press and release are a button going down and up at a cell.
func press(sc tcell.SimulationScreen, x, y int, button tcell.ButtonMask, mods tcell.ModMask) {
	sc.InjectMouse(x, y, button, mods)
}

func release(sc tcell.SimulationScreen, x, y int, mods tcell.ModMask) {
	sc.InjectMouse(x, y, tcell.ButtonNone, mods)
}

func click(sc tcell.SimulationScreen, x, y int, mods tcell.ModMask) {
	press(sc, x, y, tcell.ButtonPrimary, mods)
	release(sc, x, y, mods)
}

// selectedRepository is the repository under the Repositories' cursor.
func selectedRepository(a *App) string {
	_, path := a.selectedProjectOf(a.projectsPane)
	return path
}

// TestTheMouseDoesWhatTheKeysDo: a click puts the cursor on a row, Ctrl-
// click marks one, a drag marks those it passes over, the right button
// opens the actions of the row under it, and a click beside the dialog in
// front reaches nothing under it. A click on a tab goes there.
func TestTheMouseDoesWhatTheKeysDo(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")
	p := a.projectsPane

	x, y := at(t, a, sc, "acme/billing")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the cursor on billing", func() bool { return selectedRepository(a) == "acme/billing" })
	if n := onLoop(a, func() int { return len(p.marks) }); n != 0 {
		t.Fatalf("a plain click marked %d rows", n)
	}

	click(sc, x, y, tcell.ModCtrl)
	eventually(t, a, "billing marked", func() bool { return len(p.marks) == 1 })
	click(sc, x, y, tcell.ModAlt)
	eventually(t, a, "billing unmarked", func() bool { return len(p.marks) == 0 })

	// A drag from gateway down to billing marks both.
	gx, gy := at(t, a, sc, "acme/gateway")
	press(sc, gx, gy, tcell.ButtonPrimary, tcell.ModNone)
	press(sc, gx, y, tcell.ButtonPrimary, tcell.ModNone)
	release(sc, gx, y, tcell.ModNone)
	eventually(t, a, "both marked by the drag", func() bool { return len(p.marks) == 2 })
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	eventually(t, a, "the marks cleared", func() bool { return len(p.marks) == 0 })

	// The right button: the cursor goes there and the row's actions open.
	press(sc, gx, gy, tcell.ButtonSecondary, tcell.ModNone)
	release(sc, gx, gy, tcell.ModNone)
	waitFor(t, a, sc, "Actions · acme/gateway")
	if got := onLoop(a, func() string { return selectedRepository(a) }); got != "acme/gateway" {
		t.Errorf("the right button left the cursor on %q", got)
	}

	// Beside the picker, the list under it is out of reach.
	click(sc, x, y, tcell.ModNone)
	time.Sleep(100 * time.Millisecond)
	if got := onLoop(a, func() string { return selectedRepository(a) }); got != "acme/gateway" {
		t.Errorf("a click beside the picker moved the list under it to %q", got)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	eventually(t, a, "the picker gone", func() bool { return !a.modalOpen() })

	tx, ty := at(t, a, sc, "Merge requests")
	click(sc, tx+2, ty, tcell.ModNone)
	eventually(t, a, "the merge requests' tab", func() bool { return a.currentTab() == pageMRs })
	sx, sy := at(t, a, sc, "Settings [S]")
	click(sc, sx+1, sy, tcell.ModNone)
	eventually(t, a, "the Settings tab", func() bool { return a.currentTab() == pageSettings })
}

// TestTheMouseInSettings: a click on a section shows it, a double click
// goes into it as Enter does, and a click into a field types there.
func TestTheMouseInSettings(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGeneral)
	waitFor(t, a, sc, "Git email")

	x, y := at(t, a, sc, "Git email")
	click(sc, x+20, y, tcell.ModNone)
	eventually(t, a, "Git email typing", func() bool {
		form, mode := a.focusedForm()
		if form == nil || mode == nil || !mode.insert {
			return false
		}
		f, ok := focusedItem(form).(*tview.InputField)
		return ok && f.GetLabel() == "Git email"
	})
	eventually(t, a, "the content focused", func() bool { return a.settings.contentFocused })

	gx, gy := at(t, a, sc, "Groups & roots")
	click(sc, gx, gy, tcell.ModNone)
	eventually(t, a, "Groups & roots shown", func() bool {
		return a.settings.current == sectionGroups && !a.settings.contentFocused
	})
	click(sc, gx, gy, tcell.ModNone)
	eventually(t, a, "into the groups", func() bool { return a.settings.contentFocused && a.settings.tree.HasFocus() })

	// A click on a group puts the cursor there and changes nothing; a
	// double click is Enter, which cycles what the group brings.
	waitFor(t, a, sc, "incl. subgroups")
	cx, cy := at(t, a, sc, "incl. subgroups")
	click(sc, cx, cy, tcell.ModNone)
	time.Sleep(tview.DoubleClickInterval + 50*time.Millisecond)
	click(sc, cx, cy, tcell.ModNone)
	time.Sleep(100 * time.Millisecond)
	if !strings.Contains(a.screenText(sc), "incl. subgroups") {
		t.Fatal("a single click on a group changed it")
	}
	click(sc, cx, cy, tcell.ModNone)
	click(sc, cx, cy, tcell.ModNone)
	waitGone(t, a, sc, "incl. subgroups")
}
