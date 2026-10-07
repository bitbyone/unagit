package ui

import (
	"fmt"
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
	cmd, err := ed.Command(dir)
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
