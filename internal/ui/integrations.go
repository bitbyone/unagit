package ui

import (
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
	// agent is the agent a card is, whose glyph is its icon.
	agent   string
	enabled func() bool
	toggle  func()
	// found, when set, is a line under the description saying what the
	// integration found on this machine.
	found func() string
	check func()
	// onKey is a card's own letters, before the shared ones; it reports
	// whether it took the letter. keys names them in the card's hint.
	onKey func(r rune) bool
	keys  string
	// title, when set, is the card's name as its top edge shows it.
	title func() string
	// missing, when set, says what to do when it is not installed, and
	// absent is the state then, "not installed" unless set.
	missing, absent func() string
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
		description: "Shows a merge request's comments in your editor, beside the code they are about. Ctrl-R imports them before a review opens.",
		enabled:     func() bool { return s.app.cfg.Integrations.Incomm },
		toggle:      func() { s.app.cfg.Integrations.Incomm = !s.app.cfg.Integrations.Incomm },
	}, {
		name: "Hunk", command: "hunk",
		description: "Reviews changes in the terminal. D opens the changes of the selected row; a grouped worktree opens as one review across its repositories.",
		enabled:     s.app.hunkOn,
		toggle: func() {
			on := !s.app.hunkOn()
			s.app.cfg.Integrations.Hunk = &on
		},
	}, {
		name: "Chezmoi", command: "chezmoi",
		description: "Uses chezmoi's checkout of your dotfiles repository instead of cloning it a second time. Its worktrees and merge requests work as for any repository.",
		enabled:     s.app.chezmoiOn,
		toggle: func() {
			on := !s.app.chezmoiOn()
			s.app.cfg.Integrations.Chezmoi = &on
			s.app.detectChezmoi()
		},
		found: s.app.chezmoiFound,
	}, {
		name: "Zoxide", command: "zoxide",
		description: "Records the directories you work in and sorts repositories and worktrees by how often and how recently you visited them.",
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
		description: "Browse Files opens the selected directory in Yazi. A file chosen there opens in your favourite editor.",
		enabled:     s.app.yaziOn,
		toggle:      func() { on := !s.app.yaziOn(); s.app.cfg.Integrations.Yazi = &on },
		found:       func() string { return hint },
		check:       func() { hint = s.app.yaziFound() },
	}
	byName["Zellij"] = &integrationCard{
		name: "Zellij", command: "zellij",
		description: "When unagit runs in Zellij, editors and agents can also be opened in a new Zellij tab or in a split beside unagit.",
		enabled:     s.app.zellijOn,
		toggle: func() {
			on := !s.app.zellijOn()
			s.app.cfg.Integrations.Zellij = &on
			s.app.detectMultiplexer()
		},
	}
	byName["Herdr"] = &integrationCard{
		name: "Herdr", command: "herdr",
		description: "Coding agents can be started in a herdr workspace, where herdr reports what each is doing. When unagit runs in herdr, editors can also be opened in its tabs and splits.",
		enabled:     s.app.herdrOn,
		toggle: func() {
			on := !s.app.herdrOn()
			s.app.cfg.Integrations.Herdr = &on
			s.app.detectMultiplexer()
		},
	}
	byName["Ghostty"] = &integrationCard{
		name: "Ghostty", command: "ghostty",
		description: "Editors and agents can also be opened in a new Ghostty window or tab, or in a split beside unagit when it runs in Ghostty. macOS asks once for permission.",
		enabled:     s.app.ghosttyOn,
		toggle: func() {
			on := !s.app.ghosttyOn()
			s.app.cfg.Integrations.Ghostty = &on
		},
	}
	byName["Notifications"] = s.app.notificationsCard()
	editorCards := v.editorCards()
	var agentCards []*integrationCard
	for _, ag := range agents.All {
		ag := ag
		agentCards = append(agentCards, &integrationCard{
			name: ag.Name, command: ag.Command, agent: ag.ID,
			description: "Open with " + ag.Name + "… starts " + ag.Name + " in the selected repository, merge request or worktree: in this terminal, or wherever else it can be opened.",
			enabled:     func() bool { return s.app.agentOn(ag) },
			toggle:      func() { s.app.setAgentOn(ag, !s.app.agentOn(ag)) },
		})
	}
	// By what they are for: what opens the code, what reviews it, where
	// things open beside unagit, the agents, and the rest of the desk.
	v.categories = []integrationCategory{
		{"Editors", editorCards},
		{"Review", []*integrationCard{byName["Incomm"], byName["Hunk"]}},
		{"Terminals", []*integrationCard{byName["Zellij"], byName["Herdr"], byName["Ghostty"], byName["Notifications"]}},
		{"AI Agents", agentCards},
		{"Files & Navigation", []*integrationCard{byName["Zoxide"], byName["Yazi"], byName["Chezmoi"]}},
	}
	for _, c := range v.categories {
		v.cards = append(v.cards, c.cards...)
	}
	for _, card := range v.cards {
		card.view = tview.NewTextView().SetDynamicColors(true).SetScrollable(false).SetTextColor(colText)
		box(card.view.Box, card.name).SetBorderPadding(1, 0, 2, 2)
		if card.title == nil {
			card.title = func() string { return card.name }
		}
		// A card stands out from the page, as a tile on it.
		card.view.SetBackgroundColor(colCard)
		card.view.SetInputCapture(v.keys)
	}
	v.check()
	return v
}

