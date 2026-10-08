package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// A theme is everything unagit draws with that is a choice rather than a
// layout: the colours of text, borders, surfaces and states, the background,
// and the glyphs that say what something is. It is a JSON file - the ones
// unagit comes with are in themes/, the user's own in <config>/themes - and
// what a file leaves out it takes from the theme it extends, or from the
// default one.
//
// A colour is "default" (the terminal's own), "#rrggbb", a number of the
// 256-colour palette, or a colour name tcell knows.

//go:embed themes/*.json
var builtinThemeFiles embed.FS

// defaultThemeName is the theme unagit starts with: the terminal's own
// background and a muted palette that does not shout over an editor.
const defaultThemeName = "unagit"

// Theme is a theme as its file spells it.
type Theme struct {
	// Name is how it is listed and chosen; a user's file is named by its file
	// name when it says nothing. Description is a line about it.
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Extends names the theme this one starts from; the default one when
	// empty.
	Extends string `json:"extends,omitempty"`

	// Background is the screen's background; "default" leaves the
	// terminal's, so unagit sits on whatever the editor around it looks like.
	Background string `json:"background"`

	Text struct {
		// Normal is body text; Muted quieter, for labels and secondary
		// columns; Dim the quietest, for hints and headers.
		Normal string `json:"normal"`
		Muted  string `json:"muted"`
		Dim    string `json:"dim"`
		// Accent is for what can be followed - links, the !reference, the
		// lit part of a hint; Branch for branch names; Key for the letter
		// that presses a button.
		Accent string `json:"accent"`
		Branch string `json:"branch"`
		Key    string `json:"key"`
	} `json:"text"`

	State struct {
		// Good is done and well: up to date, passed, approved, on. Warning is
		// what needs a look; Bad what failed; Force a step short of bad - to
		// be done on purpose; Favourite the star.
		Good      string `json:"good"`
		Warning   string `json:"warning"`
		Bad       string `json:"bad"`
		Force     string `json:"force"`
		Favourite string `json:"favourite"`
	} `json:"state"`

	Border struct {
		// Normal frames a panel, Focus the one the keys go to, Title names
		// it.
		Normal string `json:"normal"`
		Focus  string `json:"focus"`
		Title  string `json:"title"`
	} `json:"border"`

	Tabs struct {
		Active    string `json:"active"`
		Inactive  string `json:"inactive"`
		Separator string `json:"separator"`
	} `json:"tabs"`

	Surface struct {
		// Field is a raised panel: a field or a button at rest, a message's
		// fill - and the ink on one that is active. Raised is a step above
		// it. FieldFocus is the field a form's focus is on, FieldTyping the
		// same while it is typed into.
		Field       string `json:"field"`
		Raised      string `json:"raised"`
		FieldFocus  string `json:"field_focus"`
		FieldTyping string `json:"field_typing"`
	} `json:"surface"`

	Selection struct {
		// Background and Text are the cursor's band across a row. Marked is
		// a row marked with space; MarkedCursor the cursor on a marked row.
		Background   string `json:"background"`
		Text         string `json:"text"`
		Marked       string `json:"marked"`
		MarkedCursor string `json:"marked_cursor"`
	} `json:"selection"`

	Backdrop struct {
		// Dim is how much of the screen's brightness is left behind a
		// dialog, from 0 to 1; Text stands in for the terminal's own text
		// colour there, which cannot be dimmed because it cannot be known.
		Dim  float64 `json:"dim"`
		Text string  `json:"text"`
	} `json:"backdrop"`

	Markdown struct {
		Text    string `json:"text"`
		Heading string `json:"heading"`
		Code    string `json:"code"`
		Quote   string `json:"quote"`
		Link    string `json:"link"`
		Muted   string `json:"muted"`
	} `json:"markdown"`

	Chezmoi struct {
		// Badge is the quiet pill on a repository chezmoi keeps, Heading the
		// line that says so in its detail.
		BadgeInk    string `json:"badge_ink"`
		BadgeFill   string `json:"badge_fill"`
		HeadingInk  string `json:"heading_ink"`
		HeadingFill string `json:"heading_fill"`
	} `json:"chezmoi"`

	// Tags are the colours a tag can be given, by name: ink on a fill of
	// the same hue. A theme can repaint any of them; the names stay, since
	// tags are saved by them. Those a theme with a background of its own
	// leaves out are worked out of its colours (deriveTags).
	Tags map[string]TagInk `json:"tags"`
	// tagsNamed are the tags this theme, or one it extends short of the
	// default, names itself; the rest are worked out of its colours when it
	// paints a background of its own (deriveTags).
	tagsNamed map[string]bool

	// Roles colour what the theme wants set apart, by the role's name
	// (themeroles.go); every role it leaves out falls back on another, down
	// to the colours above.
	Roles map[string]string `json:"colours,omitempty"`
	// Heat is the colours a size is drawn in from the least to the most -
	// two or more, spread over sixteen shades - none for no heat.
	Heat []string `json:"heat,omitempty"`

	Glyphs Glyphs `json:"glyphs"`
	// ActionIcons go before the actions in the action pickers, by the
	// action's name, muted. They are Nerd Font icons and nothing else: drawn
	// only where the terminal can, and an action without one keeps its place
	// in the column.
	ActionIcons map[string]string `json:"action_icons,omitempty"`
	// NerdGlyphs are the glyphs drawn instead when the terminal's font is a
	// Nerd Font (Settings › Theme): only the ones it names, the rest as
	// Glyphs has them. A theme that wants an icon gives it here and a plain
	// character in Glyphs, so it reads well either way.
	NerdGlyphs Glyphs `json:"nerd_glyphs"`

	Borders struct {
		Horizontal  string `json:"horizontal"`
		Vertical    string `json:"vertical"`
		TopLeft     string `json:"top_left"`
		TopRight    string `json:"top_right"`
		BottomLeft  string `json:"bottom_left"`
		BottomRight string `json:"bottom_right"`
	} `json:"borders"`

	// file is where a user's theme was read from; "" for a built-in one.
	file string
}

