package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/mux"
)

type integrationCard struct {
	view                               *tview.TextView
	name, command, description, binary string
	enabled                            func() bool
	toggle                             func()
	// found, when set, is a line under the description saying what the
	// integration found on this machine.
	found func() string
	check func()
	// render and onKey replace the enable/disable card with one of its own
	// making; onKey reports whether it took the letter.
	render func(focused bool) string
	onKey  func(r rune) bool
}

type integrationsView struct {
	*tview.Flex
	settings *settingsView
	cards    []*integrationCard
	current  int
	active   bool
	editors  []editors.Editor // as last detected
	width    int              // inside a card, as last drawn
}

func (s *settingsView) newIntegrationsView() *integrationsView {
	v := &integrationsView{Flex: tview.NewFlex().SetDirection(tview.FlexRow), settings: s}
	box(v.Box, "Integrations").SetBorderPadding(1, 0, 1, 1)
	v.cards = []*integrationCard{{
		name: "Incomm", command: "incomm",
		description: "Show merge request comments in your editor.\nCtrl-R imports comments before opening the review.",
		enabled:     func() bool { return s.app.cfg.Integrations.Incomm },
		toggle:      func() { s.app.cfg.Integrations.Incomm = !s.app.cfg.Integrations.Incomm },
	}, {
		name:   "Editors",
		render: v.renderEditors,
		onKey:  v.editorKeys,
	}, {
		name: "Hunk", command: "hunk",
		description: "Review changes in the terminal: D opens what a row holds, and a grouped worktree as one review of all its repositories.",
		enabled:     s.app.hunkOn,
		toggle: func() {
			on := !s.app.hunkOn()
			s.app.cfg.Integrations.Hunk = &on
		},
	}, {
		name: "Chezmoi", command: "chezmoi",
		description: "Open the dotfiles repository where chezmoi keeps it instead of cloning it again. Its worktrees and merge requests work as usual.",
		enabled:     s.app.chezmoiOn,
		toggle: func() {
			on := !s.app.chezmoiOn()
			s.app.cfg.Integrations.Chezmoi = &on
			s.app.detectChezmoi()
		},
		found: s.app.chezmoiFound,
	}, {
		name: "Zoxide", command: "zoxide",
		description: "Remember directories opened in an editor or a shell. Sort repositories and worktrees by frequent and recent visits.",
		enabled:     s.app.zoxideOn,
		toggle: func() {
			on := !s.app.zoxideOn()
			s.app.cfg.Integrations.Zoxide = &on
			s.app.refreshZoxide()
		},
		found: s.app.zoxideFound,
	}}
	hint := ""
	v.cards = append(v.cards, &integrationCard{
		name: "Yazi", command: "yazi",
		description: "Browse Files opens the selected directory in Yazi. Choose a file to open it in your favourite editor.",
		enabled:     s.app.yaziOn,
		toggle:      func() { on := !s.app.yaziOn(); s.app.cfg.Integrations.Yazi = &on },
		found:       func() string { return hint },
		check:       func() { hint = s.app.yaziFound() },
	})
	v.cards = append(v.cards, &integrationCard{
		name: "Herdr", command: "herdr",
		description: "Inside herdr, open editors in its tabs and splits as in Zellij. From anywhere, start agents in a herdr workspace, where herdr follows what they do.",
		enabled:     s.app.herdrOn,
		toggle: func() {
			on := !s.app.herdrOn()
			s.app.cfg.Integrations.Herdr = &on
			s.app.detectMultiplexer()
		},
	}, &integrationCard{
		name: "Ghostty", command: "ghostty",
		description: "Open editors and agents in a Ghostty window or tab, or beside unagit in a split when it runs in Ghostty. macOS asks once to let unagit control Ghostty.",
		enabled:     s.app.ghosttyOn,
		toggle: func() {
			on := !s.app.ghosttyOn()
			s.app.cfg.Integrations.Ghostty = &on
		},
	})
	for _, ag := range agents.All {
		ag := ag
		v.cards = append(v.cards, &integrationCard{
			name: ag.Name, command: ag.Command,
			description: "Open in " + ag.Name + "… starts it in the selected repository, merge request or worktree: in this terminal, or in a tab, split or window of herdr, Zellij or Ghostty.",
			enabled:     func() bool { return s.app.agentOn(ag) },
			toggle:      func() { s.app.setAgentOn(ag, !s.app.agentOn(ag)) },
		})
	}
	for _, card := range v.cards {
		card.view = tview.NewTextView().SetDynamicColors(true).SetScrollable(false).SetTextColor(colText)
		box(card.view.Box, card.name).SetBorderPadding(0, 0, 2, 2)
		card.view.SetInputCapture(v.keys)
		v.AddItem(card.view, 9, 0, false)
	}
	v.AddItem(nil, 0, 1, false)
	v.check()
	return v
}

