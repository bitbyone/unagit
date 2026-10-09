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
	{"mark.editor", "state.good", "a directory open in Neovim"},
	// What every list's column of the kind is.
	{"column.header", "text.dim", "the column names over a list"},
	{"column.server", "text.accent", "which server a row is on"},
	{"column.name", "text.normal", "what a row is: a repository, a title"},
	{"column.path", "text.muted", "a directory on disk"},
	{"column.branch", "text.branch", "a branch of work, not the default one"},
	{"column.default_branch", "text.branch", "a repository's default branch, set apart from the others"},
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

	// A merge request's threads and approvals.
	{"comments.unresolved", "state.warning", "threads still to resolve"},
	{"comments.resolved", "state.good", "threads resolved"},
	{"comments.all", "column.mr", "comments in all"},
	{"approvals.missing", "state.warning", "approvals still missing"},
	{"approvals.done", "state.good", "every approval asked for is in"},
	{"approvals.mine", "state.good", "the mark of your own approval"},

	// A pipeline's state, wherever it is drawn.
	{"ci.success", "state.good", "a pipeline or a job that passed"},
	{"ci.failed", "state.bad", "one that failed"},
	{"ci.running", "text.accent", "one under way, or waiting its turn"},
	{"ci.idle", "text.dim", "one skipped or canceled"},
	{"ci.manual", "text.accent", "one waiting to be started by hand, or for its time"},

	// The list of starred repositories.
	{"starred.description", "text.muted", "what a starred repository says it is"},
	{"starred.language", "text.dim", "the language it is written in"},
	{"starred.stars", "state.favourite", "how many stars it has"},
	{"starred.activity", "column.age", "how long ago it moved"},

	// A picker whose items are tried on the screen behind it, the themes.
	{"picker.background", "surface.field", "its background on the terminal's own; on a theme's, that darkened"},

	// Badges in the tags column.
	{"badge.starred.ink", "chezmoi.badge_ink", "the badge of a repository cloned from the stars"},
	{"badge.starred.fill", "chezmoi.badge_fill", ""},

	// Repositories.
	{"repositories.header", "column.header", ""},
	{"repositories.server", "column.server", ""},
	{"repositories.name", "column.name", ""},
	{"repositories.branch", "column.branch", ""},
	{"repositories.default_branch", "column.default_branch", ""},
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
	{"merge_requests.repository", "column.name", "named as Repositories names it"},
	{"merge_requests.iid", "column.server", "the !number, in the server's colour, so server, repository and number stand apart"},
	{"merge_requests.title", "column.name", ""},
	{"merge_requests.author", "column.author", ""},
	{"merge_requests.assignees", "merge_requests.author", "who it is assigned to"},
	{"merge_requests.reviewers", "merge_requests.author", "who is asked to review it"},
	{"merge_requests.branch", "column.branch", ""},
	{"merge_requests.pending", "column.pending", ""},
	{"merge_requests.new", "column.new", ""},
	{"merge_requests.updated", "column.age", ""},

	// Worktrees.
	{"worktrees.header", "column.header", ""},
	{"worktrees.server", "column.server", ""},
	{"worktrees.repository", "column.name", ""},
	{"worktrees.branch", "column.branch", ""},
	{"worktrees.default_branch", "column.default_branch", ""},
	{"worktrees.edits", "column.edits", ""},
	{"worktrees.mr", "column.mr", ""},
	{"worktrees.path", "column.path", ""},
	{"worktrees.size", "column.size", ""},
	{"worktrees.created", "column.age", ""},
	{"worktrees.activity", "column.age", ""},

	// Integrations: a card stands out from the page, and says whether it is
	// on.
	{"card.background", "picker.background", "a card's background on the terminal's own; on a theme's, that a tenth darker"},
	// A row with something open in it - Neovim, an agent - in the lists.
	{"row.open", "card.background", "its background on the terminal's own; on a theme's, that a twentieth lighter"},
	{"integration.enabled", "state.good", "an integration that is on"},
	{"integration.disabled", "state.bad", "one installed but turned off"},
	{"integration.missing", "text.dim", "one not installed"},

	// Agents.
	{"agents.header", "column.header", ""},
	{"agents.working", "text.accent", "an agent at work"},
	{"agents.waiting", "state.warning", "an agent waiting for an answer"},
	{"agents.idle", "state.good", "an agent done, waiting for the next thing to do"},
	{"agents.unknown", "text.dim", "an agent whose state nothing can tell"},
	{"agents.agent", "column.name", ""},
	{"agents.repository", "column.server", "what it works on, as the lists name it"},
	{"agents.branch", "column.branch", ""},
	{"agents.title", "column.name", "what the agent calls its conversation"},
	{"agents.where", "text.muted", "where it runs"},
	{"agents.path", "column.path", ""},
	// An agent's mark in the lists, in the colour of what it is doing.
	{"mark.agent", "mark.editor", "a directory an agent unagit started works in"},
	{"mark.agent_waiting", "agents.waiting", "the agent there waits for an answer"},
	{"mark.agent_working", "agents.working", "the agent there is at work"},
	{"mark.watched", "text.accent", "what has its pipelines watched"},
	{"watched.header", "column.header", ""},
	{"watched.unseen", "state.warning", "a change on the Watched screen not yet seen"},
	{"watched.what", "column.name", "what is watched: a merge request, a branch"},
	{"watched.by", "column.author", "whom the pipeline was started by"},
	{"watched.changed", "column.age", "when the pipeline last changed"},
	{"watched.latest", "text.normal", "what was last said of a watch"},
	{"watched.error", "state.warning", "why the last reading failed"},
	// The Activity screen. A section's heading is a pill of its colour on
	// its fill; what is under way is blue, as an info toast is, unless a
	// theme names it (activityRoles), and so are the cards' borders, a
	// shade off their fill.
	{"activity.needs", "state.bad", "the heading of what needs you, and its count on the tab"},
	{"activity.needs_fill", "surface.raised", "its pill's fill"},
	{"activity.under_way", "text.accent", "the heading of what is under way"},
	{"activity.under_way_fill", "surface.raised", "its pill's fill"},
	{"activity.quiet", "text.muted", "the heading of what is quiet"},
	{"activity.quiet_fill", "surface.raised", "its pill's fill"},
	{"activity.rule", "border.normal", "the rule after a heading, between new and old"},
	{"activity.new", "state.good", "the mark of what came since the last visit"},
	{"activity.visit", "state.good", "the line of the last visit"},
	{"activity.what", "column.name", "what a row is: a merge request, a branch, an agent"},
	{"activity.what_quiet", "text.muted", "the same, in the quiet section"},
	{"activity.about", "text.muted", "what it is about, and an event's sentence"},
	{"activity.state", "text.muted", "a state with no colour of its own"},
	{"activity.age", "column.age", "how long ago it changed"},
	{"activity.label", "text.dim", "the names of the detail's lines"},
	{"activity.when", "text.dim", "when an event came"},
	{"activity.watching", "mark.watched", "the mark of what is watched"},
	{"activity.lit", "selection.marked", "the log's rows of the thing under the list's cursor, faintly"},
	{"activity.key", "text.key", "a key named in a panel"},
	{"activity.card_icon", "text.accent", "an editor's icon and name on its card"},
	{"tabs.waiting", "state.warning", "on the Activity tab: agents waiting for an answer"},
	{"tabs.running", "text.accent", "pipelines under way"},
	{"tabs.new", "state.good", "changes not seen yet"},

	// A toast is filled with its severity's colour. Unless a theme names
	// them, its colours are worked out of the severity's and the
	// background (toastRoles); the fallbacks are for a theme of an older
	// unagit that named the general ones.
	{"toast.background", "surface.raised", "a toast: news from the background, in the bottom right corner"},
	{"toast.text", "text.normal", "what a toast says"},
	{"toast.about", "text.muted", "what a toast is about: the repository, a merge request's title"},
	{"toast.info", "text.accent", "the colour of a toast of something that began or came"},
	{"toast.success", "state.good", "of something that passed"},
	{"toast.warning", "state.warning", "of something cancelled, or waiting for a hand"},
	{"toast.danger", "state.bad", "of something that failed"},
}

