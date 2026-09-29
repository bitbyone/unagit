package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
)

// fakeEditors puts stand-ins for nvim and zed on an otherwise empty PATH and
// hides the machine's own applications. Zed writes where it was started and
// what it was asked to open into the file it returns the path of.
func fakeEditors(t *testing.T) (marker string) {
	t.Helper()
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

// TestEditorsCardChoosesTheFavourite: the card lists what is installed and
// what is not, and f makes another one the favourite, saved.
func TestEditorsCardChoosesTheFavourite(t *testing.T) {
	fakeEditors(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionIntegrations)
	typeRunes(sc, "j") // from Incomm to Editors
	waitFor(t, a, sc, "f favourite")
	text := a.screenText(sc)
	for _, want := range []string{"★ Neovim", "terminal", "Zed", "window", "IntelliJ IDEA  not found", "VS Code"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not on the card:\n%s", want, text)
		}
	}
	assertLegible(t, a, sc, "the editors card")

	typeRunes(sc, "f")
	waitFor(t, a, sc, "Favourite editor")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "★ Zed")
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.FavouriteEditor != editors.Zed {
		t.Errorf("favourite saved as %q", saved.FavouriteEditor)
	}
}

// TestEditorsCardFits draws the card at several sizes: every editor on screen,
// the frame whole.
func TestEditorsCardFits(t *testing.T) {
	fakeEditors(t)
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, size.w, size.h)
			openSection(t, a, sc, sectionIntegrations)
			typeRunes(sc, "j")
			waitFor(t, a, sc, "f favourite")
			text := a.screenText(sc)
			for _, want := range []string{"Neovim", "IntelliJ IDEA", "VS Code", "Zed", "c check"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			// The card is drawn inside the panel, and nothing is drawn over
			// the status line below it.
			inside := onLoop(a, func() bool {
				v := a.settings.integrations
				px, py, pw, ph := v.GetInnerRect()
				x, y, w, h := v.cards[len(v.cards)-1].view.GetRect()
				return rect{x, y, w, h}.within(rect{px, py, pw, ph})
			})
			if !inside {
				t.Errorf("the editors card overflows its panel:\n%s", text)
			}
			lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
			if last := lines[len(lines)-1]; !strings.Contains(last, "? help") {
				t.Errorf("the status line was drawn over: %q\n%s", last, text)
			}
			for _, line := range lines {
				if strings.Contains(line, "Neovim") && !strings.Contains(line, "terminal") {
					t.Errorf("an editor's line wraps: %q", line)
				}
			}
			assertLegible(t, a, sc, "the editors card")
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
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/window")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/window")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "feat/window")

	sc.InjectKey(tcell.KeyRune, 'o', tcell.ModAlt)
	waitFor(t, a, sc, "Open with")
	text := a.screenText(sc)
	if !strings.Contains(text, "★ Neovim") || !strings.Contains(text, "Zed") {
		t.Fatalf("the editors are not offered, favourite first:\n%s", text)
	}
	if strings.Contains(text, "by name") {
		t.Errorf("Alt-O acted as o and reordered the list:\n%s", text)
	}
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "opened in Zed")

	deadline := time.Now().Add(5 * time.Second)
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
