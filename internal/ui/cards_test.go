package ui

import (
	"math"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCardsAreStepsOffThePage: a card is a twentieth darker than the page,
// framed a tenth lighter - darker on a light page; chosen, its fill and
// frame are a step darker and its fill quieter; a colour only tints it at
// its own lightness; on the terminal's own background it has no fill.
func TestCardsAreStepsOffThePage(t *testing.T) {
	restoreDefaultTheme(t)
	chroma := func(c tcell.Color) float64 { l := toOklab(c); return math.Hypot(l.a, l.b) }
	light := func(c tcell.Color) float64 { return toOklab(c).l }
	red := tcell.NewRGBColor(220, 60, 60)
	for _, page := range []tcell.Color{tcell.NewRGBColor(0x28, 0x25, 0x27), tcell.NewRGBColor(0xf6, 0xf4, 0xef)} {
		colCard = cardBackground(page)
		fill, frame := cardColours(tcell.ColorDefault, tcell.ColorDefault, false)
		if fill != mix(page, black, cardStep) {
			t.Errorf("%v: the card is %v, not a twentieth darker", page, fill)
		}
		if lightPage := light(page) >= 0.6; lightPage != (light(frame) < light(fill)) {
			t.Errorf("%v: the frame %v goes the wrong way from the card %v", page, frame, fill)
		}
		cfill, cframe := cardColours(tcell.ColorDefault, tcell.ColorDefault, true)
		if light(cfill) >= light(fill) || light(cframe) >= light(frame) {
			t.Errorf("%v: the card chosen is not darker: %v %v, unchosen %v %v", page, cfill, cframe, fill, frame)
		}
		tfill, tframe := cardColours(tcell.ColorDefault, red, false)
		if math.Abs(light(tfill)-light(fill)) > 0.01 || chroma(tfill) > cardTint+0.005 || chroma(tfill) <= chroma(fill) {
			t.Errorf("%v: the red card %v is not the card %v touched with red", page, tfill, fill)
		}
		if math.Abs(light(tframe)-light(frame)) > 0.01 {
			t.Errorf("%v: the red card's frame %v is not at its frame's lightness %v", page, tframe, frame)
		}
		if cf, _ := cardColours(tcell.ColorDefault, red, true); chroma(cf) >= chroma(tfill) {
			t.Errorf("%v: the red card chosen %v is not quieter than %v", page, cf, tfill)
		}
	}
	colCard = cardBackground(tcell.ColorDefault)
	if fill, frame := cardColours(tcell.ColorDefault, tcell.ColorDefault, false); fill != tcell.ColorDefault || frame != colBorder {
		t.Errorf("on the terminal's own background the card is %v framed %v, want no fill and the border", fill, frame)
	}
}
