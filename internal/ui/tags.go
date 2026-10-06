package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// tagColour is one colour of the palette tags choose from: a deep fill and
// the light ink of the same hue that is written on it.
type tagColour struct{ name, ink, fill string }

// tagPalette is sixteen colours, each a pastel written on a deep shade of
// itself: dark enough for the ink to read on every one, and apart enough to
// tell side by side. A terminal without true colour gets the nearest of its
// own, which tcell picks. The names are what a tag is saved with; the
// colours come from the theme (setTheme).
var tagPalette = []tagColour{
	{name: "rose"}, {name: "coral"}, {name: "peach"}, {name: "apricot"},
	{name: "butter"}, {name: "lime"}, {name: "mint"}, {name: "sage"},
	{name: "teal"}, {name: "sky"}, {name: "azure"}, {name: "periwinkle"},
	{name: "lavender"}, {name: "lilac"}, {name: "pink"}, {name: "sand"},
}

// paletteIndex finds a colour by name; an unknown one is the first.
func paletteIndex(name string) int {
	for i, c := range tagPalette {
		if c.name == name {
			return i
		}
	}
	return 0
}

func tagColourOf(name string) tagColour { return tagPalette[paletteIndex(name)] }

// pillEnds are the glyphs a pill starts and ends with, by style.
func pillEnds(style string) (left, right string) {
	switch style {
	case config.TagEndsCircles:
		return "◖", "◗"
	case config.TagEndsSquare:
		return "", ""
	}
	return "\ue0b6", "\ue0b4"
}

// behind is what a pill's ends are drawn on: the list's own background, or
// the selection band when the pill is drawn again over it.
const behindList = "-"

func behindBand() string { _, bg, _ := styleSelected.Decompose(); return bg.String() }

// pill draws a tag as a pill: its name in light ink on a deep fill, rounded
// at both ends when the style has ends, padded with a space when it has
// none. It returns the markup and how many cells it takes.
func pill(t config.Tag, style, behind string) (string, int) {
	return pillOf(t.Name, tagColourOf(t.Color), style, behind)
}

// pillOf draws any text as a pill in the given colours.
func pillOf(text string, c tagColour, style, behind string) (string, int) {
	left, right := pillEnds(style)
	if left == "" {
		text = " " + text + " "
	}
	width := len([]rune(text)) + len([]rune(left)) + len([]rune(right))
	var b strings.Builder
	if left != "" {
		b.WriteString("[" + c.fill + ":" + behind + "]" + left)
	}
	b.WriteString("[" + c.ink + ":" + c.fill + "]" + tview.Escape(text))
	if right != "" {
		b.WriteString("[" + c.fill + ":" + behind + "]" + right)
	}
	b.WriteString("[-:-:-]")
	return b.String(), width
}

