// Package keymap is the keys of unagit's actions as the user chose them in
// keybinds.toml, and what a terminal can send at all.
//
// A key has two spellings. The one in the file is Ghostty's and Helix's
// lower case with a plus - ctrl+e, alt+enter, shift+f5 - and the one on
// screen is the one every hint and picker of unagit has always shown -
// Ctrl-E, Alt-Enter, Shift-F5. Parse reads the first, Display the second,
// and both come back out of a Key the same way, so a key compares by its
// String.
package keymap

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
)

// Key is one key a terminal can send.
type Key struct {
	// Key is tcell's key: a control code (KeyCtrlE, KeyCtrlSpace), a named
	// key (KeyF5, KeyPgUp, KeyEnter) or KeyRune.
	Key tcell.Key
	// Rune is the character of a KeyRune, lower case when Mod has Alt:
	// Alt is matched without case, as it always was, since terminals
	// differ in whether Shift reaches it.
	Rune rune
	// Mod is the modifiers of a named key or a rune; a control code
	// carries its Ctrl in itself.
	Mod tcell.ModMask
}

// A control code is one key whatever modifiers tcell reports with it. Enter
// is a control code too, but bound only with Alt, which tells it apart.
func (k Key) control() bool { return k.Key < tcell.KeyDEL && k.Mod == 0 }

// Matches reports whether a key event is this key.
func (k Key) Matches(ev *tcell.EventKey) bool {
	switch {
	case k.control():
		return ev.Key() == k.Key
	case k.Key == tcell.KeyRune:
		if ev.Key() != tcell.KeyRune || ev.Modifiers()&tcell.ModCtrl != 0 {
			return false
		}
		if k.Mod&tcell.ModAlt != 0 {
			return ev.Modifiers()&tcell.ModAlt != 0 && unicode.ToLower(ev.Rune()) == k.Rune
		}
		return ev.Modifiers()&tcell.ModAlt == 0 && ev.Rune() == k.Rune
	default:
		return ev.Key() == k.Key && ev.Modifiers()&(tcell.ModAlt|tcell.ModCtrl|tcell.ModShift) == k.Mod
	}
}

// The named keys a binding may use, by their names in the file and on
// screen. Tab, Esc, Backspace and the arrows are not here: every list and
// form of unagit moves with them.
var named = []struct {
	key           tcell.Key
	file, display string
	// bare says whether the key may be bound without a modifier: Enter,
	// Home and the pages already move and choose in every list.
	bare bool
}{
	{tcell.KeyEnter, "enter", "Enter", false},
	{tcell.KeyHome, "home", "Home", false},
	{tcell.KeyEnd, "end", "End", false},
	{tcell.KeyPgUp, "pageup", "PgUp", false},
	{tcell.KeyPgDn, "pagedown", "PgDn", false},
	{tcell.KeyInsert, "insert", "Insert", true},
	{tcell.KeyDelete, "delete", "Delete", true},
}

// The control codes beyond the letters, by the character typed with Ctrl.
var controls = map[rune]tcell.Key{
	' ':  tcell.KeyCtrlSpace,
	'\\': tcell.KeyCtrlBackslash,
	']':  tcell.KeyCtrlRightSq,
	'^':  tcell.KeyCtrlCarat,
	'_':  tcell.KeyCtrlUnderscore,
}

// The letters a terminal sends with Ctrl as another key, and Ctrl-C, which
// ends unagit before any action sees it.
var ctrlTaken = map[rune]string{
	'c': "ends unagit",
	'h': "is Backspace to a terminal",
	'i': "is Tab to a terminal",
	'm': "is Enter to a terminal",
	'[': "is Esc to a terminal",
}

// Reserved is the plain keys every list of unagit moves, filters and asks
// for help with, which no action can have: bound to one, it would never be
// reached.
const Reserved = "jkgGhlq/?123456789"

// Parse reads a key as keybinds.toml spells it: modifiers and a key joined
// by +, as in ctrl+e, alt+o, shift+f5, alt+enter, ctrl+space, or a single
// character, P, +. It refuses a key a terminal cannot send apart from
// another, and says why.
func Parse(s string) (Key, error) { return parse(s, true) }

// Default reads a key as Parse does, but takes the keys every list keeps
// for itself too: unagit's own defaults use some of them where no list
// moves with them.
func Default(s string) (Key, error) { return parse(s, false) }

