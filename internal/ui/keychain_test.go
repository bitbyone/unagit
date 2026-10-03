package ui

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/keychain"
	"github.com/tobola/unagit/internal/secret"
)

// fakeKeychain stands in for the macOS keychain, so no test reads or writes
// the real one.
type fakeKeychain struct {
	mu     sync.Mutex
	stored []byte
}

func (k *fakeKeychain) value() string { k.mu.Lock(); defer k.mu.Unlock(); return string(k.stored) }

func useFakeKeychain(t *testing.T, stored string) *fakeKeychain {
	t.Helper()
	k := &fakeKeychain{}
	if stored != "" {
		k.stored = []byte(stored)
	}
	saved := passphraseStore
	passphraseStore.available = func() bool { return true }
	passphraseStore.get = func() ([]byte, error) {
		k.mu.Lock()
		defer k.mu.Unlock()
		if k.stored == nil {
			return nil, keychain.ErrNotFound
		}
		return append([]byte(nil), k.stored...), nil
	}
	passphraseStore.set = func(p []byte) error {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.stored = append([]byte(nil), p...)
		return nil
	}
	passphraseStore.forget = func() error {
		k.mu.Lock()
		defer k.mu.Unlock()
		k.stored = nil
		return nil
	}
	t.Cleanup(func() { passphraseStore = saved })
	return k
}

// newRememberingApp is newLockedTestApp with the passphrase remembered.
func newRememberingApp(t *testing.T, passphrase string) (*App, tcell.SimulationScreen) {
	t.Helper()
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	cfg.RememberPassphrase = true
	v, err := secret.NewVault([]byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	v.Set(cfg.Instances[0].ID, "glpat-test-token")
	if err := v.Save(config.VaultPath()); err != nil {
		t.Fatal(err)
	}
	return startApp(t, NewLocked(cfg))
}

func TestARememberedPassphraseOpensWithoutAsking(t *testing.T) {
	useFakeKeychain(t, "hunter2")
	a, sc := newRememberingApp(t, "hunter2")
	waitFor(t, a, sc, "acme/gateway")
	waitGone(t, a, sc, "Passphrase")
	if onLoop(a, func() string { return a.vault.Token(a.cfg.Instances[0].ID) }) != "glpat-test-token" {
		t.Error("the vault was not opened")
	}
}

// TestAStaleRememberedPassphraseAsksAndIsPutRight: the passphrase changed
// elsewhere; the dialog says so, and the one typed replaces the stale one.
func TestAStaleRememberedPassphraseAsksAndIsPutRight(t *testing.T) {
	k := useFakeKeychain(t, "the old one")
	a, sc := newRememberingApp(t, "hunter2")
	waitFor(t, a, sc, "The Keychain's passphrase no longer works.")
	waitFor(t, a, sc, "Try again")
	typeRunes(sc, "hunter2")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway")
	if got := k.value(); got != "hunter2" {
		t.Errorf("the Keychain holds %q, want it put right", got)
	}
}

func TestAMissingRememberedPassphraseAsks(t *testing.T) {
	useFakeKeychain(t, "")
	a, sc := newRememberingApp(t, "hunter2")
	waitFor(t, a, sc, "The Keychain gave no passphrase (none stored).")
	waitFor(t, a, sc, "Try again")
	typeRunes(sc, "hunter2")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway")
}

// TestSecurityRemembersAndForgets: k asks for the passphrase, checks it
// against the vault, remembers it and says so; k again forgets it.
func TestSecurityRemembersAndForgets(t *testing.T) {
	k := useFakeKeychain(t, "")
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool { return a.vault.Save(config.VaultPath()) == nil })
	openSection(t, a, sc, sectionSecurity)
	waitFor(t, a, sc, "Keychain not used")
	waitFor(t, a, sc, "k keychain")

	typeRunes(sc, "k")
	waitFor(t, a, sc, "Remember the passphrase")
	typeRunes(sc, "wrong")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing
	typeRunes(sc, "r")
	waitFor(t, a, sc, "wrong passphrase")
	if k.value() != "" {
		t.Fatal("a wrong passphrase was remembered")
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // the message over the form
	waitGone(t, a, sc, "wrong passphrase")
	typeRunes(sc, "i")
	typeRunes(sc, "test-passphrase")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing
	typeRunes(sc, "r")
	waitFor(t, a, sc, "remembers the passphrase")
	if got := k.value(); got != "test-passphrase" {
		t.Errorf("the Keychain holds %q", got)
	}
	if saved, err := config.Load(); err != nil || !saved.RememberPassphrase {
		t.Errorf("the choice was not saved: %v", err)
	}

	typeRunes(sc, "k")
	waitFor(t, a, sc, "Keychain not used")
	if k.value() != "" {
		t.Error("forgetting left the passphrase in the Keychain")
	}
	if saved, _ := config.Load(); saved.RememberPassphrase {
		t.Error("forgetting was not saved")
	}
}

// TestRememberFormFits draws the dialog at several sizes: the field inside
// its frame, the frame whole, the words on screen.
func TestRememberFormFits(t *testing.T) {
	useFakeKeychain(t, "")
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, size.w, size.h)
			openSection(t, a, sc, sectionSecurity)
			typeRunes(sc, "k")
			waitFor(t, a, sc, "Remember the passphrase")
			text := a.screenText(sc)
			for _, want := range []string{"Passphrase", "only unagit", "may read it", "Remember", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			frame := onLoop(a, func() rect {
				_, p := a.pages.GetFrontPage()
				x, y, w, h := p.(*modalBox).content.(interface {
					GetRect() (int, int, int, int)
				}).GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is drawn over (%q):\n%s", y, r, text)
					break
				}
			}
			assertLegible(t, a, sc, "the remember dialog")
		})
	}
}
