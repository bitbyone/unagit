package ui

import (
	"errors"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/keychain"
	"github.com/tobola/unagit/internal/secret"
)

// showUnlock renders the passphrase dialog: opening the token vault, or
// creating it on a first run. The key derivation runs on a background
// goroutine so the interface stays responsive, and the passphrase buffer is
// wiped as soon as it has been used.
func (a *App) showUnlock() {
	_, statErr := os.Stat(a.cfg.VaultPath())
	creating := os.IsNotExist(statErr)

	// The dialog is a form like every other, so it wears the theme the same
	// way: a field one can see, buttons with their letters lit.
	form := tview.NewForm()
	styleForm(form)
	pass := addPassword(form, "Passphrase", 0)
	repeat := pass
	height := 9
	if creating {
		repeat = addPassword(form, "Repeat", 0)
		height += 2
	}
	// What the dialog has to say goes under the fields, where every form
	// keeps its note.
	form.AddTextView("", "", 0, 2, true, false)
	msg := form.GetFormItem(form.GetFormItemCount() - 1).(*tview.TextView)
	say := func(text string) { msg.SetText(tag(colMuted) + text + tagEnd) }
	if creating {
		say("Welcome. Choose a passphrase; your tokens are\nencrypted with it and never stored in the open.")
	} else {
		say("The tokens are encrypted.")
	}

	// typeAgain puts the cursor back in the passphrase, typing: a field that
	// was disabled while the key was derived lost the focus to the buttons.
	typeAgain := func() {
		form.SetFocus(0)
		a.tv.SetFocus(form)
		if mode := a.formModes[form]; mode != nil {
			mode.insert = true
		}
	}

	busy := false
	fail := func(text string) {
		msg.SetText(tag(colBad) + tview.Escape(text) + tagEnd + "\n" +
			tag(colMuted) + "Try again, or press q to quit." + tagEnd)
	}

	// attempt opens the vault with a passphrase, typed or remembered. A typed
	// one that works is remembered again when the user chose that - which is
	// what puts the keychain right after the passphrase changed elsewhere.
	var attempt func(entered []byte, remembered bool)
	attempt = func(entered []byte, remembered bool) {
		busy = true
		pass.SetDisabled(true)
		repeat.SetDisabled(true)
		if remembered {
			say("Opening with the passphrase in the Keychain…")
		} else {
			say("Deriving the key…")
		}
		remember := a.cfg.RememberPassphrase && !remembered && passphraseStore.available()
		go func() {
			vault, isNew, err := secret.OpenOrCreate(a.cfg.VaultPath(), entered)
			var imported bool
			if err == nil && len(a.cfg.Instances) > 0 {
				// A token.enc from before unagit had a vault belongs to the
				// instance the old single-server config became.
				imported, _ = vault.ImportSingleToken(a.cfg.LegacyTokenPath(), a.cfg.Instances[0].ID, entered)
			}
			var rememberErr error
			if err == nil && remember {
				rememberErr = passphraseStore.set(entered)
			}
			for i := range entered {
				entered[i] = 0
			}
			a.tv.QueueUpdateDraw(func() {
				busy = false
				pass.SetDisabled(false)
				repeat.SetDisabled(false)
				clearMasked(pass)
				clearMasked(repeat)
				typeAgain()
				if err != nil {
					switch {
					case err == secret.ErrWrongPassphrase && remembered:
						// One line: the dialog has two, and the second says
						// what to do.
						fail("The Keychain's passphrase no longer works.")
					case err == secret.ErrWrongPassphrase:
						fail("Wrong passphrase.")
					default:
						fail(err.Error())
					}
					return
				}
				a.setVault(vault)
				if isNew || imported {
					if err := vault.Save(a.cfg.VaultPath()); err != nil {
						fail(err.Error())
						return
					}
					a.rebuildClients()
				}
				a.forgetForm(form)
				a.pages.RemovePage(pageUnlock)
				a.start()
				if imported {
					a.done("Imported the token from token.enc; you can delete that file.")
				}
				if rememberErr != nil {
					a.errorf("the Keychain was not updated: %v", rememberErr)
				}
			})
		}()
	}

	submit := func() {
		if busy {
			return
		}
		entered := []byte(pass.GetText())
		if len(entered) == 0 {
			return
		}
		if creating && pass.GetText() != repeat.GetText() {
			clearMasked(pass)
			clearMasked(repeat)
			typeAgain()
			fail("The two entries do not match.")
			return
		}
		clearMasked(pass)
		clearMasked(repeat)
		attempt(entered, false)
	}

	form.AddButton("Unlock", submit)
	form.AddButton("Quit", a.tv.Stop)
	form.SetFocus(0)
	a.showFormOn(pageUnlock, "unagit", form, 64, height, a.tv.Stop)
	// Enter in the last field unlocks rather than moving on to the buttons,
	// which is what the form does with it otherwise.
	moving := form.GetInputCapture()
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEnter && form.HasFocus() && repeat.HasFocus() {
			if _, mode := a.focusedForm(); mode != nil && mode.insert {
				submit()
				return nil
			}
		}
		return moving(ev)
	})

	// A remembered passphrase is tried first; the dialog is there if it is
	// missing, refused, or no longer the right one.
	if !creating && a.cfg.RememberPassphrase && passphraseStore.available() {
		busy = true
		pass.SetDisabled(true)
		say("Asking the Keychain…")
		go func() {
			remembered, err := passphraseStore.get()
			a.tv.QueueUpdateDraw(func() {
				busy = false
				pass.SetDisabled(false)
				typeAgain()
				if err != nil {
					why := "it refused"
					if errors.Is(err, keychain.ErrNotFound) {
						why = "none stored"
					}
					fail("The Keychain gave no passphrase (" + why + ").")
					return
				}
				attempt(remembered, true)
			})
		}()
	}
}

// passphraseStore is where a remembered passphrase is kept: the macOS
// keychain, readable by the unagit binary alone. Tests replace it.
var passphraseStore = struct {
	available func() bool
	get       func() ([]byte, error)
	set       func([]byte) error
	forget    func() error
}{
	available: keychain.Available,
	get:       func() ([]byte, error) { return keychain.Get(keychainService, config.VaultPath()) },
	set:       func(p []byte) error { return keychain.Set(keychainService, config.VaultPath(), p) },
	forget:    func() error { return keychain.Delete(keychainService, config.VaultPath()) },
}

// keychainService names the item; the account is the vault's path, so two
// configurations keep two passphrases.
const keychainService = "unagit vault passphrase"

// pad right-pads a label so a column of fields lines up.
func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}
