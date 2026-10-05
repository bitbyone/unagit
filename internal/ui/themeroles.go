package ui

import (
	"fmt"
	"math"
	"sort"

	"github.com/gdamore/tcell/v2"
)

// Every colour drawn is a role with a name - "repositories.mr", "column.age"
// - so a theme can colour anything at all; but a theme names only what it
// wants to set apart. Each role falls back on another: a list's column on
// the column of every list ("repositories.size" on "column.size"), that on
// a base colour of the theme ("column.size" on "text.muted"). Changing a
// few base colours changes everything that falls back on them; one role
// changed changes that alone. A theme gives its roles under "colours".
//
// A new colour is a new role here, with the role it falls back on, never a
// colour written where it is drawn.

// colourRole is one role: its name, what it falls back on when no theme
// names it, and what it colours.
type colourRole struct {
	key, fallback, about string
}

// colourRoles is every role, the general before the particular.
var colourRoles = []colourRole{
	// What every list's column of the kind is.
	{"column.header", "text.dim", "the column names over a list"},
	{"column.server", "text.accent", "which server a row is on"},
	{"column.name", "text.normal", "what a row is: a repository, a title"},
	{"column.path", "text.muted", "a directory on disk"},
	{"column.branch", "text.branch", "a branch"},
	{"column.author", "text.muted", "who wrote it"},
	{"column.count", "state.warning", "a count of things to look at"},
	{"column.mr", "text.accent", "merge requests: their number, or how many"},
	{"column.wt", "column.count", "worktrees: how many"},
	{"column.edits", "column.count", "files not committed"},
	{"column.new", "column.count", "commits since the last review"},
	{"column.pending", "column.count", "comments waiting to be published"},
	{"column.hidden", "text.dim", "the mark of something hidden"},
	{"column.size", "text.muted", "what it takes on disk, when the theme has no heat"},
	{"column.age", "text.muted", "how long ago: activity, updated, created"},

	// Repositories.
	{"repositories.header", "column.header", ""},
	{"repositories.server", "column.server", ""},
	{"repositories.name", "column.name", ""},
	{"repositories.branch", "column.branch", ""},
	{"repositories.edits", "column.edits", ""},
	{"repositories.path", "column.path", ""},
	{"repositories.mr", "column.mr", "merge requests with a worktree on disk"},
	{"repositories.wt", "column.wt", ""},
	{"repositories.hidden", "column.hidden", "its merge requests are hidden"},
	{"repositories.size", "column.size", ""},
	{"repositories.activity", "column.age", ""},

	// Merge requests.
	{"merge_requests.header", "column.header", ""},
	{"merge_requests.server", "column.server", ""},
	{"merge_requests.repository", "column.server", ""},
	{"merge_requests.iid", "column.mr", "the !number"},
	{"merge_requests.title", "column.name", ""},
	{"merge_requests.author", "column.author", ""},
	{"merge_requests.branch", "column.branch", ""},
	{"merge_requests.pending", "column.pending", ""},
	{"merge_requests.new", "column.new", ""},
	{"merge_requests.updated", "column.age", ""},

	// Worktrees.
	{"worktrees.header", "column.header", ""},
	{"worktrees.server", "column.server", ""},
	{"worktrees.repository", "column.name", ""},
	{"worktrees.edits", "column.edits", ""},
	{"worktrees.mr", "column.mr", ""},
	{"worktrees.path", "column.path", ""},
	{"worktrees.size", "column.size", ""},
	{"worktrees.created", "column.age", ""},
	{"worktrees.activity", "column.age", ""},
}

// roleColours is every role as the theme on resolves it (setTheme).
var roleColours map[string]tcell.Color

// role is the colour of a role. An unknown one is a bug, and shows as the
// body text rather than as nothing.
func role(key string) tcell.Color {
	if c, ok := roleColours[key]; ok {
		return c
	}
	return colText
}

// resolveRoles follows every role to the colour it comes to in a theme:
// its own, or its fallback's, down to a base colour.
func resolveRoles(t Theme) map[string]tcell.Color {
	base := t.colours()
	fallback := map[string]string{}
	for _, r := range colourRoles {
		fallback[r.key] = r.fallback
	}
	out := map[string]tcell.Color{}
	var resolve func(key string, depth int) string
	resolve = func(key string, depth int) string {
		if v := t.Roles[key]; v != "" {
			return v
		}
		if next, ok := fallback[key]; ok && depth < len(colourRoles) {
			return resolve(next, depth+1)
		}
		return base[key]
	}
	for _, r := range colourRoles {
		out[r.key] = colour(resolve(r.key, 0))
	}
	return out
}

