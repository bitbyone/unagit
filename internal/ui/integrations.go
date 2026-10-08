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

// integrationCategory is a heading and the cards under it.
type integrationCategory struct {
	title string
	cards []*integrationCard
}

// integrationsView lays the cards out as tiles, under the heading of what
// kind of integration they are, as many across as the width allows. cards
// is every card in reading order, current the one with the cursor.
type integrationsView struct {
	*tview.Box
	settings   *settingsView
	categories []integrationCategory
	cards      []*integrationCard
	current    int
	active     bool
	editors    []editors.Editor // as last detected
	width      int              // inside a card, as last drawn
	columns    int              // across, as last drawn
	offset     int              // the rows scrolled past, as last drawn
}

func (s *settingsView) newIntegrationsView() *integrationsView {
	v := &integrationsView{Box: tview.NewBox(), settings: s, columns: 1}
	box(v.Box, "Integrations").SetBorderPadding(1, 0, 1, 1)
	byName := map[string]*integrationCard{}
	for _, card := range []*integrationCard{{
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
	}} {
		byName[card.name] = card
	}
	hint := ""
	byName["Yazi"] = &integrationCard{
		name: "Yazi", command: "yazi",
		description: "Browse Files opens the selected directory in Yazi. Choose a file to open it in your favourite editor.",
		enabled:     s.app.yaziOn,
		toggle:      func() { on := !s.app.yaziOn(); s.app.cfg.Integrations.Yazi = &on },
		found:       func() string { return hint },
		check:       func() { hint = s.app.yaziFound() },
	}
	byName["Zellij"] = &integrationCard{
		name: "Zellij", command: "zellij",
		description: "Inside Zellij, open editors and agents in a new tab or beside unagit in a split. A Neovim in a pane can be put aside with Ctrl-Z and brought back.",
		enabled:     s.app.zellijOn,
		toggle: func() {
			on := !s.app.zellijOn()
			s.app.cfg.Integrations.Zellij = &on
			s.app.detectMultiplexer()
		},
	}
	byName["Herdr"] = &integrationCard{
		name: "Herdr", command: "herdr",
		description: "Inside herdr, open editors in its tabs and splits as in Zellij. From anywhere, start agents in a herdr workspace, where herdr follows what they do.",
		enabled:     s.app.herdrOn,
		toggle: func() {
			on := !s.app.herdrOn()
			s.app.cfg.Integrations.Herdr = &on
			s.app.detectMultiplexer()
		},
	}
	byName["Ghostty"] = &integrationCard{
		name: "Ghostty", command: "ghostty",
		description: "Open editors and agents in a Ghostty window or tab, or beside unagit in a split when it runs in Ghostty. macOS asks once to let unagit control Ghostty.",
		enabled:     s.app.ghosttyOn,
		toggle: func() {
			on := !s.app.ghosttyOn()
			s.app.cfg.Integrations.Ghostty = &on
		},
	}
	var agentCards []*integrationCard
	for _, ag := range agents.All {
		ag := ag
		agentCards = append(agentCards, &integrationCard{
			name: ag.Name, command: ag.Command,
			description: "Open in " + ag.Name + "… starts it in the selected repository, merge request or worktree: in this terminal, or in a tab, split or window of herdr, Zellij or Ghostty.",
			enabled:     func() bool { return s.app.agentOn(ag) },
			toggle:      func() { s.app.setAgentOn(ag, !s.app.agentOn(ag)) },
		})
	}
	// By what they are for: what opens the code and reviews it, where
	// things open beside unagit, the agents, and the rest of the desk.
	v.categories = []integrationCategory{
		{"Editors & Review", []*integrationCard{byName["Editors"], byName["Incomm"], byName["Hunk"]}},
		{"Terminals", []*integrationCard{byName["Zellij"], byName["Herdr"], byName["Ghostty"]}},
		{"AI Agents", agentCards},
		{"Files & Navigation", []*integrationCard{byName["Zoxide"], byName["Yazi"], byName["Chezmoi"]}},
	}
	for _, c := range v.categories {
		v.cards = append(v.cards, c.cards...)
	}
	for _, card := range v.cards {
		card.view = tview.NewTextView().SetDynamicColors(true).SetScrollable(false).SetTextColor(colText)
		box(card.view.Box, card.name).SetBorderPadding(1, 0, 2, 2)
		// A card stands out from the page, as a tile on it.
		card.view.SetBackgroundColor(colCard)
		card.view.SetInputCapture(v.keys)
	}
	v.check()
	return v
}

