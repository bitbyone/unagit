package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
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
func (a *App) keyIn(p tview.Primitive, name, about, keys string, rank int, when func() bool) uiAction {
	return uiAction{name: name, about: about, keys: keys, rank: rank, when: when, run: func() { a.pressIn(p, keys) }}
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
			a.keyIn(t, "Add…", "Add a server or an account, then give it a token.", "a", 10, nil),
			a.keyIn(t, "Edit…", "Change its name, its address or how it is cloned.", "e", 20, some),
			a.keyIn(t, "Set Token…", "Store a new token for it in the encrypted vault.", "t", 30, some),
			a.keyIn(t, "Verify Token", "Ask the server whom the token belongs to.", "v", 40, some),
			a.keyIn(t, "Remove…", "Forget it, its groups and its token; nothing on disk is touched.", "d", 800, some),
		}
	case sectionGroups:
		return sectionNames[s.current], []uiAction{
			a.keyIn(s.tree, "Toggle Selection", "Cycle the group: not used, this group alone, or with its subgroups.", "space", 10, nil),
			a.keyIn(s.tree, "Set Clone Directory…", "Choose where the group's repositories are cloned.", "d", 20, nil),
			a.keyIn(s.tree, "Edit Tags…", "Put tags on the group; its subgroups and repositories wear them too.", "t", 30, nil),
			a.keyIn(s.tree, "Reload Groups", "Ask the servers for the groups again.", "r", 40, nil),
			a.keyIn(s.tree, "Refresh Repositories", "Ask the servers for the repositories of the selected groups.", "p", 50, nil),
			a.keyIn(s.tree, "Refresh Merge Requests", "Ask the servers for the open merge requests of the selected groups.", "m", 60, nil),
		}
	case sectionTags:
		some := func() bool { return s.selectedTag() != "" }
		return sectionNames[s.current], []uiAction{
			a.keyIn(s.tags, "New Tag…", "Make a new tag with a name and a colour.", "a", 10, nil),
			a.keyIn(s.tags, "Edit Tag…", "Rename the tag or change its colour; it stays on its repositories.", "e", 20, some),
			a.keyIn(s.tags, "Change Pill Ends", "How the pills end: rounded (needs a Nerd Font), circles or square.", "s", 30, nil),
			a.keyIn(s.tags, "Remove Tag…", "Delete the tag and take it off every repository.", "d", 800, some),
		}
	case sectionTheme:
		return sectionNames[s.current], []uiAction{
			{name: "Use Theme", about: "Draw unagit in the theme under the cursor, from now on.", keys: "Enter", rank: 10,
				when: func() bool { return s.selectedTheme() != "" }, run: func() { s.useSelectedTheme() }},
			a.keyIn(s.themes, "Fork Theme…", "Copy the theme into a file of your own, every colour spelled out, and put it on; unagit follows each save of it.", "f", 15,
				func() bool { return s.selectedTheme() != "" }),
			a.keyIn(s.themes, "Read Themes Again", "Read the themes in the configuration's themes folder again, after editing one.", "r", 20, nil),
			a.keyIn(s.themes, "Nerd Font Icons", "Draw icons from a Nerd Font where a theme has them: told from the terminal, on, or off.", "n", 25, nil),
			a.keyIn(s.themes, "Terminal Background", "Leave the terminal's own background under every theme, so a translucent or blurred terminal shows through; the rest of the colours stay the theme's.", "b", 27, nil),
		}
	case sectionSecurity:
		return sectionNames[s.current], []uiAction{
			a.keyIn(s.security, "Change Passphrase…", "Encrypt the tokens again under a new passphrase.", "c", 10, nil),
			a.keyIn(s.security, "Toggle Keychain", "Keep the passphrase in the macOS login keychain so unagit opens without asking, or forget it again.", "k", 20,
				func() bool { return passphraseStore.available() }),
		}
	case sectionDebug:
		return debugSectionName, []uiAction{
			{name: "Fire", about: "Show the toast, the news or the notification under the cursor, as if a server had said so.", keys: "Enter", rank: 10,
				run: s.debug.fire},
		}
	case sectionIntegrations:
		v := s.integrations
		card := v.cards[v.current]
		return card.name, []uiAction{
			a.keyIn(v, "Enable or Disable", "Turn this integration on or off.", "e", 10, func() bool { return card.toggle != nil }),
			a.keyIn(v, "Toggle Favourite Editor", "Make this the editor everything opens in unless you ask for another; on the favourite, leave none, so every open asks.", "f", 10,
				func() bool { return strings.HasPrefix(card.command, editorCommand) }),
			a.keyIn(v, "Set Up Custom Editor…", "The command of your own, its arguments, and whether it takes the terminal or opens a window.", "o", 15,
				func() bool { return card.command == editorCommand+editors.Custom }),
			a.keyIn(v, "Check Installation", "Look for the program on PATH again.", "c", 20, nil),
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
		acts = append(acts, uiAction{name: "Go to " + name, about: "Open the " + name + " section.", rank: 10 + i, when: func() bool { return s.current != i },
			run: func() {
				s.selectSection(i)
				s.focusContent()
			}})
	}
	if s.debug != nil {
		acts = append(acts, uiAction{name: "Go to " + debugSectionName, about: "Fire toasts, watched news and desktop notifications by hand, to see how they look.",
			rank: 10 + sectionDebug, when: func() bool { return s.current != sectionDebug },
			run: func() {
				s.selectSection(sectionDebug)
				s.focusContent()
			}})
	}
	acts = append(acts,
		uiAction{name: "Back to Sections", about: "Return to the list of sections.", keys: "Esc", rank: 100, when: func() bool { return s.contentFocused },
			run: s.focusList},
		uiAction{name: "Go to Repositories", about: "Every repository of the servers and groups you picked.", keys: "1", rank: 900, run: func() { a.switchTab(pageProjects) }},
		uiAction{name: "Go to Merge Requests", about: "The open merge requests of those repositories.", keys: "2", rank: 900, run: func() { a.switchTab(pageMRs) }},
		uiAction{name: "Go to Worktrees", about: "Every worktree on disk, plain and grouped.", keys: "3", rank: 900, run: func() { a.switchTab(pageWorktrees) }},
		uiAction{name: "Help", about: "Every key of every screen, the ones that work here lit.", keys: "?", rank: 950, run: a.showHelp},
		uiAction{name: "Quit", about: "Leave unagit. Window editors it opened stay open.", keys: "q", rank: 999, run: a.tv.Stop},
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
