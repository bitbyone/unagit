package keymap

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

// TestKeysReadBothWays: a key read from the file comes out in the hints'
// spelling and back, and matches the event a terminal sends for it.
func TestKeysReadBothWays(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		file, display string
		ev            *tcell.EventKey
	}{
		{"ctrl+e", "Ctrl-E", tcell.NewEventKey(tcell.KeyCtrlE, 0, tcell.ModCtrl)},
		{"CTRL+E", "Ctrl-E", tcell.NewEventKey(tcell.KeyCtrlE, 0, tcell.ModCtrl)},
		{"ctrl+space", "Ctrl-Space", tcell.NewEventKey(tcell.KeyCtrlSpace, 0, tcell.ModCtrl)},
		{"ctrl+\\", "Ctrl-\\", tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, tcell.ModCtrl)},
		{"alt+o", "Alt-O", tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModAlt)},
		{"alt+O", "", nil},
		{"option+enter", "Alt-Enter", tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt)},
		{"P", "P", tcell.NewEventKey(tcell.KeyRune, 'P', tcell.ModNone)},
		{"shift+p", "P", tcell.NewEventKey(tcell.KeyRune, 'P', tcell.ModNone)},
		{"+", "+", tcell.NewEventKey(tcell.KeyRune, '+', tcell.ModNone)},
		{"alt++", "Alt-+", tcell.NewEventKey(tcell.KeyRune, '+', tcell.ModAlt)},
		{"f5", "F5", tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone)},
		{"shift+f5", "Shift-F5", tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModShift)},
		{"alt+pageup", "Alt-PgUp", tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModAlt)},
		{"delete", "Delete", tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone)},
	} {
		k, err := Parse(c.file)
		if c.display == "" {
			if err == nil {
				t.Errorf("%s: read as %s, want refused", c.file, k)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if got := k.String(); got != c.display {
			t.Errorf("%s shows as %q, want %q", c.file, got, c.display)
		}
		back, ok := Display(c.display)
		if !ok || back != k {
			t.Errorf("%s does not read back as %s: %v %v", c.display, c.file, back, ok)
		}
		if again, err := Parse(k.File()); err != nil || again != k {
			t.Errorf("%s written as %s does not read back: %v", c.file, k.File(), err)
		}
		if !k.Matches(c.ev) {
			t.Errorf("%s does not match its own key", c.file)
		}
	}
}

// TestKeysThatCannotBeTold: a key a terminal sends as another, or one every
// list moves with, is refused with the reason, never taken quietly.
func TestKeysThatCannotBeTold(t *testing.T) {
	t.Parallel()
	for spell, why := range map[string]string{
		"ctrl+h":       "Backspace",
		"ctrl+i":       "Tab",
		"ctrl+m":       "Enter",
		"ctrl+c":       "ends unagit",
		"ctrl+shift+o": "ctrl alone",
		"ctrl+alt+o":   "ctrl alone",
		"ctrl+1":       "sends no",
		"alt+shift+o":  "without case",
		"j":            "every list",
		"?":            "every list",
		"space":        "ctrl+space",
		"tab":          "every list",
		"enter":        "alt+enter",
		"ctrl+enter":   "alt+enter",
		"pageup":       "modifier",
		"cmd+o":        "never reaches",
		"hyper+o":      "not a modifier",
		"f13":          "f1 to f12",
		"backtick":     "not a key",
		"":             "empty",
	} {
		if _, err := Parse(spell); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want a refusal saying %q", spell, err, why)
		}
	}
}

// TestAnAltKeyIsMatchedWithoutCase: as the keys always were, since whether
// Shift reaches Alt differs by terminal; a plain letter keeps its case.
func TestAnAltKeyIsMatchedWithoutCase(t *testing.T) {
	t.Parallel()
	alt, _ := Parse("alt+o")
	if !alt.Matches(tcell.NewEventKey(tcell.KeyRune, 'O', tcell.ModAlt)) {
		t.Error("alt+o does not match Alt-Shift-O")
	}
	if alt.Matches(tcell.NewEventKey(tcell.KeyRune, 'o', tcell.ModNone)) {
		t.Error("alt+o matches a plain o")
	}
	p, _ := Parse("p")
	if p.Matches(tcell.NewEventKey(tcell.KeyRune, 'P', tcell.ModNone)) || p.Matches(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModAlt)) {
		t.Error("p matches P or Alt-P")
	}
	enter, _ := Parse("alt+enter")
	if enter.Matches(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)) {
		t.Error("alt+enter matches a plain Enter")
	}
}

