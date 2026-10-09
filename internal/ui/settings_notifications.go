package ui

import (
	"fmt"
	"slices"

	"github.com/rivo/tview"

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
	// One select a severity, each of the lengths offered and, written
	// into the file by hand, its own.
	type length struct {
		level    string
		choices  []int
		dropdown *tview.DropDown
	}
	var lengths []length
	for _, level := range config.ToastLevels {
		seconds := *cfg.ToastSeconds.Of(level)
		if seconds <= 0 {
			seconds = config.DefaultToastSeconds
		}
		choices := slices.Clone(toastChoices)
		if !slices.Contains(choices, seconds) {
			choices = append(choices, seconds)
		}
		options := make([]string, len(choices))
		for i, n := range choices {
			options[i] = fmt.Sprintf("%d s", n)
		}
		label := toastLevelLabel(level)
		lengths = append(lengths, length{level, choices, addSelect(form, label, options, slices.Index(choices, seconds))})
	}
	form.AddTextView("", "How long a toast - a watched pipeline that\n"+
		"began, passed or failed - stays in the corner,\n"+
		"by how it went, counted only while the\n"+
		"terminal is in front.", 46, 4, true, false)
	form.AddTextView("", "Desktop notifications are a card of\n"+
		"Integrations.", 46, 2, true, false)
	form.AddButton("Save", func() {
		for _, l := range lengths {
			seconds := config.DefaultToastSeconds
			if i, _ := l.dropdown.GetCurrentOption(); i >= 0 && i < len(l.choices) {
				seconds = l.choices[i]
			}
			if seconds == config.DefaultToastSeconds {
				seconds = 0
			}
			*cfg.ToastSeconds.Of(l.level) = seconds
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

// toastLevelLabel is a severity's select in Settings › Notifications.
func toastLevelLabel(level string) string {
	return map[string]string{
		config.ToastInfo: "Info stays", config.ToastSuccess: "Success stays",
		config.ToastWarning: "Warning stays", config.ToastDanger: "Danger stays",
	}[level]
}
