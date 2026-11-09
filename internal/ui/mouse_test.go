package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/unibox/uniboxtest"
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

// TestTheDetailClosesByItsMark: the detail column has a close mark in its
// corner, on the border, and a click on it closes the column as Esc does.
func TestTheDetailClosesByItsMark(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	p := a.projectsPane
	eventually(t, a, "the detail", func() bool { return p.detailShown })
	mark := " " + glyphClose + " ╮"
	waitFor(t, a, sc, mark)
	x, y := at(t, a, sc, mark)
	if dx, dy, dw, _ := onLoopRect(a, p.detail.Box); y != dy || x+len([]rune(mark))-1 != dx+dw-1 {
		t.Errorf("the mark is at %d,%d, not in the detail's corner %d,%d", x, y, dx+dw-1, dy)
	}
	click(sc, x+1, y, tcell.ModNone)
	eventually(t, a, "the detail closed", func() bool { return !p.detailShown && p.table.HasFocus() })
	waitGone(t, a, sc, mark)
}

func onLoopRect(a *App, b *tview.Box) (int, int, int, int) {
	r := onLoop(a, func() [4]int { x, y, w, h := b.GetRect(); return [4]int{x, y, w, h} })
	return r[0], r[1], r[2], r[3]
}

// TestTheWheelMovesTheCursorAndKeepsUp: the wheel moves a list's cursor as
// j and k do - a picker's too, which did not scroll at all - and a burst of
// it, as a trackpad sends, leaves the keys answered at once after it.
func TestTheWheelMovesTheCursorAndKeepsUp(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")
	x, y := at(t, a, sc, "acme/gateway")
	sc.InjectMouse(x, y, tcell.WheelDown, tcell.ModNone)
	eventually(t, a, "the cursor a row down", func() bool { return selectedRepository(a) == "acme/billing" })
	sc.InjectMouse(x, y, tcell.WheelUp, tcell.ModNone)
	eventually(t, a, "the cursor back up", func() bool { return selectedRepository(a) == "acme/gateway" })

	typeRunes(sc, ".")
	waitFor(t, a, sc, "Actions · acme/gateway")
	px, py := onLoopPair(a, func() (int, int) {
		var list *bandedList
		var find func(p tview.Primitive)
		find = func(p tview.Primitive) {
			switch v := p.(type) {
			case *bandedList:
				list = v
			case *modalBox:
				find(v.content)
			case interface {
				GetItemCount() int
				GetItem(int) tview.Primitive
			}:
				for i := range v.GetItemCount() {
					find(v.GetItem(i))
				}
			}
		}
		find(a.mouseTop())
		lx, ly, _, _ := list.GetInnerRect()
		return lx + 2, ly + 1
	})
	current := func() int {
		return onLoop(a, func() int {
			if list, ok := primitiveAt(a.mouseTop(), px, py).(*bandedList); ok {
				return list.GetCurrentItem()
			}
			return -1
		})
	}
	before := current()
	sc.InjectMouse(px, py, tcell.WheelDown, tcell.ModNone)
	if before < 0 {
		t.Fatal("no picker under the pointer")
	}
	eventually(t, a, "the picker's cursor a row down", func() bool {
		list, ok := primitiveAt(a.mouseTop(), px, py).(*bandedList)
		return ok && list.GetCurrentItem() > before
	})

	for range 200 {
		sc.InjectMouse(px, py, tcell.WheelDown, tcell.ModNone)
		sc.InjectMouse(px, py, tcell.WheelUp, tcell.ModNone)
	}
	start := time.Now()
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	eventually(t, a, "the picker closed", func() bool { return !a.modalOpen() })
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Esc after the wheel took %v", took)
	}
}

// TestAClickOnACardLightsItsPanelAlone: the cards are a widget of unagit's
// own, built on a Box, and a click on one gave that Box the focus - the
// card lit as chosen while the screen still lit the tasks. It goes to the
// panel the way B goes there, and back to the tasks the way t does.
func TestAClickOnACardLightsItsPanelAlone(t *testing.T) {
	t.Parallel()
	_, prepare := fakeUnibox(t, nil)
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	v := a.sandboxes
	lit := func(b *tview.Box) bool { return b.GetBorderColor() == colBorderFocus }

	x, y := at(t, a, sc, "2 tasks")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the cards with the keys, lit alone", func() bool {
		return a.tv.GetFocus() == v.cards && v.current == v.cards && lit(v.cards.Box) && !lit(v.pane.table.Box)
	})

	x, y = at(t, a, sc, "rate ")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the tasks with the keys, lit alone", func() bool {
		return v.pane.table.HasFocus() && v.current == v.pane.table && lit(v.pane.table.Box) && !lit(v.cards.Box) && !v.cards.HasFocus()
	})
}