// pills draws the named tags side by side, as many as fit in room; the rest
// are counted, +2, rather than cut in half.
func (a *App) pills(names []string, room int, behind string) (string, int) {
	var parts []string
	width := 0
	for i, name := range names {
		t, ok := a.cfg.Tag(name)
		if !ok {
			continue
		}
		markup, w := pill(t, a.cfg.Ends(), behind)
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

// tagsField is the tags of a row as pills, in exactly width cells, after the
// chezmoi badge when the row is the repository chezmoi keeps. The pills come
// back as well, to be drawn again over the selection band, at where they
// start in the field. A marked row has a band of its own under both.
func (a *App) tagsField(tags []string, width int, marked, managed bool, starred *forge.Project) (string, keptMarkup) {
	behind, band := behindList, behindBand()
	if marked {
		_, bg, _ := styleMarkedSelected.Decompose()
		behind, band = colMarked.String(), bg.String()
	}
	// A badge is the chezmoi one, or the star of a repository cloned from
	// the stars; both say where the row comes from, not a tag of the user's.
	badgeOn := func(room int, behind string) (string, int) {
		switch {
		case managed:
			return chezmoiBadge(room, a.cfg.Ends(), behind)
		case starred != nil:
			return a.starredBadge(*starred, room, a.cfg.Ends(), behind)
		}
		return "", 0
	}
	roomBeside := func(bw int) int {
		if bw > 0 {
			return width - bw - 1
		}
		return width
	}
	// The badge gives way to the tags: it shortens, step by step, until
	// every tag fits beside it, and only at its shortest are tags counted
	// away.
	badge, bw := badgeOn(width, behind)
	markup, w := a.pills(tags, roomBeside(bw), behind)
	if _, all := a.pills(tags, math.MaxInt, behind); w < all {
		for bw > 1 {
			shorter, sw := badgeOn(bw-1, behind)
			if sw == 0 {
				break
			}
			badge, bw = shorter, sw
			if markup, w = a.pills(tags, roomBeside(bw), behind); w == all {
				break
			}
		}
	}
	room := roomBeside(bw)
	var kept keptMarkup
	if w > 0 {
		kept.markup, kept.width = a.pills(tags, room, band)
		if marked {
			kept.banded = markup
		}
	}
	if bw > 0 {
		gap := ""
		if w > 0 {
			gap, w = " ", w+1
		}
		banded, _ := badgeOn(bw, band)
		markup = badge + gap + markup
		kept.markup, kept.width = banded+gap+kept.markup, bw+w
		if marked {
			kept.banded = markup
		}
		w += bw
	}
	return markup + strings.Repeat(" ", max(0, width-w)), kept
}

// tagMark is the box in front of a tag in a multiple choice.
func tagMark(on bool) string {
	if on {
		return tag(colOn) + glyphCheck + tagEnd
	}
	return tag(colDim) + "·" + tagEnd
}

// showTagChoice puts tags on a server, a group or a repository and takes them
// off: space or Enter for each, Esc when done. A tag passed down from above
// says so, and taking it off here leaves the server or group as it is.
func (a *App) showTagChoice(title string, worn, inherited func() []string, toggle func(name string)) {
	has := func(names []string, name string) bool {
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}
	a.showToggles(toggles{
		title: title,
		verb:  "on/off",
		items: func() []toggleItem {
			on, from := worn(), inherited()
			var items []toggleItem
			for _, t := range a.cfg.TagList() {
				markup, _ := pill(t, a.cfg.Ends(), behindList)
				label := tagMark(has(on, t.Name)) + " " + markup
				switch {
				case has(from, t.Name) && has(on, t.Name):
					label += "  " + tag(colDim) + "inherited" + tagEnd
				case has(from, t.Name):
					label += "  " + tag(colDim) + "inherited, taken off here" + tagEnd
				}
				items = append(items, toggleItem{Label: label, Search: t.Name, Data: t.Name})
			}
			return items
		},
		toggle: func(it toggleItem) { toggle(it.Data.(string)) },
		status: func() string {
			if len(a.cfg.TagList()) == 0 {
				return tag(colWarn) + "no tags yet · Settings › Tags makes them" + tagEnd
			}
			return fmt.Sprintf("%s%d on%s", tag(colDim), len(worn()), tagEnd)
		},
	})
}

// showRepositoryTags chooses the tags of one repository.
func (a *App) showRepositoryTags(instance, path string) {
	a.showTagChoice("Tags of "+path,
		func() []string { return a.cfg.TagsOf(instance, path) },
		func() []string { return a.cfg.InheritedTags(instance, path) },
		func(name string) {
			a.cfg.ToggleTag(instance, path, name)
			a.applyFilters()
		})
}

// showGroupTags chooses the tags of a group, which its subgroups and their
// repositories wear too.
func (a *App) showGroupTags(instance, path string) {
	a.showTagChoice("Tags of "+path+" · its subgroups and repositories inherit them",
		func() []string { return a.cfg.GroupTagsOf(instance, path) },
		func() []string { return a.cfg.InheritedTags(instance, path) },
		func(name string) {
			a.cfg.ToggleGroupTag(instance, path, name)
			a.applyFilters()
			a.settings.fillTree()
		})
}

// showServerTags chooses the tags of a whole server, which everything on it
// wears too.
func (a *App) showServerTags(instance string) {
	a.showTagChoice("Tags of "+a.instanceLabel(instance)+" · everything on it inherits them",
		func() []string { return a.cfg.ServerTagsOf(instance) },
		func() []string { return nil },
		func(name string) {
			a.cfg.ToggleServerTag(instance, name)
			a.applyFilters()
			a.settings.fillTree()
		})
}

// showViewOptions switches what the repository list shows and how, and
// which of its columns.
func (a *App) showViewOptions() {
	f := &a.cfg.Filters
	type option struct {
		label string
		on    func() bool
		flip  func()
	}
	options := []option{
		{"grouped by group (Ctrl-G)", func() bool { return f.GroupRepositories }, func() { f.GroupRepositories = !f.GroupRepositories }},
		{"favourites first, flat (o)", f.FavouritesFirst, func() { f.FavouritesInPlace = !f.FavouritesInPlace }},
		{"only what is cloned (L)", func() bool { return f.ClonedOnly }, func() { f.ClonedOnly = !f.ClonedOnly }},
	}
	a.showToggles(toggles{
		title: "View · Repositories",
		verb:  "on/off",
		items: func() []toggleItem {
			items := make([]toggleItem, len(options))
			for i, o := range options {
				items[i] = toggleItem{Label: tagMark(o.on()) + " " + o.label, Search: o.label, Data: i}
			}
			return append(items, a.columnItems(config.ListRepositories)...)
		},
		toggle: func(it toggleItem) {
			if a.toggleColumnItem(it) {
				return
			}
			options[it.Data.(int)].flip()
			a.applyFilters()
		},
	})
}

// showTagFilter narrows the repositories to those wearing all of the chosen
// tags.
func (a *App) showTagFilter() {
	f := &a.cfg.Filters
	a.showToggles(toggles{
		title: "Show the repositories tagged",
		verb:  "on/off",
		items: func() []toggleItem {
			var items []toggleItem
			for _, t := range a.cfg.TagList() {
				markup, _ := pill(t, a.cfg.Ends(), behindList)
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
			return fmt.Sprintf("%sall of %d%s", tag(colDim), len(f.Tags), tagEnd)
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
			markup, _ := pill(t, a.cfg.Ends(), behindList)
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

// tagUse counts the repositories wearing each tag, inherited ones included.
func (s *settingsView) tagUse() map[string]int {
	use := map[string]int{}
	for _, p := range s.app.projects {
		for _, n := range s.app.cfg.TagsOf(p.Instance, p.PathWithNamespace) {
			use[n]++
		}
	}
	return use
}

func (s *settingsView) fillTags() {
	t := s.tags
	t.Clear()
	s.tagsKept.reset()
	for c, h := range []string{"TAG", "COLOUR", "REPOSITORIES"} {
		t.SetCell(0, c, tview.NewTableCell(h).SetTextColor(colDim).SetSelectable(false))
	}
	t.SetCell(0, 3, tview.NewTableCell("").SetSelectable(false).SetExpansion(1))
	tags := s.app.cfg.TagList()
	if len(tags) == 0 {
		t.SetCell(1, 0, tview.NewTableCell("No tags - press a to make one").SetTextColor(colWarn).SetSelectable(false))
		return
	}
	use := s.tagUse()
	for i, tg := range tags {
		// The pill is the first column, so it can be drawn again, at the
		// start of the row, over the selection band.
		markup, width := pill(tg, s.app.cfg.Ends(), behindList)
		t.SetCell(i+1, 0, tview.NewTableCell(markup).SetReference(tg.Name))
		over, _ := pill(tg, s.app.cfg.Ends(), behindBand())
		s.tagsKept.keep(i+1, keptMarkup{markup: over, width: width})
		t.SetCell(i+1, 1, tview.NewTableCell(tagColourOf(tg.Color).name).SetTextColor(colMuted))
		count := ""
		if n := use[tg.Name]; n > 0 {
			count = fmt.Sprintf("%d", n)
		}
		t.SetCell(i+1, 2, tview.NewTableCell(count).SetTextColor(colMuted))
		t.SetCell(i+1, 3, tview.NewTableCell("").SetExpansion(1))
	}
	if row, _ := t.GetSelection(); row < 1 || row >= t.GetRowCount() {
		t.Select(1, 0)
	}
}

// colourOptions are the palette as a select shows it: each name in its own
// ink on its own fill.
func colourOptions() []string {
	options := make([]string, len(tagPalette))
	for i, c := range tagPalette {
		options[i] = "[" + c.ink + ":" + c.fill + "] " + c.name + " [-:-:-]"
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
		a.done("Saved tag " + next.Name)
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
		a.done("Removed tag " + name)
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

// tagsLine puts every tag of a repository under its name in the detail,
// where there is room for the ones the list had to count away.
func (a *App) tagsLine(d *detailBuf, pr forge.Project) {
	tags := a.cfg.TagsOf(pr.Instance, pr.PathWithNamespace)
	if markup, w := a.pills(tags, math.MaxInt, behindList); w > 0 {
		d.raw(markup + "\n")
	}
}