// roleProblems says what is wrong with a theme's roles: one that is not a
// role, a fallback that leads nowhere.
func roleProblems(t Theme) []string {
	known := map[string]bool{}
	for _, r := range colourRoles {
		known[r.key] = true
	}
	var problems []string
	for key := range t.Roles {
		if !known[key] {
			problems = append(problems, fmt.Sprintf("colours.%s: there is no such colour - see the default theme's list", key))
		}
	}
	base := t.colours()
	for _, r := range colourRoles {
		if !known[r.fallback] {
			if _, ok := base[r.fallback]; !ok {
				problems = append(problems, fmt.Sprintf("colours.%s falls back on %s, which is nothing", r.key, r.fallback))
			}
		}
	}
	if len(t.Heat) == 1 {
		problems = append(problems, "heat: it needs two colours at least, the coldest and the hottest")
	}
	sort.Strings(problems)
	return problems
}

// heatSteps is how many shades the heat has, whatever the theme gives.
const heatSteps = 16

// heatScale is the theme's heat as heatSteps shades, cold to hot, worked
// out between the colours the theme gives (setTheme).
var heatScale []tcell.Color

// heatShades spreads the colours a theme gives - the coldest and the
// hottest, or more on the way - over heatSteps shades. They are worked out
// in hue, saturation and lightness rather than in red, green and blue: the
// hue walks from the cold one to the hot one through the colours between
// (blue, green, yellow, orange, red), while saturation and lightness go
// evenly from the one end's to the other's, so every shade is as faded as
// the ends the theme chose. Mixed in RGB the middle came out grey.
func heatShades(anchors []string) []tcell.Color {
	if len(anchors) < 2 {
		return nil
	}
	cols := make([]hsl, len(anchors))
	for i, a := range anchors {
		cols[i] = toHSL(colour(a))
	}
	out := make([]tcell.Color, heatSteps)
	for i := range heatSteps {
		at := float64(i) / float64(heatSteps-1) * float64(len(cols)-1)
		lo := min(int(at), len(cols)-2)
		f := at - float64(lo)
		a, b := cols[lo], cols[lo+1]
		// The hue only ever goes down the wheel towards the hot end, so a
		// pink red is reached through green and yellow, never through
		// violet.
		if b.h > a.h {
			b.h -= 360
		}
		out[i] = hsl{a.h + (b.h-a.h)*f, a.s + (b.s-a.s)*f, a.l + (b.l-a.l)*f}.colour()
	}
	return out
}

// hsl is a colour as hue (degrees), saturation and lightness (0 to 1).
type hsl struct{ h, s, l float64 }

// toHSL is a colour in hue, saturation and lightness.
func toHSL(c tcell.Color) hsl {
	hex := c.Hex()
	if hex < 0 {
		hex = 0
	}
	r, g, b := float64((hex>>16)&0xff)/255, float64((hex>>8)&0xff)/255, float64(hex&0xff)/255
	hi, lo := max(r, g, b), min(r, g, b)
	l := (hi + lo) / 2
	if hi == lo {
		return hsl{0, 0, l}
	}
	d := hi - lo
	s := d / (1 - math.Abs(2*l-1))
	var h float64
	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return hsl{h, s, l}
}

// colour is the colour back in red, green and blue.
func (c hsl) colour() tcell.Color {
	h := math.Mod(c.h, 360)
	if h < 0 {
		h += 360
	}
	k := (1 - math.Abs(2*c.l-1)) * c.s
	x := k * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := c.l - k/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g = k, x
	case h < 120:
		r, g = x, k
	case h < 180:
		g, b = k, x
	case h < 240:
		g, b = x, k
	case h < 300:
		r, b = x, k
	default:
		r, b = k, x
	}
	part := func(v float64) int32 { return int32(math.Round((v + m) * 255)) }
	return tcell.NewRGBColor(part(r), part(g), part(b))
}

// heatColour is how hot a size is among sizes from least to most: on a
// logarithmic scale, since repositories differ by orders of magnitude and
// on a straight one nearly all of them would be the coldest. Without heat
// in the theme it is the fallback.
func heatColour(n, least, most int64, fallback tcell.Color) tcell.Color {
	if len(heatScale) == 0 || n <= 0 {
		return fallback
	}
	if most <= least {
		return heatScale[0]
	}
	lo, hi := math.Log(float64(max(least, 1))), math.Log(float64(most))
	f := (math.Log(float64(n)) - lo) / (hi - lo)
	step := int(math.Round(max(0, min(1, f)) * float64(heatSteps-1)))
	return heatScale[step]
}