// TagInk is one tag colour.
type TagInk struct {
	Ink  string `json:"ink"`
	Fill string `json:"fill"`
}

// over is the glyphs with the ones n names put in their place.
func (g Glyphs) over(n Glyphs) Glyphs {
	out := reflect.ValueOf(&g).Elem()
	in := reflect.ValueOf(n)
	for i := range in.NumField() {
		if v := in.Field(i).String(); v != "" {
			out.Field(i).SetString(v)
		}
	}
	return g
}

// named lists the glyphs n gives, by their names in the file.
func (n Glyphs) named() map[string]string {
	out := map[string]string{}
	v, t := reflect.ValueOf(n), reflect.TypeOf(n)
	for i := range v.NumField() {
		if s := v.Field(i).String(); s != "" {
			out["nerd_glyphs."+strings.Split(t.Field(i).Tag.Get("json"), ",")[0]] = s
		}
	}
	return out
}

// Glyphs are the characters that say what something is.
type Glyphs struct {
	Editor string `json:"editor"`
	// On disk: nothing, a branch worktree, a review worktree, both.
	DiskNone   string `json:"disk_none"`
	DiskBranch string `json:"disk_branch"`
	DiskReview string `json:"disk_review"`
	DiskBoth   string `json:"disk_both"`
	// Worktree marks a worktree of one repository in Worktrees, Group a
	// grouped one, Hidden a hidden repository, Favourite a starred one.
	Worktree string `json:"worktree"`
	Group    string `json:"group"`
	// Repos follows the count of repositories in a worktree's row, Draft
	// stands for the word before a draft merge request's title, and
	// ForgeGitHub and ForgeGitLab go before a repository's name, saying its
	// server. They are icons and nothing else: they have no plain character,
	// so they are given under nerd_glyphs alone and drawn only where the
	// terminal can.
	Repos       string `json:"repos"`
	Draft       string `json:"draft"`
	ForgeGitHub string `json:"forge_github"`
	ForgeGitLab string `json:"forge_gitlab"`
	// The coding agents' own icons, before an agent's name; icons alone,
	// like the forges'.
	AgentClaude   string `json:"agent_claude"`
	AgentCodex    string `json:"agent_codex"`
	AgentCopilot  string `json:"agent_copilot"`
	AgentOpencode string `json:"agent_opencode"`
	AgentAgy      string `json:"agent_agy"`
	// EditorNeovim marks a directory open in Neovim, in place of Editor,
	// which stays the mark of an editor without an icon of its own; an
	// icon alone.
	EditorNeovim string `json:"editor_neovim"`
	// IntegrationState goes before an integration's state on its card, in
	// the state's colour; an icon alone, without which a cell of the colour
	// stands there.
	IntegrationState string `json:"integration_state"`
	Hidden           string `json:"hidden"`
	Favourite        string `json:"favourite"`
	// Check and Cross are yes and no: up to date, approved, chosen; failed.
	Check string `json:"check"`
	Cross string `json:"cross"`
	// Dot is a state's bullet - running, on, new - and Ring its empty one.
	Dot  string `json:"dot"`
	Ring string `json:"ring"`
	// Manual is a job that waits for a hand to start it, Scheduled one that
	// waits for its time, Trigger one that starts a pipeline of its own.
	Manual    string `json:"manual"`
	Scheduled string `json:"scheduled"`
	Trigger   string `json:"trigger"`
	// Retried marks an earlier attempt of a job that was run again.
	Retried string `json:"retried"`
	// Approved marks an approval of one's own.
	Approved string `json:"approved"`
	// Starred marks a repository from the user's stars.
	Starred string `json:"starred"`
	// User marks a person among other things: a hidden author beside the
	// hidden repositories.
	User string `json:"user"`
	// Agent marks a directory an agent works in, for an agent without an
	// icon of its own or a terminal without a Nerd Font.
	Agent string `json:"agent"`
	// Ahead and Behind are commits not pushed and not pulled.
	Ahead  string `json:"ahead"`
	Behind string `json:"behind"`
	// External marks what lives outside unagit (chezmoi's checkout), Merge
	// a merge commit, Select the end of a closed select, Mask a typed
	// character of a passphrase, Bar a horizontal bar, TabSeparator what
	// stands between tabs.
	External     string `json:"external"`
	Merge        string `json:"merge"`
	Select       string `json:"select"`
	Mask         string `json:"mask"`
	Bar          string `json:"bar"`
	TabSeparator string `json:"tab_separator"`
	// Elided stands for the folders a path shortened to fit leaves out: not
	// two dots, which on a file system mean the folder above. ElidedGroup
	// is the same for the groups in front of a repository's name.
	Elided      string `json:"elided"`
	ElidedGroup string `json:"elided_group"`
	// Edits heads the column counting the files not committed.
	Edits string `json:"edits"`
	// Shown and Hidden mark a column of a list as shown or left out, in
	// View options: an eye and the eye struck through.
	ColumnShown  string `json:"column_shown"`
	ColumnHidden string `json:"column_hidden"`
	// Spinner is the frames of what turns while a job runs behind the
	// interface, one character each, in order.
	Spinner string `json:"spinner"`
	// CIRunning is the frames of a pipeline under way, turning in its
	// column until it ends; CIDone the mark of one that passed or failed
	// (its colour says which), CIIdle of one skipped or canceled.
	CIRunning string `json:"ci_running"`
	CIDone    string `json:"ci_done"`
	CIIdle    string `json:"ci_idle"`
}

