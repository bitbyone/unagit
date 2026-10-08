package ui

import (
	"fmt"
	"slices"

	"github.com/tobola/unagit/internal/config"
)

// Settings › Notifications: how the news from the background is shown in
// unagit. Where it goes on the desktop is the Notifications card of
// Integrations, beside the terminals it goes through.

// toastChoices are the lengths a toast can stay, in seconds.
var toastChoices = []int{3, 5, 8, 10, 15, 20, 30, 60}

func (s *settingsView) fillNotifications() {
	cfg := s.app.cfg
	form := s.notices
	focused := form.HasFocus()
	item, button := form.GetFocusedItemIndex()
	form.Clear(true)
	seconds := cfg.ToastSeconds
	if seconds <= 0 {
		seconds = config.DefaultToastSeconds
	}
	options := make([]string, len(toastChoices))
	for i, n := range toastChoices {
		options[i] = fmt.Sprintf("%d s", n)
	}
	chosen := max(slices.Index(toastChoices, seconds), 0)
	if !slices.Contains(toastChoices, seconds) {
		// A length written into the file by hand is kept as it is.
		options = append(options, fmt.Sprintf("%d s", seconds))
		chosen = len(options) - 1
	}
	length := addSelect(form, "Toasts stay", options, chosen)
	form.AddTextView("", "How long a toast - a watched pipeline that\n"+
		"began, passed or failed - stays in the corner,\n"+
		"counted only while the terminal is in front.", 46, 3, true, false)
	form.AddTextView("", "Desktop notifications are a card of\n"+
		"Integrations.", 46, 2, true, false)
	form.AddButton("Save", func() {
		i, _ := length.GetCurrentOption()
		if i >= 0 && i < len(toastChoices) {
			cfg.ToastSeconds = toastChoices[i]
		}
		if cfg.ToastSeconds == config.DefaultToastSeconds {
			cfg.ToastSeconds = 0
		}
		s.app.saveConfig()
		s.reload()
		s.app.done("Saved")
	})
	form.AddButton("Revert", func() {
		s.fillNotifications()
		s.app.note("Reverted")
	})
	s.app.hintForm(form)
	if focused {
		if button >= 0 {
			item = form.GetFormItemCount() + button
		}
		form.SetFocus(item)
		s.app.tv.SetFocus(form)
	}
}
