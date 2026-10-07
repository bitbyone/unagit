package ui

import (
	"math"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestTagsAreATenthQuieter: a tag colour keeps its lightness and hue and
// loses a tenth of its chroma, so its ink reads on its fill as before.
func TestTagsAreATenthQuieter(t *testing.T) {
	t.Parallel()
	ink, fill := colour("#f4a6b8"), colour("#5b2431")
	for _, c := range []string{"#f4a6b8", "#5b2431"} {
		before, after := toOklab(colour(c)), toOklab(quieter(colour(c)))
		ratio := math.Hypot(after.a, after.b) / math.Hypot(before.a, before.b)
		if math.Abs(ratio-tagSaturation) > 0.02 || math.Abs(after.l-before.l) > 0.005 {
			t.Errorf("%s: chroma kept %.2f, lightness %.3f -> %.3f", c, ratio, before.l, after.l)
		}
	}
	if got, was := contrast(quieter(ink), quieter(fill)), contrast(ink, fill); math.Abs(got-was) > 0.1 {
		t.Errorf("the contrast moved from %.2f to %.2f", was, got)
	}
}

// TestHeatReadsOnALightBackground: on a light theme every shade of the size
// heat reads at least as well as the theme's muted text, all the way from
// its cold end to its hot one; a theme on the terminal's own background
// keeps its heat as it is.
func TestHeatReadsOnALightBackground(t *testing.T) {
	t.Parallel()
	bg, muted := colour("#eff1f5"), colour("#6c6f85")
	want := contrast(muted, bg) - 0.1
	for i, c := range legibleOn(heatShades([]string{"#7aa3e6", "#e07a8c"}), bg, muted) {
		if got := contrast(c, bg); got < want {
			t.Errorf("shade %d #%06x reads at %.1f, under the muted text's %.1f", i, c.Hex(), got, want)
		}
	}
	plain := heatShades([]string{"#7aa3e6", "#e07a8c"})
	for i, c := range legibleOn(plain, tcell.ColorDefault, muted) {
		if c != plain[i] {
			t.Errorf("shade %d changed on the terminal's own background", i)
		}
	}
}