// readTheme reads one theme file over a base: what the file does not say
// stays as the base has it.
func readTheme(data []byte, base Theme) (Theme, error) {
	t := base
	// The base's tags are copied so the file adds to them rather than to the
	// base's own map.
	t.Tags = map[string]TagInk{}
	for name, ink := range base.Tags {
		t.Tags[name] = ink
	}
	t.Roles = map[string]string{}
	for key, value := range base.Roles {
		t.Roles[key] = value
	}
	t.ActionIcons = map[string]string{}
	for name, icon := range base.ActionIcons {
		t.ActionIcons[name] = icon
	}
	// A copy too: decoding into the base's own slice wrote every theme's
	// heat over the one before it.
	t.Heat = append([]string(nil), base.Heat...)
	t.Name, t.Description, t.Extends, t.file = "", "", "", ""
	if err := json.Unmarshal(data, &t); err != nil {
		return Theme{}, err
	}
	return t, nil
}

// nameTags records which tags a theme names, its own and those of what it
// extends, and works out the rest when it paints a background of its own:
// the default's pills are made for a dark terminal.
func nameTags(t *Theme, base Theme, data []byte) {
	var own struct {
		Tags map[string]json.RawMessage `json:"tags"`
	}
	_ = json.Unmarshal(data, &own)
	t.tagsNamed = map[string]bool{}
	for name := range base.tagsNamed {
		t.tagsNamed[name] = true
	}
	for name := range own.Tags {
		t.tagsNamed[name] = true
	}
	if colour(t.Background) == tcell.ColorDefault {
		return
	}
	for name, ink := range deriveTags(*t) {
		if !t.tagsNamed[name] {
			t.Tags[name] = ink
		}
	}
}

