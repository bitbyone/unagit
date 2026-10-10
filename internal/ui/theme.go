package ui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/md"
)

// The colours everything is drawn with, set from the theme (themes.go) by
// setTheme. They are package variables, as tview's own styles are: a theme
// is the process's, and every application of it draws with the same one.
var (
	colBackground  tcell.Color // the screen; ColorDefault is the terminal's own
	colBorder      tcell.Color
	colBorderFocus tcell.Color
	colTitle       tcell.Color
	colMuted       tcell.Color
	colDim         tcell.Color
	colText        tcell.Color
	colAccent      tcell.Color
	colOn          tcell.Color
	colWarn        tcell.Color
	colStar        tcell.Color
	colBad         tcell.Color
	// colForce is a step short of colBad: something to do on purpose, not
	// something wrong.
	colForce        tcell.Color
	colBranch       tcell.Color
	colTabActive    tcell.Color
	colTabInactive  tcell.Color
	colTabSeparator tcell.Color
	// colSurface is a raised panel: the background of a field or a button at
	// rest, and the ink on one that is active.
	colSurface tcell.Color
	colRaised  tcell.Color
	// colFieldFocus is the field a form's focus is on, colFieldTyping the same
	// while it is typed into.
	colFieldFocus  tcell.Color
	colFieldTyping tcell.Color
	// colKey marks the letter that presses a button.
	colKey tcell.Color
	// colPicker is the background of a picker that leaves the screen behind
	// undimmed (pickerOptions.bright), a step darker than the screen's.
	colPicker tcell.Color
)

// The glyphs that say what something is, set from the theme.
var (
	glyphDiskNone, glyphDiskBranch, glyphDiskReview, glyphDiskBoth string
	glyphEditor, glyphWatched                                      string
	glyphWorktree, glyphGroup, glyphHidden, glyphFavourite         string
	glyphRepos, glyphForgeGitHub, glyphForgeGitLab, glyphDraft     string
	glyphIntegrationState, glyphEditorNeovim                       string
	// agentIcons are the agents' icons by their id, empty without a Nerd
	// Font.
	agentIcons map[string]string
	// actionIcons are the theme's icons of the actions, nil without a
	// Nerd Font.
	actionIcons map[string]string
	// columnIcons are the theme's icons of the columns' headings, nil
	// without a Nerd Font.
	columnIcons map[string]string
	// integrationIcons are the theme's icons of the integrations' cards,
	// nil without a Nerd Font.
	integrationIcons map[string]string
	// sectionIcons are the theme's icons of Settings' sections, nil
	// without a Nerd Font.
	sectionIcons                                            map[string]string
	glyphCheck, glyphCross, glyphDot, glyphRing             string
	glyphManual, glyphScheduled, glyphTrigger, glyphRetried string
	glyphUser, glyphStarred, glyphAgent                     string
	glyphCIDone, glyphCIIdle, glyphApproved                 string
	glyphAhead, glyphBehind                                 string
	glyphExternal, glyphMerge, glyphBar, glyphTabSeparator  string
	glyphElided, glyphElidedGroup, glyphEdits               string
	glyphPicked, glyphUnpicked, glyphFolded, glyphUnfolded  string
	glyphCIUnknown                                          string
	glyphColumnShown, glyphColumnHidden                     string
	glyphMask                                               rune
	// selectMarker ends a closed select, so it looks like something that
	// opens.
	selectMarker string
)

// selectPadding is the room around an option the widest of the two takes
// (the open list's two spaces either side).
const selectPadding = 4

// theme is the theme in use. themeOnce puts the default one in place the
// first time anything is drawn; setTheme changes it after.
var (
	theme     Theme
	themeOnce sync.Once
)

// applyTheme puts the default theme in place unless one is already: tview's
// widgets copy its styles when they are made, so this comes before any.
func applyTheme() {
	themeOnce.Do(func() {
		if theme.Name == "" {
			setTheme(loadThemes("").byName[defaultThemeName])
		}
	})
}

