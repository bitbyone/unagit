package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
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
