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
		for _, banned := range []string{".AddDropDown(", "tview.NewDropDown(", ".AddCheckbox(", "tview.NewCheckbox("} {
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
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feature/audit-log")
	commitIn(t, dir, "n.txt", "Add the audit log", "")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feature/audit-log")
	p.rescan()
	typeRunes(sc, "W")
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

	// Letters are not a way in and do not change the choice.
	typeRunes(sc, "jj")
	if now, open := current(); now != before || open {
		t.Fatalf("typing changed the select: %q -> %q, open=%v\n%s", before, now, open, a.screenText(sc))
	}

	// The arrows open it, and its rows can be read: the current one is the
	// selection band, the others sit on the field colour.
	sc.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
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