func parse(s string, strict bool) (Key, error) {
	if s == "" {
		return Key{}, fmt.Errorf("an empty key: leave the action out, or bind it to [] to take its key away")
	}
	parts := strings.Split(s, "+")
	// The last part is the key, and the key can be + itself: "+", "alt++".
	if strings.HasSuffix(s, "+") {
		parts = append(parts[:len(parts)-2], "+")
	}
	var mod tcell.ModMask
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "ctrl", "control":
			mod |= tcell.ModCtrl
		case "alt", "opt", "option", "meta":
			mod |= tcell.ModAlt
		case "shift":
			mod |= tcell.ModShift
		case "super", "cmd", "command", "win":
			return Key{}, fmt.Errorf("%s: %s never reaches a program in a terminal", s, m)
		default:
			return Key{}, fmt.Errorf("%s: %q is not a modifier (ctrl, alt, shift)", s, m)
		}
	}
	key := parts[len(parts)-1]
	runes := []rune(key)
	if len(runes) == 1 {
		return parseChar(s, runes[0], mod, strict)
	}
	return parseNamed(s, strings.ToLower(key), mod, strict)
}

func parseChar(s string, r rune, mod tcell.ModMask, strict bool) (Key, error) {
	lower := unicode.ToLower(r)
	switch {
	case mod&tcell.ModCtrl != 0:
		if mod != tcell.ModCtrl {
			return Key{}, fmt.Errorf("%s: a terminal sends ctrl with shift or alt as ctrl alone; use ctrl+%c", s, lower)
		}
		if why, ok := ctrlTaken[lower]; ok {
			return Key{}, fmt.Errorf("ctrl+%c %s", lower, why)
		}
		if lower >= 'a' && lower <= 'z' {
			return Key{Key: tcell.KeyCtrlA + tcell.Key(lower-'a')}, nil
		}
		if k, ok := controls[r]; ok {
			return Key{Key: k}, nil
		}
		return Key{}, fmt.Errorf("%s: a terminal sends no ctrl+%c; ctrl goes with a letter, space, \\, ], ^ or _", s, r)
	case r == ' ':
		return Key{}, fmt.Errorf("%s: space marks rows in every list; ctrl+space is free", s)
	case !unicode.IsPrint(r):
		return Key{}, fmt.Errorf("%s: not a key that can be typed", s)
	case mod&tcell.ModAlt != 0:
		if mod&tcell.ModShift != 0 || unicode.IsUpper(r) {
			return Key{}, fmt.Errorf("%s: alt keys are told apart without case; use alt+%c", s, lower)
		}
		return Key{Key: tcell.KeyRune, Rune: lower, Mod: tcell.ModAlt}, nil
	case mod == tcell.ModShift:
		if !unicode.IsLetter(r) {
			return Key{}, fmt.Errorf("%s: write the character shift makes instead", s)
		}
		r = unicode.ToUpper(r)
	}
	if strict && strings.ContainsRune(Reserved, r) {
		return Key{}, fmt.Errorf("%s: %c moves, filters or asks for help in every list, so no action can have it", s, r)
	}
	return Key{Key: tcell.KeyRune, Rune: r}, nil
}

func parseNamed(s, key string, mod tcell.ModMask, strict bool) (Key, error) {
	var f int
	if n, err := fmt.Sscanf(key, "f%d", &f); err == nil && n == 1 && fmt.Sprintf("f%d", f) == key {
		if f < 1 || f > 12 {
			return Key{}, fmt.Errorf("%s: the function keys are f1 to f12", s)
		}
		return Key{Key: tcell.KeyF1 + tcell.Key(f-1), Mod: mod}, nil
	}
	switch key {
	case "space":
		return parseChar(s, ' ', mod, strict)
	case "pgup":
		key = "pageup"
	case "pgdn":
		key = "pagedown"
	case "del":
		key = "delete"
	case "return":
		key = "enter"
	case "tab", "esc", "escape", "backspace", "up", "down", "left", "right":
		return Key{}, fmt.Errorf("%s: %s moves and leaves in every list and form, so no action can have it", s, key)
	}
	for _, n := range named {
		if n.file != key {
			continue
		}
		switch {
		case mod == 0 && !n.bare:
			return Key{}, fmt.Errorf("%s: %s moves or chooses in every list; give it a modifier, as alt+%s", s, key, key)
		case n.key == tcell.KeyEnter && mod != tcell.ModAlt:
			return Key{}, fmt.Errorf("%s: a terminal sends enter with ctrl or shift as enter alone; alt+enter is the one it tells apart", s)
		}
		return Key{Key: n.key, Mod: mod}, nil
	}
	return Key{}, fmt.Errorf("%s: %q is not a key unagit knows (a character, space, enter, f1-f12, home, end, pageup, pagedown, insert, delete)", s, key)
}

