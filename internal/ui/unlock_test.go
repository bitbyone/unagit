package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/secret"
)

func newLockedTestApp(t *testing.T, passphrase string) (*App, tcell.SimulationScreen) {
	t.Helper()
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	blob, err := secret.Encrypt([]byte("glpat-test-token"), []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}
	return startApp(t, NewLocked(cfg, blob))
}

func TestUnlockModalGatesTheInterface(t *testing.T) {
	a, sc := newLockedTestApp(t, "hunter2")

	waitFor(t, a, sc, "Passphrase")
	waitFor(t, a, sc, "The GitLab token is encrypted")
	if got := a.screenText(sc); contains(got, "acme/gateway") {
		t.Fatal("the project list was visible before unlocking")
	}

	typeRunes(sc, "hunter2")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "acme/gateway")
	waitGone(t, a, sc, "Passphrase")
	if a.token != "glpat-test-token" {
		t.Errorf("token = %q", a.token)
	}
	if a.client == nil || a.ws == nil {
		t.Error("the GitLab client was not wired up after unlocking")
	}
}

func TestUnlockRejectsTheWrongPassphrase(t *testing.T) {
	a, sc := newLockedTestApp(t, "hunter2")
	waitFor(t, a, sc, "Passphrase")

	typeRunes(sc, "nope")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "Wrong passphrase")
	if a.token != "" {
		t.Error("a token was set despite the wrong passphrase")
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
