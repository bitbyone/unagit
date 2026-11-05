package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/keymap"
)

// Settings › Keys is where keybinds.toml is found: where it is, what in it
// is wrong, and a file to start from that names every action there is.

func (s *settingsView) keysPath() string {
	return filepath.Join(s.app.cfg.Dir(), keymap.FileName)
}

func (s *settingsView) newKeysPane() *tview.TextView {
	v := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	v.SetTextColor(colText)
	box(v.Box, "Keys").SetBorderPadding(1, 1, 2, 2)
	v.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		if ev.Key() != tcell.KeyRune {
			return ev
		}
		switch ev.Rune() {
		case 'w':
			s.writeKeysTemplate()
		case 'e':
			s.editKeys()
		case 'r':
			s.readKeysAgain()
		case 'u':
			s.updateKeys()
		default:
			return ev
		}
		return nil
	})
	return v
}

func (s *settingsView) fillKeys() {
	path := s.keysPath()
	label := func(name string) string { return fmt.Sprintf("%s%-9s%s", tag(colDim), name, tagEnd) }
	var b strings.Builder
	b.WriteString(label("File") + tview.Escape(tildePath(path)) + "\n")
	_, problems, err := CheckKeybinds(s.app.cfg.Dir())
	switch _, statErr := os.Stat(path); {
	case errors.Is(statErr, fs.ErrNotExist):
		b.WriteString(label("State") + "none yet: every key is unagit's own\n")
	case err != nil:
		b.WriteString(label("State") + tag(colWarn) + tview.Escape(err.Error()) + tagEnd + "\n")
	case len(problems) > 0:
		b.WriteString(label("State") + tag(colWarn) + fmt.Sprintf("%d lines keep unagit's own keys:", len(problems)) + tagEnd + "\n")
		for _, p := range problems {
			b.WriteString("         " + tview.Escape(p) + "\n")
		}
	default:
		b.WriteString(label("State") + "read, nothing wrong in it\n")
	}
	if ch, err := bringKeybindsUp(s.app.cfg.Dir(), false); err == nil && ch.Rewritten {
		b.WriteString(label("Release") + tag(colWarn) + changesSaid(ch) + tagEnd + " since it was written; u brings it up to date\n")
	}
	b.WriteString("\nEvery action of a list, the worktree view, the changes, the log and " +
		"the branches can have keys of your own. w writes the file with every " +
		"action in it, commented out: uncomment a line and change its key. " +
		"What stays commented keeps unagit's key, and u rewrites those lines " +
		"to a new release's keys and adds its new actions, leaving what you " +
		"uncommented and your notes alone. r reads the file again after you change it.")
	s.keys.SetText(b.String())
}

