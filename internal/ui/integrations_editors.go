package ui

import (
	"strings"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
)

// editorCommand prefixes an editor's id where a card names its command, so
// the card looks the editor up among those detected rather than on PATH:
// an editor can be an application with no launcher at all.
const editorCommand = "editor:"

// editorCards are a card for each editor things open in, Neovim to the
// custom one. Each is turned on and off as any integration is, and f makes
// it the favourite - "(default)" in its title - or, on the favourite, leaves
// none, so every open asks.
func (v *integrationsView) editorCards() []*integrationCard {
	a := v.settings.app
	window := "Opens the directory in a window of its own; unagit stays as it is."
	var cards []*integrationCard
	for _, e := range []struct{ id, name, about string }{
		{editors.Nvim, "Neovim", "Takes this terminal, or a tab or split beside unagit. Ctrl-Z puts it aside with its buffers; E brings it back."},
		{editors.Idea, "IntelliJ IDEA", window},
		{editors.Code, "VS Code", window},
		{editors.Zed, "Zed", window},
		{editors.Custom, "Custom", "A command of your own, with its arguments, taking this terminal or opening a window."},
	} {
		card := &integrationCard{
			name: e.name, command: editorCommand + e.id, description: e.about,
			enabled: func() bool { return a.editorOn(e.id) },
			toggle:  func() { a.setEditorOn(e.id, !a.editorOn(e.id)) },
			keys:    "f favourite",
			title: func() string {
				if a.cfg.FavouriteEditor == e.id {
					return e.name + " " + defaultMark()
				}
				return e.name
			},
			onKey: func(r rune) bool {
				switch {
				case r == 'f':
					v.favourite(e.id, e.name)
					return true
				case r == 'o' && e.id == editors.Custom:
					v.showCustomEditorForm()
					return true
				}
				return false
			},
		}
		if e.id == editors.Custom {
			card.keys = "o set up · f favourite"
			unset := func() bool { return strings.TrimSpace(a.cfg.Editor) == "" }
			card.missing = func() string {
				if unset() {
					return "Press o to give it a command."
				}
				return tview.Escape(a.cfg.Editor) + " is not on PATH; press o to change it."
			}
			card.absent = func() string {
				if unset() {
					return "not set up"
				}
				return "not installed"
			}
			card.found = func() string { return customEditorLine(a.cfg.Editor, a.cfg.EditorArgs, a.cfg.EditorWindow) }
		} else {
			card.missing = func() string { return "Not found on PATH or among the applications." }
		}
		cards = append(cards, card)
	}
	return cards
}

// favourite makes an editor the one everything opens in, or - pressed on
// the favourite - leaves none, and every open asks.
func (v *integrationsView) favourite(id, name string) {
	a := v.settings.app
	switch e, found := editors.Pick(v.editors, id); {
	case a.cfg.FavouriteEditor == id:
		a.cfg.FavouriteEditor = askEveryTime
		a.note("no favourite editor: every open asks which")
	case !found || !e.Found:
		a.flash(name + " is not installed - nothing can open in it")
		return
	case !a.editorOn(id):
		a.flash(name + " is off - turn it on with e first")
		return
	default:
		a.cfg.FavouriteEditor = id
		a.done(name + " is the favourite editor")
	}
	a.saveConfig()
	v.paintFocus(true)
}

// customEditorLine is what the custom editor runs, and where.
func customEditorLine(command string, args []string, window bool) string {
	if strings.TrimSpace(command) == "" {
		return ""
	}
	where := "takes the terminal"
	if window {
		where = "opens a window"
	}
	return tview.Escape(strings.TrimSpace(command+" "+strings.Join(args, " "))) + tag(colMuted) + " · " + where + tagEnd
}

// Labels of the custom editor form.
const (
	labelCustomCommand = "Command"
	labelCustomArgs    = "Arguments"
	labelCustomWindow  = "Opens a window"
)

// showCustomEditorForm sets the custom editor: its command, its arguments
// - the directory is the working directory, and "." when there are none -
// and whether it opens a window of its own or takes the terminal.
func (v *integrationsView) showCustomEditorForm() {
	a := v.settings.app
	cfg := a.cfg
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(labelCustomCommand, cfg.Editor, 0, nil, nil)
	form.AddInputField(labelCustomArgs, strings.Join(cfg.EditorArgs, " "), 0, nil, nil)
	window := addCheckbox(form, labelCustomWindow, cfg.EditorWindow)
	form.AddButton("Save", func() {
		cfg.Editor = strings.TrimSpace(form.GetFormItemByLabel(labelCustomCommand).(*tview.InputField).GetText())
		cfg.EditorArgs = strings.Fields(form.GetFormItemByLabel(labelCustomArgs).(*tview.InputField).GetText())
		cfg.EditorWindow = window.IsChecked()
		a.saveConfig()
		a.closeModal(pageForm)
		v.check()
		a.done("saved the custom editor")
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Custom editor", form, 64, 9)
}
