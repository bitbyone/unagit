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
	waitFor(t, a, sc, "2 of 3 to commit")
	waitFor(t, a, sc, "+ edited")
}

// TestChangesShowsEachFileAndItsDiff: the versioned files start ticked
// for the commit and the unversioned not, each name in the colour of what happened to it, and
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
		glyphPicked + " main.go", glyphUnpicked + " Unversioned Files  1 file", glyphUnpicked + " new.md +1  docs"} {
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

// TestChangesCommitsWhatIsTicked: an unversioned file ticked goes into the
// commit with the versioned ones, and the dialog then says nothing is left.
func TestChangesCommitsWhatIsTicked(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	openChanges(t, a, sc)

	typeRunes(sc, "Gx")
	waitFor(t, a, sc, "3 of 3 to commit")
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

// TestChangesRollbackAndDelete: R rolls back the file under the cursor -
// not what is ticked for the commit - and d deletes an unversioned one,
// each after asking.
func TestChangesRollbackAndDelete(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	openChanges(t, a, sc)

	// a.txt is under the cursor; main.go, ticked as well, stays.
	typeRunes(sc, "R")
	waitFor(t, a, sc, "Roll back 1 file(s)")
	assertLegible(t, a, sc, "the question before a rollback")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "rolled back 1 file(s)")
	if got := gitIn(t, p.clone, "status", "--porcelain", "--untracked-files=all"); got != "M main.go\n?? docs/new.md" {
		t.Errorf("after the rollback: %q", got)
	}
	waitGone(t, a, sc, "a.txt")

	typeRunes(sc, "R")
	waitFor(t, a, sc, "Roll back 1 file(s)")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Roll back 1 file(s)")

	typeRunes(sc, "GR")
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

// TestChangesMarksAreForActionsTicksForTheCommit: space marks rows on a
// band of their own, and the actions take the marked rows - a mix is split,
// each action saying what it leaves - while the ticks stay as they were;
// x ticks or unticks the marked rows, and Esc takes the marks away before
// it closes anything.
func TestChangesMarksAreForActionsTicksForTheCommit(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)

	typeRunes(sc, "  ")
	waitFor(t, a, sc, "2 of 3 to commit · 2 marked")
	if bg := rowBackground(a, sc, "a.txt"); !sameColour(bg, colMarked) {
		t.Errorf("a marked row is on %v, not the marks' band %v", bg, colMarked)
	}
	typeRunes(sc, "R")
	waitFor(t, a, sc, "Roll back 2 file(s)")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Roll back 2 file(s)")

	// The unversioned file marked too: d deletes it alone, and says so.
	typeRunes(sc, " ")
	waitFor(t, a, sc, "3 marked")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete 1 unversioned file(s)")
	waitFor(t, a, sc, "R rolls them back")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Delete 1 unversioned file(s)")

	typeRunes(sc, "x")
	waitFor(t, a, sc, "3 of 3 to commit")
	typeRunes(sc, "x")
	waitFor(t, a, sc, "0 of 3 to commit")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "marked")
	waitFor(t, a, sc, "Changes · acme/gateway")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Changes · acme/gateway")
}