// keybindsTemplate is the reference with every binding commented out: the
// file a user starts from, naming every action, changing none. keymap.Update
// brings a file written from it up to a later release's.
func keybindsTemplate() string {
	var b strings.Builder
	b.WriteString(`# unagit's keys: every action, with the key it has, commented out.
#
# Uncomment a line and change its key to make it yours: one key as
# "ctrl+e", several as ["ctrl+e", "f4"], none as []. Keys are written
# ctrl+e, alt+o, alt+enter, ctrl+space, f5, shift+f5, alt+pageup, P.
#
# A line left commented keeps unagit's own key. Settings > Keys > u, or
# unagit keys --update, rewrites the commented lines to a new release's keys
# and adds its new actions; what you uncommented and your own notes stay.
#
# [global] reaches an action on every screen that has it, and a screen's
# own section wins over it. A key you give an action is taken from whatever
# had it there. unagit keys --check says what is wrong in the file.
`)
	body := keybindsReference[strings.Index(keybindsReference, "\n\n")+1:]
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "#") {
			line = "# " + line
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// writeKeybindsTemplate writes the template to a file that is not there.
func writeKeybindsTemplate(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(keybindsTemplate())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// bringKeybindsUp brings the file in a configuration directory up to this
// release's template; write false only says what it would change.
func bringKeybindsUp(dir string, write bool) (keymap.Changes, error) {
	path := filepath.Join(dir, keymap.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return keymap.Changes{}, err
	}
	text, ch := keymap.Update(string(data), keybindsTemplate())
	if write && ch.Rewritten {
		if _, problems, err := keymap.Read([]byte(text), keyRegistry()); err != nil {
			return ch, fmt.Errorf("the file would not read after the update, so it is left as it was: %v", err)
		} else if _, before, _ := keymap.Read(data, keyRegistry()); len(problems) > len(before) {
			return ch, fmt.Errorf("the update would leave more wrong in it, so it is left as it was")
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return ch, err
		}
	}
	return ch, nil
}

// UpdateKeybinds is unagit keys --update: the file brought up to this
// release, or written from the template when there is none, said in a line.
func UpdateKeybinds(dir string) (string, error) {
	path := filepath.Join(dir, keymap.FileName)
	err := writeKeybindsTemplate(path)
	if err == nil {
		return "written " + path + ": every action, commented out", nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	ch, err := bringKeybindsUp(dir, true)
	if err != nil {
		return "", fmt.Errorf("%s: %v", path, err)
	}
	if !ch.Rewritten {
		return path + ": up to date", nil
	}
	return "updated " + path + ": " + changesSaid(ch), nil
}

// changesSaid is what an update changes, in words.
func changesSaid(ch keymap.Changes) string {
	var parts []string
	plural := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	plural(ch.Added, "new action", "new actions")
	plural(ch.Changed, "key unagit changed", "keys unagit changed")
	plural(ch.Removed, "action gone", "actions gone")
	if len(parts) == 0 && ch.Rewritten {
		return "newer descriptions"
	}
	return strings.Join(parts, ", ")
}

// writeKeysTemplate starts keybinds.toml; a file that is there already is
// the user's, and is never written over.
func (s *settingsView) writeKeysTemplate() {
	a, path := s.app, s.keysPath()
	err := writeKeybindsTemplate(path)
	if errors.Is(err, fs.ErrExist) {
		a.flash(tildePath(path) + " is there already, and it is yours - u brings it up to date, e opens it")
		return
	}
	if err != nil {
		a.errorf("cannot write %s: %v", tildePath(path), err)
		return
	}
	s.fillKeys()
	a.done("Written " + tildePath(path) + ": every action, commented out")
}

// editKeys opens the file, writing it first when there is none yet.
func (s *settingsView) editKeys() {
	a, path := s.app, s.keysPath()
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		s.writeKeysTemplate()
		if _, err := os.Stat(path); err != nil {
			return
		}
	}
	a.withEditor(false, func(ed *editors.Editor) { go a.openFile(path, ed) })
}

// readKeysAgain puts the file's keys on at once: every key and hint asks
// for them as it is pressed or drawn, so nothing needs a new start.
func (s *settingsView) readKeysAgain() {
	a := s.app
	a.keysProblem = ""
	a.loadKeys()
	s.fillKeys()
	if a.keysProblem != "" {
		a.flash(a.keysProblem)
		return
	}
	if a.keys == nil {
		a.done("No " + keymap.FileName + ": every key is unagit's own")
		return
	}
	a.done("Read " + tildePath(s.keysPath()))
}

// updateKeys rewrites the file to this release's actions and keys, and puts
// it on.
func (s *settingsView) updateKeys() {
	a, path := s.app, s.keysPath()
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		a.flash("there is no " + keymap.FileName + " yet - w writes it")
		return
	}
	ch, err := bringKeybindsUp(a.cfg.Dir(), true)
	if err != nil {
		a.errorf("cannot update %s: %v", tildePath(path), err)
		return
	}
	if !ch.Rewritten {
		s.fillKeys()
		a.done(tildePath(path) + " is up to date")
		return
	}
	a.keysProblem = ""
	a.loadKeys()
	s.fillKeys()
	if a.keysProblem != "" {
		a.flash(a.keysProblem)
		return
	}
	a.done("Updated " + tildePath(path) + ": " + changesSaid(ch))
}