// themeSet is every theme there is: the built-in ones and the user's, by
// name, with what went wrong reading the user's.
type themeSet struct {
	byName map[string]Theme
	names  []string
	// problems names the user's files that could not be used, and why.
	problems []string
}

// builtinThemes reads the themes unagit comes with. A broken one is a bug of
// unagit's, caught by the tests, so it panics.
func builtinThemes() map[string][]byte {
	entries, err := builtinThemeFiles.ReadDir("themes")
	if err != nil {
		panic(err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		data, err := builtinThemeFiles.ReadFile("themes/" + e.Name())
		if err != nil {
			panic(err)
		}
		out[strings.TrimSuffix(e.Name(), ".json")] = data
	}
	return out
}

// loadThemes reads the built-in themes and those in dir. A user's theme may
// extend any other, built-in or not, and may take a built-in one's name to
// replace it.
func loadThemes(dir string) themeSet {
	raw := map[string][]byte{}
	files := map[string]string{}
	for name, data := range builtinThemes() {
		raw[name] = data
	}
	set := themeSet{byName: map[string]Theme{}}
	if dir != "" {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(paths)
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				set.problems = append(set.problems, fmt.Sprintf("%s: %v", filepath.Base(path), err))
				continue
			}
			var head struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(data, &head); err != nil {
				set.problems = append(set.problems, fmt.Sprintf("%s: %v", filepath.Base(path), err))
				continue
			}
			name := head.Name
			if name == "" {
				name = strings.TrimSuffix(filepath.Base(path), ".json")
			}
			raw[name], files[name] = data, path
		}
	}

	// Each theme is built on what it extends, which is built first; a chain
	// that comes back on itself is reported rather than followed for ever.
	var build func(name string, seen map[string]bool) (Theme, error)
	build = func(name string, seen map[string]bool) (Theme, error) {
		if t, ok := set.byName[name]; ok {
			return t, nil
		}
		data, ok := raw[name]
		if !ok {
			return Theme{}, fmt.Errorf("there is no theme %q to extend", name)
		}
		if seen[name] {
			return Theme{}, fmt.Errorf("%q extends itself", name)
		}
		seen[name] = true
		var head struct {
			Extends string `json:"extends"`
		}
		_ = json.Unmarshal(data, &head)
		base := Theme{}
		if name != defaultThemeName {
			parent := head.Extends
			if parent == "" {
				parent = defaultThemeName
			}
			var err error
			if base, err = build(parent, seen); err != nil {
				return Theme{}, err
			}
		}
		t, err := readTheme(data, base)
		if err != nil {
			return Theme{}, err
		}
		t.Name, t.file = name, files[name]
		if err := t.validate(); err != nil {
			return Theme{}, err
		}
		if name != defaultThemeName {
			nameTags(&t, base, data)
		}
		set.byName[name] = t
		return t, nil
	}
	for name := range raw {
		if _, err := build(name, map[string]bool{}); err != nil {
			if files[name] == "" {
				panic(fmt.Sprintf("built-in theme %s: %v", name, err))
			}
			set.problems = append(set.problems, fmt.Sprintf("%s: %v", filepath.Base(files[name]), err))
		}
	}
	for name := range set.byName {
		set.names = append(set.names, name)
	}
	// The default first, then the rest by name.
	sort.Slice(set.names, func(i, j int) bool {
		if (set.names[i] == defaultThemeName) != (set.names[j] == defaultThemeName) {
			return set.names[i] == defaultThemeName
		}
		return set.names[i] < set.names[j]
	})
	sort.Strings(set.problems)
	return set
}

