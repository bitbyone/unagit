package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
)

// TestGuessingANerdFont: the terminals that bring the icons with them are
// trusted whatever the font; the others by the font their settings name.
func TestGuessingANerdFont(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		env  map[string]string
		font string
		want bool
	}{
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, "", true},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, "", true},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, "", true},
		{"iterm with a nerd font", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "SauceCodeProNFM 13", true},
		{"iterm with a long name", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "JetBrainsMono Nerd Font Mono 14", true},
		{"iterm with menlo", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "Menlo-Regular 12", false},
		{"alacritty", map[string]string{"ALACRITTY_WINDOW_ID": "1"}, "Hack Nerd Font", true},
		{"apple terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "", false},
		{"unknown", map[string]string{}, "", false},
	} {
		got, why := guessNerdFontFrom(func(k string) string { return c.env[k] }, "/home", func(string, string) string { return c.font })
		if got != c.want || why == "" {
			t.Errorf("%s: %v (%s), want %v", c.name, got, why, c.want)
		}
	}
}

// TestNerdGlyphsStandInForThePlainOnes: a theme's nerd_glyphs take the
// place of its glyphs, and only those it names.
func TestNerdGlyphsStandInForThePlainOnes(t *testing.T) {
	t.Parallel()
	g := Glyphs{User: "@", Hidden: "⊘"}.over(Glyphs{User: ""})
	if g.User != "" || g.Hidden != "⊘" {
		t.Errorf("over = %+v", g)
	}
	def := loadThemes("").byName[defaultThemeName]
	if def.NerdGlyphs.User == "" || def.Glyphs.User == def.NerdGlyphs.User {
		t.Errorf("the default theme has no icon of its own for a user: %q / %q", def.Glyphs.User, def.NerdGlyphs.User)
	}
	bad := def
	bad.NerdGlyphs = Glyphs{User: "ab"}
	if bad.validate() == nil {
		t.Error("a nerd glyph of two characters was taken")
	}
}

// TestNerdFontIconsAreChosenInSettings: n in Settings › Theme steps the
// icons from the terminal's guess to on to off, draws everything again
// with the glyphs that follow, and remembers the choice. Serial: the
// glyphs are the process's.
func TestNerdFontIconsAreChosenInSettings(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "Nerd Font icons")
	waitFor(t, a, sc, "auto: off - tests")
	user := func() string { return onLoop(a, func() string { return glyphUser }) }
	if user() != "@" {
		t.Fatalf("without a Nerd Font the user is %q", user())
	}
	sc.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, a, sc, "on, as chosen")
	if user() != "" {
		t.Errorf("with the icons on the user is %q", user())
	}
	if got := onLoop(a, func() int { return a.settings.current }); got != sectionTheme {
		t.Errorf("the change left Settings › Theme for section %d", got)
	}
	saved, err := config.LoadFrom(a.cfg.Dir())
	must(t, err)
	if saved.NerdFont != config.NerdFontOn {
		t.Errorf("the choice was not saved: %q", saved.NerdFont)
	}
	sc.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, a, sc, "off, as chosen")
	if user() != "@" {
		t.Errorf("with the icons off the user is %q", user())
	}
	sc.InjectKey(tcell.KeyRune, 'n', tcell.ModNone)
	waitFor(t, a, sc, "auto: off - tests")
	assertLegible(t, a, sc, "Settings › Theme with the Nerd Font line")
}

func TestNerdFontNames(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"SauceCodePro Nerd Font Mono": true, "Hack NF": true, "SauceCodeProNFM 13": true,
		"JetBrainsMonoNFP-Regular 12": true, "Menlo-Regular 12": false, "Fira Code": false,
		"CONFIG Mono": false, "JetBrains Mono NL": false,
	} {
		if got := isNerdFontName(name); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
}

