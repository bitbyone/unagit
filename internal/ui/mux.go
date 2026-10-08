package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/session"
)

// The zero place keeps all existing opening keys in this terminal.
type editorPlace struct {
	client *mux.Client
	where  mux.Placement
}

func (a *App) muxActions(open func(*editors.Editor, editorPlace)) []uiAction {
	var actions []uiAction
	for _, spec := range []struct {
		name, about string
		where       mux.Placement
	}{
		{"Open in New Tab", "Open the favourite terminal editor in a named Zellij tab.", mux.Tab},
		{"Open in Vertical Split", "Open the favourite terminal editor beside unagit in Zellij.", mux.Vertical},
		{"Open in Horizontal Split", "Open the favourite terminal editor below unagit in Zellij.", mux.Horizontal},
	} {
		actions = append(actions, uiAction{name: spec.name, about: spec.about, rank: 19, when: func() bool { return a.multiplexer != nil }, run: func() {
			if a.multiplexer == nil {
				a.flash("run unagit inside Zellij to open a tab or split")
				return
			}
			place := editorPlace{a.multiplexer, spec.where}
			a.withEditorKind(false, true, func(ed *editors.Editor) { open(ed, place) })
		}})
	}
	return actions
}

func (a *App) openNowIn(dir string, what session.Record, ed *editors.Editor, place editorPlace) {
	go a.openEditorIn(dir, what, ed, "", place)
}

func (a *App) runTaskOpeningIn(title string, what session.Record, ed *editors.Editor, place editorPlace, fn func(func(string)) (string, error)) {
	if place.client == nil {
		a.runTaskOpening(title, what, ed, fn)
		return
	}
	a.runTaskThen(title, fn, func(dir string) { a.openNowIn(dir, what, ed, place) })
}

func muxTabName(what session.Record) string {
	name := filepath.Base(what.Project)
	if what.IID > 0 {
		return fmt.Sprintf("%s !%d", name, what.IID)
	}
	if what.Mode != session.ModeRepository && what.Mode != session.ModeGroup && what.Branch != "" {
		name += " · " + what.Branch
	}
	return name
}

func (a *App) openMuxEditor(dir string, what session.Record, ed editors.Editor, place editorPlace) {
	if !ed.Terminal {
		a.tv.QueueUpdateDraw(func() { a.flash("choose a terminal editor for a Zellij tab or split") })
		return
	}
	// Neovim listens, without the Ctrl-Z of one put aside - its pane is
	// where it waits - so that unagit can still open a file in it, ask
	// about unsaved changes and close it.
	// Without a socket - a configuration directory whose path leaves no room
	// for one - it opens all the same; its pane still says it is there.
	var cmd *exec.Cmd
	var err error
	if ed.ID == editors.Nvim && ed.Found {
		if socket, sockErr := a.sessions.NewSocket(); sockErr == nil {
			what.Socket = socket
			cmd, err = ed.ServerCommandAt(dir, socket, "")
		}
	}
	if cmd == nil && err == nil {
		cmd, err = ed.Command(dir)
	}
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	var job *bgJob
	a.tv.QueueUpdateDraw(func() { job = a.startJob("Opening editor in Zellij") })
	defer a.tv.QueueUpdateDraw(func() { a.endJob(job) })
	pane, err := place.client.Open(place.where, dir, muxTabName(what), cmd)
	if err != nil && pane == "" {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	openErr := err
	what.Launcher = ed.Where
	what.Mux, what.MuxSession, what.MuxLauncher = place.client.Kind, place.client.Session, place.client.Binary
	what.Pane = pane
	_, err = a.sessions.Add(what)
	a.zoxideAdd(dir)
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		if err != nil {
			a.errorf("editor opened in zellij, but cannot record its session: %v; check the configuration directory permissions", err)
		} else if openErr != nil {
			a.errorf("editor opened in zellij, but could not focus its pane: %v", openErr)
		} else {
			a.done("opened in Zellij: " + what.Label())
		}
	})
}

// goToPane brings forward the pane a Neovim runs in. It can only be done
// from inside the same Zellij session; from anywhere else the way there is
// the user's, and a second Neovim is not the answer. Closing one with
// unsaved changes is asked about there, where they can be seen.
func (a *App) goToPane(r session.Record, closing bool) {
	c := a.multiplexer
	if c == nil || c.Kind != r.Mux || c.Session != r.MuxSession {
		a.tv.QueueUpdateDraw(func() {
			a.flash(fmt.Sprintf("%s is open in Neovim in Zellij session %s - go there, or close it from E", r.Label(), r.MuxSession))
		})
		return
	}
	if r.Socket != "" {
		_ = editors.Checktime(r.Launcher, r.Socket)
	}
	if err := c.Focus(r.Pane); err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	a.zoxideAdd(r.Dir)
	a.tv.QueueUpdateDraw(func() {
		if closing {
			a.flash(r.Label() + " has unsaved changes - save or discard them in its pane")
		} else {
			a.done("went to the Neovim of " + r.Label())
		}
	})
}