// parseColour reads a colour as a theme spells it.
func parseColour(s string) (tcell.Color, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "" || s == "default":
		return tcell.ColorDefault, nil
	case strings.HasPrefix(s, "#"):
		if len(s) != 7 {
			return 0, fmt.Errorf("%q is not #rrggbb", s)
		}
		if _, err := strconv.ParseUint(s[1:], 16, 32); err != nil {
			return 0, fmt.Errorf("%q is not #rrggbb", s)
		}
		return tcell.GetColor(s), nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > 255 {
			return 0, fmt.Errorf("%d is not a colour of the 256", n)
		}
		return tcell.PaletteColor(n), nil
	}
	if c := tcell.GetColor(strings.ToLower(s)); c != tcell.ColorDefault {
		return c, nil
	}
	return 0, fmt.Errorf("%q is not a colour", s)
}

// colours lists every colour of the theme with its name in the file, so
// that one wrong value can be named.
func (t Theme) colours() map[string]string {
	out := map[string]string{
		"background":              t.Background,
		"text.normal":             t.Text.Normal,
		"text.muted":              t.Text.Muted,
		"text.dim":                t.Text.Dim,
		"text.accent":             t.Text.Accent,
		"text.branch":             t.Text.Branch,
		"text.key":                t.Text.Key,
		"state.good":              t.State.Good,
		"state.warning":           t.State.Warning,
		"state.bad":               t.State.Bad,
		"state.force":             t.State.Force,
		"state.favourite":         t.State.Favourite,
		"border.normal":           t.Border.Normal,
		"border.focus":            t.Border.Focus,
		"border.title":            t.Border.Title,
		"tabs.active":             t.Tabs.Active,
		"tabs.inactive":           t.Tabs.Inactive,
		"tabs.separator":          t.Tabs.Separator,
		"surface.field":           t.Surface.Field,
		"surface.raised":          t.Surface.Raised,
		"surface.field_focus":     t.Surface.FieldFocus,
		"surface.field_typing":    t.Surface.FieldTyping,
		"selection.background":    t.Selection.Background,
		"selection.text":          t.Selection.Text,
		"selection.marked":        t.Selection.Marked,
		"selection.marked_cursor": t.Selection.MarkedCursor,
		"backdrop.text":           t.Backdrop.Text,
		"markdown.text":           t.Markdown.Text,
		"markdown.heading":        t.Markdown.Heading,
		"markdown.code":           t.Markdown.Code,
		"markdown.quote":          t.Markdown.Quote,
		"markdown.link":           t.Markdown.Link,
		"markdown.muted":          t.Markdown.Muted,
		"chezmoi.badge_ink":       t.Chezmoi.BadgeInk,
		"chezmoi.badge_fill":      t.Chezmoi.BadgeFill,
		"chezmoi.heading_ink":     t.Chezmoi.HeadingInk,
		"chezmoi.heading_fill":    t.Chezmoi.HeadingFill,
	}
	for name, ink := range t.Tags {
		out["tags."+name+".ink"] = ink.Ink
		out["tags."+name+".fill"] = ink.Fill
	}
	return out
}

