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
	b.WriteString("\nEvery action of a list, the worktree view, the changes, the log and " +
		"the branches can have keys of your own. w writes the file with every " +
		"action in it, commented out: uncomment a line and change its key, and " +
		"what you leave commented keeps unagit's key, so a new release's keys " +
		"still reach you. r reads the file again after you change it.")
	s.keys.SetText(b.String())
}

// keybindsTemplate is the reference with every binding commented out: the
// file a user starts from, naming every action, changing none.
func keybindsTemplate() string {
	var b strings.Builder
	b.WriteString(`# unagit's keys. Every action is here with the key it has, commented
# out: uncomment a line and change the key to make it yours. A line left
# commented keeps unagit's own key, and follows it when a release changes
# it.
#
#   review = "ctrl+v"           # one key
#   open = ["ctrl+e", "f4"]     # several
#   toggle_draft = []           # none
#
# [global] reaches an action in every section that has it, and a section's
# own line wins over it. A key given to an action is taken from whatever had
# it there. Keys are written ctrl+e, alt+o, alt+enter, ctrl+space, f5,
# shift+f5, alt+pageup, P. Settings keeps its keys. unagit keys --check
# says what is wrong in the file; unagit keys prints every action again,
# with the actions a newer release added.
`)
	body := keybindsReference[strings.Index(keybindsReference, "\n["):]
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "#") {
			line = "# " + line
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// writeKeysTemplate starts keybinds.toml; a file that is there already is
// the user's, and is never written over.
func (s *settingsView) writeKeysTemplate() {
	a, path := s.app, s.keysPath()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		a.flash(tildePath(path) + " is there already, and it is yours - e opens it; unagit keys prints every action")
		return
	}
	if err == nil {
		_, err = f.WriteString(keybindsTemplate())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
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
