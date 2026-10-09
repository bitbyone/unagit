package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestSharedFieldsAreTheOnlyWayToMakeThem is the guard for a mistake made twice:
// a select box or checkbox built straight from tview is unstyled - its list
// paints unreadable text and it takes typed letters - and it looks different from
// the ones in Settings. Repeated elements come from fields.go.
func TestSharedFieldsAreTheOnlyWayToMakeThem(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file == "fields.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{".AddDropDown(", "tview.NewDropDown(", ".AddCheckbox(", "tview.NewCheckbox(", ".AddPasswordField("} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s builds a field with %s: use the shared constructor in fields.go (see AGENTS.md, repeated elements)", file, banned)
			}
		}
	}
}

// TestSelectBoxIsReadableAndIgnoresTyping opens the target branch select of the
// merge request form, as it was seen going wrong: an unreadable highlight, and
// letters typed into it.
func TestSelectBoxIsReadableAndIgnoresTyping(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feature/audit-log")
	commitIn(t, dir, "n.txt", "Add the audit log", "")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feature/audit-log")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "in sync")
	form := openForm(t, a, sc)
	waitFor(t, a, sc, "Target branch")
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone) // Title -> Target branch
	waitFor(t, a, sc, "▾")

	current := func() (string, bool) {
		type state struct {
			option string
			open   bool
		}
		s := onLoop(a, func() state {
			d := form.GetFormItemByLabel("Target branch").(*tview.DropDown)
			_, option := d.GetCurrentOption()
			return state{option, d.IsOpen()}
		})
		return s.option, s.open
	}
	before, _ := current()

	// Letters are not a way in and do not change the choice (j and k move
	// between the fields, as everywhere in a form).
	typeRunes(sc, "xm")
	if now, open := current(); now != before || open {
		t.Fatalf("typing changed the select: %q -> %q, open=%v\n%s", before, now, open, a.screenText(sc))
	}

	// Enter opens it, and its rows can be read: the current one is the
	// selection band, the others sit on the field colour.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "feat/rate")
	assertLegible(t, a, sc, "the open select")
	cells, w, h := onLoopCells(a, sc)
	sawBand, sawField := false, false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 || c.Runes[0] == ' ' {
				continue
			}
			fg, bg, _ := c.Style.Decompose()
			if bg == tcell.Color238 && fg == tcell.Color231 {
				sawBand = true
			}
			if bg == colSurface && fg == colText {
				sawField = true
			}
			// Nothing may be drawn on a pale grey slab like the one that was seen.
			if bg == tcell.Color252 || bg == tcell.Color250 || bg == tcell.Color253 {
				t.Errorf("%q at %d,%d is on a pale background %v\n%s", string(c.Runes[0]), x, y, bg, a.screenText(sc))
			}
		}
	}
	if !sawBand || !sawField {
		t.Errorf("the open list should draw its current row as the selection band (%v) and the rest on the field colour (%v)\n%s",
			sawBand, sawField, a.screenText(sc))
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
}

// TestTextAreaOpensOnItsFirstLine: a proposed description of several lines is
// read from the top; tview's own text area opened scrolled to its last line.
func TestTextAreaOpensOnItsFirstLine(t *testing.T) {
	t.Parallel()
	form := tview.NewForm()
	area := addTextArea(form, "Description", "- first\n- second\n- third\n- fourth\n- fifth\n- sixth\n- seventh", 3)
	screen := tcell.NewSimulationScreen("UTF-8")
	must(t, screen.Init())
	screen.SetSize(60, 10)
	form.SetRect(0, 0, 60, 10)
	form.Draw(screen)
	screen.Show()
	cells, w, _ := screen.GetContents()
	var top strings.Builder
	_, y, _, _ := area.GetRect()
	for x := 0; x < w; x++ {
		if r := cells[y*w+x].Runes; len(r) > 0 {
			top.WriteRune(r[0])
		}
	}
	if !strings.Contains(top.String(), "- first") {
		t.Errorf("the first line of the text area shows %q", strings.TrimSpace(top.String()))
	}
}

