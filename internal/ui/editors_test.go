package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
)

// fakeEditors puts stand-ins for nvim and zed on an otherwise empty PATH and
// hides the machine's own applications. Zed writes where it was started and
// what it was asked to open into the file it returns the path of.
func fakeEditors(t *testing.T) (marker string) {
	t.Helper()
	saved := editors.ScriptDirs
	editors.ScriptDirs = func() []string { return nil }
	t.Cleanup(func() { editors.ScriptDirs = saved })
	return fakeEditorsOnPath(t)
}

// useFavourite makes id the favourite and forgets the fixture's custom
// editor, as the machine is now what fakeEditors made it.
func useFavourite(a *App, id string) {
	onLoop(a, func() bool {
		a.cfg.FavouriteEditor, a.cfg.Editor = id, ""
		a.settings.integrations.check()
		return true
	})
}

func fakeEditorsOnPath(t *testing.T) (marker string) {
	bin := t.TempDir()
	marker = filepath.Join(t.TempDir(), "zed-opened")
	must(t, os.WriteFile(filepath.Join(bin, "nvim"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	must(t, os.WriteFile(filepath.Join(bin, "zed"),
		[]byte("#!/bin/sh\nprintf '%s\\n%s\\n' \"$PWD\" \"$1\" > "+marker+"\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	saved := editors.AppDirs
	editors.AppDirs = func() []string { return nil }
	t.Cleanup(func() { editors.AppDirs = saved })
	return marker
}

// TestEditorCardsChooseTheFavourite: every editor has a card saying whether
// it is here, d makes it the default - the star moves to its title, and
// it is saved - d on the default leaves none, and e turns an editor off,
// so it is offered nowhere.
func TestEditorCardsChooseTheFavourite(t *testing.T) {
	fakeEditors(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	resizeApp(a, sc, 160, 44)
	openSection(t, a, sc, sectionIntegrations)
	waitFor(t, a, sc, "Neovim (default)")
	text := a.screenText(sc)
	for _, want := range []string{"╭ Neovim (default) ─", "╭ IntelliJ IDEA ─", "╭ VS Code ─", "╭ Zed ─", "╭ Custom ─", "not set up", "Review"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not on the page:\n%s", want, text)
		}
	}
	if line := lineAt(text, "╭ IntelliJ IDEA"); !strings.Contains(line, "not installed") {
		t.Errorf("IDEA is said to be here: %q", line)
	}
	assertLegible(t, a, sc, "the editor cards")

	saved := func() string {
		t.Helper()
		cfg, err := config.LoadFrom(a.cfg.Dir())
		if err != nil {
			t.Fatal(err)
		}
		return cfg.FavouriteEditor
	}
	focusCard(t, a, sc, "Zed")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Zed (default)")
	if strings.Contains(a.screenText(sc), "Neovim (default)") {
		t.Error("two favourites")
	}
	if got := saved(); got != editors.Zed {
		t.Errorf("favourite saved as %q", got)
	}
	typeRunes(sc, "d")
	waitGone(t, a, sc, "Zed (default)")
	if got := saved(); got != askEveryTime {
		t.Errorf("no favourite saved as %q", got)
	}

	// Off, Zed is offered nowhere and cannot be the favourite.
	typeRunes(sc, "e")
	waitEditorState(t, a, func() bool { return !a.editorOn(editors.Zed) })
	for _, e := range onLoop(a, a.editorsOn) {
		if e.ID == editors.Zed {
			t.Fatal("Zed is offered while off")
		}
	}
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Zed is off")
}

// TestTheCustomEditorIsSetUpOnItsCard: o on the Custom card sets its
// command, arguments and kind, and the card says what it runs.
func TestTheCustomEditorIsSetUpOnItsCard(t *testing.T) {
	fakeEditors(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	resizeApp(a, sc, 160, 44)
	openSection(t, a, sc, sectionIntegrations)
	focusCard(t, a, sc, "Custom")
	typeRunes(sc, "o")
	waitFor(t, a, sc, "Custom editor")
	assertLegible(t, a, sc, "the custom editor form")
	form := onLoop(a, func() *tview.Form { f, _ := a.focusedForm(); return f })
	setField(t, a, form, 0, "zed")
	setField(t, a, form, 1, "--new .")
	changeOnLoop(a, func() { form.GetFormItemByLabel(labelCustomWindow).(*tview.Checkbox).SetChecked(true) })
	pressButton(t, a, sc, form, "Save")
	waitFor(t, a, sc, "saved the custom editor")
	waitFor(t, a, sc, "zed --new . · opens a window")
	if a.cfg.Editor != "zed" || strings.Join(a.cfg.EditorArgs, " ") != "--new ." || !a.cfg.EditorWindow {
		t.Fatalf("custom editor = %q %q window %v", a.cfg.Editor, a.cfg.EditorArgs, a.cfg.EditorWindow)
	}
	if line := lineAt(a.screenText(sc), "╭ Custom"); !strings.Contains(line, "enabled") {
		t.Errorf("the custom editor is not on once found: %q", line)
	}
}

// TestEditorCardsFit draws each editor's card at several sizes, focused:
// inside the panel, nothing over the status line, legible.
func TestEditorCardsFit(t *testing.T) {
	fakeEditors(t)
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			useFavourite(a, "")
			resizeApp(a, sc, size.w, size.h)
			openSection(t, a, sc, sectionIntegrations)
			for _, name := range []string{"Neovim", "IntelliJ IDEA", "VS Code", "Zed", "Custom"} {
				focusCard(t, a, sc, name)
				waitFor(t, a, sc, "╭ "+name)
				text := a.screenText(sc)
				inside := onLoop(a, func() bool {
					v := a.settings.integrations
					px, py, pw, ph := v.GetInnerRect()
					x, y, w, h := v.card(name).view.GetRect()
					return rect{x, y, w, h}.within(rect{px, py, pw, ph})
				})
				if !inside {
					t.Errorf("the %s card overflows its panel:\n%s", name, text)
				}
				lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
				if last := lines[len(lines)-1]; !strings.Contains(last, "? help") {
					t.Errorf("the status line was drawn over: %q\n%s", last, text)
				}
				assertLegible(t, a, sc, "the "+name+" card")
			}
		})
	}
}

// TestAltOpensInAChosenWindowEditor: Alt-O asks which editor, and a window
// editor is started with the directory while the interface stays up - and
// the directory is on record for another terminal.
func TestAltOpensInAChosenWindowEditor(t *testing.T) {
	marker := fakeEditors(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/window")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/window")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "feat/window")

	sc.InjectKey(tcell.KeyRune, 'o', tcell.ModAlt)
	waitFor(t, a, sc, "Open with")
	text := a.screenText(sc)
	if !strings.Contains(text, "Neovim (default)") || !strings.Contains(text, "Zed") {
		t.Fatalf("the editors are not offered, favourite first:\n%s", text)
	}
	if strings.Contains(text, "by name") {
		t.Errorf("Alt-O acted as o and reordered the list:\n%s", text)
	}
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "opened in Zed")

	deadline := time.Now().Add(patience)
	var got []byte
	for time.Now().Before(deadline) {
		if got, _ = os.ReadFile(marker); len(got) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	real, _ := filepath.EvalSymlinks(dir)
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 2 || lines[1] != dir {
		t.Fatalf("zed was run as %q, want it to open %s", got, dir)
	}
	if pwd, _ := filepath.EvalSymlinks(lines[0]); pwd != real {
		t.Errorf("zed ran in %s, want %s", lines[0], dir)
	}
	listed := false
	for _, r := range a.sessions.List() {
		listed = listed || r.Dir == dir
	}
	if !listed {
		t.Error("the window editor's directory is not on record for unagit cd")
	}
}

// TestOpeningWithoutAFavouriteAsks: with no favourite - or one that is not
// installed - Ctrl-O asks which editor, exactly as Alt-O does, rather than
// quietly opening some other one.
func TestOpeningWithoutAFavouriteAsks(t *testing.T) {
	fakeEditors(t)
	for favourite, says := range map[string]string{
		"":           "no default yet",
		askEveryTime: "Open with",
		editors.Code: "the default, code, is not installed",
	} {
		t.Run("favourite="+favourite, func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			useFavourite(a, favourite)
			sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
			waitFor(t, a, sc, says)
			text := a.screenText(sc)
			if strings.Contains(text, "★") || !strings.Contains(text, "Neovim") || !strings.Contains(text, "Zed") {
				t.Errorf("the installed editors are not offered, unmarked:\n%s", text)
			}
			if strings.Contains(text, "Opening acme/gateway") {
				t.Errorf("it opened without asking:\n%s", text)
			}
		})
	}
}