// Keep the shortcuts visible when a narrow terminal wraps the description.
func (v *integrationsView) Draw(screen tcell.Screen) {
	_, _, width, _ := v.GetInnerRect()
	if inner := width - 6; inner != v.width {
		// The editors card fits its lines to the width; paint it again for
		// the width it now has.
		v.width = inner
		v.paintFocus(v.active)
	}
	_, _, _, room := v.GetInnerRect()
	heights := make([]int, len(v.cards))
	for i, card := range v.cards {
		heights[i] = max(9, len(tview.WordWrap(card.view.GetText(false), max(1, width-6)))+2)
	}
	// A Flex draws an item of fixed height whole, past its own frame when the
	// room runs out; the cards that do not fit are left out instead, starting
	// far enough down for the one with the cursor to be whole.
	first, used := 0, 0
	for i := 0; i <= v.current; i++ {
		used += heights[i]
	}
	for used > room && first < v.current {
		used -= heights[first]
		first++
	}
	used = 0
	for i, card := range v.cards {
		height := heights[i]
		if i < first || used+height > room && i != v.current {
			height = 0
		}
		used += height
		v.ResizeItem(card.view, height, 0)
	}
	v.Flex.Draw(screen)
}

func (v *integrationsView) Focus(delegate func(tview.Primitive)) {
	delegate(v.cards[v.current].view)
}

func (v *integrationsView) check() {
	v.editors = v.settings.app.detectEditors()
	for _, card := range v.cards {
		if card.command != "" {
			card.binary = v.integrationBinary(card.command)
			if card.check != nil && card.binary != "" {
				card.check()
			}
		}
	}
	v.paintFocus(v.active)
}

func (v *integrationsView) paintFocus(active bool) {
	v.active = active
	for i, card := range v.cards {
		focused := active && i == v.current
		focusBox(card.view.Box, focused)
		if card.render != nil {
			card.view.SetText(card.render(focused))
			continue
		}
		state, color := "disabled", colMuted
		if card.binary == "" {
			state = "not installed"
		} else if card.enabled() {
			state, color = "enabled", colOn
		}
		text := tag(color) + glyphDot + " " + state + tagEnd + "\n\n" + card.description + "\n"
		if card.found != nil && card.binary != "" {
			if found := card.found(); found != "" {
				text += found + "\n"
			}
		}
		if card.binary == "" && card.command == "ghostty" {
			text += tag(colMuted) + "Install Ghostty; it is scripted on macOS only." + tagEnd
		} else if card.binary == "" {
			text += tag(colMuted) + "Install " + card.command + " and add it to PATH." + tagEnd
		} else {
			text += tag(colMuted) + tview.Escape(card.binary) + tagEnd
		}
		text += "\n\n"
		if focused {
			if card.binary != "" {
				text += tag(colDim) + "e toggle · " + tagEnd
			}
			text += tag(colDim) + "c check" + tagEnd
		}
		card.view.SetText(text)
	}
}

func (v *integrationsView) keys(ev *tcell.EventKey) *tcell.EventKey {
	if v.settings.settingsPickers(ev) {
		return nil
	}
	move := 0
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyLeft:
		v.settings.focusList()
		return nil
	case tcell.KeyTab, tcell.KeyDown:
		move = 1
	case tcell.KeyBacktab, tcell.KeyUp:
		move = -1
	case tcell.KeyRune:
		if card := v.cards[v.current]; card.onKey != nil && card.onKey(ev.Rune()) {
			v.paintFocus(true)
			return nil
		}
		switch ev.Rune() {
		case 'j':
			move = 1
		case 'k':
			move = -1
		case 'e':
			card := v.cards[v.current]
			if card.toggle == nil {
				return nil
			}
			// Recheck before enabling so a removed executable cannot be enabled.
			if card.command == "zoxide" {
				v.settings.app.checkZoxide()
			}
			card.binary = v.integrationBinary(card.command)
			if card.check != nil && card.binary != "" {
				card.check()
			}
			if card.binary != "" {
				card.toggle()
				v.settings.app.saveConfig()
			}
			v.paintFocus(true)
			return nil
		case 'c':
			card := v.cards[v.current]
			if card.command == "zoxide" {
				v.settings.app.checkZoxide()
			}
			card.binary = v.integrationBinary(card.command)
			if card.check != nil && card.binary != "" {
				card.check()
			}
			v.paintFocus(true)
			return nil
		default:
			if key, handled := v.settings.contentKeys(ev); handled {
				return key
			}
		}
	}
	if move != 0 {
		v.current = (v.current + move + len(v.cards)) % len(v.cards)
		v.settings.app.tv.SetFocus(v)
		v.paintFocus(true)
	}
	return nil
}

