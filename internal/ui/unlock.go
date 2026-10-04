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

// maskRune hides the passphrase as it is typed.
const maskRune = '•'

// showUnlock renders the passphrase dialog: opening the token vault, or
// creating it on a first run. The key derivation runs on a background
// goroutine so the interface stays responsive, and the passphrase buffer is
// wiped as soon as it has been used.
func (a *App) showUnlock() {
	_, statErr := os.Stat(a.cfg.VaultPath())
	creating := os.IsNotExist(statErr)

	msg := tview.NewTextView().SetDynamicColors(true)
	pass := passphraseField("Passphrase")
	repeat := passphraseField("Repeat")

	if creating {
		msg.SetText(tag(colMuted) + "Welcome. Choose a passphrase; your GitLab tokens\nare encrypted with it and never stored in the open." + tagEnd)
	} else {
		msg.SetText(tag(colMuted) + "The GitLab tokens are encrypted." + tagEnd)
	}

	hint := tview.NewTextView().SetDynamicColors(true).
		SetText(tag(colDim) + "Enter  unlock        Esc  quit" + tagEnd)

	form := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(msg, 2, 0, false).
		AddItem(pass, 1, 0, true)
	height := 9
	if creating {
		form.AddItem(repeat, 1, 0, false)
		height++
	}
	form.AddItem(nil, 1, 0, false).AddItem(hint, 1, 0, false)
	form.SetBorderPadding(1, 1, 3, 3)
	box(form.Box, "unagit")

	busy := false
	fail := func(text string) {
		msg.SetText(tag(colBad) + tview.Escape(text) + tagEnd + "\n" +
			tag(colMuted) + "Try again, or press Esc to quit." + tagEnd)
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
			msg.SetText(tag(colMuted) + "Opening with the passphrase in the Keychain…" + tagEnd)
		} else {
			msg.SetText(tag(colMuted) + "Deriving the key…" + tagEnd)
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
				a.tv.SetFocus(pass)
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
			a.tv.SetFocus(pass)
			fail("The two entries do not match.")
			return
		}
		clearMasked(pass)
		clearMasked(repeat)
		attempt(entered, false)
	}

	pass.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEsc:
			a.tv.Stop()
		case tcell.KeyEnter, tcell.KeyTab:
			if creating {
				a.tv.SetFocus(repeat)
				return
			}
			submit()
		}
	})
	repeat.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEsc:
			a.tv.Stop()
		case tcell.KeyEnter:
			submit()
		case tcell.KeyBacktab:
			a.tv.SetFocus(pass)
		}
	})

	a.pages.AddPage(pageUnlock, modalFixed(form, 64, height), true, true)
	a.tv.SetFocus(pass)

	// A remembered passphrase is tried first; the dialog is there if it is
	// missing, refused, or no longer the right one.
	if !creating && a.cfg.RememberPassphrase && passphraseStore.available() {
		busy = true
		pass.SetDisabled(true)
		msg.SetText(tag(colMuted) + "Asking the Keychain…" + tagEnd)
		go func() {
			remembered, err := passphraseStore.get()
			a.tv.QueueUpdateDraw(func() {
				busy = false
				pass.SetDisabled(false)
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

// passphraseField is a masked input styled like the rest of the interface.
func passphraseField(label string) *tview.InputField {
	return tview.NewInputField().
		SetLabel(pad(label, 12)).
		SetMaskCharacter(maskRune).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(colText).
		SetLabelColor(colMuted)
}

// clearMasked empties a masked input field.
//
// tview v0.42's InputField.SetText leaves part of the old value behind when a
// mask character is set, so the mask is lifted for the reset. Without this a
// second attempt would start with leftovers from the first one.
func clearMasked(input *tview.InputField) {
	input.SetMaskCharacter(0)
	input.SetText("")
	input.SetMaskCharacter(maskRune)
}

// pad right-pads a label so a column of fields lines up.
func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}