// integrationColumns is how many cards stand side by side in a panel this
// wide inside: three on a large screen, two on an ordinary one, one on a
// narrow one.
func integrationColumns(width int) int {
	switch {
	case width >= 135:
		return 3
	case width >= 80:
		return 2
	}
	return 1
}

// cardSpot is where a card stands in the grid: its category, and its row and
// column across the whole page - the rows run on from one category to the
// next, so j and k move between them as within one.
type cardSpot struct{ category, row, column int }

func (v *integrationsView) spots(columns int) []cardSpot {
	var out []cardSpot
	row := 0
	for c, cat := range v.categories {
		for i := range cat.cards {
			out = append(out, cardSpot{c, row + i/columns, i % columns})
		}
		row += (len(cat.cards) + columns - 1) / columns
	}
	return out
}

// Draw lays the categories out: a heading with a rule across, then the
// cards in rows, each row as tall as its tallest card so the descriptions
// keep their shortcuts. What does not fit scrolls, keeping the card with the
// cursor whole and its heading in view when there is room.
func (v *integrationsView) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, room := v.GetInnerRect()
	// The panel's last row stays empty, as every panel's does.
	room = max(1, room-1)
	v.columns = integrationColumns(width)
	const gap = 1
	cardW := (width - gap*(v.columns-1)) / v.columns
	if inner := cardW - 6; inner != v.width {
		// The cards fit their lines to the width; paint them again for it.
		v.width = inner
		v.paintFocus(v.active)
	}
	type placed struct {
		top, height int
	}
	spots := v.spots(v.columns)
	cardAt := make([]placed, len(v.cards))
	type heading struct {
		title string
		top   int
	}
	var headings []heading
	top, card := 0, 0
	for c, cat := range v.categories {
		if c > 0 {
			top++ // a blank line between categories
		}
		headings = append(headings, heading{cat.title, top})
		top++
		for first := 0; first < len(cat.cards); first += v.columns {
			height := 7
			for i := first; i < min(first+v.columns, len(cat.cards)); i++ {
				lines := len(tview.WordWrap(cat.cards[i].view.GetText(false), max(1, cardW-6)))
				// The frame, and the blank line under the top edge.
				height = max(height, lines+3)
			}
			for i := first; i < min(first+v.columns, len(cat.cards)); i++ {
				cardAt[card+i] = placed{top, height}
			}
			top += height
		}
		card += len(cat.cards)
	}
	// Scroll just enough: up to the cursor's heading when it is the first
	// row of its category, down to the cursor's bottom edge.
	cur := cardAt[v.current]
	want := cur.top
	if spots[v.current].row == spots[firstOfCategory(spots, v.current)].row {
		want = headings[spots[v.current].category].top
	}
	if want < v.offset {
		v.offset = want
	}
	if cur.top+cur.height > v.offset+room {
		v.offset = cur.top + cur.height - room
	}
	v.offset = max(0, min(v.offset, max(0, top-room)))

	visible := func(t, h int) bool { return t >= v.offset && t+h <= v.offset+room }
	for _, h := range headings {
		if !visible(h.top, 1) {
			continue
		}
		row := y + h.top - v.offset
		_, w := tview.Print(screen, tag(colAccent)+"[::b]"+tview.Escape(h.title)+"[::-]"+tagEnd, x, row, width, tview.AlignLeft, colAccent)
		if rest := width - w - 1; rest > 0 {
			tview.Print(screen, tag(colDim)+strings.Repeat(string(tview.Borders.Horizontal), rest)+tagEnd, x+w+1, row, rest, tview.AlignLeft, colDim)
		}
	}
	for i, c := range v.cards {
		at := cardAt[i]
		if !visible(at.top, at.height) {
			// Left out whole rather than drawn past the panel's edge.
			c.view.SetRect(0, 0, 0, 0)
			continue
		}
		c.view.SetRect(x+spots[i].column*(cardW+gap), y+at.top-v.offset, cardW, at.height)
		c.view.Draw(screen)
		drawCardState(screen, c)
	}
}

// cardState is whether an integration is on, and the role it is drawn in.
func cardState(c *integrationCard) (string, string) {
	switch {
	case c.binary == "":
		return "not installed", "integration.missing"
	case c.enabled():
		return "enabled", "integration.enabled"
	}
	return "disabled", "integration.disabled"
}