// TestTheSelectsOfAFormAreOneWidth: tview sizes a select to its longest
// option, and a column of them ended ragged; they are as wide as the widest,
// on screen as well as in their rectangles.
func TestTheSelectsOfAFormAreOneWidth(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	form := openNewRepository(t, a, sc)
	widths := onLoop(a, func() map[string]int {
		out := map[string]int{}
		for i := 0; i < form.GetFormItemCount(); i++ {
			if d, ok := form.GetFormItem(i).(*tview.DropDown); ok {
				out[d.GetLabel()] = d.GetFieldWidth()
			}
		}
		return out
	})
	if len(widths) < 4 {
		t.Fatalf("selects: %v", widths)
	}
	// The visibility select has the longest option; the others follow it.
	want := widths[labelRepoVisibility]
	for label, w := range widths {
		if w != want {
			t.Errorf("%s is %d wide, %s %d", label, w, labelRepoVisibility, want)
		}
	}
	// Drawn: each select's band ends in the same column.
	text := strings.Split(a.screenText(sc), "\n")
	end := func(label string) int {
		row := lineOf(a.screenText(sc), label)
		x := len([]rune(text[row][:strings.Index(text[row], label)])) + len(label)
		last := -1
		for col := x; col < x+80; col++ {
			if _, bg, _ := cellStyleAt(a, sc, col, row).Decompose(); bg == colSurface || bg == colFieldFocus {
				last = col
			}
		}
		return last
	}
	if l, g := end(labelRepoLicense), end(labelRepoGitignore); l != g || l < 0 {
		t.Errorf("the license select ends at %d, the .gitignore one at %d", l, g)
	}
}

// TestEscClosesAnOpenSelectAndNotTheDialog: Esc on an open select's list
// closes the list, keeps what was chosen, and leaves the dialog and the
// keyboard where they were; a second Esc leaves the dialog.
func TestEscClosesAnOpenSelectAndNotTheDialog(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	form := openNewRepository(t, a, sc)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing the name
	waitFor(t, a, sc, "i type")
	visibility := onLoop(a, func() *tview.DropDown {
		return form.GetFormItemByLabel(labelRepoVisibility).(*tview.DropDown)
	})
	onLoop(a, func() bool {
		form.SetFocus(form.GetFormItemIndex(labelRepoVisibility))
		a.tv.SetFocus(form)
		return true
	})
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "public")
	typeRunes(sc, "jj") // moves in the list, chooses nothing yet
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "public")
	state := onLoop(a, func() string {
		_, text := visibility.GetCurrentOption()
		name, _ := a.pages.GetFrontPage()
		focused := "nothing"
		if item, _ := form.GetFocusedItemIndex(); item >= 0 {
			focused = form.GetFormItem(item).GetLabel()
		}
		return name + " · " + text + " · " + focused
	})
	if want := pageForm + " · private · " + labelRepoVisibility; state != want {
		t.Fatalf("after Esc on the open list: %q, want %q", state, want)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "New repository")
}

// TestJAndKMoveInAnOpenSelectInSettings: a select in a section of
// Settings, which is no modal, moves with j and k once open, as one in a
// dialog does.
func TestJAndKMoveInAnOpenSelectInSettings(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionNotifications)
	waitFor(t, a, sc, "Danger stays")
	drop := onLoop(a, func() *tview.DropDown { return a.settings.notices.GetFormItemByLabel("Info stays").(*tview.DropDown) })
	option := func() (int, bool) {
		type state struct {
			at   int
			open bool
		}
		s := onLoop(a, func() state { at, _ := drop.GetCurrentOption(); return state{at, drop.IsOpen()} })
		return s.at, s.open
	}
	changeOnLoop(a, func() { a.settings.notices.SetFocus(0); a.tv.SetFocus(a.settings.notices) })
	start, _ := option()
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "Enter did not open the select", func() bool { _, open := option(); return open })
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "j did not move in the open select", func() bool {
		at, open := option()
		return !open && at == start+2
	})
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "Enter did not open the select again", func() bool { _, open := option(); return open })
	typeRunes(sc, "k")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "k did not move in the open select", func() bool {
		at, open := option()
		return !open && at == start+1
	})
}