// TestAClickChoosesTheCardUnderIt: a click on a card of Sandboxes chose
// nothing - the panel took the keys and the card chosen stayed the first.
// It chooses the card under the pointer, and the detail follows it as it
// follows h and l.
func TestAClickChoosesTheCardUnderIt(t *testing.T) {
	t.Parallel()
	boxes := strings.Replace(uniboxtest.Overview()["sandbox ls --json"].Out, `"sandboxes":[`,
		`"sandboxes":[{"name":"spare","env":"api","state":"stopped"},`, 1)
	_, prepare := fakeUnibox(t, map[string]uniboxtest.Answer{"sandbox ls --json": uniboxtest.JSON(boxes)})
	a, sc, _ := newTestAppSrv(t, prepare)
	openSandboxes(t, a, sc)
	v := a.sandboxes
	eventually(t, a, "two cards", func() bool { return len(v.boxes) == 2 })
	waitFor(t, a, sc, "2 tasks")

	// "2 tasks" is on web's card; x is the same column of the first card.
	x, y := at(t, a, sc, "2 tasks")
	if onLoop(a, func() string { return v.boxes[1].sandbox.Name }) == "web" {
		x -= sandboxCardWidth + 1
	}
	click(sc, x+sandboxCardWidth+1, y, tcell.ModNone)
	eventually(t, a, "the second card chosen, with the keys", func() bool {
		return v.current == v.cards && v.cards.at == 1
	})
	name := onLoop(a, func() string { return v.boxes[1].sandbox.Name })
	eventually(t, a, "the detail on the second card", func() bool {
		return v.pane.detailShown && strings.Contains(v.pane.detail.GetTitle(), name)
	})

	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the first card chosen again", func() bool { return v.cards.at == 0 })
}

// TestCardAtTellsTheCards: a band of two cards ten wide and the count of
// the rest - the gaps between them are no card.
func TestCardAtTellsTheCards(t *testing.T) {
	t.Parallel()
	for x, want := range map[int]int{0: -1, 1: 0, 10: 0, 11: -1, 12: 1, 21: 1, 22: -1, 23: 2, 23 + moreWidth - 1: 2, 23 + moreWidth: -1} {
		if got := cardAt(x, 1, 10, 2, true); got != want {
			t.Errorf("cardAt(%d) = %d, want %d", x, got, want)
		}
	}
	if got := cardAt(23, 1, 10, 2, false); got != -1 {
		t.Errorf("past the last card without a count: %d, want -1", got)
	}
}

// TestAClickChoosesTheEditorCardUnderIt: Activity's cards of the editors
// open are chosen by a click as the sandboxes' are.
func TestAClickChoosesTheEditorCardUnderIt(t *testing.T) {
	t.Parallel()
	a, sc, _ := activityFixture(t)
	x, y := at(t, a, sc, "tobola/incomm")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the second editor chosen, with the keys", func() bool {
		return a.tv.GetFocus() == a.activity.cards && a.activity.cards.at == 1
	})
	x, y = at(t, a, sc, "tobola/unagit")
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the first editor chosen again", func() bool { return a.activity.cards.at == 0 })
}

// TestADoubleClickOnATextPanelIsItsEnter: Watching is a few lines of
// text, no rows; a double click anywhere on it does what Enter does there.
func TestADoubleClickOnATextPanelIsItsEnter(t *testing.T) {
	t.Parallel()
	a, sc, _ := activityFixture(t)
	x, y := at(t, a, sc, "repository branch")
	click(sc, x, y, tcell.ModNone)
	click(sc, x, y, tcell.ModNone)
	eventually(t, a, "the watches listed", func() bool { return a.modalOpen() })
	waitFor(t, a, sc, "KIND")
}

// TestARightClickOnATabIsItsColon: the right button on a tab goes there
// and opens what : opens on it - the screen's actions.
func TestARightClickOnATabIsItsColon(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	x, y := at(t, a, sc, "Sandboxes")
	press(sc, x, y, tcell.ButtonSecondary, tcell.ModNone)
	release(sc, x, y, tcell.ModNone)
	waitFor(t, a, sc, "Go to Merge Requests")
	if got := onLoop(a, func() string { return a.currentTab() }); got != pageSandboxes {
		t.Errorf("the right button left the tab on %q", got)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Go to Merge Requests")

	x, y = at(t, a, sc, "Settings")
	press(sc, x, y, tcell.ButtonSecondary, tcell.ModNone)
	release(sc, x, y, tcell.ModNone)
	waitFor(t, a, sc, "Go to Merge Requests")
}
