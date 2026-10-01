package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
)

// tagColour is one colour of the palette tags choose from.
type tagColour struct{ name, hex string }

// tagPalette is sixteen pastels: light enough for dark text on every one, and
// apart enough to tell side by side. A terminal without true colour gets the
// nearest of its own, which tcell picks.
var tagPalette = []tagColour{
	{"rose", "#f4a6b8"},
	{"coral", "#f6ac9c"},
	{"peach", "#f8c4a0"},
	{"apricot", "#f7d49e"},
	{"butter", "#f2e6a2"},
	{"lime", "#d3eaa2"},
	{"mint", "#a8e6c4"},
	{"sage", "#bfd6b2"},
	{"teal", "#9fd8d2"},
	{"sky", "#a3d5f0"},
	{"azure", "#a8c2f2"},
	{"periwinkle", "#babaf4"},
	{"lavender", "#cdb8f2"},
	{"lilac", "#e2b9ec"},
	{"pink", "#f2b8d8"},
	{"sand", "#e3d4c0"},
}

// tagInk is the text on every pill: dark, since the palette is all light.
const tagInk = "#25232e"

// paletteIndex finds a colour by name; an unknown one is the first.
func paletteIndex(name string) int {
	for i, c := range tagPalette {
		if c.name == name {
			return i
		}
	}
	return 0
}

func tagHex(name string) string { return tagPalette[paletteIndex(name)].hex }

// pillEnds are the glyphs a pill starts and ends with, by style.
func pillEnds(style string) (left, right string) {
	switch style {
	case config.TagEndsCircles:
		return "◖", "◗"
	case config.TagEndsSquare:
		return "", ""
	}
	return "", ""
}

// pill draws a tag as a pill: its name on its colour, rounded at both ends
// when the style has ends, padded with a space when it has none. It returns
// the markup and how many cells it takes.
func pill(t config.Tag, style string) (string, int) {
	hex := tagHex(t.Color)
	left, right := pillEnds(style)
	text := t.Name
	if left == "" {
		text = " " + text + " "
	}
	width := len([]rune(text)) + len([]rune(left)) + len([]rune(right))
	var b strings.Builder
	if left != "" {
		b.WriteString("[" + hex + ":-]" + left)
	}
	b.WriteString("[" + tagInk + ":" + hex + "]" + tview.Escape(text))
	if right != "" {
		b.WriteString("[" + hex + ":-]" + right)
	}
	b.WriteString("[-:-:-]")
	return b.String(), width
}

// pills draws the named tags side by side, as many as fit in room; the rest
// are counted, +2, rather than cut in half.
func (a *App) pills(names []string, room int) (string, int) {
	var parts []string
	width := 0
	for i, name := range names {
		t, ok := a.cfg.Tag(name)
		if !ok {
			continue
		}
		markup, w := pill(t, a.cfg.Ends())
		more := ""
		if left := len(names) - i - 1; left > 0 {
			more = fmt.Sprintf(" +%d", left)
		}
		need := width + w + len(more)
		if width > 0 {
			need++
		}
		if need > room {
			rest := fmt.Sprintf("+%d", len(names)-i)
			if width > 0 {
				rest = " " + rest
			}
			if width+len(rest) <= room {
				parts = append(parts, tag(colDim)+rest+tagEnd)
				width += len(rest)
			}
			break
		}
		if width > 0 {
			parts = append(parts, " ")
			width++
		}
		parts = append(parts, markup)
		width += w
	}
	return strings.Join(parts, ""), width
}

// nameWithTags is a repository's name followed by its tags, in exactly width
// cells. The name gives way to the tags only down to a few letters.
func (a *App) nameWithTags(name string, tags []string, width int, colour tcell.Color) string {
	const keep = 12
	markup, w := "", 0
	if len(tags) > 0 {
		room := width - min(len([]rune(name)), keep) - 1
		markup, w = a.pills(tags, room)
	}
	nameW := width
	if w > 0 {
		nameW = width - w - 1
	}
	text := trunc(name, nameW)
	out := tag(colour) + tview.Escape(text) + tagEnd
	used := len([]rune(text))
	if w > 0 {
		out += " " + markup
		used += 1 + w
	}
	return out + strings.Repeat(" ", max(0, width-used))
}

