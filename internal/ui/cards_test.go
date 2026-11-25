package ui

import (
	"math"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCardsAreStepsOffThePage: a card is a twentieth darker than the page,
// framed lighter - darker on a light page; chosen, its fill is darker and
// quieter and its frame brighter - darker on a light page; a colour only tints it at
// its own lightness; on the terminal's own background it has no fill.
func TestCardsAreStepsOffThePage(t *testing.T) {
	restoreDefaultTheme(t)
	chroma := func(c tcell.Color) float64 { l := toOklab(c); return math.Hypot(l.a, l.b) }
	light := func(c tcell.Color) float64 { return toOklab(c).l }
	hue := func(c tcell.Color) float64 { l := toOklab(c); return math.Atan2(l.b, l.a) * 180 / math.Pi }
	red := tcell.NewRGBColor(220, 60, 60)
	for _, page := range []tcell.Color{tcell.NewRGBColor(0x28, 0x25, 0x27), tcell.NewRGBColor(0xf6, 0xf4, 0xef)} {
		colBackground, colCard = page, cardBackground(page)
		fill, frame := cardColours(tcell.ColorDefault, tcell.ColorDefault, false)
		if fill != mix(page, black, cardStep) {
			t.Errorf("%v: the card is %v, not a twentieth darker", page, fill)
		}
		if lightPage := light(page) >= 0.6; lightPage != (light(frame) < light(fill)) {
			t.Errorf("%v: the frame %v goes the wrong way from the card %v", page, frame, fill)
		}
		cfill, cframe := cardColours(tcell.ColorDefault, tcell.ColorDefault, true)
		if lightPage := light(page) >= 0.6; light(cfill) >= light(fill) || lightPage != (light(cframe) < light(frame)) {
			t.Errorf("%v: the card chosen is not a darker fill in a brighter frame: %v %v, unchosen %v %v", page, cfill, cframe, fill, frame)
		}
		tfill, tframe := cardColours(tcell.ColorDefault, red, false)
		if math.Abs(light(tfill)-light(fill)) > 0.01 || chroma(tfill) > cardTint+0.003 || math.Abs(hue(tfill)-hue(red)) > 25 {
			t.Errorf("%v: the red card %v is not the card %v touched with red", page, tfill, fill)
		}
		if math.Abs(light(tframe)-light(frame)) > 0.01 {
			t.Errorf("%v: the red card's frame %v is not at its frame's lightness %v", page, tframe, frame)
		}
	}
	// A fill chosen is quieter: a sixth of its colour gone, past the step.
	if c := cardChosen(red, true); chroma(c) > cardChosenChroma*chroma(red)+0.01 {
		t.Errorf("red chosen %v keeps too much of its colour", c)
	}
	colBackground, colCard = tcell.ColorDefault, cardBackground(tcell.ColorDefault)
	if fill, frame := cardColours(tcell.ColorDefault, tcell.ColorDefault, false); fill != tcell.ColorDefault || frame != colBorder {
		t.Errorf("on the terminal's own background the card is %v framed %v, want no fill and the border", fill, frame)
	}
}