// setTheme makes t the theme everything is drawn with from now on. Widgets
// already made keep what they copied; the App rebuilds them (switchTheme).
//
// tview builds every interactive widget - buttons, drop-downs, check boxes,
// form fields - out of one pair of colours used both ways round:
//
//	at rest:  background ContrastBackgroundColor, text PrimaryTextColor
//	active:   background PrimaryTextColor,        text ContrastBackgroundColor
//
// So the pair has to read in both directions. PrimaryTextColor is the light
// half and ContrastBackgroundColor the dark one: as a background the dark one
// is a raised surface, as a text colour it is dark ink on the light active
// background. Getting this pair right is what keeps every widget legible
// without being styled one at a time - an earlier version left the dark half
// as the terminal default, which cannot be reasoned about as ink, and buttons
// and drop-downs came out invisible.
//
// PrimitiveBackgroundColor is the screen's background: the terminal default
// unless the theme says otherwise, so panels can be transparent and sit on
// whatever the editor around them looks like.
// nerdFont is whether the terminal draws Nerd Font icons, so a theme's
// nerd_glyphs are used (nerdfont.go). Like the theme it is the process's.
var nerdFont bool

// terminalBackground leaves the screen on the terminal's own background
// whatever the theme paints (Settings › Theme b), so a translucent or
// blurred terminal shows through. What is worked out of the background -
// the tags' colours, the heat - still goes by the theme's, which is what
// its palette was made for. Like the theme it is the process's.
var terminalBackground bool

