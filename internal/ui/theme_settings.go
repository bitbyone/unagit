package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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
	box(s.themePanel.Box, "Theme").SetBorderPadding(0, 0, 1, 1)
	t.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
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
	notes = append(notes, tag(colMuted)+"Nerd Font icons "+tagEnd+tview.Escape(s.app.nerdWhy)+tag(colDim)+" · n changes"+tagEnd)
	notes = append(notes, tag(colDim)+"Your own themes go in "+tview.Escape(tildePath(s.app.cfg.ThemesDir()))+
		"/*.json. A theme names only what it changes; \"extends\" names the theme the rest comes from."+tagEnd)
	s.themeNotes.SetText(strings.Join(notes, "\n"))
	// A note wraps to two rows on a narrow terminal; one more keeps it off
	// the hint.
	s.themePanel.ResizeItem(s.themeNotes, 2*len(notes)+1, 0)
}

// themeSwatch shows a theme by its colours, each a block in the theme's own
// colour: its background, text, accent, the three states, its border and its
// selection band. onBand is for the row under the cursor, whose band shows
// through: a block of the band's own colour would vanish into it, so there it
// is a shaded block in the text's colour - the band itself is that colour.
func themeSwatch(t Theme, onBand bool) string {
	_, band, _ := styleSelected.Decompose()
	var b strings.Builder
	for _, c := range []string{t.Background, t.Text.Normal, t.Text.Muted, t.Text.Accent,
		t.State.Good, t.State.Warning, t.State.Bad, t.Border.Normal, t.Selection.Background} {
		col := colour(c)
		switch {
		case col == tcell.ColorDefault:
			// The terminal's own background has no colour to show.
			b.WriteString(tag(colDim) + "░░" + tagEnd)
		case onBand && col.Hex() == band.Hex():
			b.WriteString(tag(colText) + "░░" + tagEnd)
		default:
			b.WriteString(tag(col) + "██" + tagEnd)
		}
	}
	return b.String()
}
