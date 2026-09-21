package ui

import (
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
	colTabActive   = tcell.Color109
)

// applyTheme switches tview to single line rounded borders and a muted,
// background-transparent colour scheme.
func applyTheme() {
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

// tag renders a colour as a tview colour tag.
func tag(c tcell.Color) string { return "[" + c.String() + "]" }

const tagEnd = "[-]"
