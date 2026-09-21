package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/secret"
)

// maskRune hides the passphrase as it is typed.
const maskRune = '\u2022'

// showUnlock renders the passphrase dialog. The key derivation runs on a
// background goroutine so the interface stays responsive, and the passphrase
// buffer is wiped as soon as it has been used.
func (a *App) showUnlock() {
	input := tview.NewInputField().
		SetLabel("Passphrase  ").
		SetMaskCharacter(maskRune).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(colText).
		SetLabelColor(colMuted)

	msg := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)
	msg.SetText(tag(colMuted) + "The GitLab token is encrypted." + tagEnd)

	hint := tview.NewTextView().SetDynamicColors(true).
		SetText(tag(colDim) + "Enter  unlock        Esc  quit" + tagEnd)

	form := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(msg, 2, 0, false).
		AddItem(input, 1, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(hint, 1, 0, false)
	form.SetBorderPadding(1, 1, 3, 3)
	box(form.Box, "unagit")

	busy := false
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc {
			a.tv.Stop()
			return
		}
		if key != tcell.KeyEnter || busy {
			return
		}
		pass := []byte(input.GetText())
		if len(pass) == 0 {
			return
		}
		busy = true
		clearMasked(input)
		input.SetDisabled(true)
		msg.SetText("[" + colMuted.String() + "]Deriving the key…[-]")

		go func() {
			token, err := secret.Decrypt(a.blob, pass)
			for i := range pass {
				pass[i] = 0
			}
			a.tv.QueueUpdateDraw(func() {
				busy = false
				input.SetDisabled(false)
				// Wipe anything typed while the key was being derived.
				clearMasked(input)
				a.tv.SetFocus(input)
				if err != nil {
					text := err.Error()
					if err == secret.ErrWrongPassphrase {
						text = "Wrong passphrase."
					}
					msg.SetText(tag(colBad) + tview.Escape(text) + tagEnd + "\n" +
						tag(colMuted) + "Try again, or press Esc to quit." + tagEnd)
					return
				}
				a.setToken(string(token))
				for i := range token {
					token[i] = 0
				}
				a.pages.RemovePage(pageUnlock)
				a.start()
			})
		}()
	})

	a.pages.AddPage(pageUnlock, modalFixed(form, 62, 9), true, true)
	a.tv.SetFocus(input)
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