// TestChangesContextActions: Alt-Enter lists what can be done with the row
// under the cursor, named after it, and : what the dialog can do.
func TestChangesContextActions(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · a.txt")
	for _, want := range []string{"Rollback Changes…", "Include or Exclude", "Open With…"} {
		waitFor(t, a, sc, want)
	}
	if onLoop(a, func() bool { return a.tv.GetFocus() == a.changes.diff }) {
		t.Error("Alt-Enter went into the diff as an Enter")
	}
	for _, offered := range []string{"Add to Git", "Add to .gitignore", "Delete…"} {
		if strings.Contains(a.screenText(sc), offered) {
			t.Errorf("a versioned file is offered %q", offered)
		}
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · a.txt")

	// An unversioned file is offered what is done with one, and no rollback.
	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · docs/new.md")
	waitFor(t, a, sc, "Add to Git")
	if strings.Contains(a.screenText(sc), "Rollback Changes") {
		t.Error("an unversioned file is offered a rollback")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · docs/new.md")

	typeRunes(sc, ":")
	waitFor(t, a, sc, "Refresh")
	waitFor(t, a, sc, "Include All or None")
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

// TestChangesTabGoesIntoTheDiffAndBack: Tab gives the diff the focus - its
// border lit, the hint its own - and Tab or Esc gives it back. Going into
// the diff once hung unagit for good: the pane's focus function asked the
// TextView whether it had the focus, under the lock its Focus held.
func TestChangesTabGoesIntoTheDiffAndBack(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)
	lit := func(box *tview.Box) bool {
		return onLoop(a, func() bool { return box.GetBorderColor() == colBorderFocus })
	}

	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitGone(t, a, sc, "space mark")
	if !lit(a.changes.diff.Box) || lit(a.changes.table.Box) {
		t.Error("the diff's border is not the one lit")
	}
	if !onLoop(a, func() bool { return a.changes.inDiff && a.tv.GetFocus() == a.changes.diff }) {
		t.Error("the diff does not have the focus")
	}
	typeRunes(sc, "j")

	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitFor(t, a, sc, "space mark")
	if !lit(a.changes.table.Box) || lit(a.changes.diff.Box) {
		t.Error("the list's border is not the one lit")
	}
	typeRunes(sc, "l")
	waitGone(t, a, sc, "space mark")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "space mark")
	if !onLoop(a, func() bool { return a.tv.GetFocus() == a.changes.table }) {
		t.Error("Esc in the diff did not give the list the focus")
	}
}

// TestChangesHAndL: h and l fold and unfold a group and go up to a file's
// group while the tree has a use for them, and otherwise go between the
// list and the diff, as they go between panels everywhere.
func TestChangesHAndL(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)
	inDiff := func() bool { return onLoop(a, func() bool { return a.tv.GetFocus() == a.changes.diff }) }
	cursor := func() int { return onLoop(a, func() int { at, _ := a.changes.table.GetSelection(); return at }) }

	typeRunes(sc, "l")
	waitGone(t, a, sc, "space mark")
	typeRunes(sc, "h")
	waitFor(t, a, sc, "space mark")

	// From a file, h goes up to its group; there it folds, l unfolds, and
	// l again, with nothing to unfold, goes on to the diff.
	typeRunes(sc, "jh")
	if at := cursor(); at != 0 {
		t.Errorf("h from a file left the cursor on row %d, not on its group", at)
	}
	typeRunes(sc, "h")
	waitFor(t, a, sc, glyphFolded+" "+glyphPicked+" Changes")
	waitGone(t, a, sc, "main.go ")
	typeRunes(sc, "l")
	waitFor(t, a, sc, glyphUnfolded+" "+glyphPicked+" Changes")
	if inDiff() {
		t.Error("l unfolding a group also went to the diff")
	}
	typeRunes(sc, "l")
	waitGone(t, a, sc, "space mark")
	if !inDiff() {
		t.Error("l on an open group did not go to the diff")
	}
}

// TestChangesOpensTheFileInAnEditor: Ctrl-O opens the whole working tree
// in the default editor with the file under the cursor in front, and the
// dialog is there again after.
func TestChangesOpensTheFileInAnEditor(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	editor := yaziFavourite(t, a)
	openChanges(t, a, sc)

	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	// Over a dialog the word goes to its bottom edge.
	waitFor(t, a, sc, "opened ")
	waitEditorIdle(t, a)
	if got := editorLog(t, editor); got != p.clone+"\n--test\n"+filepath.Join(p.clone, "a.txt")+"\n" {
		t.Errorf("editor arguments: %q", got)
	}
	waitFor(t, a, sc, "2 of 3 to commit")
}