// TestServerIconsGoBeforeRepositoryNames: with the icons on, a repository's
// name in a list has its server's icon before it and a worktree's count of
// repositories one after; with them off there is nothing in their place.
// Serial: the glyphs are the process's.
func TestServerIconsGoBeforeRepositoryNames(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	gitlab := "\U000F0BA0 acme/gateway"
	if strings.Contains(a.screenText(sc), gitlab) {
		t.Fatal("the server's icon is drawn without a Nerd Font")
	}
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "auto: off - tests")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "on, as chosen")
	typeRunes(sc, "1")
	waitFor(t, a, sc, gitlab)
	// The icon is the name's colour, a shade darker. The cursor's band is on
	// the first row, so the second - drawn as it is - is the one looked at.
	text := a.screenText(sc)
	y := lineOf(text, "\U000F0BA0 acme/billing")
	x := strings.Index(lineAt(text, "\U000F0BA0 acme/billing"), "\U000F0BA0")
	x = len([]rune(lineAt(text, "\U000F0BA0 acme/billing")[:x]))
	if r, style := cellAt(a, sc, x, y); r != '\U000F0BA0' {
		t.Errorf("no icon at %d,%d: %q", x, y, r)
	} else if _, name := cellAt(a, sc, x+2, y); fg(style) != iconShade(fg(name)) {
		t.Errorf("the icon is %v, want a shade of the name's %v", fg(style), fg(name))
	}
	typeRunes(sc, "2")
	waitFor(t, a, sc, "\U000F0BA0 acme/gateway")
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", time.Now())
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })
	typeRunes(sc, "3")
	waitFor(t, a, sc, "feat/x")
	waitFor(t, a, sc, "\U000F0BA0 acme/gateway   1 \U000F0CCF feat/x")
	assertLegible(t, a, sc, "the worktrees with icons")
}

// TestAnIntegrationsStateIsAnIconWithANerdFont: the state on a card's top
// edge is a dot in the state's colour with the icons on, and a cell of that
// colour without. Serial: the glyphs are the process's.
func TestAnIntegrationsStateIsAnIconWithANerdFont(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() { nerdFont = true; setTheme(loadThemes("").byName[defaultThemeName]) })
	openSection(t, a, sc, sectionIntegrations)
	focusCard(t, a, sc, "Incomm")
	waitFor(t, a, sc, "\uf192 disabled")
	text := a.screenText(sc)
	y := lineOf(text, "\uf192 disabled")
	line := lineAt(text, "\uf192 disabled")
	x := len([]rune(line[:strings.Index(line, "\uf192 disabled")]))
	r, style := cellAt(a, sc, x, y)
	if _, bg, _ := style.Decompose(); r != '\uf192' || fg(style) != role("integration.disabled") || bg != colCard {
		t.Fatalf("the state's icon is %q in %v on %v", r, fg(style), bg)
	}
	assertLegible(t, a, sc, "integration states as icons")
}

// TestNeovimsMarkIsItsIcon: a directory open in Neovim is marked with
// Neovim's icon with a Nerd Font, and with the plain mark without one.
// Serial: the glyphs are the process's.
func TestNeovimsMarkIsItsIcon(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.rescan()
	changeOnLoop(a, func() {
		a.openDirs = map[string]session.Record{filepath.Clean(p.clone): {Dir: p.clone, Editor: editors.Nvim}}
		a.projectsPane.reload()
	})
	waitFor(t, a, sc, "▣")
	changeOnLoop(a, func() {
		nerdFont = true
		setTheme(loadThemes("").byName[defaultThemeName])
		a.projectsPane.reload()
	})
	waitFor(t, a, sc, "\ue6ae")
	if got := onLoop(a, func() string { return editorGlyph(editors.Zed) }); got != onLoop(a, func() string { return glyphEditor }) {
		t.Errorf("an editor without an icon of its own is marked %q", got)
	}
	assertLegible(t, a, sc, "Neovim's icon in the marks")
}

func fg(s tcell.Style) tcell.Color {
	c, _, _ := s.Decompose()
	return c
}

// TestActionIconsKeepTheNamesInLine: with the icons on, every action in a
// picker has a muted icon before it, or a blank where it has none, so the
// names start in one column. Serial: the glyphs are the process's.
func TestActionIconsKeepTheNamesInLine(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "auto: off - tests")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "on, as chosen")
	typeRunes(sc, "1")
	waitFor(t, a, sc, "acme/gateway")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · acme/gateway")
	icon := onLoop(a, func() string { return actionIcons["Open"] })
	if icon == "" {
		t.Fatal("Open has no icon in the default theme")
	}
	waitFor(t, a, sc, icon+" Open ")
	text := a.screenText(sc)
	start := -1
	for _, name := range []string{"Open With…", "New Worktree…", "Show Details", "Copy…"} {
		line := lineAt(text, name)
		if line == "" {
			t.Fatalf("%s is not listed:\n%s", name, text)
		}
		col := len([]rune(line[:strings.Index(line, name)]))
		if start >= 0 && col != start {
			t.Errorf("%s starts at %d, the others at %d:\n%s", name, col, start, text)
		}
		start = col
	}
	assertLegible(t, a, sc, "the actions with icons")
}

