package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The actions of Settings. Each section answers its keys itself, so an
// action here does nothing but press its own key in the section: what the
// picker says a key does is what the key does, by construction.

// pressIn sends a key, written as an action writes it, to a primitive.
func (a *App) pressIn(p tview.Primitive, spec string) {
	ev := tcell.NewEventKey(tcell.KeyRune, []rune(spec)[0], tcell.ModNone)
	if spec == "space" {
		ev = tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	}
	if h := p.InputHandler(); h != nil {
		h(ev, func(next tview.Primitive) { a.tv.SetFocus(next) })
	}
}

// keyIn is an action that presses its key in a primitive.
func (a *App) keyIn(p tview.Primitive, name, keys string, rank int, when func() bool) uiAction {
	return uiAction{name: name, keys: keys, rank: rank, when: when, run: func() { a.pressIn(p, keys) }}
}

// settingsSelection is what can be done in the section shown, with the row
// under its cursor.
func (s *settingsView) settingsSelection() (string, []uiAction) {
	a := s.app
	switch s.current {
	case sectionGitLab, sectionGitHub:
		t := s.gitlab
		if s.current == sectionGitHub {
			t = s.github
		}
		some := func() bool { return s.selectedInstance(t) != nil }
		return sectionNames[s.current], []uiAction{
			a.keyIn(t, "Add one", "a", 10, nil),
			a.keyIn(t, "Edit it", "e", 20, some),
			a.keyIn(t, "Set its token", "t", 30, some),
			a.keyIn(t, "Verify the token", "v", 40, some),
			a.keyIn(t, "Remove it", "d", 800, some),
		}
	case sectionGroups:
		return sectionNames[s.current], []uiAction{
			a.keyIn(s.tree, "Select: off, this group, with subgroups", "space", 10, nil),
			a.keyIn(s.tree, "Set the clone directory", "d", 20, nil),
			a.keyIn(s.tree, "Tag it; everything below inherits", "t", 30, nil),
			a.keyIn(s.tree, "Reload the groups", "r", 40, nil),
			a.keyIn(s.tree, "Refresh the repositories", "p", 50, nil),
			a.keyIn(s.tree, "Refresh the merge requests", "m", 60, nil),
		}
	case sectionTags:
		some := func() bool { return s.selectedTag() != "" }
		return sectionNames[s.current], []uiAction{
			a.keyIn(s.tags, "Add a tag", "a", 10, nil),
			a.keyIn(s.tags, "Edit it", "e", 20, some),
			a.keyIn(s.tags, "Pill ends: rounded, circles, square", "s", 30, nil),
			a.keyIn(s.tags, "Remove it", "d", 800, some),
		}
	case sectionSecurity:
		return sectionNames[s.current], []uiAction{
			a.keyIn(s.security, "Change the passphrase", "c", 10, nil),
			a.keyIn(s.security, "Remember it in the macOS Keychain, or forget it", "k", 20,
				func() bool { return passphraseStore.available() }),
		}
	case sectionIntegrations:
		v := s.integrations
		card := v.cards[v.current]
		return card.name, []uiAction{
			a.keyIn(v, "Turn it on or off", "e", 10, func() bool { return card.toggle != nil }),
			a.keyIn(v, "Choose the favourite editor", "f", 10, func() bool { return card.onKey != nil }),
			a.keyIn(v, "Check the installation", "c", 20, nil),
		}
	}
	return s.settingsScreen()
}

// settingsScreen is what Settings itself can do: go to a section, or leave.
func (s *settingsView) settingsScreen() (string, []uiAction) {
	a := s.app
	var acts []uiAction
	for i, name := range sectionNames {
		i := i
		acts = append(acts, uiAction{name: "Go to " + name, rank: 10 + i, when: func() bool { return s.current != i },
			run: func() {
				s.selectSection(i)
				s.focusContent()
			}})
	}
	acts = append(acts,
		uiAction{name: "Back to the sections", keys: "Esc", rank: 100, when: func() bool { return s.contentFocused },
			run: s.focusList},
		uiAction{name: "Go to Repositories", keys: "R", rank: 900, run: func() { a.switchTab(pageProjects) }},
		uiAction{name: "Go to Merge requests", keys: "M", rank: 900, run: func() { a.switchTab(pageMRs) }},
		uiAction{name: "Go to Worktrees", keys: "W", rank: 900, run: func() { a.switchTab(pageWorktrees) }},
		uiAction{name: "Help: every key", keys: "?", rank: 950, run: a.showHelp},
		uiAction{name: "Quit unagit", keys: "q", rank: 999, run: a.tv.Stop},
	)
	return "Settings", acts
}

// settingsPickers opens Settings' action pickers on their keys.
func (s *settingsView) settingsPickers(ev *tcell.EventKey) bool {
	if !opensPicker(ev) {
		return false
	}
	selection := s.settingsSelection
	if !s.contentFocused {
		selection = s.settingsScreen
	}
	return s.app.actionKeys(ev, selection, s.settingsScreen)
}