func registry() Registry {
	k := func(s string) []Key {
		key, ok := Display(s)
		if !ok {
			panic(s)
		}
		return []Key{key}
	}
	return Registry{
		Global:  {SelectionActions: append(k("Alt-Enter"), k("Ctrl-A")...), ScreenActions: k(":"), "find_repository": k("+")},
		"repos": {"open": k("Ctrl-O"), "open_with": k("Alt-O"), "edit_tags": k("Ctrl-T")},
		"mrs":   {"open": k("Ctrl-O"), "review": k("Ctrl-R")},
	}
}

func keysOf(m *Map, section, id string) string {
	keys, ok := m.Keys(section, id)
	if !ok {
		return "default"
	}
	var out []string
	for _, k := range keys {
		out = append(out, k.File())
	}
	return "[" + strings.Join(out, " ") + "]"
}

// TestASectionWinsOverGlobal: global reaches an action in every section, a
// section's own binding wins there, and an empty list takes the key away.
func TestASectionWinsOverGlobal(t *testing.T) {
	t.Parallel()
	m, problems, err := Read([]byte(`
[global]
open = "ctrl+e"
actions = ["ctrl+space", "alt+enter"]

[mrs]
open = ["f5", "ctrl+y"]
review = []
`), registry())
	if err != nil || len(problems) > 0 {
		t.Fatalf("%v %v", err, problems)
	}
	for _, c := range []struct{ section, id, want string }{
		{"repos", "open", "[ctrl+e]"},
		{"mrs", "open", "[f5 ctrl+y]"},
		{"mrs", "review", "[]"},
		{"repos", "edit_tags", "default"},
		{Global, SelectionActions, "[ctrl+space alt+enter]"},
		{"repos", SelectionActions, "[ctrl+space alt+enter]"},
	} {
		if got := keysOf(m, c.section, c.id); got != c.want {
			t.Errorf("[%s] %s: %s, want %s", c.section, c.id, got, c.want)
		}
	}
}

// TestASlipLeavesTheRestWorking: whatever is wrong is said, that binding is
// left out, and every other one of the file still holds.
func TestASlipLeavesTheRestWorking(t *testing.T) {
	t.Parallel()
	m, problems, err := Read([]byte(`
stray = "x"

[nowhere]
open = "ctrl+e"

[repos]
open = "ctrl+e"
edit_tags = "ctrl+e"
open_with = "alt+enter"
reveiw = "f5"

[mrs]
review = ["ctrl+h", "f6"]
open = 5
`), registry())
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(problems, "\n")
	for _, want := range []string{
		"stray: a binding goes in a section",
		"[nowhere] is not a section",
		"[repos] reveiw: no action has that name",
		"[repos] open_with: alt+enter opens the actions",
		"[mrs] review: ctrl+h is Backspace",
		"[mrs] open: a key is written in quotes",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no problem says %q:\n%s", want, all)
		}
	}
	for _, c := range []struct{ section, id, want string }{
		{"repos", "edit_tags", "[ctrl+e]"},
		{"repos", "open", "[ctrl+e]"},
		{"repos", "open_with", "[]"},
		{"mrs", "review", "[f6]"},
		{"mrs", "open", "default"},
	} {
		if got := keysOf(m, c.section, c.id); got != c.want {
			t.Errorf("[%s] %s: %s, want %s", c.section, c.id, got, c.want)
		}
	}

	if _, _, err := Read([]byte("[repos\nopen = "), registry()); err == nil || !strings.Contains(err.Error(), "not TOML") {
		t.Errorf("a broken file: %v", err)
	}
}

// TestNoFileIsTheDefaults: without a file, or with a nil map, every action
// keeps the key it has.
func TestNoFileIsTheDefaults(t *testing.T) {
	t.Parallel()
	var m *Map
	if _, ok := m.Keys("repos", "open"); ok {
		t.Error("a nil map chose a key")
	}
	m, problems, err := Read(nil, registry())
	if err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	if _, ok := m.Keys("repos", "open"); ok {
		t.Error("an empty file chose a key")
	}
}