// drawCardState puts the state at the right end of the card's top edge: a
// cell filled with its colour, then the word, so it reads at a glance down
// a column of cards. The editors card has no state of its own.
func drawCardState(screen tcell.Screen, c *integrationCard) {
	if c.render != nil || c.enabled == nil {
		return
	}
	x, y, w, _ := c.view.GetRect()
	word, colourRole := cardState(c)
	colour := role(colourRole)
	// " ■ word " ending one rule cell short of the corner.
	start := x + w - 2 - (len(word) + 4)
	if start <= x+2+len([]rune(c.name))+3 {
		return
	}
	style := baseStyle().Background(colCard).Foreground(colText)
	screen.SetContent(start, y, ' ', nil, style)
	screen.SetContent(start+1, y, ' ', nil, style.Background(colour))
	screen.SetContent(start+2, y, ' ', nil, style)
	for i, r := range word {
		screen.SetContent(start+3+i, y, r, nil, style.Foreground(colour))
	}
	screen.SetContent(start+3+len(word), y, ' ', nil, style)
}

// card is the card of an integration by its name.
func (v *integrationsView) card(name string) *integrationCard {
	for _, c := range v.cards {
		if c.name == name {
			return c
		}
	}
	return nil
}

// firstOfCategory is the first card of the category card i is in.
func firstOfCategory(spots []cardSpot, i int) int {
	for i > 0 && spots[i-1].category == spots[i].category {
		i--
	}
	return i
}

func (v *integrationsView) Focus(delegate func(tview.Primitive)) {
	delegate(v.cards[v.current].view)
}

func (v *integrationsView) HasFocus() bool {
	for _, c := range v.cards {
		if c.view.HasFocus() {
			return true
		}
	}
	return v.Box.HasFocus()
}

// InputHandler hands the keys to the card that has the focus: tview passes
// them down from the root, and the cards are drawn here, not held by a
// container that would pass them on.
func (v *integrationsView) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return v.WrapInputHandler(func(ev *tcell.EventKey, setFocus func(tview.Primitive)) {
		for _, c := range v.cards {
			if c.view.HasFocus() {
				if handler := c.view.InputHandler(); handler != nil {
					handler(ev, setFocus)
				}
				return
			}
		}
	})
}

// neighbour is the card a move by rows and columns lands on: along a row,
// the next one or none; across rows, the same column, or the last card of a
// shorter row.
func (v *integrationsView) neighbour(rows, columns int) int {
	spots := v.spots(v.columns)
	here := spots[v.current]
	if rows == 0 {
		for i, s := range spots {
			if s.row == here.row && s.column == here.column+columns {
				return i
			}
		}
		return -1
	}
	best := -1
	for i, s := range spots {
		if s.row == here.row+rows && (best < 0 || s.column <= here.column) {
			best = i
		}
	}
	return best
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
		text := card.description + "\n"
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
	target, move := -1, 0
	// h, j, k and l move through the grid as they read; Tab through every
	// card in order. Left of the first column is the list of sections.
	switch ev.Key() {
	case tcell.KeyEsc:
		v.settings.focusList()
		return nil
	case tcell.KeyLeft:
		if target = v.neighbour(0, -1); target < 0 {
			v.settings.focusList()
			return nil
		}
	case tcell.KeyRight:
		target = v.neighbour(0, 1)
	case tcell.KeyDown:
		target = v.neighbour(1, 0)
	case tcell.KeyUp:
		target = v.neighbour(-1, 0)
	case tcell.KeyTab:
		move = 1
	case tcell.KeyBacktab:
		move = -1
	case tcell.KeyRune:
		if card := v.cards[v.current]; card.onKey != nil && card.onKey(ev.Rune()) {
			v.paintFocus(true)
			return nil
		}
		switch ev.Rune() {
		case 'h':
			if target = v.neighbour(0, -1); target < 0 {
				v.settings.focusList()
				return nil
			}
		case 'l':
			target = v.neighbour(0, 1)
		case 'j':
			target = v.neighbour(1, 0)
		case 'k':
			target = v.neighbour(-1, 0)
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
		target = (v.current + move + len(v.cards)) % len(v.cards)
	}
	if target >= 0 && target != v.current {
		v.current = target
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