func setTheme(t Theme) {
	theme = t

	colBackground = colour(t.Background)
	if terminalBackground {
		colBackground = tcell.ColorDefault
	}
	colText, colMuted, colDim = colour(t.Text.Normal), colour(t.Text.Muted), colour(t.Text.Dim)
	colAccent, colBranch, colKey = colour(t.Text.Accent), colour(t.Text.Branch), colour(t.Text.Key)
	colOn, colWarn, colBad = colour(t.State.Good), colour(t.State.Warning), colour(t.State.Bad)
	colForce, colStar = colour(t.State.Force), colour(t.State.Favourite)
	colBorder, colBorderFocus, colTitle = colour(t.Border.Normal), colour(t.Border.Focus), colour(t.Border.Title)
	colTabActive, colTabInactive, colTabSeparator = colour(t.Tabs.Active), colour(t.Tabs.Inactive), colour(t.Tabs.Separator)
	colSurface, colRaised = colour(t.Surface.Field), colour(t.Surface.Raised)
	colFieldFocus, colFieldTyping = colour(t.Surface.FieldFocus), colour(t.Surface.FieldTyping)

	styleSelected = tcell.StyleDefault.
		Background(colour(t.Selection.Background)).
		Foreground(colour(t.Selection.Text)).
		Bold(true)
	colMarked = colour(t.Selection.Marked)
	styleMarkedSelected = styleSelected.Background(colour(t.Selection.MarkedCursor))
	dimFactor, colDimmedText = t.Backdrop.Dim, colour(t.Backdrop.Text)

	mdTheme = md.Theme{
		Text: colour(t.Markdown.Text).String(), Heading: colour(t.Markdown.Heading).String(),
		Code: colour(t.Markdown.Code).String(), Quote: colour(t.Markdown.Quote).String(),
		Link: colour(t.Markdown.Link).String(), Muted: colour(t.Markdown.Muted).String(),
	}
	chezmoiColour = tagColour{name: "chezmoi", ink: colour(t.Chezmoi.BadgeInk).String(), fill: colour(t.Chezmoi.BadgeFill).String()}
	chezmoiHeading = "[" + colour(t.Chezmoi.HeadingInk).String() + ":" + colour(t.Chezmoi.HeadingFill).String() + ":b]"
	for i, c := range tagPalette {
		if ink, ok := t.Tags[c.name]; ok {
			tagPalette[i].ink, tagPalette[i].fill = quieter(colour(ink.Ink)).String(), quieter(colour(ink.Fill)).String()
		}
	}

	labelColoursMu.Lock()
	labelPillMaker, labelColours = newPillMaker(t), map[string]tagColour{}
	labelColoursMu.Unlock()
	roleColours = resolveRoles(t)
	toastRoles(t, roleColours)
	activityRoles(t, roleColours)
	colPicker = pickerBackground(colBackground)
	colCard = cardBackground(colBackground)
	colOpen = openRowBackground(colBackground)
	heatScale = legibleOn(heatShades(t.Heat), colour(t.Background), colour(t.Text.Muted))

	g := t.Glyphs
	if nerdFont {
		g = g.over(t.NerdGlyphs)
	}
	glyphEditor = g.Editor
	glyphDiskNone, glyphDiskBranch, glyphDiskReview, glyphDiskBoth = g.DiskNone, g.DiskBranch, g.DiskReview, g.DiskBoth
	glyphWorktree, glyphGroup, glyphHidden, glyphFavourite = g.Worktree, g.Group, g.Hidden, g.Favourite
	glyphRepos, glyphForgeGitHub, glyphForgeGitLab, glyphDraft = g.Repos, g.ForgeGitHub, g.ForgeGitLab, g.Draft
	glyphIntegrationState = g.IntegrationState
	glyphEditorNeovim = g.EditorNeovim
	glyphWatched = g.Watched
	agentIcons = map[string]string{"claude": g.AgentClaude, "codex": g.AgentCodex, "copilot": g.AgentCopilot,
		"opencode": g.AgentOpencode, "agy": g.AgentAgy}
	actionIcons, columnIcons, integrationIcons, sectionIcons = nil, nil, nil, nil
	if nerdFont {
		actionIcons, columnIcons, integrationIcons, sectionIcons = t.ActionIcons, t.ColumnIcons, t.IntegrationIcons, t.SectionIcons
	}
	glyphCheck, glyphCross, glyphDot, glyphRing = g.Check, g.Cross, g.Dot, g.Ring
	glyphManual, glyphScheduled, glyphTrigger, glyphRetried = g.Manual, g.Scheduled, g.Trigger, g.Retried
	glyphUser, glyphStarred, glyphAgent = g.User, g.Starred, g.Agent
	glyphCIDone, glyphCIIdle, glyphApproved = g.CIDone, g.CIIdle, g.Approved
	glyphAhead, glyphBehind = g.Ahead, g.Behind
	glyphExternal, glyphMerge, glyphBar, glyphTabSeparator = g.External, g.Merge, g.Bar, g.TabSeparator
	glyphElided, glyphElidedGroup, glyphEdits = g.Elided, g.ElidedGroup, g.Edits
	glyphPicked, glyphUnpicked, glyphFolded, glyphUnfolded = g.Picked, g.Unpicked, g.Folded, g.Unfolded
	glyphCIUnknown = g.CIUnknown
	glyphColumnShown, glyphColumnHidden = g.ColumnShown, g.ColumnHidden
	toastIcons = map[severity]string{sevInfo: g.ToastInfo, sevSuccess: g.ToastSuccess, sevWarning: g.ToastWarning, sevError: g.ToastDanger}
	glyphMask = rune0(g.Mask)
	selectMarker = " " + g.Select

	b := t.Borders
	r := tview.Borders
	r.Horizontal, r.HorizontalFocus = rune0(b.Horizontal), rune0(b.Horizontal)
	r.Vertical, r.VerticalFocus = rune0(b.Vertical), rune0(b.Vertical)
	r.TopLeft, r.TopLeftFocus = rune0(b.TopLeft), rune0(b.TopLeft)
	r.TopRight, r.TopRightFocus = rune0(b.TopRight), rune0(b.TopRight)
	r.BottomLeft, r.BottomLeftFocus = rune0(b.BottomLeft), rune0(b.BottomLeft)
	r.BottomRight, r.BottomRightFocus = rune0(b.BottomRight), rune0(b.BottomRight)
	r.LeftT, r.RightT, r.TopT, r.BottomT, r.Cross = '├', '┤', '┬', '┴', '┼'
	tview.Borders = r

	tview.Styles.PrimitiveBackgroundColor = colBackground
	// The pair described above.
	tview.Styles.PrimaryTextColor = colText
	tview.Styles.ContrastBackgroundColor = colSurface
	tview.Styles.MoreContrastBackgroundColor = colRaised
	// Text drawn on a contrasting background, and the quieter text colours.
	tview.Styles.InverseTextColor = colText
	tview.Styles.ContrastSecondaryTextColor = colMuted
	tview.Styles.SecondaryTextColor = colMuted
	tview.Styles.TertiaryTextColor = colDim
	tview.Styles.BorderColor = colBorder
	tview.Styles.TitleColor = colTitle
	tview.Styles.GraphicsColor = colBorder
}

