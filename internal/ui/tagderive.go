package ui

import (
	"fmt"
	"math"

	"github.com/gdamore/tcell/v2"
)

// A tag's pill is ink on a fill of the same hue. The default theme's pills
// are made for a dark terminal and look pasted onto any other background, so
// a theme with a background of its own has them worked out of its colours
// instead (unless it names them): the fill a small step off the background
// and tinted by it, so the pills share the theme's temperature; the ink as
// colourful and as light as the theme's own accents, moved just far enough
// from the fill to read. The work is done in OKLab, where equal steps look
// equal, so the sixteen hues come out alike in weight.

// tagHue is where a tag colour sits on the OKLCH wheel, and how much of
// the theme's colourfulness it takes: sage and sand are greys by name.
type tagHue struct{ hue, chroma float64 }

var tagHues = map[string]tagHue{
	"rose": {10, 1}, "coral": {35, 1}, "peach": {55, 1}, "apricot": {75, 1},
	"butter": {100, 1}, "lime": {125, 1}, "mint": {160, 1}, "sage": {140, 0.45},
	"teal": {190, 1}, "sky": {225, 1}, "azure": {252, 1}, "periwinkle": {275, 1},
	"lavender": {295, 1}, "lilac": {318, 1}, "pink": {350, 1}, "sand": {80, 0.4},
}

// tagContrast is how far apart a pill's ink and fill must be, as WCAG
// counts it: a little short of what body text needs, so a pill reads
// without standing out of the background - a tag is a word, not a line.
const tagContrast = 4.0

// tagQuiet is how much a pill's ink is held back from the theme's own
// accents: its colour and its distance from the fill both go by this
// much, so the pills sit in the background rather than on it.
const tagQuiet = 0.9

// deriveTags works out every tag colour for a theme with a background of
// its own.
func deriveTags(t Theme) map[string]TagInk {
	bg := toOklab(colour(t.Background))
	// The theme's accents say how colourful and how light its ink is.
	var sumC, sumL float64
	n := 0
	for _, c := range []string{t.Text.Accent, t.Text.Branch, t.Text.Key, t.State.Good, t.State.Warning,
		t.State.Bad, t.State.Force, t.State.Favourite, t.Markdown.Link, t.Markdown.Heading} {
		col := colour(c)
		if col == tcell.ColorDefault || !col.Valid() {
			continue
		}
		lab := toOklab(col)
		if chroma := math.Hypot(lab.a, lab.b); chroma >= 0.04 {
			sumC, sumL, n = sumC+chroma, sumL+lab.l, n+1
		}
	}
	meanC, meanL := 0.12, 0.75
	if n > 0 {
		meanC, meanL = sumC/float64(n), sumL/float64(n)
	}
	dark := bg.l < 0.6

	fillL, fillC, inkL := bg.l+0.09, clamp(meanC*0.4, 0.03, 0.08), max(meanL, bg.l+0.45)
	if !dark {
		fillL, fillC, inkL = bg.l-0.07, clamp(meanC*0.35, 0.03, 0.07), min(meanL, bg.l-0.45)
	}
	inkL = fillL + (inkL-fillL)*tagQuiet
	inkC := clamp(meanC*0.75*tagQuiet, 0.045, 0.13)

	out := make(map[string]TagInk, len(tagHues))
	for name, h := range tagHues {
		// The background's own tint is kept in the fill.
		fill := fromOklch(fillL, fillC*h.chroma, h.hue, bg.a*0.6, bg.b*0.6)
		ink := fromOklch(inkL, inkC*h.chroma, h.hue, 0, 0)
		// Moved away from the fill until it reads, a step at a time.
		step := 0.01
		if !dark {
			step = -0.01
		}
		for l := inkL; contrast(ink, fill) < tagContrast && l > 0 && l < 1; {
			l += step
			ink = fromOklch(l, inkC*h.chroma, h.hue, 0, 0)
		}
		out[name] = TagInk{Ink: fmt.Sprintf("#%06x", ink.Hex()), Fill: fmt.Sprintf("#%06x", fill.Hex())}
	}
	return out
}

type oklab struct{ l, a, b float64 }

func toOklab(c tcell.Color) oklab {
	r, g, b := c.RGB()
	lr, lg, lb := linear(float64(r)/255), linear(float64(g)/255), linear(float64(b)/255)
	l := math.Cbrt(0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb)
	m := math.Cbrt(0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb)
	s := math.Cbrt(0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb)
	return oklab{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

// fromOklch is the colour of a lightness, chroma and hue, shifted by da
// and db, with the chroma taken down until it can be shown.
func fromOklch(l, chroma, hue, da, db float64) tcell.Color {
	rad := hue * math.Pi / 180
	for ; ; chroma *= 0.9 {
		r, g, b, ok := labToRGB(oklab{l, chroma*math.Cos(rad) + da, chroma*math.Sin(rad) + db})
		if ok || chroma < 0.001 {
			return tcell.NewRGBColor(r, g, b)
		}
	}
}

// labToRGB is a colour in sRGB, and whether it fits there unclipped.
func labToRGB(c oklab) (int32, int32, int32, bool) {
	l := cube(c.l + 0.3963377774*c.a + 0.2158037573*c.b)
	m := cube(c.l - 0.1055613458*c.a - 0.0638541728*c.b)
	s := cube(c.l - 0.0894841775*c.a - 1.2914855480*c.b)
	rgb := [3]float64{
		+4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s,
	}
	ok := true
	var out [3]int32
	for i, v := range rgb {
		if v < -0.0001 || v > 1.0001 {
			ok = false
		}
		out[i] = int32(math.Round(gamma(clamp(v, 0, 1)) * 255))
	}
	return out[0], out[1], out[2], ok
}

// contrast is WCAG's contrast ratio of two colours, 1 to 21.
func contrast(x, y tcell.Color) float64 {
	lx, ly := relLuminance(x), relLuminance(y)
	if lx < ly {
		lx, ly = ly, lx
	}
	return (lx + 0.05) / (ly + 0.05)
}

func relLuminance(c tcell.Color) float64 {
	r, g, b := c.RGB()
	return 0.2126*linear(float64(r)/255) + 0.7152*linear(float64(g)/255) + 0.0722*linear(float64(b)/255)
}

func linear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func gamma(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

func cube(v float64) float64 { return v * v * v }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
