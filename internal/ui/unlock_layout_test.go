package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/secret"
)

// newLockedApp is newLockedTestApp with the configuration's theme and a
// first run's missing vault to choose from.
func newLockedApp(t *testing.T, themeName string, firstRun bool) (*App, tcell.SimulationScreen) {
	t.Helper()
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	cfg.Theme = themeName
	must(t, cfg.Save())
	if !firstRun {
		data, err := lockedLayoutVault()
		must(t, err)
		must(t, os.WriteFile(cfg.VaultPath(), data, 0o600))
	}
	a, sc, _ := startLocked(t, cfg)
	return a, sc
}

// The layout needs a valid existing vault to choose the ordinary dialog.
// Each app gets its own copy; unlocking tests still derive and open real keys.
var lockedLayoutVault = sync.OnceValues(func() ([]byte, error) {
	v, err := secret.NewVault([]byte("hunter2"))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(fixtureRoot, "layout-vault.enc")
	if err := v.Save(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
})

// startLocked is startApp for a locked app, with a channel closed when it
// has stopped.
func startLocked(t *testing.T, cfg *config.Config) (*App, tcell.SimulationScreen, chan struct{}) {
	t.Helper()
	return startAppWithStop(t, NewLocked(cfg))
}

// unlockForm is the unlock dialog's form, once it is in front.
func unlockForm(t *testing.T, a *App) *tview.Form {
	t.Helper()
	return onLoop(a, func() *tview.Form {
		if name, prim := a.pages.GetFrontPage(); name == pageUnlock {
			if box, ok := prim.(*modalBox); ok {
				form, _ := box.content.(*tview.Form)
				return form
			}
		}
		return nil
	})
}

// TestUnlockFitsItsFrame draws the unlock dialog, the usual one and a first
// run's, at several terminal sizes: every field and button inside the frame,
// the frame whole on every row, the labels and the buttons' letters there.
func TestUnlockFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, firstRun := range []bool{false, true} {
		for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}, {66, 20}} {
			t.Run(fmt.Sprintf("first=%v/%dx%d", firstRun, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				a, sc := newLockedApp(t, defaultThemeName, firstRun)
				resizeApp(a, sc, size.w, size.h)
				waitFor(t, a, sc, "Passphrase")
				waitFor(t, a, sc, "Unlock")
				waitFor(t, a, sc, "Quit")
				if firstRun {
					waitFor(t, a, sc, "Repeat")
				}
				form := unlockForm(t, a)
				if form == nil {
					t.Fatalf("the unlock dialog is not a form in front:\n%s", a.screenText(sc))
				}
				inner, frame := onLoop(a, func() [2]rect {
					x, y, w, h := form.GetInnerRect()
					fx, fy, fw, fh := form.GetRect()
					return [2]rect{{x, y, w, h}, {fx, fy, fw, fh}}
				})[0], onLoop(a, func() rect {
					x, y, w, h := form.GetRect()
					return rect{x, y, w, h}
				})
				onLoop(a, func() bool {
					for i := 0; i < form.GetFormItemCount(); i++ {
						x, y, w, h := form.GetFormItem(i).GetRect()
						if r := (rect{x, y, w, h}); !r.within(inner) {
							t.Errorf("item %d is drawn at %v, outside %v", i, r, inner)
						}
					}
					for i := 0; i < form.GetButtonCount(); i++ {
						x, y, w, h := form.GetButton(i).GetRect()
						if r := (rect{x, y, w, h}); !r.within(inner) {
							t.Errorf("button %d is drawn at %v, outside %v", i, r, inner)
						}
					}
					return true
				})
				for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
					if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
						t.Fatalf("the frame's right edge is broken on row %d:\n%s", y, a.screenText(sc))
					}
				}
				assertLegible(t, a, sc, "the unlock dialog")
			})
		}
	}
}

// TestUnlockQuitsFromNormal: Esc stops typing, as in every form, and then q
// or a second Esc ends unagit rather than leaving it locked and empty.
func TestUnlockQuitsFromNormal(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"q", "esc"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			cfg := writeTestConfig(t, fakeGitLab(t).URL)
			v, err := secret.NewVault([]byte("hunter2"))
			must(t, err)
			must(t, v.Save(cfg.VaultPath()))
			a, sc, stopped := startLocked(t, cfg)
			waitFor(t, a, sc, "Passphrase")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitFor(t, a, sc, "q quit")
			if key == "q" {
				typeRunes(sc, "q")
			} else {
				sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			}
			select {
			case <-stopped:
			case <-time.After(patience):
				t.Fatalf("unagit did not quit:\n%s", a.screenText(sc))
			}
		})
	}
}