// TestAnAgentsActionsWearItsIcon: with the icons on, Open with <agent>… has
// that agent's icon before it, the one the Agents tab draws, in line with
// the other actions. Serial: the glyphs are the process's.
func TestAnAgentsActionsWearItsIcon(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	_, prepareAgents := fakeAgents(t, "", "claude", "codex")
	a, sc, _ := newTestAppSrv(t, prepareAgents)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() { nerdFont = true; setTheme(loadThemes("").byName[defaultThemeName]) })
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · acme/gateway")
	for _, ag := range []struct{ id, name string }{{"claude", "Claude Code"}, {"codex", "Codex"}} {
		icon := onLoop(a, func() string { return agentIcons[ag.id] })
		if icon == "" {
			t.Fatalf("%s has no icon in the default theme", ag.name)
		}
		waitFor(t, a, sc, icon+" Open with "+ag.name+"…")
	}
	text := a.screenText(sc)
	in := func(name string) int {
		line := lineAt(text, name)
		return len([]rune(line[:strings.Index(line, name)]))
	}
	if in("Open with Claude Code…") != in("Open With…") {
		t.Errorf("the agent's action is out of line with the others:\n%s", text)
	}
	assertLegible(t, a, sc, "the agents' actions with their icons")
}

// TestADraftsIconStandsForItsWord: with the icons on, a draft merge request
// has the draft icon before its title and its own "Draft:" is not drawn.
func TestADraftsIconStandsForItsWord(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.mrs[0].Draft, a.mrs[0].Title = true, "Draft: Rate limiting"
		a.mrsPane.reload()
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "draft Draft: Rate limiting")
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "auto: off - tests")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "on, as chosen")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "\uebd9 Rate limiting")
	if strings.Contains(a.screenText(sc), "Draft:") {
		t.Errorf("the title still says Draft:\n%s", a.screenText(sc))
	}
}

// TestTheTerminalsBackgroundCanStayUnderATheme: b in Settings › Theme
// leaves the terminal's own background under a theme that paints one, the
// rest of its colours kept, remembers it, and gives the theme its
// background back. Serial: the theme is the process's.
func TestTheTerminalsBackgroundCanStayUnderATheme(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { terminalBackground = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() { a.switchTheme("catppuccin-latte") })
	painted := onLoop(a, func() tcell.Color { return colBackground })
	text := onLoop(a, func() tcell.Color { return colText })
	if painted == tcell.ColorDefault {
		t.Fatal("the theme paints no background to begin with")
	}
	openSection(t, a, sc, sectionTheme)
	waitFor(t, a, sc, "Background the theme's")
	sc.InjectKey(tcell.KeyRune, 'b', tcell.ModNone)
	waitFor(t, a, sc, "Background the terminal's own")
	if got := onLoop(a, func() tcell.Color { return colBackground }); got != tcell.ColorDefault {
		t.Errorf("the theme's background is still painted: %v", got)
	}
	if got := onLoop(a, func() tcell.Color { return colText }); got != text {
		t.Errorf("the text lost the theme's colour: %v, was %v", got, text)
	}
	saved, err := config.LoadFrom(a.cfg.Dir())
	must(t, err)
	if !saved.TerminalBackground {
		t.Error("the choice was not saved")
	}
	assertLegible(t, a, sc, "Settings › Theme on the terminal's background")
	sc.InjectKey(tcell.KeyRune, 'b', tcell.ModNone)
	waitFor(t, a, sc, "Background the theme's")
	if got := onLoop(a, func() tcell.Color { return colBackground }); got != painted {
		t.Errorf("the theme's background did not come back: %v", got)
	}
}

// TestTheDefaultEditorIsMarkedByAnIcon: with the icons on, the default
// editor's card says so with a cursor icon and the muted word. Serial: the
// glyphs are the process's.
func TestTheDefaultEditorIsMarkedByAnIcon(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	fakeEditors(t)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	useFavourite(a, editors.Nvim)
	changeOnLoop(a, func() { nerdFont = true; setTheme(loadThemes("").byName[defaultThemeName]) })
	openSection(t, a, sc, sectionIntegrations)
	waitFor(t, a, sc, "Neovim \U000F01BF (default)")
	text := a.screenText(sc)
	line := lineAt(text, "(default)")
	x := len([]rune(line[:strings.Index(line, "(default)")]))
	if _, style := cellAt(a, sc, x, lineOf(text, "(default)")); fg(style).Hex() != colMuted.Hex() {
		t.Errorf("the word is %v, not muted", fg(style))
	}
	assertLegible(t, a, sc, "the default editor's mark")
}
