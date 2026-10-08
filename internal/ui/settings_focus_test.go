package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
)

// TestSavingGeneralKeepsTheKeyboard: Save and Revert rebuild the form they
// are pressed in, and the keyboard must not stay with the button that went
// away - Esc still leads back to the sections, and the form still answers.
func TestSavingGeneralKeepsTheKeyboard(t *testing.T) {
	t.Parallel()
	for _, button := range []string{"Save", "Revert"} {
		t.Run(button, func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			openSection(t, a, sc, sectionGeneral)
			waitFor(t, a, sc, "Default root")

			form := a.settings.general
			setField(t, a, form, 0, t.TempDir())
			pressButton(t, a, sc, form, button)

			inForm := onLoop(a, func() bool { return form.HasFocus() })
			if !inForm {
				t.Fatalf("after %s the keyboard left the form for %T", button,
					onLoop(a, func() tview.Primitive { return a.tv.GetFocus() }))
			}
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitFocus(t, a, func() bool { return a.settings.list.HasFocus() })
		})
	}

	// Esc and s save from inside a field, and the field is rebuilt as well.
	t.Run("Esc s", func(t *testing.T) {
		a, sc := newTestApp(t)
		waitFor(t, a, sc, "acme/gateway")
		openSection(t, a, sc, sectionGeneral)
		waitFor(t, a, sc, "Default root")

		form := a.settings.general
		dir := t.TempDir()
		setField(t, a, form, 0, dir)
		onLoop(a, func() bool {
			form.SetFocus(0)
			a.tv.SetFocus(form)
			a.formModes[form].insert = true
			return true
		})
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing
		typeRunes(sc, "s")
		waitFocus(t, a, func() bool { return a.cfg.RootDir == dir })
		waitFocus(t, a, func() bool {
			field, _ := form.GetFocusedItemIndex()
			return form.HasFocus() && field == 0
		})
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitFocus(t, a, func() bool { return a.settings.list.HasFocus() })
	})
}

func waitFocus(t *testing.T, a *App, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if onLoop(a, ok) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("focus never arrived; it is on %T", onLoop(a, func() tview.Primitive { return a.tv.GetFocus() }))
}

// TestANewRootReachesGroupsAndClones: the root saved in General is the one
// Groups & roots shows and the one a clone goes to - straight away, and after
// a restart from the saved file.
func TestANewRootReachesGroupsAndClones(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGeneral)
	waitFor(t, a, sc, "Default root")

	root := filepath.Join(t.TempDir(), "new-root")
	setField(t, a, a.settings.general, 0, root)
	pressButton(t, a, sc, a.settings.general, "Save")

	check := func(a *App, sc tcell.SimulationScreen, when string) {
		t.Helper()
		resizeApp(a, sc, 160, 44)
		openSection(t, a, sc, sectionGroups)
		waitFor(t, a, sc, "→ "+tildePath(root))
		inst := onLoop(a, func() string { return a.cfg.Instances[0].ID })
		if dir := onLoop(a, func() string { return a.projectDir(inst, "acme/gateway") }); dir != filepath.Join(root, "acme", "gateway") {
			t.Errorf("%s a clone goes to %s, not under %s", when, dir, root)
		}
	}
	check(a, sc, "after Save")

	saved, err := config.LoadFrom(a.cfg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	b, bsc := startApp(t, newApp(saved, testVault(t, saved)))
	waitFor(t, b, bsc, "acme/gateway")
	check(b, bsc, "after a restart")
}

// TestHGoesBackToTheSections: in every section h leads back to the list of
// sections wherever the pane has no use of its own for it - a form in
// NORMAL, a list, a text - and in the groups tree it folds first.
func TestHGoesBackToTheSections(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	for _, section := range []int{sectionGeneral, sectionGitLab, sectionGitHub, sectionTags, sectionTheme, sectionSecurity} {
		openSection(t, a, sc, section)
		waitFocus(t, a, func() bool { return a.settings.contentFocused })
		if section == sectionGeneral {
			// It opens typing, where h is a letter.
			changeOnLoop(a, func() { a.formModes[a.settings.general].insert = false })
		}
		typeRunes(sc, "h")
		waitFocus(t, a, func() bool { return a.settings.list.HasFocus() && !a.settings.contentFocused })
	}

	// The tree: h folds an open server, l opens it again, and h on a
	// folded one goes back.
	openSection(t, a, sc, sectionGroups)
	waitFocus(t, a, func() bool { return a.settings.tree.HasFocus() })
	expanded := func() bool { return a.settings.tree.GetCurrentNode().IsExpanded() }
	if !onLoop(a, expanded) {
		t.Fatal("the server starts folded")
	}
	typeRunes(sc, "h")
	waitFocus(t, a, func() bool { return !expanded() && a.settings.tree.HasFocus() })
	typeRunes(sc, "l")
	waitFocus(t, a, func() bool { return expanded() })
	changeOnLoop(a, a.settings.fillTree)
	if !onLoop(a, expanded) {
		t.Fatal("the tree forgot what l unfolded")
	}
	typeRunes(sc, "hh")
	waitFocus(t, a, func() bool { return a.settings.list.HasFocus() })
	changeOnLoop(a, a.settings.fillTree)
	if onLoop(a, expanded) {
		t.Fatal("the tree forgot what h folded")
	}
}