// renderEditors lists the editors unagit can open in, which of them this
// machine has, and the favourite everything opens in by default.
func (v *integrationsView) renderEditors(focused bool) string {
	cfg := v.settings.app.cfg
	_, hasFav := editors.Favourite(v.editors, cfg.FavouriteEditor)
	width := 0
	for _, e := range v.editors {
		width = max(width, len([]rune(e.Name)))
	}
	// One line per editor, whatever the width: name, kind, and where it was
	// found, shortened from the left, where paths say least - or left out
	// when there is no room for it.
	room := v.width - (2 + width + 2 + 8 + 2)
	text := ""
	for _, e := range v.editors {
		// The favourite is marked even when it is gone, so it is plain why
		// opening asks.
		mark := "  "
		if e.ID == cfg.FavouriteEditor {
			mark = tag(colOn) + glyphFavourite + " " + tagEnd
		}
		name := fmt.Sprintf("%-*s", width, e.Name)
		if !e.Found {
			text += mark + tag(colMuted) + tview.Escape(name) + "  not found" + tagEnd + "\n"
			continue
		}
		kind := "window  "
		if e.Terminal {
			kind = "terminal"
		}
		line := mark + tview.Escape(name) + "  " + kind
		if room >= 12 {
			line += "  " + tag(colMuted) + tview.Escape(shortPath(e.Where, room)) + tagEnd
		}
		text += line + "\n"
	}
	if !hasFav {
		text += tag(colWarn) + "No favourite: every open asks which." + tagEnd + "\n"
	}
	if focused {
		text += "\n" + tag(colDim) + "f favourite · c check" + tagEnd
	}
	return text
}

func (v *integrationsView) editorKeys(r rune) bool {
	switch r {
	case 'c':
		v.check()
		return true
	case 'f':
		// Asking every time is a choice too, and the way back to it.
		items := []pickItem{{Label: "None", Sub: "ask every time", Data: askEveryTime}}
		start := 0
		for _, e := range v.editors {
			if !e.Found {
				continue
			}
			if e.ID == v.settings.app.cfg.FavouriteEditor {
				start = len(items)
			}
			items = append(items, pickItem{Label: e.Name, Sub: kindOf(e), Data: e.ID})
		}
		if len(items) == 1 {
			v.settings.app.flash("no editor found - install one, or set a custom editor in General")
			return true
		}
		v.settings.app.showPickerAt("Favourite editor", items, start, func(it pickItem) {
			v.settings.app.cfg.FavouriteEditor = it.Data.(string)
			v.settings.app.saveConfig()
			v.paintFocus(true)
		})
		return true
	}
	return false
}

// askEveryTime is the favourite of someone who wants to be asked. It is not
// an editor, so it is never found; an empty favourite would instead be taken
// for a configuration from before there was a choice.
const askEveryTime = "ask"

// shortPath is a path in at most n characters: the home directory as ~, and
// what is still too long cut from the left, keeping the name at the end.
func shortPath(path string, n int) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+"/") {
		path = "~" + strings.TrimPrefix(path, home)
	}
	if r := []rune(path); len(r) > n {
		return "…" + string(r[len(r)-n+1:])
	}
	return path
}

func (v *integrationsView) integrationBinary(command string) string {
	switch command {
	case "zoxide":
		return v.settings.app.zoxideTool().Binary()
	case "ghostty":
		if v.settings.app.findGhosttyClient() == nil {
			return ""
		}
		if app := mux.GhosttyApp(); app != "" {
			return app
		}
		return "Ghostty"
	}
	bin, _ := v.settings.app.executable(command)
	return bin
}
