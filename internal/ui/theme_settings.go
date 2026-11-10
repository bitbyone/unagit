package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
)

// Settings › Theme lists the themes there are - unagit's own and the user's,
// from <config>/themes - each with a strip of its colours, the one in use
// marked. Enter puts the one under the cursor on at once; f forks it into a
// file of the user's to tune, which is followed as it is saved; r reads the
// folder again. A file that cannot be used is listed with what is wrong
// with it.

// newThemeTable makes the section: the table of themes, and under it a note
// on where the user's own go and what could not be read, in one frame.
func (s *settingsView) newThemeTable() *tview.Table {
	t := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' ')
	t.SetSelectedStyle(styleSelected)
	s.themeNotes = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	// The swatches are blocks of colour drawn as ink, which the selection
	// band would turn into its own; they are drawn again over it.
	s.themesKept = newKeptTable(t, 1)
	s.themePanel = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.themesKept, 0, 1, true).
		AddItem(s.themeNotes, 3, 0, false)
	box(s.themePanel.Box, "Themes").SetBorderPadding(0, 0, 1, 1)
	s.newDisplayPane()
	s.themeSection = newThemeSection(s.themePanel, s.displayPanel)
	t.SetFocusFunc(func() { s.themeDisplay = false })
	t.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// Tab and l go over to the pane of how the themes are drawn.
		if ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyRune && ev.Rune() == 'l' {
			s.themeDisplay = true
			s.focusContent()
			return nil
		}
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		switch ev.Key() {
		case tcell.KeyEnter:
			s.useSelectedTheme()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case ' ':
				s.useSelectedTheme()
			case 'r':
				before := theme
				s.app.reloadThemes()
				if before.Name == theme.Name && s.app.themes.byName[theme.Name].file == before.file {
					s.app.note(fmt.Sprintf("%d themes · yours in %s", len(s.app.themes.names), tildePath(s.app.cfg.ThemesDir())))
				}
			case 'f':
				if name := s.selectedTheme(); name != "" {
					s.showForkForm(name)
				}
			case 'n':
				s.app.cycleNerdFont()
			case 'b':
				s.app.toggleTerminalBackground()
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			default:
				return ev
			}
			return nil
		}
		return ev
	})
	return t
}

// selectedTheme is the name of the theme under the cursor, "" on a row that
// is not one.
func (s *settingsView) selectedTheme() string {
	row, _ := s.themes.GetSelection()
	if name, ok := s.themes.GetCell(row, 0).GetReference().(string); ok {
		return name
	}
	return ""
}

func (s *settingsView) useSelectedTheme() {
	if name := s.selectedTheme(); name != "" {
		s.app.switchTheme(name)
	}
}

func (s *settingsView) fillThemes() {
	t := s.themes
	keep := s.selectedTheme()
	t.Clear()
	s.themesKept.reset()
	for c, h := range []string{"", "THEME", "COLOURS", "", "FROM"} {
		t.SetCell(0, c, tview.NewTableCell(h).SetTextColor(colDim).SetSelectable(false))
	}
	t.SetCell(0, 5, tview.NewTableCell("").SetSelectable(false).SetExpansion(1))
	set := s.app.themes
	// Where the swatches start in a row: after the mark and the names, each
	// column followed by the table's one-cell separator.
	nameW := len("THEME")
	for _, name := range set.names {
		nameW = max(nameW, tview.TaggedStringWidth(tview.Escape(name)))
	}
	swatchX := 1 + 1 + nameW + 1
	row, at := 1, 1
	for _, name := range set.names {
		th := set.byName[name]
		mark := " "
		if name == theme.Name {
			mark = tag(colOn) + glyphCheck + tagEnd
		}
		from := "built in"
		if th.file != "" {
			from = tildePath(th.file)
		}
		t.SetCell(row, 0, tview.NewTableCell(mark).SetReference(name))
		t.SetCell(row, 1, tview.NewTableCell(tview.Escape(name)).SetTextColor(colText))
		swatch := themeSwatch(th, false)
		t.SetCell(row, 2, tview.NewTableCell(swatch))
		on := themeSwatch(th, true)
		s.themesKept.keep(row, keptMarkup{x: swatchX, markup: on, width: tview.TaggedStringWidth(on)})
		t.SetCell(row, 3, tview.NewTableCell(tview.Escape(th.Description)).SetTextColor(colDim).SetMaxWidth(48))
		t.SetCell(row, 4, tview.NewTableCell(from).SetTextColor(colMuted))
		t.SetCell(row, 5, tview.NewTableCell("").SetExpansion(1))
		if name == keep || keep == "" && name == theme.Name {
			at = row
		}
		row++
	}
	t.Select(at, 0)

	var notes []string
	for _, problem := range set.problems {
		notes = append(notes, tag(colWarn)+"! "+tagEnd+tview.Escape(problem))
	}
	notes = append(notes, tag(colDim)+"Your own themes go in "+tview.Escape(tildePath(s.app.cfg.ThemesDir()))+
		"/*.json. A theme names only what it changes; \"extends\" names the theme the rest comes from."+tagEnd)
	s.themeNotes.SetText(strings.Join(notes, "\n"))
	// A note wraps to two rows on a narrow terminal; one more keeps it off
	// the hint.
	s.themePanel.ResizeItem(s.themeNotes, 2*len(notes)+1, 0)
	s.fillDisplay()
}