// tagMark is the box in front of a tag in a multiple choice.
func tagMark(on bool) string {
	if on {
		return tag(colOn) + "✓" + tagEnd
	}
	return tag(colDim) + "·" + tagEnd
}

// showRepositoryTags puts tags on a repository and takes them off: space or
// Enter for each, Esc when done.
func (a *App) showRepositoryTags(instance, path string) {
	a.showToggles(toggles{
		title: "Tags of " + path,
		verb:  "on/off",
		items: func() []toggleItem {
			worn := a.cfg.TagsOf(instance, path)
			var items []toggleItem
			for _, t := range a.cfg.TagList() {
				markup, _ := pill(t, a.cfg.Ends())
				on := false
				for _, n := range worn {
					on = on || n == t.Name
				}
				items = append(items, toggleItem{Label: tagMark(on) + " " + markup, Search: t.Name, Data: t.Name})
			}
			return items
		},
		toggle: func(it toggleItem) {
			a.cfg.ToggleTag(instance, path, it.Data.(string))
			a.applyFilters()
		},
		status: func() string {
			if len(a.cfg.TagList()) == 0 {
				return tag(colWarn) + "no tags yet · Settings › Tags makes them" + tagEnd
			}
			return fmt.Sprintf("%s%d on%s", tag(colDim), len(a.cfg.TagsOf(instance, path)), tagEnd)
		},
	})
}

// showTagFilter narrows the repositories to those wearing any of the chosen
// tags.
func (a *App) showTagFilter() {
	f := &a.cfg.Filters
	a.showToggles(toggles{
		title: "Show the repositories tagged",
		verb:  "on/off",
		items: func() []toggleItem {
			var items []toggleItem
			for _, t := range a.cfg.TagList() {
				markup, _ := pill(t, a.cfg.Ends())
				on := false
				for _, n := range f.Tags {
					on = on || n == t.Name
				}
				items = append(items, toggleItem{Label: tagMark(on) + " " + markup, Search: t.Name, Data: t.Name})
			}
			return items
		},
		toggle: func(it toggleItem) {
			f.ToggleTagFilter(it.Data.(string))
			a.applyFilters()
		},
		status: func() string {
			if len(f.Tags) == 0 {
				return tag(colDim) + "showing every repository" + tagEnd
			}
			return fmt.Sprintf("%sany of %d%s", tag(colDim), len(f.Tags), tagEnd)
		},
		keys: []toggleKey{{key: 'a', hint: "all", run: func() {
			f.Tags = nil
			a.applyFilters()
		}}},
	})
}

// tagSummary is the part of the repositories' header that names the tags
// they are narrowed to.
func (a *App) tagSummary() string {
	if len(a.cfg.Filters.Tags) == 0 {
		return ""
	}
	var parts []string
	for _, n := range a.cfg.Filters.Tags {
		if t, ok := a.cfg.Tag(n); ok {
			markup, _ := pill(t, a.cfg.Ends())
			parts = append(parts, markup)
		}
	}
	return " · " + strings.Join(parts, " ") + tag(colMuted)
}

// ------------------------------------------------------------- settings

// tagEndsOrder is the order s cycles the pill styles in.
var tagEndsOrder = []string{config.TagEndsRounded, config.TagEndsCircles, config.TagEndsSquare}

func tagEndsLabel(style string) string {
	switch style {
	case config.TagEndsCircles:
		return "half circles"
	case config.TagEndsSquare:
		return "square"
	}
	return "rounded (Nerd Font)"
}

