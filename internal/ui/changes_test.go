package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// textStyle is the style of the first cell of text on screen, and whether
// it is there at all.
func textStyle(a *App, sc tcell.SimulationScreen, text string) (tcell.Style, bool) {
	cells, width, height := onLoopCells(a, sc)
	needle := []rune(text)
	for y := 0; y < height; y++ {
		for x := 0; x+len(needle) <= width; x++ {
			match := true
			for i, r := range needle {
				if c := cells[y*width+x+i]; len(c.Runes) == 0 || c.Runes[0] != r {
					match = false
					break
				}
			}
			if match {
				return cells[y*width+x].Style, true
			}
		}
	}
	return tcell.StyleDefault, false
}

// sameColour compares colours however each is written, a palette index
// or its RGB.
func sameColour(x, y tcell.Color) bool { return x.Hex() == y.Hex() }

// changesFixture is a clone with a file changed, a Go file changed in a
// way that has keywords to colour, and an unversioned file in a folder.
func changesFixture(t *testing.T, a *App) *realProject {
	t.Helper()
	p := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(filepath.Join(p.clone, "main.go"), []byte("package main\n\n// Count counts.\nfunc Count(n int) string {\n\treturn \"x\"\n}\n"), 0o644))
	gitIn(t, p.clone, "add", "main.go")
	gitIn(t, p.clone, "commit", "-qm", "Count")
	must(t, os.WriteFile(filepath.Join(p.clone, "main.go"), []byte("package main\n\n// Count counts requests.\nfunc Count(n int) string {\n\treturn \"y\"\n}\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p.clone, "a.txt"), []byte("edited\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(p.clone, "docs"), 0o755))
	must(t, os.WriteFile(filepath.Join(p.clone, "docs", "new.md"), []byte("# New\n"), 0o644))
	p.rescan()
	return p
}

// openChanges opens the Changes dialog on the first repository.
func openChanges(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlK, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "2 of 3 picked")
	waitFor(t, a, sc, "+ edited")
}

// TestChangesShowsEachFileAndItsDiff: the versioned files start picked and
// the unversioned not, each name in the colour of what happened to it, and
// the file under the cursor's diff beside the list - its lines on their
// fills, its code in its language's colours.
func TestChangesShowsEachFileAndItsDiff(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)

	text := a.screenText(sc)
	for _, want := range []string{glyphUnfolded + " " + glyphPicked + " Changes  2 files", glyphPicked + " a.txt",
		glyphPicked + " main.go", glyphUnpicked + " Unversioned Files  1 file", glyphUnpicked + " new.md  docs"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not in the list:\n%s", want, text)
		}
	}
	// a.txt is under the cursor, in the selection's colours.
	for name, colour := range map[string]string{"main.go": "files.changed", "new.md": "files.unversioned"} {
		if st, _ := textStyle(a, sc, name+" "); !sameColour(func() tcell.Color { fg, _, _ := st.Decompose(); return fg }(), role(colour)) {
			t.Errorf("%s is not drawn in %s", name, colour)
		}
	}
	if st, ok := textStyle(a, sc, "+ edited"); !ok {
		t.Error("the added line is not drawn")
	} else if _, bg, _ := st.Decompose(); !sameColour(bg, role("diff.added_fill")) {
		t.Errorf("the added line is on %v, not on its fill %v", bg, role("diff.added_fill"))
	}

	typeRunes(sc, "j")
	waitFor(t, a, sc, "Count counts requests")
	if st, ok := textStyle(a, sc, "func Count"); !ok {
		t.Error("the Go code is not drawn")
	} else if fg, _, _ := st.Decompose(); !sameColour(fg, role("syntax.keyword")) {
		t.Errorf("func is drawn in %v, not as a keyword %v", fg, role("syntax.keyword"))
	}
	assertLegible(t, a, sc, "the Changes dialog")
}

// TestChangesCommitsWhatIsPicked: an unversioned file picked goes into the
// commit with the versioned ones, and the dialog then says nothing is left.
func TestChangesCommitsWhatIsPicked(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	openChanges(t, a, sc)

	typeRunes(sc, "G ")
	waitFor(t, a, sc, "3 of 3 picked")
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Commit · acme/gateway · 3 file(s)")
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel("Message").(*tview.TextArea).SetText("Count, and say so", false)
		return true
	})
	pressButton(t, a, sc, form, "Commit")
	waitFor(t, a, sc, "nothing to commit")
	if got := gitIn(t, p.clone, "show", "--name-only", "--format=", "HEAD"); got != "a.txt\ndocs/new.md\nmain.go" {
		t.Errorf("committed %q", got)
	}
	if got := gitIn(t, p.clone, "status", "--porcelain"); got != "" {
		t.Errorf("left %q", got)
	}
}

// TestChangesRollbackAndDelete: u rolls the file under the cursor back when
// it is not picked, and d deletes an unversioned one, each after asking.
func TestChangesRollbackAndDelete(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	openChanges(t, a, sc)

	// Unpicked, a.txt is the one rolled back; main.go, picked, stays.
	typeRunes(sc, " k")
	waitFor(t, a, sc, "1 of 3 picked")
	typeRunes(sc, "u")
	waitFor(t, a, sc, "Roll back 1 file(s)")
	assertLegible(t, a, sc, "the question before a rollback")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "rolled back 1 file(s)")
	if got := gitIn(t, p.clone, "status", "--porcelain", "--untracked-files=all"); got != "M main.go\n?? docs/new.md" {
		t.Errorf("after the rollback: %q", got)
	}
	waitGone(t, a, sc, "a.txt")

	typeRunes(sc, "u")
	waitFor(t, a, sc, "Roll back 1 file(s)")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Roll back 1 file(s)")

	typeRunes(sc, "Gu")
	waitFor(t, a, sc, "nothing to roll back to")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "nothing to roll back to")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete 1 unversioned file(s)")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "deleted 1 file(s)")
	if _, err := os.Stat(filepath.Join(p.clone, "docs", "new.md")); !os.IsNotExist(err) {
		t.Errorf("new.md is still there: %v", err)
	}
}

// TestChangesFitsItsFrame draws the dialog at several sizes.
func TestChangesFitsItsFrame(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			resizeApp(a, sc, size.w, size.h)
			waitFor(t, a, sc, "Unversioned Files")
			frame := onLoop(a, func() rect {
				x, y, w, h := a.changes.frame.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Fatalf("row %d: the frame's right border is drawn over:\n%s", y, a.screenText(sc))
				}
			}
			for _, want := range []string{"Changes · acme/gateway", "a.txt", "new.md", "c commit"} {
				if !strings.Contains(a.screenText(sc), want) {
					t.Errorf("%q is not on screen:\n%s", want, a.screenText(sc))
				}
			}
			assertLegible(t, a, sc, "the Changes dialog")
		})
	}
}

// TestChangesIsLegibleInEachKindOfTheme: the diff's fills and the code's
// colours come from the theme, so they are looked at in a dark and a light
// one too. Serial: a theme is the process's.
func TestChangesIsLegibleInEachKindOfTheme(t *testing.T) {
	restoreDefaultTheme(t)
	for _, name := range legibleThemes {
		t.Run(name, func(t *testing.T) {
			a, sc := newThemedApp(t, name)
			waitFor(t, a, sc, "acme/gateway")
			changesFixture(t, a)
			openChanges(t, a, sc)
			assertLegible(t, a, sc, "the Changes dialog in "+name)
			typeRunes(sc, "j")
			waitFor(t, a, sc, "Count counts requests")
			assertLegible(t, a, sc, "Go code in "+name)
		})
	}
}
