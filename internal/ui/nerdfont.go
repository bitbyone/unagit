package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tobola/unagit/internal/config"
)

// A Nerd Font adds icons to a font, and a theme can draw some of its glyphs
// as icons (nerd_glyphs) - but only a terminal whose font has them shows
// them; any other shows an empty box. No terminal can be asked what its
// font is, so unagit tells what it can: some terminals bring the icons with
// them whatever the font, and the rest say their font in a file of their
// own. Settings › Theme turns the icons on or off when that guess is wrong.

// nerdFontGuess is what the terminal unagit runs in says about the icons:
// whether it draws them, and how that is known. Tests replace it.
var nerdFontGuess = guessNerdFont

// guessNerdFont tells from the terminal whether it draws Nerd Font icons.
func guessNerdFont() (bool, string) {
	home, _ := os.UserHomeDir()
	return guessNerdFontFrom(os.Getenv, home, readTerminalFont)
}

// guessNerdFontFrom is guessNerdFont with the environment, the home
// directory and the reading of a terminal's settings handed in.
func guessNerdFontFrom(env func(string) string, home string, font func(terminal, home string) string) (bool, string) {
	terminal := env("TERM_PROGRAM")
	switch {
	case terminal == "ghostty":
		return true, "Ghostty draws them with any font"
	case terminal == "WezTerm":
		return true, "WezTerm draws them with any font"
	case env("KITTY_WINDOW_ID") != "" || env("TERM") == "xterm-kitty":
		return true, "kitty draws them with any font"
	case env("ALACRITTY_WINDOW_ID") != "" || env("ALACRITTY_LOG") != "":
		terminal = "Alacritty"
	}
	if terminal == "" {
		return false, "the terminal does not say which it is"
	}
	name := font(terminal, home)
	switch {
	case name == "":
		return false, "the font of " + terminal + " could not be read"
	case isNerdFontName(name):
		return true, terminal + " uses " + name
	}
	return false, terminal + " uses " + name + ", not a Nerd Font"
}

// isNerdFontName reports whether a font's name is a Nerd Font's: "… Nerd
// Font", "… Nerd Font Mono", or the short names, with or without a space
// and a size after - "Hack NF", "SauceCodeProNFM 13", "…NFP-Regular".
func isNerdFontName(name string) bool {
	if strings.Contains(strings.ToLower(name), "nerd font") || strings.Contains(name, "NerdFont") {
		return true
	}
	return shortNerdName.MatchString(name)
}

// shortNerdName matches NF, NFM or NFP ending a family's name.
var shortNerdName = regexp.MustCompile(`(?:^|[a-z0-9\s])NF[MP]?(?:[-\s]|$)`)

// fontSetting matches the line of a terminal's settings that names its font.
var fontSetting = regexp.MustCompile(`(?m)^\s*(?:font-family|family|font_family)\s*=\s*"?([^"\n]+?)"?\s*$`)

// readTerminalFont is the font a terminal's own settings name, "" when it
// cannot be read.
func readTerminalFont(terminal, home string) string {
	switch terminal {
	case "iTerm.app":
		// "Normal Font" = "SauceCodeProNFM 13";
		out, err := exec.Command("defaults", "read", "com.googlecode.iterm2", "New Bookmarks").Output()
		if err != nil {
			return ""
		}
		if m := regexp.MustCompile(`"Normal Font" = "?([^";]+)`).FindSubmatch(out); m != nil {
			return strings.TrimSpace(string(m[1]))
		}
	case "Alacritty":
		for _, f := range []string{".config/alacritty/alacritty.toml", ".alacritty.toml"} {
			if data, err := os.ReadFile(filepath.Join(home, f)); err == nil {
				if m := fontSetting.FindSubmatch(data); m != nil {
					return strings.TrimSpace(string(m[1]))
				}
			}
		}
	}
	return ""
}

// useNerdFont settles whether the icons are drawn: as the configuration
// says, or as the terminal is guessed; and why, for Settings › Theme.
func useNerdFont(setting string) (bool, string) {
	switch setting {
	case config.NerdFontOn:
		return true, "on, as chosen"
	case config.NerdFontOff:
		return false, "off, as chosen"
	}
	on, why := nerdFontGuess()
	state := "off"
	if on {
		state = "on"
	}
	return on, "auto: " + state + " - " + why
}