// toastLevels are the toasts' severities by the names of their roles.
var toastLevels = []string{"info", "success", "warning", "danger"}

func init() {
	for _, level := range toastLevels {
		colourRoles = append(colourRoles,
			colourRole{"toast." + level + ".background", "toast.background", "the fill of a toast of " + level},
			colourRole{"toast." + level + ".border", "toast." + level, "its border"},
			colourRole{"toast." + level + ".text", "toast.text", "its heading and what it says"},
			colourRole{"toast." + level + ".about", "toast.about", "the line of what it is about"})
	}
}

// activityRoles works out the Activity screen's colours a theme does not
// name: what is under way in the info toast's blue, what needs you in the
// danger toast's colours. It runs after toastRoles.
func activityRoles(t Theme, roles map[string]tcell.Color) {
	derived := map[string]tcell.Color{
		"activity.needs":          roles["toast.danger.border"],
		"activity.needs_fill":     roles["toast.danger.background"],
		"activity.under_way":      roles["toast.info.border"],
		"activity.under_way_fill": roles["toast.info.background"],
		"tabs.running":            roles["toast.info.border"],
		"activity.lit":            shade(colour(t.Background), 0.035),
	}
	for key, c := range derived {
		if t.Roles[key] == "" && c != tcell.ColorDefault {
			roles[key] = c
		}
	}
}

