package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/secret"
)

func newLockedTestApp(t *testing.T, passphrase string) (*App, tcell.SimulationScreen) {
	t.Helper()
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
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

func TestUnlockModalGatesTheInterface(t *testing.T) {
	a, sc := newLockedTestApp(t, "hunter2")

	waitFor(t, a, sc, "Passphrase")
	waitFor(t, a, sc, "The GitLab tokens are encrypted")
	if got := a.screenText(sc); contains(got, "acme/gateway") {
		t.Fatal("the project list was visible before unlocking")
	}

	typeRunes(sc, "hunter2")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "acme/gateway")
	waitGone(t, a, sc, "Passphrase")
	if got := a.vault.Token(a.cfg.Instances[0].ID); got != "glpat-test-token" {
		t.Errorf("token = %q", got)
	}
	if a.client(a.cfg.Instances[0].ID) == nil {
		t.Error("the GitLab client was not wired up after unlocking")
	}
}

func TestUnlockRejectsTheWrongPassphrase(t *testing.T) {
	a, sc := newLockedTestApp(t, "hunter2")
	waitFor(t, a, sc, "Passphrase")

	typeRunes(sc, "nope")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "Wrong passphrase")
	if a.vault != nil {
		t.Error("the vault was opened despite the wrong passphrase")
	}

	// The dialog stays usable for another attempt.
	typeRunes(sc, "hunter2")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/gateway")
}

func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestClearMaskedReallyClears pins the workaround for tview's masked
// InputField: a plain SetText("") leaves part of the previous value behind.
func TestClearMaskedReallyClears(t *testing.T) {
	in := tview.NewInputField().SetMaskCharacter(maskRune)
	feed := func(s string) {
		h := in.InputHandler()
		for _, r := range s {
			h(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone), func(tview.Primitive) {})
		}
	}
	feed("wrong-one")
	clearMasked(in)
	if got := in.GetText(); got != "" {
		t.Fatalf("field not cleared: %q", got)
	}
	feed("second")
	if got := in.GetText(); got != "second" {
		t.Fatalf("leftovers from the first attempt: %q", got)
	}
}

// TestFirstRunCreatesTheVault: with nothing configured at all, the interface
// asks for a passphrase, creates the vault and points at the servers section.
func TestFirstRunCreatesTheVault(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a, sc := startApp(t, NewLocked(cfg))

	waitFor(t, a, sc, "Choose a passphrase")
	typeRunes(sc, "brand-new")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // on to the repeat field
	typeRunes(sc, "brand-new")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "Add your first GitLab server")
	waitFor(t, a, sc, "No servers yet")
	if a.vault == nil {
		t.Fatal("no vault after the first run")
	}
	if _, err := secret.OpenVault(config.VaultPath(), []byte("brand-new")); err != nil {
		t.Errorf("the vault was not written: %v", err)
	}
}

// TestFirstRunRejectsMismatchedPassphrases
func TestFirstRunRejectsMismatchedPassphrases(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	cfg, _ := config.Load()
	a, sc := startApp(t, NewLocked(cfg))

	waitFor(t, a, sc, "Choose a passphrase")
	typeRunes(sc, "one")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	typeRunes(sc, "two")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "do not match")
	if a.vault != nil {
		t.Error("a vault was created despite the mismatch")
	}
}