// integrationColumns is how many cards stand side by side in a panel this
// wide inside: four on a very large screen - past 400 columns - three on a
// large one, two on an ordinary one, one on a narrow one.
func integrationColumns(width int) int {
	switch {
	case width >= 360:
		return 4
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
	// A card at the edge is drawn in part, sliding under it as the page
	// scrolls: tview would draw it past its rectangle and over the panel's
	// frame, so it is drawn through a screen that keeps to the room there is.
	shows := func(t, h int) bool { return t+h > v.offset && t < v.offset+room }
	clip := clippedScreen{Screen: screen, x: x, y: y, w: width, h: room}
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
		if !shows(at.top, at.height) {
			c.view.SetRect(0, 0, 0, 0)
			continue
		}
		c.view.SetRect(x+spots[i].column*(cardW+gap), y+at.top-v.offset, cardW, at.height)
		c.view.Draw(clip)
		drawCardState(clip, c)
	}
}

// clippedScreen draws only inside a rectangle and drops the rest.
type clippedScreen struct {
	tcell.Screen
	x, y, w, h int
}

func (c clippedScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	if x >= c.x && x < c.x+c.w && y >= c.y && y < c.y+c.h {
		c.Screen.SetContent(x, y, primary, combining, style)
	}
}

// cardState is whether an integration is on, and the role it is drawn in.
func cardState(c *integrationCard) (string, string) {
	switch {
	case c.binary == "" && c.absent != nil:
		return c.absent(), "integration.missing"
	case c.binary == "":
		return "not installed", "integration.missing"
	case c.enabled():
		return "enabled", "integration.enabled"
	}
	return "disabled", "integration.disabled"
}

// drawCardState puts the state at the right end of the card's top edge: an
// icon in its colour - a cell filled with it without a Nerd Font - then the
// word, so it reads at a glance down a column of cards. The editors card
// has no state of its own.
func drawCardState(screen tcell.Screen, c *integrationCard) {
	if c.enabled == nil {
		return
	}
	x, y, w, _ := c.view.GetRect()
	word, colourRole := cardState(c)
	colour := role(colourRole)
	// " ■ word " ending one rule cell short of the corner.
	start := x + w - 2 - (len(word) + 4)
	if start <= x+2+cells(plainText(c.title()))+3 {
		return
	}
	style := baseStyle().Background(colCard).Foreground(colText)
	screen.SetContent(start, y, ' ', nil, style)
	if icon := []rune(glyphIntegrationState); len(icon) > 0 {
		screen.SetContent(start+1, y, icon[0], icon[1:], style.Foreground(colour))
	} else {
		screen.SetContent(start+1, y, ' ', nil, style.Background(colour))
	}
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

// icon is what goes before a card's name: the integration's own icon,
// where the theme has one and the terminal a Nerd Font, and a space.
func (c *integrationCard) icon() string {
	icon := integrationIcons[c.name]
	if c.agent != "" && nerdFont {
		icon = agentIcons[c.agent]
	}
	if icon == "" {
		return ""
	}
	return icon + " "
}

func (v *integrationsView) paintFocus(active bool) {
	v.active = active
	for i, card := range v.cards {
		focused := active && i == v.current
		focusBox(card.view.Box, focused)
		card.view.SetTitle(" " + card.icon() + card.title() + " ")
		text := card.description + "\n"
		if card.found != nil && card.binary != "" {
			if found := card.found(); found != "" {
				text += found + "\n"
			}
		}
		if card.binary == "" && card.missing != nil {
			text += tag(colMuted) + card.missing() + tagEnd
		} else if card.binary == "" && card.command == "ghostty" {
			text += tag(colMuted) + "Install Ghostty; it is scripted on macOS only." + tagEnd
		} else if card.binary == "" {
			text += tag(colMuted) + "Install " + card.command + " and add it to PATH." + tagEnd
		} else {
			text += tag(colMuted) + tview.Escape(card.binary) + tagEnd
		}
		text += "\n\n"
		if focused {
			var hints []string
			if card.keys != "" {
				hints = append(hints, card.keys)
			}
			if card.binary != "" {
				hints = append(hints, "e toggle")
			}
			text += litHint(strings.Join(append(hints, "c check"), " · "))
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
	if id, ok := strings.CutPrefix(command, editorCommand); ok {
		if e, found := editors.Pick(v.editors, id); found && e.Found {
			return e.Where
		}
		return ""
	}
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