// shade is a background a step of lightness off itself - lighter on a dark
// theme, darker on a light one - for what is set apart only faintly.
// ColorDefault where the background is the terminal's, which cannot be
// worked from.
func shade(background tcell.Color, step float64) tcell.Color {
	if background == tcell.ColorDefault || !background.Valid() {
		return tcell.ColorDefault
	}
	lab := toOklab(background)
	if lab.l >= 0.6 {
		step = -step
	}
	return fromOklch(lab.l+step, math.Hypot(lab.a, lab.b), math.Atan2(lab.b, lab.a)*180/math.Pi, 0, 0)
}

// toastInfoBlue is the hue an info toast is worked out of, unless the
// theme names toast.info or its highlight is blue.
var toastInfoBlue = tcell.NewRGBColor(0x4a, 0x6f, 0xa5)

// bluish is c when it is a blue with some colour to it, ColorDefault
// otherwise.
func bluish(c tcell.Color) tcell.Color {
	if c == tcell.ColorDefault || !c.Valid() {
		return tcell.ColorDefault
	}
	lab := toOklab(c)
	hue := math.Atan2(lab.b, lab.a) * 180 / math.Pi
	if hue < 0 {
		hue += 360
	}
	if math.Hypot(lab.a, lab.b) < 0.04 || hue < 220 || hue > 280 {
		return tcell.ColorDefault
	}
	return c
}

// toastRoles works out each severity's toast colours the theme does not
// name, from its severity's colour and the background.
func toastRoles(t Theme, roles map[string]tcell.Color) {
	for _, level := range toastLevels {
		accent, own := roles["toast."+level], tcell.ColorDefault
		if level == "info" && t.Roles["toast.info"] == "" {
			// Info is blue whatever the theme's accent is - an orange one
			// read as a warning: the theme's own highlight where that is
			// blue, a quiet blue otherwise.
			accent, own = toastInfoBlue, bluish(colour(t.Selection.Background))
		}
		ink := deriveToastInk(colour(t.Background), accent, own)
		for part, c := range map[string]tcell.Color{"background": ink.fill, "border": ink.border, "text": ink.text, "about": ink.about} {
			if key := "toast." + level + "." + part; t.Roles[key] == "" {
				roles[key] = c
			}
		}
	}
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

// legibleOn makes every colour read on a theme's background at least as
// well as the theme's muted text does - between 3 and 4.5 as WCAG counts
// it - by taking its lightness away from the background a step at a time
// in OKLab, its hue and chroma kept. The heat walks through green and
// yellow, which at the lightness of its two ends are nearly the colour of
// a light background. A theme on the terminal's own background is left as
// it is, since that background is not known.
func legibleOn(cols []tcell.Color, bg, muted tcell.Color) []tcell.Color {
	if bg == tcell.ColorDefault || !bg.Valid() || len(cols) == 0 {
		return cols
	}
	want := 4.5
	if muted != tcell.ColorDefault && muted.Valid() {
		want = clamp(contrast(muted, bg), 3, 4.5)
	}
	step := 0.01
	if toOklab(bg).l >= 0.6 {
		step = -0.01
	}
	out := make([]tcell.Color, len(cols))
	for i, c := range cols {
		lab := toOklab(c)
		chroma, hue := math.Hypot(lab.a, lab.b), math.Atan2(lab.b, lab.a)*180/math.Pi
		for l := lab.l; contrast(c, bg) < want && l > 0 && l < 1; {
			l += step
			c = fromOklch(l, chroma, hue, 0, 0)
		}
		out[i] = c
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