// baseStyle is a cell of the screen's own background, for whatever is drawn
// by hand rather than by a widget.
func baseStyle() tcell.Style { return tcell.StyleDefault.Background(colBackground) }

// focusBox brightens the border of the primitive that currently has focus.
// tview v0.42 has no separate focused border colour, and the focus runes are
// identical to the normal ones here, so this is done by hand.
func focusBox(b *tview.Box, focused bool) {
	if focused {
		b.SetBorderColor(colBorderFocus).SetTitleColor(colBorderFocus)
		return
	}
	b.SetBorderColor(colBorder).SetTitleColor(colTitle)
}

// box applies the shared border styling to any bordered primitive.
func box(b *tview.Box, title string) *tview.Box {
	b.SetBorder(true).
		SetBorderColor(colBorder).
		SetTitleColor(colTitle).
		SetTitleAlign(tview.AlignLeft)
	if title != "" {
		b.SetTitle(" " + title + " ")
	}
	return b
}

// modalBox centres a primitive over a dimmed copy of whatever is already on
// screen.
//
// It cannot be built out of a Flex: every tview Box fills its rectangle with
// spaces before drawing, so any wrapper would erase the interface underneath
// before it could be dimmed. modalBox therefore draws nothing of its own - it
// restyles the cells that are already there and then lets the content draw on
// top.
type modalBox struct {
	*tview.Box
	content tview.Primitive

	// Percentages of the available area; zero means the content is handed the
	// whole area and positions itself (tview.Modal does that).
	wPct, hPct int
	// Fixed size, used when non-zero.
	w, h int
	// fit, when set, chooses the size for the area there is: content that
	// knows how big it wants to be, up to a share of the screen.
	fit func(w, h int) (int, int)
	// bright leaves what is beneath as it is: the content tries something
	// on it that must be seen in its own colours.
	bright bool
}

// modalPct centres content at a percentage of the available area.
func modalPct(content tview.Primitive, wPct, hPct int) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content, wPct: wPct, hPct: hPct}
}

// modalFixed centres content at a fixed size.
func modalFixed(content tview.Primitive, w, h int) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content, w: w, h: h}
}

// modalFit centres content at the size fit chooses for the area.
func modalFit(content tview.Primitive, fit func(w, h int) (int, int)) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content, fit: fit}
}

// modalFull dims the background and lets the content place itself.
func modalFull(content tview.Primitive) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content}
}

func (m *modalBox) Draw(screen tcell.Screen) {
	x, y, w, h := m.GetRect()
	if m.bright {
		m.drawContent(screen, x, y, w, h)
		return
	}
	// The lowest modal dims the whole terminal, the tabs above the pages
	// too, once a frame. One over it dims only the dialog it stands on: a
	// second dimming of everything - a message over a dialog - took what we
	// coloured darker twice while the terminal's own ink stayed at the one
	// grey it falls back to, and the screen came out looking inverted. So
	// every cell is dimmed once at most, and the dialog in front is the one
	// left bright.
	q, quiet := screen.(*quietScreen)
	switch {
	case !quiet || !q.dimmed:
		sw, sh := screen.Size()
		dimArea(screen, 0, 0, sw, sh)
		if quiet {
			q.dimmed = true
		}
	case q.under.w > 0:
		dimArea(screen, q.under.x, q.under.y, q.under.w, q.under.h)
	}

	m.drawContent(screen, x, y, w, h)
}