// displayChoice is a choice of how the themes are drawn: its values side by
// side, the one chosen lit, h and l moving to the one before or after, as
// an accordion folds out its options.
type displayChoice struct {
	name   string
	values []string
	at     func() int
	set    func(i int)
	about  func() string
}

func (s *settingsView) displayChoices() []displayChoice {
	a := s.app
	nerd := []string{"", config.NerdFontOn, config.NerdFontOff}
	return []displayChoice{{
		name:   "Nerd Font icons",
		values: []string{"auto", "on", "off"},
		at:     func() int { return max(0, slices.Index(nerd, a.cfg.NerdFont)) },
		set:    func(i int) { a.setNerdFont(nerd[i]) },
		about: func() string {
			return "Icons from a Nerd Font where a theme has them: told from the terminal, on, or off. Now " + a.nerdWhy + "."
		},
	}, {
		name:   "Background",
		values: []string{"theme", "terminal"},
		at: func() int {
			if a.cfg.TerminalBackground {
				return 1
			}
			return 0
		},
		set: func(i int) {
			if (i == 1) != a.cfg.TerminalBackground {
				a.toggleTerminalBackground()
			}
		},
		about: func() string {
			return "Whose background is under unagit: the theme's, or the terminal's own, so a translucent one shows through. Now " + backgroundWords(terminalBackground) + "."
		},
	}}
}

// newDisplayPane makes the pane of how the themes are drawn, beside them.
func (s *settingsView) newDisplayPane() {
	d := tview.NewTable().SetSelectable(true, false).SetSeparator(' ')
	d.SetSelectedStyle(styleSelected)
	s.display = d
	s.displayAbout = tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true)
	s.displayPanel = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d, len(s.displayChoices()), 0, true).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(s.displayAbout, 0, 1, false)
	box(s.displayPanel.Box, "Display").SetBorderPadding(1, 0, 1, 1)
	d.SetFocusFunc(func() { s.themeDisplay = true })
	d.SetSelectionChangedFunc(func(int, int) { s.fillDisplayAbout() })
	d.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		choices := s.displayChoices()
		row, _ := d.GetSelection()
		step := 0
		switch ev.Key() {
		case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyEsc:
			// Back to the themes; from there h and Esc go on to the sections.
			s.themeDisplay = false
			s.focusContent()
			return nil
		case tcell.KeyLeft:
			step = -1
		case tcell.KeyRight, tcell.KeyEnter:
			step = 1
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'h':
				step = -1
			case 'l', ' ':
				step = 1
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			case 'n':
				s.app.cycleNerdFont()
				return nil
			case 'b':
				s.app.toggleTerminalBackground()
				return nil
			default:
				if ev, handled := s.contentKeys(ev); handled {
					return ev
				}
				return ev
			}
		default:
			return ev
		}
		if row < 0 || row >= len(choices) {
			return nil
		}
		c := choices[row]
		next := c.at() + step
		// Enter and Space go round; h and l stop at the ends.
		if ev.Key() == tcell.KeyEnter || ev.Rune() == ' ' {
			next %= len(c.values)
		}
		if next >= 0 && next < len(c.values) && next != c.at() {
			c.set(next)
			s.fillDisplay()
		}
		return nil
	})
}