func (s *settingsView) newTagTable() *tview.Table {
	t := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' ')
	t.SetSelectedStyle(styleSelected)
	box(t.Box, "Tags").SetBorderPadding(0, 0, 1, 1)
	t.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		name := s.selectedTag()
		switch ev.Key() {
		case tcell.KeyEnter:
			if name != "" {
				s.showTagForm(name)
			}
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'a':
				s.showTagForm("")
			case 'e':
				if name != "" {
					s.showTagForm(name)
				}
			case 'd':
				if name != "" {
					s.confirmRemoveTag(name)
				}
			case 's':
				s.cycleTagEnds()
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

func (s *settingsView) selectedTag() string {
	row, _ := s.tags.GetSelection()
	if row < 1 || row >= s.tags.GetRowCount() {
		return ""
	}
	name, _ := s.tags.GetCell(row, 0).GetReference().(string)
	return name
}

// tagUse counts the repositories wearing each tag.
func (s *settingsView) tagUse() map[string]int {
	use := map[string]int{}
	for _, r := range s.app.cfg.RepositoryTags {
		for _, n := range r.Tags {
			use[n]++
		}
	}
	return use
}

func (s *settingsView) fillTags() {
	t := s.tags
	t.Clear()
	for c, h := range []string{"", "TAG", "COLOUR", "REPOSITORIES"} {
		t.SetCell(0, c, tview.NewTableCell(h).SetTextColor(colDim).SetSelectable(false))
	}
	t.SetCell(0, 4, tview.NewTableCell("").SetSelectable(false).SetExpansion(1))
	tags := s.app.cfg.TagList()
	if len(tags) == 0 {
		t.SetCell(1, 1, tview.NewTableCell("No tags - press a to make one").SetTextColor(colWarn).SetSelectable(false))
		return
	}
	use := s.tagUse()
	for i, tg := range tags {
		markup, _ := pill(tg, s.app.cfg.Ends())
		t.SetCell(i+1, 0, tview.NewTableCell(" ").SetReference(tg.Name))
		t.SetCell(i+1, 1, tview.NewTableCell(markup))
		t.SetCell(i+1, 2, tview.NewTableCell(tagPalette[paletteIndex(tg.Color)].name).SetTextColor(colMuted))
		count := ""
		if n := use[tg.Name]; n > 0 {
			count = fmt.Sprintf("%d", n)
		}
		t.SetCell(i+1, 3, tview.NewTableCell(count).SetTextColor(colMuted))
		t.SetCell(i+1, 4, tview.NewTableCell("").SetExpansion(1))
	}
	if row, _ := t.GetSelection(); row < 1 || row >= t.GetRowCount() {
		t.Select(1, 0)
	}
}

// colourOptions are the palette as a select shows it: a swatch and a name.
func colourOptions() []string {
	options := make([]string, len(tagPalette))
	for i, c := range tagPalette {
		options[i] = "[" + c.hex + "]████[-] " + c.name
	}
	return options
}

// showTagForm makes a tag, or changes the one called name.
func (s *settingsView) showTagForm(name string) {
	a := s.app
	current := config.Tag{Color: tagPalette[len(a.cfg.TagList())%len(tagPalette)].name}
	title := "New tag"
	if name != "" {
		current, _ = a.cfg.Tag(name)
		title = "Tag · " + name
	}
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Name", current.Name, 0, nil, nil)
	field := form.GetFormItemByLabel("Name").(*tview.InputField)
	colour := addSelect(form, "Colour", colourOptions(), paletteIndex(current.Color))
	form.AddButton("Save", func() {
		i, _ := colour.GetCurrentOption()
		next := config.Tag{Name: strings.TrimSpace(field.GetText()), Color: tagPalette[max(i, 0)].name}
		if !a.cfg.SetTag(name, next) {
			a.errorf("a tag needs a name no other tag has")
			return
		}
		a.closeModal(pageForm)
		a.applyFilters()
		s.fillTags()
		a.note("Saved tag " + next.Name)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized(title, form, 56, 8)
}

func (s *settingsView) confirmRemoveTag(name string) {
	a := s.app
	body := fmt.Sprintf("Remove the tag %s?", name)
	if n := s.tagUse()[name]; n > 0 {
		body = fmt.Sprintf("Remove the tag %s? %d repositor%s wear%s it.", name, n,
			plural(n, "y", "ies"), plural(n, "s", ""))
	}
	a.confirm("Remove tag", body, nil, func() {
		a.cfg.RemoveTag(name)
		a.applyFilters()
		s.fillTags()
		a.note("Removed tag " + name)
	})
}

// cycleTagEnds moves to the next pill style. The rounded one needs a Nerd
// Font, and only the user can see whether the terminal has one.
func (s *settingsView) cycleTagEnds() {
	a := s.app
	now := a.cfg.Ends()
	for i, style := range tagEndsOrder {
		if style == now {
			a.cfg.TagEnds = tagEndsOrder[(i+1)%len(tagEndsOrder)]
			break
		}
	}
	a.applyFilters()
	s.fillTags()
	a.note("Tags end " + tagEndsLabel(a.cfg.Ends()))
}
