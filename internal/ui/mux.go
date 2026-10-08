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
	// Neovim listens in a pane as it does in unagit's own terminal, Ctrl-Z
	// and all: put aside, its pane closes and the server runs on, for E to
	// list and any terminal or pane to attach to. One too old to put aside
	// still listens, so a file can be opened in it and it can be closed.
	// Without a socket - a configuration directory whose path leaves no room
	// for one - it opens all the same; its pane still says it is there.
	var cmd *exec.Cmd
	var err error
	if ed.ID == editors.Nvim && ed.Found {
		if socket, sockErr := a.sessions.NewSocket(); sockErr == nil {
			what.Socket = socket
			if ed.CanDetach() {
				cmd, err = ed.BackgroundCommandAt(dir, socket, "")
			} else {
				cmd, err = ed.ServerCommandAt(dir, socket, "")
			}
		}
	}
	if cmd == nil && err == nil {
		cmd, err = ed.Command(dir)
	}
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	what.Launcher = ed.Where
	a.openInPane(what, place, cmd, "opened in Zellij: ")
}

// openInPane runs cmd in a new tab or split and records the editor as being
// there. It serves a new editor and one put aside coming back alike.
func (a *App) openInPane(what session.Record, place editorPlace, cmd *exec.Cmd, said string) {
	var job *bgJob
	a.tv.QueueUpdateDraw(func() { job = a.startJob("Opening editor in Zellij") })
	defer a.tv.QueueUpdateDraw(func() { a.endJob(job) })
	pane, err := place.client.Open(place.where, what.Dir, muxTabName(what), cmd)
	if err != nil && pane == "" {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	openErr := err
	what.Mux, what.MuxSession, what.MuxLauncher = place.client.Kind, place.client.Session, place.client.Binary
	what.Pane = pane
	_, err = a.sessions.Add(what)
	a.zoxideAdd(what.Dir)
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		if err != nil {
			a.errorf("editor opened in zellij, but cannot record its session: %v; check the configuration directory permissions", err)
		} else if openErr != nil {
			a.errorf("editor opened in zellij, but could not focus its pane: %v", openErr)
		} else {
			a.done(said + what.Label())
		}
	})
}

// attachInPane brings a Neovim put aside back in a tab or split of its own.
// Its old record goes - it named no pane, or one that is gone - and the new
// one names the pane it is in now.
func (a *App) attachInPane(r session.Record, place editorPlace) {
	_ = editors.Checktime(r.Launcher, r.Socket)
	a.sessions.Remove(r)
	a.openInPane(r, place, editors.AttachCommand(r.Launcher, r.Socket, r.Dir), "attached in Zellij: ")
}

// reachRunning takes an open of a directory to the Neovim that already runs
// there, rather than starting a second one to fight over its swap files: in
// its pane when it has one this unagit can bring forward, in the tab or
// split asked for, or in this terminal. It runs holding editorMu.
func (a *App) reachRunning(r session.Record, file string, place editorPlace) {
	if file != "" && r.Socket != "" {
		if err := editors.OpenFile(r.Launcher, r.Socket, file); err != nil {
			a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
			return
		}
	}
	switch {
	case r.Pane != "" && (a.inSessionOf(r) || r.Socket == ""):
		a.goToPane(r, false)
	case r.Pane != "":
		a.askAboutPane(r, place)
	case place.client != nil:
		a.attachInPane(r, place)
	default:
		a.attachEditorLocked(r, false)
	}
}

func (a *App) inSessionOf(r session.Record) bool {
	c := a.multiplexer
	return c != nil && c.Kind == r.Mux && c.Session == r.MuxSession
}

// askAboutPane is for a Neovim in a pane of another Zellij session, or open
// while unagit runs outside Zellij: nothing here can bring that pane
// forward. It can be attached here as well, or - when the pane is its only
// window - taken over: put aside there, as Ctrl-Z would, and opened here.
// With two windows Neovim would put aside whichever was used last, so
// taking over is not offered then.
func (a *App) askAboutPane(r session.Record, place editorPlace) {
	windows, err := editors.UIs(r.Launcher, r.Socket)
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	where := "this terminal"
	if place.client != nil {
		where = "a new tab or split"
	}
	items := []pickItem{{Label: "Attach Here Too", Data: "attach",
		About: "Open the same Neovim in this terminal as well. Both windows show the same files, cursor and mode, sized to the smaller; Ctrl-Z here leaves the pane as it is."}}
	if windows == 1 {
		items = append(items, pickItem{Label: "Take Over", Data: "take",
			About: "Put the Neovim in the pane aside, as Ctrl-Z there would - the pane closes - and open it in " + where + ", unsaved changes and all."})
	}
	here := r
	here.Pane, here.Mux, here.MuxSession, here.MuxLauncher = "", "", "", ""
	a.tv.QueueUpdateDraw(func() {
		title := fmt.Sprintf("%s · Neovim in Zellij session %s", r.Label(), r.MuxSession)
		a.showPickerWith(title, items, pickerOptions{pack: true, explain: true}, func(it pickItem) {
			go func() {
				a.editorMu.Lock()
				defer a.editorMu.Unlock()
				if it.Data == "attach" {
					a.attachEditorLocked(here, false)
					return
				}
				if err := editors.DetachUI(r.Launcher, r.Socket); err != nil {
					a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
					return
				}
				if place.client != nil {
					a.attachInPane(here, place)
					return
				}
				// The record still names the closed pane until it is read
				// again; it is the server's either way.
				a.attachEditorLocked(here, false)
			}()
		})
	})
}

// goToPane brings forward the pane a Neovim runs in, from inside the same
// Zellij session. From anywhere else it says where the editor is: an open
// asks first (askAboutPane), and closing one with unsaved changes is
// asked about there, where they can be seen.
func (a *App) goToPane(r session.Record, closing bool) {
	if !a.inSessionOf(r) {
		a.tv.QueueUpdateDraw(func() {
			a.flash(fmt.Sprintf("%s is open in Neovim in Zellij session %s - go there, or close it from E", r.Label(), r.MuxSession))
		})
		return
	}
	if r.Socket != "" {
		_ = editors.Checktime(r.Launcher, r.Socket)
	}
	if err := a.multiplexer.Focus(r.Pane); err != nil {
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