// drawContent places the content in the area and draws it.
func (m *modalBox) drawContent(screen tcell.Screen, x, y, w, h int) {
	q, quiet := screen.(*quietScreen)
	cw, ch := w, h
	switch {
	case m.fit != nil:
		cw, ch = m.fit(w, h)
		cw, ch = min(cw, w), min(ch, h)
	case m.w > 0 || m.h > 0:
		cw, ch = min(m.w, w), min(m.h, h)
	case m.wPct > 0 || m.hPct > 0:
		cw, ch = w*m.wPct/100, h*m.hPct/100
	}
	m.content.SetRect(x+(w-cw)/2, y+(h-ch)/2, cw, ch)
	m.content.Draw(screen)
	if quiet {
		// A content that places itself (tview's Modal) is handed the whole
		// area, which is already dimmed; what it draws is not known, so a
		// modal over it dims nothing more.
		q.under.w = 0
		if cw < w || ch < h {
			q.under.x, q.under.y, q.under.w, q.under.h = m.content.GetRect()
		}
	}
}

func (m *modalBox) Focus(delegate func(p tview.Primitive)) { delegate(m.content) }
func (m *modalBox) HasFocus() bool                         { return m.content.HasFocus() }
func (m *modalBox) Blur()                                  { m.content.Blur() }

func (m *modalBox) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return m.content.InputHandler()
}

func (m *modalBox) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return m.content.MouseHandler()
}

// dimFactor is how much of the original brightness survives behind a modal.
var dimFactor float64

// dimArea darkens every cell in the rectangle while keeping its character, so
// the interface stays recognisable behind the modal.
func dimArea(screen tcell.Screen, x, y, w, h int) {
	for i := 0; i < w; i++ {
		for j := 0; j < h; j++ {
			text, style, _ := screen.Get(x+i, y+j)
			fg, bg, attr := style.Decompose()
			screen.Put(x+i, y+j, text, tcell.StyleDefault.
				Foreground(darken(fg, colDimmedText)).
				Background(darken(bg, tcell.ColorDefault)).
				Attributes(attr&^tcell.AttrBold))
		}
	}
}

// pickerBackground is a step darker than the screen's background: on a
// light theme a little, on a dark one more, since the eye tells dark
// shades apart less well. The terminal's own background cannot be
// darkened, so there it is the role picker.background.
func pickerBackground(bg tcell.Color) tcell.Color {
	if bg == tcell.ColorDefault || !bg.Valid() {
		return role("picker.background")
	}
	r, g, b := bg.RGB()
	factor := 0.6
	if 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) > 128 {
		factor = 0.92
	}
	scale := func(v int32) int32 { return int32(float64(v) * factor) }
	return tcell.NewRGBColor(scale(r), scale(g), scale(b))
}

// colOpen is the background of a row with something open in it - Neovim
// or an agent - or watched, a step off the page's.
var colOpen tcell.Color

// openRowBackground is the theme's background a twentieth of the way to
// white - to black on a light theme. The terminal's own background cannot
// be shifted, so there it is the role row.open.
func openRowBackground(bg tcell.Color) tcell.Color {
	if bg == tcell.ColorDefault || !bg.Valid() {
		return role("row.open")
	}
	r, g, b := bg.RGB()
	towards := int32(255)
	if 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) > 128 {
		towards = 0
	}
	step := func(v int32) int32 { return v + (towards-v)/20 }
	return tcell.NewRGBColor(step(r), step(g), step(b))
}

// colCard is a card's background: it stands out from the page it is on.
var colCard tcell.Color

// cardBackground is the theme's background a tenth darker. The terminal's
// own background cannot be darkened, so there it is the role
// card.background.
func cardBackground(bg tcell.Color) tcell.Color {
	if bg == tcell.ColorDefault || !bg.Valid() {
		return role("card.background")
	}
	r, g, b := bg.RGB()
	scale := func(v int32) int32 { return int32(float64(v) * 0.9) }
	return tcell.NewRGBColor(scale(r), scale(g), scale(b))
}

// colDimmedText stands in for the terminal's own foreground, whose RGB we
// cannot know.
var colDimmedText tcell.Color

// darken scales a colour towards black. Colours the terminal owns rather than
// us - the default foreground and background - cannot be scaled, so a fallback
// is used instead.
func darken(c tcell.Color, fallback tcell.Color) tcell.Color {
	if c == tcell.ColorDefault || !c.Valid() {
		return fallback
	}
	hex := c.Hex()
	scale := func(shift int32) int32 { return int32(float64((hex>>shift)&0xff) * dimFactor) }
	return tcell.NewRGBColor(scale(16), scale(8), scale(0))
}

