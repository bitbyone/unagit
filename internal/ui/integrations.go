package ui

import (
	"os/exec"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type integrationCard struct {
	view                               *tview.TextView
	name, command, description, binary string
	enabled                            func() bool
	toggle                             func()
}

type integrationsView struct {
	*tview.Flex
	settings *settingsView
	cards    []*integrationCard
	current  int
	active   bool
}

func (s *settingsView) newIntegrationsView() *integrationsView {
	v := &integrationsView{Flex: tview.NewFlex().SetDirection(tview.FlexRow), settings: s}
	box(v.Box, "Integrations").SetBorderPadding(1, 0, 1, 1)
	v.cards = []*integrationCard{{
		name: "Incomm", command: "incomm",
		description: "Show merge request comments in your editor.\nCtrl-R imports comments before opening the review.",
		enabled:     func() bool { return s.app.cfg.Integrations.Incomm },
		toggle:      func() { s.app.cfg.Integrations.Incomm = !s.app.cfg.Integrations.Incomm },
	}}
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
	for _, card := range v.cards {
		height := max(9, len(tview.WordWrap(card.view.GetText(false), max(1, width-6)))+2)
		v.ResizeItem(card.view, height, 0)
	}
	v.Flex.Draw(screen)
}

func (v *integrationsView) Focus(delegate func(tview.Primitive)) {
	delegate(v.cards[v.current].view)
}

func (v *integrationsView) check() {
	for _, card := range v.cards {
		card.binary, _ = exec.LookPath(card.command)
	}
	v.paintFocus(v.active)
}

func (v *integrationsView) paintFocus(active bool) {
	v.active = active
	for i, card := range v.cards {
		focused := active && i == v.current
		focusBox(card.view.Box, focused)
		state, color := "disabled", colMuted
		if card.binary == "" {
			state = "not installed"
		} else if card.enabled() {
			state, color = "enabled", colOn
		}
		text := tag(color) + "● " + state + tagEnd + "\n\n" + card.description + "\n"
		if card.binary == "" {
			text += tag(colMuted) + "Install " + card.command + " and add it to PATH." + tagEnd
		} else {
			text += tag(colMuted) + tview.Escape(card.binary) + tagEnd
		}
		text += "\n\n"
		if focused {
			if card.binary != "" {
				text += tag(colAccent) + "e" + tagEnd + " toggle   ·   "
			}
			text += tag(colAccent) + "c" + tagEnd + " check"
		}
		card.view.SetText(text)
	}
}

func (v *integrationsView) keys(ev *tcell.EventKey) *tcell.EventKey {
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
		switch ev.Rune() {
		case 'j':
			move = 1
		case 'k':
			move = -1
		case 'e':
			card := v.cards[v.current]
			// Recheck before enabling so a removed executable cannot be enabled.
			card.binary, _ = exec.LookPath(card.command)
			if card.binary != "" {
				card.toggle()
				v.settings.app.saveConfig()
			}
			v.paintFocus(true)
			return nil
		case 'c':
			card := v.cards[v.current]
			card.binary, _ = exec.LookPath(card.command)
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