// fillDisplay writes the choices with the one chosen of each lit.
func (s *settingsView) fillDisplay() {
	d := s.display
	row, _ := d.GetSelection()
	d.Clear()
	nameW := 0
	choices := s.displayChoices()
	for _, c := range choices {
		nameW = max(nameW, cells(c.name))
	}
	for r, c := range choices {
		var values []string
		for i, v := range c.values {
			if i == c.at() {
				values = append(values, tag(colOn)+"‹"+tview.Escape(v)+"›"+tagEnd)
				continue
			}
			values = append(values, tag(colMuted)+" "+tview.Escape(v)+" "+tagEnd)
		}
		d.SetCell(r, 0, tview.NewTableCell(tview.Escape(c.name)).SetTextColor(colText).SetMaxWidth(nameW))
		d.SetCell(r, 1, tview.NewTableCell(strings.Join(values, "")).SetExpansion(1))
	}
	d.Select(max(0, min(row, len(choices)-1)), 0)
	s.fillDisplayAbout()
}

// displayRow is the choice under the Display pane's cursor.
func (s *settingsView) displayRow() int {
	row, _ := s.display.GetSelection()
	return row
}

func (s *settingsView) fillDisplayAbout() {
	choices := s.displayChoices()
	if row := s.displayRow(); row >= 0 && row < len(choices) {
		s.displayAbout.SetText(tag(colDim) + tview.Escape(choices[row].about()) + tagEnd)
	}
}

// themeSection lays the themes and the Display pane side by side, the
// themes the wider, or one under the other where the two would not fit.
type themeSection struct {
	*tview.Flex
	themes, display tview.Primitive
	stacked         bool
}

// themeSectionWide is the narrowest the two fit side by side in: the
// themes' names, colours and descriptions, and the choices beside them.
const themeSectionWide = 110

func newThemeSection(themes, display tview.Primitive) *themeSection {
	t := &themeSection{Flex: tview.NewFlex(), themes: themes, display: display}
	t.lay(false)
	return t
}

func (t *themeSection) lay(stacked bool) {
	t.stacked = stacked
	t.Clear()
	if stacked {
		t.SetDirection(tview.FlexRow).AddItem(t.themes, 0, 1, true).AddItem(t.display, 9, 0, false)
		return
	}
	t.SetDirection(tview.FlexColumn).AddItem(t.themes, 0, 13, true).AddItem(t.display, 0, 7, false)
}

func (t *themeSection) Draw(screen tcell.Screen) {
	if _, _, w, _ := t.GetRect(); (w < themeSectionWide) != t.stacked {
		t.lay(w < themeSectionWide)
	}
	t.Flex.Draw(screen)
}

// themeSwatch shows a theme by its colours, each a block in the theme's own
// colour: its background, text, accent, the three states, its border and its
// selection band. onBand is for the row under the cursor, whose band shows
// through. A block of the colour it is drawn on - the band, or the screen of
// the theme on, which another theme can share - would vanish into it, so
// there it is a shaded block in the text's colour: what is under it is that
// colour.
func themeSwatch(t Theme, onBand bool) string {
	_, under, _ := styleSelected.Decompose()
	if !onBand {
		under = colBackground
	}
	var b strings.Builder
	for _, c := range []string{t.Background, t.Text.Normal, t.Text.Muted, t.Text.Accent,
		t.State.Good, t.State.Warning, t.State.Bad, t.Border.Normal, t.Selection.Background} {
		col := colour(c)
		switch {
		case col == tcell.ColorDefault:
			// The terminal's own background has no colour to show.
			b.WriteString(tag(colDim) + "░░" + tagEnd)
		case under != tcell.ColorDefault && col.Hex() == under.Hex():
			b.WriteString(tag(colText) + "░░" + tagEnd)
		default:
			b.WriteString(tag(col) + "██" + tagEnd)
		}
	}
	return b.String()
}