// Display reads a key as unagit's hints show it - Ctrl-E, Alt-O, Alt-Enter,
// F5, x - which is how the actions name their keys in the code. ok is false
// for anything else, such as the words a list answers itself (Enter,
// space).
func Display(s string) (Key, bool) {
	var mod tcell.ModMask
	rest := s
	for {
		switch {
		case strings.HasPrefix(rest, "Ctrl-") && len(rest) > len("Ctrl-"):
			mod |= tcell.ModCtrl
			rest = rest[len("Ctrl-"):]
			continue
		case strings.HasPrefix(rest, "Alt-") && len(rest) > len("Alt-"):
			mod |= tcell.ModAlt
			rest = rest[len("Alt-"):]
			continue
		case strings.HasPrefix(rest, "Shift-") && len(rest) > len("Shift-"):
			mod |= tcell.ModShift
			rest = rest[len("Shift-"):]
			continue
		}
		break
	}
	runes := []rune(rest)
	if len(runes) == 1 {
		r := runes[0]
		switch {
		case mod == tcell.ModCtrl:
			if l := unicode.ToLower(r); l >= 'a' && l <= 'z' {
				return Key{Key: tcell.KeyCtrlA + tcell.Key(l-'a')}, true
			}
			k, ok := controls[r]
			return Key{Key: k}, ok
		case mod == tcell.ModAlt:
			return Key{Key: tcell.KeyRune, Rune: unicode.ToLower(r), Mod: tcell.ModAlt}, true
		case mod == 0 && unicode.IsPrint(r) && r != ' ':
			return Key{Key: tcell.KeyRune, Rune: r}, true
		}
		return Key{}, false
	}
	if rest == "Space" && mod == tcell.ModCtrl {
		return Key{Key: tcell.KeyCtrlSpace}, true
	}
	var f int
	if n, err := fmt.Sscanf(rest, "F%d", &f); err == nil && n == 1 && f >= 1 && f <= 12 && fmt.Sprintf("F%d", f) == rest {
		return Key{Key: tcell.KeyF1 + tcell.Key(f-1), Mod: mod}, true
	}
	for _, n := range named {
		// A bare Enter is the word a list answers itself, not a key here.
		if n.display == rest && (mod != 0 || n.key != tcell.KeyEnter) {
			return Key{Key: n.key, Mod: mod}, true
		}
	}
	return Key{}, false
}

// String is the key as the hints show it, the spelling Display reads.
func (k Key) String() string {
	if k.control() {
		if k.Key >= tcell.KeyCtrlA && k.Key <= tcell.KeyCtrlZ {
			return "Ctrl-" + string(rune('A'+k.Key-tcell.KeyCtrlA))
		}
		if k.Key == tcell.KeyCtrlSpace {
			return "Ctrl-Space"
		}
		for r, c := range controls {
			if c == k.Key {
				return "Ctrl-" + string(r)
			}
		}
		return fmt.Sprintf("Ctrl-%d", k.Key)
	}
	prefix := ""
	if k.Mod&tcell.ModCtrl != 0 {
		prefix += "Ctrl-"
	}
	if k.Mod&tcell.ModAlt != 0 {
		prefix += "Alt-"
	}
	if k.Mod&tcell.ModShift != 0 {
		prefix += "Shift-"
	}
	if k.Key == tcell.KeyRune {
		if k.Mod&tcell.ModAlt != 0 {
			return prefix + string(unicode.ToUpper(k.Rune))
		}
		return prefix + string(k.Rune)
	}
	if k.Key >= tcell.KeyF1 && k.Key <= tcell.KeyF12 {
		return fmt.Sprintf("%sF%d", prefix, k.Key-tcell.KeyF1+1)
	}
	for _, n := range named {
		if n.key == k.Key {
			return prefix + n.display
		}
	}
	return prefix + tcell.KeyNames[k.Key]
}

// File is the key as keybinds.toml spells it, the spelling Parse reads.
func (k Key) File() string {
	if k.control() {
		s := k.String()
		return "ctrl+" + strings.ToLower(strings.TrimPrefix(s, "Ctrl-"))
	}
	var prefix string
	if k.Mod&tcell.ModCtrl != 0 {
		prefix += "ctrl+"
	}
	if k.Mod&tcell.ModAlt != 0 {
		prefix += "alt+"
	}
	if k.Mod&tcell.ModShift != 0 {
		prefix += "shift+"
	}
	if k.Key == tcell.KeyRune {
		return prefix + string(k.Rune)
	}
	if k.Key >= tcell.KeyF1 && k.Key <= tcell.KeyF12 {
		return fmt.Sprintf("%sf%d", prefix, k.Key-tcell.KeyF1+1)
	}
	for _, n := range named {
		if n.key == k.Key {
			return prefix + n.file
		}
	}
	return prefix + strings.ToLower(tcell.KeyNames[k.Key])
}
