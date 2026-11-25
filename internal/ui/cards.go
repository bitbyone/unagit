package ui

import (
	"math"

	"github.com/gdamore/tcell/v2"
)

// Every card unagit draws - a task on the Board, a comment, an editor open,
// a sandbox, an integration - takes its colours from here, as steps off the
// page it is on rather than colours of its own:
//
//   - the card is a twentieth darker than the page; on the terminal's own
//     background, which cannot be darkened, it has no fill, only its frame;
//   - its frame is a sixth lighter than the card - darker on a light theme,
//     where lighter would not show;
//   - the card chosen is its fill an eighth darker, its colour a sixth
//     quieter, and its frame a third of the way to white - to black on a
//     light theme: no accent, it is the same card under the cursor, lit.
//
// A colour given to a card - a task's red - is only a tint of the card's
// own fill and frame, at their lightness, so no colour stands out more than
// another and none more than the text on it.
const (
	cardStep       = 0.05
	cardFrameStep  = 0.15
	cardChosenStep = 0.12
	// cardChosenFrameStep is how far the chosen card's frame goes towards
	// white, or black on a light theme.
	cardChosenFrameStep = 0.35
	// cardChosenChroma is what is left of the fill's colour on the card
	// chosen.
	cardChosenChroma = 0.85
	// cardTint is how much colour a tinted card's fill takes, in Oklch
	// chroma: a touch; its frame takes twice that.
	cardTint = 0.0045
)

var (
	white = tcell.NewRGBColor(255, 255, 255)
	black = tcell.NewRGBColor(0, 0, 0)
)

// cardBackground is a card's fill on a page of bg: ColorDefault on the
// terminal's own background, where a card is its frame alone.
func cardBackground(bg tcell.Color) tcell.Color {
	if bg == tcell.ColorDefault || !bg.Valid() {
		return tcell.ColorDefault
	}
	return mix(bg, black, cardStep)
}

// cardFrame is the frame of a card filled with fill: a sixth lighter, or
// darker on a light theme; on a card with no fill, the border's colour.
func cardFrame(fill tcell.Color) tcell.Color {
	if fill == tcell.ColorDefault || !fill.Valid() {
		return colBorder
	}
	if toOklab(fill).l >= 0.6 {
		return mix(fill, black, cardFrameStep)
	}
	return mix(fill, white, cardFrameStep)
}

// cardChosen is a card's colour - its fill, or its frame with fill false -
// on the card chosen: a fill darker and quieter, a frame brighter - darker
// on a light page, where brighter would not show. ColorDefault stays as it
// is.
func cardChosen(c tcell.Color, fill bool) tcell.Color {
	if c == tcell.ColorDefault || !c.Valid() {
		return c
	}
	if !fill {
		if page := colBackground; page != tcell.ColorDefault && page.Valid() && toOklab(page).l >= 0.6 {
			return mix(c, black, cardChosenFrameStep)
		}
		return mix(c, white, cardChosenFrameStep)
	}
	lab := toOklab(mix(c, black, cardChosenStep))
	return fromOklch(lab.l, cardChosenChroma*math.Hypot(lab.a, lab.b), math.Atan2(lab.b, lab.a)*180/math.Pi, 0, 0)
}

// cardColours are a card's fill and frame: frame unset is the card's own,
// worked out of the fill; tint, unset for none, tints both.
func cardColours(frame, tint tcell.Color, chosen bool) (tcell.Color, tcell.Color) {
	fill := colCard
	if tint != tcell.ColorDefault && tint.Valid() {
		fill = tinted(fill, tint, cardTint)
	}
	if frame == tcell.ColorDefault {
		frame = cardFrame(fill)
		if tint != tcell.ColorDefault && tint.Valid() {
			frame = tinted(frame, tint, 2*cardTint)
		}
	}
	if chosen {
		return cardChosen(fill, true), cardChosen(frame, false)
	}
	return fill, frame
}

// cardFill is a plain card's fill, chosen or not.
func cardFill(chosen bool) tcell.Color {
	fill, _ := cardColours(tcell.ColorDefault, tcell.ColorDefault, chosen)
	return fill
}

// tinted is c at its own lightness in tint's hue, with no more than chroma
// of colour.
func tinted(c, tint tcell.Color, chroma float64) tcell.Color {
	if c == tcell.ColorDefault || !c.Valid() {
		return c
	}
	l, t := toOklab(c), toOklab(tint)
	return fromOklch(l.l, min(chroma, math.Hypot(t.a, t.b)), math.Atan2(t.b, t.a)*180/math.Pi, 0, 0)
}
