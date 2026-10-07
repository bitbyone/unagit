package ui

import (
	"math"
	"testing"
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
