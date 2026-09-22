package ui

import (
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/secret"
)

// maskRune hides the passphrase as it is typed.
const maskRune = '•'

// showUnlock renders the passphrase dialog: opening the token vault, or
// creating it on a first run. The key derivation runs on a background
// goroutine so the interface stays responsive, and the passphrase buffer is
// wiped as soon as it has been used.
func (a *App) showUnlock() {
	_, statErr := os.Stat(config.VaultPath())
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
		busy = true
		clearMasked(pass)
		clearMasked(repeat)
		pass.SetDisabled(true)
		repeat.SetDisabled(true)
		msg.SetText(tag(colMuted) + "Deriving the key…" + tagEnd)

		go func() {
			vault, isNew, err := secret.OpenOrCreate(config.VaultPath(), entered)
			var imported bool
			if err == nil && len(a.cfg.Instances) > 0 {
				// A token.enc from before unagit had a vault belongs to the
				// instance the old single-server config became.
				imported, _ = vault.ImportSingleToken(config.LegacyTokenPath(), a.cfg.Instances[0].ID, entered)
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
					if err == secret.ErrWrongPassphrase {
						fail("Wrong passphrase.")
					} else {
						fail(err.Error())
					}
					return
				}
				a.setVault(vault)
				if isNew || imported {
					if err := vault.Save(config.VaultPath()); err != nil {
						fail(err.Error())
						return
					}
					a.rebuildClients()
				}
				a.pages.RemovePage(pageUnlock)
				a.start()
				if imported {
					a.note("Imported the token from token.enc; you can delete that file.")
				}
			})
		}()
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
}

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