// TestChangesAddToGit: A on the Unversioned Files heading puts every file
// of it under git; they join the changes as added files, ticked for the
// commit, and git has them as added.
func TestChangesAddToGit(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	openChanges(t, a, sc)

	typeRunes(sc, "GkA")
	waitFor(t, a, sc, "added 1 file(s) to git")
	waitFor(t, a, sc, "3 of 3 to commit")
	waitGone(t, a, sc, "Unversioned Files")
	if got := gitIn(t, p.clone, "status", "--porcelain", "--untracked-files=all"); !strings.Contains(got, "A  docs/new.md") {
		t.Errorf("git does not have new.md as added: %q", got)
	}
	if st, ok := textStyle(a, sc, "new.md "); !ok {
		t.Error("new.md left the list")
	} else if fg, _, _ := st.Decompose(); !sameColour(fg, role("files.added")) {
		t.Errorf("new.md is not drawn as added: %v", fg)
	}

	// A versioned file has nothing to add.
	typeRunes(sc, "gjA")
	waitFor(t, a, sc, "nothing unversioned is marked")
}

// TestChangesHintFollowsTheRow: the hint names what can be done with the
// row under the cursor - rollback for a versioned file, add and delete for
// an unversioned one.
func TestChangesHintFollowsTheRow(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)
	waitFor(t, a, sc, "R rollback")
	if strings.Contains(a.screenText(sc), "A add to git") {
		t.Error("a versioned file is offered Add to Git")
	}
	typeRunes(sc, "G")
	waitFor(t, a, sc, "A add to git")
	waitFor(t, a, sc, "d delete")
	waitGone(t, a, sc, "R rollback")
}

// TestChangesCountsFiltersAndIgnores: each file says how many of its lines
// changed, quieter than its name; / narrows the list to the paths that
// match and Esc takes it away; I writes an unversioned file into
// .gitignore, which then is a change itself.
func TestChangesCountsFiltersAndIgnores(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := changesFixture(t, a)
	openChanges(t, a, sc)
	waitFor(t, a, sc, "a.txt +1 −1")
	waitFor(t, a, sc, "main.go +2 −2")
	if st, ok := textStyle(a, sc, "+2 −2"); !ok {
		t.Error("the counts are not drawn")
	} else if fg, _, _ := st.Decompose(); !sameColour(fg, iconShade(role("files.lines_added"))) {
		t.Errorf("the added count is in %v, not the quieter added colour", fg)
	}

	typeRunes(sc, "/main")
	waitGone(t, a, sc, "a.txt")
	waitFor(t, a, sc, "/ main")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "a.txt")

	typeRunes(sc, "GI")
	waitFor(t, a, sc, "ignored 1 file(s)")
	waitGone(t, a, sc, "new.md")
	waitFor(t, a, sc, ".gitignore")
	if got := readFileUI(t, p.clone, ".gitignore"); got != "/docs/new.md\n" {
		t.Errorf(".gitignore = %q", got)
	}
}

// TestChangesCopiesPathsFoldersAndAPatch: y copies the marked files' paths,
// their folders, or a patch of them that applies elsewhere. Serial: the
// clipboard is swapped.
func TestChangesCopiesPathsFoldersAndAPatch(t *testing.T) {
	c := fakeClipboard(t, true)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changesFixture(t, a)
	openChanges(t, a, sc)

	typeRunes(sc, "  y")
	waitFor(t, a, sc, "Copy 2 marked files")
	for _, want := range []string{"Paths from the repository root", "Absolute paths", "Folder from the repository root", "Patch"} {
		waitFor(t, a, sc, want)
	}
	typeRunes(sc, "y")
	waitFor(t, a, sc, "copied")
	if got := c.get(); got != "a.txt\nmain.go" {
		t.Errorf("paths = %q", got)
	}

	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy 2 marked files")
	typeRunes(sc, "jjjjy")
	waitFor(t, a, sc, "copied")
	patch := c.get()
	for _, want := range []string{"diff --git a/a.txt b/a.txt", "+edited", "+// Count counts requests."} {
		if !strings.Contains(patch, want) {
			t.Errorf("the patch lacks %q:\n%s", want, patch)
		}
	}
}
