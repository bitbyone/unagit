package ui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Muted palette. Everything is deliberately low contrast: unagit is meant to
// be opened from inside nvim, so it should not shout over the editor.
var (
	colBorder      = tcell.Color244
	colBorderFocus = tcell.Color109
	colTitle       = tcell.Color109
	colMuted       = tcell.Color244
	colDim         = tcell.Color240
	colText        = tcell.Color252
	colAccent      = tcell.Color109
	colOn          = tcell.Color108
	colWarn        = tcell.Color179
	colBad         = tcell.Color167
	colBranch      = tcell.Color109
	colTabActive   = tcell.Color109
)

// applyTheme switches tview to single line rounded borders and a muted,
// background-transparent colour scheme.
//
// tview keeps its styles in package level variables, so this runs once per
// process rather than once per application.
func applyTheme() { themeOnce.Do(setTheme) }

var themeOnce sync.Once

func setTheme() {
	r := tview.Borders
	r.Horizontal, r.HorizontalFocus = '─', '─'
	r.Vertical, r.VerticalFocus = '│', '│'
	r.TopLeft, r.TopLeftFocus = '╭', '╭'
	r.TopRight, r.TopRightFocus = '╮', '╮'
	r.BottomLeft, r.BottomLeftFocus = '╰', '╰'
	r.BottomRight, r.BottomRightFocus = '╯', '╯'
	r.LeftT, r.RightT, r.TopT, r.BottomT, r.Cross = '├', '┤', '┬', '┴', '┼'
	tview.Borders = r

	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.MoreContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.BorderColor = colBorder
	tview.Styles.TitleColor = colTitle
	tview.Styles.GraphicsColor = colBorder
	tview.Styles.PrimaryTextColor = colText
	tview.Styles.SecondaryTextColor = colMuted
	tview.Styles.TertiaryTextColor = colDim
	tview.Styles.InverseTextColor = colAccent
}

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
}

// modalPct centres content at a percentage of the available area.
func modalPct(content tview.Primitive, wPct, hPct int) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content, wPct: wPct, hPct: hPct}
}

// modalFixed centres content at a fixed size.
func modalFixed(content tview.Primitive, w, h int) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content, w: w, h: h}
}

// modalFull dims the background and lets the content place itself.
func modalFull(content tview.Primitive) *modalBox {
	return &modalBox{Box: tview.NewBox(), content: content}
}

func (m *modalBox) Draw(screen tcell.Screen) {
	x, y, w, h := m.GetRect()
	dimArea(screen, x, y, w, h)

	cw, ch := w, h
	switch {
	case m.w > 0 || m.h > 0:
		cw, ch = min(m.w, w), min(m.h, h)
	case m.wPct > 0 || m.hPct > 0:
		cw, ch = w*m.wPct/100, h*m.hPct/100
	}
	m.content.SetRect(x+(w-cw)/2, y+(h-ch)/2, cw, ch)
	m.content.Draw(screen)
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
const dimFactor = 0.42

// dimArea darkens every cell in the rectangle while keeping its character, so
// the interface stays recognisable behind the modal.
func dimArea(screen tcell.Screen, x, y, w, h int) {
	for i := 0; i < w; i++ {
		for j := 0; j < h; j++ {
			r, combc, style, _ := screen.GetContent(x+i, y+j)
			fg, bg, attr := style.Decompose()
			screen.SetContent(x+i, y+j, r, combc, tcell.StyleDefault.
				Foreground(darken(fg, colDimmedText)).
				Background(darken(bg, tcell.ColorDefault)).
				Attributes(attr&^tcell.AttrBold))
		}
	}
}

// colDimmedText stands in for the terminal's own foreground, whose RGB we
// cannot know.
var colDimmedText = tcell.Color240

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

// tag renders a colour as a tview colour tag.
func tag(c tcell.Color) string { return "[" + c.String() + "]" }

const tagEnd = "[-]"

// Selection highlight: a full row band, readable on any terminal background.
var styleSelected = tcell.StyleDefault.
	Background(tcell.Color238).
	Foreground(tcell.Color231).
	Bold(true)