// glyphs lists every glyph with its name in the file.
func (t Theme) glyphs() map[string]string {
	g := t.Glyphs
	return map[string]string{
		"glyphs.editor":    g.Editor,
		"glyphs.disk_none": g.DiskNone, "glyphs.disk_branch": g.DiskBranch,
		"glyphs.disk_review": g.DiskReview, "glyphs.disk_both": g.DiskBoth,
		"glyphs.worktree": g.Worktree, "glyphs.group": g.Group, "glyphs.hidden": g.Hidden, "glyphs.favourite": g.Favourite,
		"glyphs.check": g.Check, "glyphs.cross": g.Cross, "glyphs.dot": g.Dot, "glyphs.ring": g.Ring,
		"glyphs.manual": g.Manual, "glyphs.scheduled": g.Scheduled, "glyphs.trigger": g.Trigger, "glyphs.retried": g.Retried, "glyphs.user": g.User, "glyphs.agent": g.Agent, "glyphs.starred": g.Starred, "glyphs.approved": g.Approved,
		"glyphs.ci_done": g.CIDone, "glyphs.ci_idle": g.CIIdle,
		"glyphs.ahead": g.Ahead, "glyphs.behind": g.Behind, "glyphs.external": g.External,
		"glyphs.merge": g.Merge, "glyphs.select": g.Select, "glyphs.mask": g.Mask,
		"glyphs.bar": g.Bar, "glyphs.tab_separator": g.TabSeparator, "glyphs.elided": g.Elided,
		"glyphs.elided_group": g.ElidedGroup, "glyphs.edits": g.Edits,
		"glyphs.column_shown": g.ColumnShown, "glyphs.column_hidden": g.ColumnHidden,
		"borders.horizontal": t.Borders.Horizontal, "borders.vertical": t.Borders.Vertical,
		"borders.top_left": t.Borders.TopLeft, "borders.top_right": t.Borders.TopRight,
		"borders.bottom_left": t.Borders.BottomLeft, "borders.bottom_right": t.Borders.BottomRight,
	}
}

// validate says what in a theme cannot be drawn: a colour that is not one,
// a glyph that is not a single character, a dim out of range.
func (t Theme) validate() error {
	var problems []string
	for key, value := range t.colours() {
		if _, err := parseColour(value); err != nil {
			problems = append(problems, key+": "+err.Error())
		}
	}
	for key, value := range t.glyphs() {
		if utf8.RuneCountInString(value) != 1 {
			problems = append(problems, fmt.Sprintf("%s: %q is not one character", key, value))
		}
	}
	for key, value := range t.Roles {
		if _, err := parseColour(value); err != nil {
			problems = append(problems, "colours."+key+": "+err.Error())
		}
	}
	for i, value := range t.Heat {
		if _, err := parseColour(value); err != nil {
			problems = append(problems, fmt.Sprintf("heat[%d]: %v", i, err))
		}
	}
	problems = append(problems, roleProblems(t)...)
	for key, value := range t.NerdGlyphs.named() {
		if key == "nerd_glyphs.spinner" || key == "nerd_glyphs.ci_running" {
			continue
		}
		if utf8.RuneCountInString(value) != 1 {
			problems = append(problems, fmt.Sprintf("%s: %q is not one character", key, value))
		}
	}
	for name, icon := range t.ActionIcons {
		if utf8.RuneCountInString(icon) > 1 {
			problems = append(problems, fmt.Sprintf("action_icons.%s: %q is not one character", name, icon))
		}
	}
	if t.Glyphs.CIRunning == "" {
		problems = append(problems, "glyphs.ci_running: it needs at least one frame")
	}
	if t.Glyphs.Spinner == "" {
		problems = append(problems, "glyphs.spinner: it needs at least one frame")
	}
	if t.Backdrop.Dim < 0 || t.Backdrop.Dim > 1 {
		problems = append(problems, fmt.Sprintf("backdrop.dim: %v is not between 0 and 1", t.Backdrop.Dim))
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// colour is a colour of a validated theme.
func colour(s string) tcell.Color {
	c, _ := parseColour(s)
	return c
}

// rune0 is the one character of a validated glyph.
func rune0(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}
