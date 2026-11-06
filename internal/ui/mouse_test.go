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

	// A click beside the picker closes it, typing into its filter as it
	// is, and reaches nothing under it.
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the picker gone", func() bool { return !a.modalOpen() })
	if got := onLoop(a, func() string { return selectedRepository(a) }); got != "acme/gateway" {
		t.Errorf("a click beside the picker moved the list under it to %q", got)
	}

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

// TestAClickIsDrawnAtOnce: what a click changes is on screen at once - the
// cursor and the marks - not when something else happens to draw. tview
// draws only after an event one of its widgets took, and unagit answers
// the mouse itself.
func TestAClickIsDrawnAtOnce(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")
	bx, by := at(t, a, sc, "acme/billing")
	gx, gy := at(t, a, sc, "acme/gateway")
	background := func(x, y int) tcell.Color {
		cells, width, _ := onLoopCells(a, sc)
		_, bg, _ := cells[y*width+x].Style.Decompose()
		return bg
	}
	drawn := func(what string, holds func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if holds() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s is not drawn", what)
	}
	plain := background(bx, by)
	band := background(gx, gy)
	if plain == band {
		t.Fatal("the cursor's row looks like any other")
	}

	click(sc, bx, by, tcell.ModNone)
	drawn("the cursor on billing", func() bool { return background(bx, by) == band && background(gx, gy) == plain })

	click(sc, gx, gy, tcell.ModCtrl)
	drawn("gateway marked", func() bool { return background(gx, gy) != plain })
	click(sc, bx, by, tcell.ModNone)
	drawn("gateway still marked with the cursor away", func() bool {
		return background(bx, by) == band && background(gx, gy) != plain
	})
}

// TestAClickBesideADialogClosesIt: the backdrop is Esc, as many as it takes
// - a form being typed into goes in one click - and the dialog it was
// opened from comes back, as it does from the keys.
func TestAClickBesideADialogClosesIt(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "incl. subgroups")
	typeRunes(sc, "ji")
	waitFor(t, a, sc, "Git identity · acme")
	eventually(t, a, "the form typing", func() bool {
		_, mode := a.focusedForm()
		return mode != nil && mode.insert
	})
	click(sc, 1, 30, tcell.ModNone)
	waitGone(t, a, sc, "Git identity · acme")
	if onLoop(a, a.modalOpen) {
		t.Error("something is still in front")
	}
}

// TestAClickInSettingsGoesStraightIn: from the sections, one click on the
// content takes the keys there - a group of the tree, a field of a form -
// and a click on another field moves into that one, typing.
func TestAClickInSettingsGoesStraightIn(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "incl. subgroups")
	changeOnLoop(a, func() { a.settings.focusList() })

	x, y := at(t, a, sc, "incl. subgroups")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the group under the cursor, the tree with the keys", func() bool {
		ref, ok := a.settings.currentRef()
		return ok && ref.group != nil && a.settings.tree.HasFocus() && a.settings.contentFocused
	})

	gx, gy := at(t, a, sc, "General")
	click(sc, gx, gy, tcell.ModNone)
	waitFor(t, a, sc, "Git email")
	eventually(t, a, "the sections with the keys", func() bool { return a.settings.list.HasFocus() })
	for _, label := range []string{"Git name", "Git email", "Default root"} {
		fx, fy := at(t, a, sc, label)
		click(sc, fx+len(label)+4, fy, tcell.ModNone)
		eventually(t, a, label+" typing", func() bool {
			form, mode := a.focusedForm()
			if form == nil || !mode.insert || !a.settings.contentFocused {
				return false
			}
			f, ok := focusedItem(form).(*tview.InputField)
			return ok && f.GetLabel() == label
		})
	}
}

// TestAClickLightsThePanelItGoesTo: on a screen of panels a click goes to
// the panel the way its key does - its border lit and the others' not -
// and back to the list the same way.
func TestAClickLightsThePanelItGoesTo(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "4")
	waitFor(t, a, sc, "Nothing has happened yet")
	v := a.activity
	lit := func(b *tview.Box) bool { return b.GetBorderColor() == colBorderFocus }

	x, y := at(t, a, sc, "Nothing has happened yet")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the log with the keys, lit alone", func() bool {
		return v.current == v.log && v.log.HasFocus() && lit(v.log.Box) && !lit(v.pane.table.Box)
	})

	x, y = at(t, a, sc, "Nothing under way")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the list with the keys, lit alone", func() bool {
		return v.current == v.pane.table && v.pane.table.HasFocus() && lit(v.pane.table.Box) && !lit(v.log.Box)
	})
}
