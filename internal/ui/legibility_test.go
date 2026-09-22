package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// assertLegible walks the whole screen and fails on any character drawn in
// its own background colour.
//
// Two ways text can be unreadable: drawn in its own background colour, or
// left at the terminal's default foreground on a background we chose - the
// terminal picks that foreground to suit its own background, so against one
// of ours it is a coin toss.
//
// tview builds every interactive widget from one pair of colours used both
// ways round, so getting that pair wrong hides text in one state while
// leaving it fine in the other - a button that vanishes only while focused,
// say. Checking the rendered cells catches that wherever it happens, rather
// than one widget at a time after someone notices.
func assertLegible(t *testing.T, a *App, sc tcell.SimulationScreen, what string) {
	t.Helper()
	cells, w, h := onLoopCells(a, sc)
	var problems []string
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 || c.Runes[0] == ' ' || c.Runes[0] == 0 {
				continue // nothing to read
			}
			fg, bg, _ := c.Style.Decompose()
			why := ""
			switch {
			case fg == bg:
				why = "drawn in its own background colour"
			case fg == tcell.ColorDefault && bg != tcell.ColorDefault:
				// The terminal picks its default foreground to suit its own
				// background, so on a colour we chose it is a coin toss - and
				// it is exactly what made buttons vanish.
				why = "left at the terminal's foreground on a chosen background"
			default:
				continue
			}
			problems = append(problems, fmt.Sprintf("%q at %d,%d %s (%v on %v)",
				string(c.Runes[0]), x, y, why, fg, bg))
			if len(problems) > 6 {
				break
			}
		}
	}
	if len(problems) > 0 {
		t.Errorf("%s: unreadable text:\n  %s\n%s",
			what, strings.Join(problems, "\n  "), a.screenText(sc))
	}
}

func onLoopCells(a *App, sc tcell.SimulationScreen) ([]tcell.SimCell, int, int) {
	type shot struct {
		cells []tcell.SimCell
		w, h  int
	}
	s := onLoop(a, func() shot {
		cells, w, h := sc.GetContents()
		copied := make([]tcell.SimCell, len(cells))
		copy(copied, cells)
		return shot{copied, w, h}
	})
	return s.cells, s.w, s.h
}

// TestEveryDialogIsLegible walks the interface with the keyboard and checks
// each screen. The focused state is the one that breaks, so each dialog is
// looked at with the focus in it.
func TestEveryDialogIsLegible(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	assertLegible(t, a, sc, "the repository list")

	// A confirm dialog: its buttons are what went missing.
	cloneOnDisk(t, a, a.cfg.Instances[0].ID, "acme/gateway")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete repository")
	assertLegible(t, a, sc, "the delete dialog")
	typeRunes(sc, "n")

	typeRunes(sc, "?")
	waitFor(t, a, sc, "GETTING AROUND")
	assertLegible(t, a, sc, "the help")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)

	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branch - acme/gateway")
	assertLegible(t, a, sc, "the branch picker")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)

	typeRunes(sc, "X")
	waitFor(t, a, sc, "Hidden repositories")
	assertLegible(t, a, sc, "the hidden repositories modal")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)

	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "MERGE REQUEST")
	assertLegible(t, a, sc, "the merge request detail")

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway")
	assertLegible(t, a, sc, "the conversation")
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Markdown is understood")
	assertLegible(t, a, sc, "the comment composer")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
}

// TestSettingsIsLegible covers the forms, where the pair matters most.
func TestSettingsIsLegible(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	for _, section := range []int{sectionGeneral, sectionGitLab, sectionGitHub, sectionGroups, sectionSecurity} {
		openSection(t, a, sc, section)
		waitFor(t, a, sc, sectionNames[section])
		assertLegible(t, a, sc, "settings: "+sectionNames[section])
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	}

	// The server form, with each item focused in turn: fields, the select
	// box and the buttons all get a look while active.
	openSection(t, a, sc, sectionGitLab)
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit server")
	form := currentForm(a)
	if form == nil {
		t.Fatal("no form on screen")
	}
	items := onLoop(a, form.GetFormItemCount)
	for i := 0; i < items; i++ {
		// The explanatory text is not focusable, and Form.SetFocus on such an
		// item sends tview round a loop that re-enters the item's own lock
		// and deadlocks. Tab skips it, so nobody can reach it anyway.
		if onLoop(a, func() bool {
			_, isText := form.GetFormItem(i).(*tview.TextView)
			return isText
		}) {
			continue
		}
		onLoop(a, func() bool {
			form.SetFocus(i)
			a.tv.SetFocus(form)
			return true
		})
		assertLegible(t, a, sc, fmt.Sprintf("the server form, item %d focused", i))
	}
	for _, label := range []string{"Save", "Cancel"} {
		onLoop(a, func() bool {
			form.SetFocus(items + form.GetButtonIndex(label))
			a.tv.SetFocus(form)
			return true
		})
		assertLegible(t, a, sc, "the server form, "+label+" focused")
	}
}
