package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestConfirmationButtonShortcuts(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	accepted := 0
	show := func(label string) {
		onLoop(a, func() bool {
			a.confirmWith("Shortcut test", "Choose an action", label, nil, func() { accepted++ })
			return true
		})
	}
	for _, action := range []struct{ label, key string }{{"Delete", "d"}, {"Approve", "a"}, {"Switch", "s"}} {
		show(action.label)
		a.tv.QueueUpdateDraw(func() {})
		assertMutedHint(t, a, sc, "c cancel")
		waitFor(t, a, sc, action.key+" "+strings.ToLower(action.label))
		assertLegible(t, a, sc, "confirmation shortcuts")
		typeRunes(sc, "c")
		waitGone(t, a, sc, "Shortcut test")
		before := onLoop(a, func() int { return accepted })
		show(action.label)
		typeRunes(sc, action.key)
		waitGone(t, a, sc, "Shortcut test")
		if got := onLoop(a, func() int { return accepted }); got != before+1 {
			t.Fatalf("%s activated %d times", action.label, got-before)
		}
	}
	if got := onLoop(a, func() int { return accepted }); got != 3 {
		t.Fatalf("cancel activated a destructive action: %d", got)
	}
}

func TestFormButtonShortcuts(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	saved, inherited, cancelled, existing := 0, 0, 0, 0
	form := onLoop(a, func() *tview.Form {
		f := tview.NewForm()
		styleForm(f)
		f.AddInputField("Value", "", 30, nil, nil)
		f.AddButton("Save", func() { saved++ })
		f.AddButton("Inherit", func() { inherited++ })
		f.AddButton("Cancel", func() { cancelled++; a.closeModal(pageForm) })
		f.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if ev.Key() == tcell.KeyCtrlS {
				existing++
				return nil
			}
			return ev
		})
		a.showFormModal("Shortcut form", f, 10)
		return f
	})
	a.tv.QueueUpdateDraw(func() {})
	waitFor(t, a, sc, "Alt-s save")
	typeRunes(sc, "scid")
	waitFor(t, a, sc, "scid")
	onLoop(a, func() bool {
		if saved+inherited+cancelled != 0 {
			t.Error("typing activated a button")
		}
		return true
	})
	sc.InjectKey(tcell.KeyRune, 's', tcell.ModAlt)
	sc.InjectKey(tcell.KeyRune, 'i', tcell.ModAlt)
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	// Closing the modal marks that every preceding event has been handled.
	sc.InjectKey(tcell.KeyRune, 'c', tcell.ModAlt)
	waitGone(t, a, sc, "Shortcut form")
	onLoop(a, func() bool {
		if saved != 1 || inherited != 1 || cancelled != 1 || existing != 1 {
			t.Errorf("actions: save=%d inherit=%d cancel=%d existing=%d", saved, inherited, cancelled, existing)
		}
		// Button focus allows plain letters without requiring another Tab.
		a.showFormModal("Shortcut form", form, 10)
		form.SetFocus(form.GetFormItemCount())
		a.tv.SetFocus(form)
		return true
	})
	typeRunes(sc, "ic")
	waitGone(t, a, sc, "Shortcut form")
	if got := onLoop(a, func() int { return inherited }); got != 2 {
		t.Fatalf("plain button shortcut did not activate: %d", got)
	}
}

func TestButtonShortcutCollisionsAndDisabled(t *testing.T) {
	keys := buttonKeys([]string{"Change", "Cancel", "Clone"})
	if string(keys) != "hcl" {
		t.Fatalf("conflicting shortcuts: %q", string(keys))
	}
	form := tview.NewForm()
	called := false
	form.AddButton("Save", func() { called = true })
	form.GetButton(0).SetDisabled(true)
	hintForm(form)
	bindFormButtons(form)
	form.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModAlt), func(tview.Primitive) {})
	if called {
		t.Fatal("shortcut activated disabled button")
	}
}

func assertMutedHint(t *testing.T, a *App, sc tcell.SimulationScreen, hint string) {
	t.Helper()
	waitFor(t, a, sc, hint)
	cells, width, height := onLoopCells(a, sc)
	needle := []rune(hint)
	for y := 0; y < height; y++ {
		for x := 0; x+len(needle) <= width; x++ {
			matches := true
			for i, r := range needle {
				cell := cells[y*width+x+i]
				if len(cell.Runes) == 0 || cell.Runes[0] != r {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			for i, r := range needle {
				if r == ' ' {
					continue
				}
				fg, _, _ := cells[y*width+x+i].Style.Decompose()
				if fg != colDim {
					t.Fatalf("hint %q is not dim: %v", hint, fg)
				}
			}
			return
		}
	}
	t.Fatalf("hint %q not rendered", hint)
}

func TestSimpleDialogsKeepInlineHints(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	waitGone(t, a, sc, "e directory")
	resize(sc, 90, 44)
	typeRunes(sc, "e")
	assertMutedHint(t, a, sc, "Alt-s save")
	assertMutedHint(t, a, sc, "Alt-c cancel")
	assertLegible(t, a, sc, "form hints in a narrow terminal")
	sc.InjectKey(tcell.KeyRune, 'c', tcell.ModAlt)
	waitGone(t, a, sc, "Clone directory ·")
}

func TestHelpUsesTheOpeningContext(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	check := func(title string, active, inactive []string) {
		t.Helper()
		typeRunes(sc, "?")
		waitFor(t, a, sc, "unagit · keys · "+title)
		onLoop(a, func() bool {
			_, primitive := a.pages.GetFrontPage()
			table := primitive.(*modalBox).content.(*tview.Flex).GetItem(0).(*tview.Table)
			for _, group := range []struct {
				keys   []string
				colour tcell.Color
			}{{active, colText}, {inactive, colDim}} {
				for _, key := range group.keys {
					found := false
					for row := 0; row < table.GetRowCount(); row++ {
						cell := table.GetCell(row, 0)
						if cell.Text != key {
							continue
						}
						found = true
						foreground, _, _ := cell.Style.Decompose()
						description, _, _ := table.GetCell(row, 1).Style.Decompose()
						if foreground != group.colour || description != group.colour {
							t.Errorf("%s: %q has colours %v/%v, want %v", title, key, foreground, description, group.colour)
						}
					}
					if !found {
						t.Errorf("help is missing %q", key)
					}
				}
			}
			return true
		})
		assertLegible(t, a, sc, "context help")
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "unagit · keys")
	}
	check("Repositories", []string{"Ctrl-C", "Ctrl-O"}, []string{"Ctrl-R", "Ctrl-G", "Ctrl-F Ctrl-B"})
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	check("Merge requests", []string{"Ctrl-R", "Ctrl-G"}, []string{"Ctrl-C", "Ctrl-F Ctrl-B"})
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")
	check("Merge request detail", []string{"Ctrl-R", "Ctrl-F Ctrl-B"}, []string{"Ctrl-C"})
	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	check("Settings", []string{}, []string{"Ctrl-O", "a e t v d"})
	openSection(t, a, sc, sectionGitLab)
	check("Servers", []string{"a e t v d"}, []string{"Ctrl-O", "r  p  m"})
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	openSection(t, a, sc, sectionGeneral)
	a.tv.QueueUpdateDraw(func() {
		form := a.settings.general
		form.SetFocus(form.GetFormItemCount())
		a.tv.SetFocus(form)
	})
	check("General settings", []string{"Alt-s / Alt-r", "?"}, []string{"Ctrl-O", "a e t v d"})

}