// styleDropDown makes a select box readable and recognisable.
//
// tview builds a focused drop-down out of Styles.PrimaryTextColor on
// Styles.ContrastBackgroundColor, and this interface leaves the latter at the
// terminal default so that panels stay transparent - which paints the text in
// the background's own colour. The styles are therefore set by hand, and a
// marker makes it look like something you can open.
func styleDropDown(d *tview.DropDown) *tview.DropDown {
	// The resting look comes from the theme. These two only make the active
	// one match the selection band the lists use, rather than tview's
	// inverted default.
	d.SetFocusedStyle(styleSelected)
	d.SetListStyles(tcell.StyleDefault.Background(colSurface).Foreground(colText), styleSelected)
	d.SetTextOptions("  ", "  ", "", selectMarker, "")
	// tview feeds every other key into a hidden search field and opens the
	// list on it. With two fixed options that is only a way of ending up
	// somewhere nobody asked for, so the arrows and Enter are the way in.
	d.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyRune {
			return nil
		}
		return ev
	})
	// The open list is tview's own and takes its keys before the select
	// does, so the application's keys move in it (openSelectKeys).
	return d
}

// openSelectKeys moves in the list of an open select with j and k, as in
// every other list, and keeps other letters out of tview's search, which
// would jump to whatever option they spell. A select sits in a form: the
// form of a modal at the front, or one of Settings' sections, which are no
// modal - looking only at the modal once left Settings' selects deaf to j
// and k.
func (a *App) openSelectKeys(ev *tcell.EventKey) (*tcell.EventKey, bool) {
	if ev.Key() != tcell.KeyRune {
		return ev, false
	}
	var forms []*tview.Form
	if _, front := a.pages.GetFrontPage(); front != nil {
		if box, ok := front.(*modalBox); ok {
			if form, ok := box.content.(*tview.Form); ok {
				forms = append(forms, form)
			}
		}
	}
	if form, _ := a.focusedForm(); form != nil {
		forms = append(forms, form)
	}
	for _, form := range forms {
		if out, ok := openSelectKey(form, ev); ok {
			return out, true
		}
	}
	return ev, false
}

// openSelectKey is openSelectKeys for one form.
func openSelectKey(form *tview.Form, ev *tcell.EventKey) (*tcell.EventKey, bool) {
	for i := 0; i < form.GetFormItemCount(); i++ {
		if d, ok := form.GetFormItem(i).(*tview.DropDown); ok && d.IsOpen() {
			switch ev.Rune() {
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), true
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone), true
			}
			return nil, true
		}
	}
	return ev, false
}

// filterField is the "/" line above a list: part of the panel rather than a
// filled box, because it is always there whether or not it is being typed in.
func filterField(input *tview.InputField) *tview.InputField {
	return input.
		SetLabel(" / ").
		SetFieldBackgroundColor(colBackground).
		SetFieldTextColor(colText).
		SetLabelColor(colAccent)
}

// tag renders a colour as a tview colour tag.
func tag(c tcell.Color) string { return "[" + c.String() + "]" }

const tagEnd = "[-]"

// styleSelected is the cursor's band across a row, readable on any
// background. colMarked is the band of a row marked with space, a hue of its
// own so that it cannot be taken for the cursor's grey; styleMarkedSelected
// is the cursor on a marked row, a brighter step of the same hue, so that the
// row says both at once.
var (
	styleSelected       tcell.Style
	colMarked           tcell.Color
	styleMarkedSelected tcell.Style
)

// iconShade is the colour of an icon beside a text of colour c: the same
// colour, a little darker, so the icon says what the text is without
// outshining it. A colour that cannot be darkened - the terminal's own -
// gives the muted one.
func iconShade(c tcell.Color) tcell.Color {
	if c == tcell.ColorDefault || !c.Valid() {
		return colMuted
	}
	const shade = 0.75
	hex := c.Hex()
	scale := func(shift int32) int32 { return int32(float64((hex>>shift)&0xff) * shade) }
	return tcell.NewRGBColor(scale(16), scale(8), scale(0))
}
