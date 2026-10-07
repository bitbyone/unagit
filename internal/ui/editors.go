package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// detectEditors lists the editors unagit knows and which of them are here.
func (a *App) detectEditors() []editors.Editor {
	return editors.Detect(editors.CustomSpec{
		Command:  a.cfg.Editor,
		Args:     a.cfg.EditorArgs,
		Terminal: !a.cfg.EditorWindow,
	})
}

// withEditor runs then with the editor to open in. Without ask, and with a
// favourite that is installed, that is nil: the favourite, looked up when the
// editor is actually started. Otherwise - Alt with an opening key, or no
// usable favourite - one is chosen first from the editors this machine has,
// the favourite on top. There is no quiet fallback to some other editor.
func (a *App) withEditor(ask bool, then func(ed *editors.Editor)) {
	a.withEditorKind(ask, false, then)
}

func (a *App) withEditorKind(ask, terminalOnly bool, then func(ed *editors.Editor)) {
	all := a.detectEditors()
	if terminalOnly {
		filtered := make([]editors.Editor, 0, len(all))
		for _, ed := range all {
			if ed.Terminal {
				filtered = append(filtered, ed)
			}
		}
		all = filtered
	}
	fav, hasFav := editors.Favourite(all, a.cfg.FavouriteEditor)
	if !ask && hasFav {
		then(nil)
		return
	}
	var items []pickItem
	if hasFav {
		items = append(items, pickItem{Label: glyphFavourite + " " + fav.Name, Sub: kindOf(fav), Data: fav})
	}
	for _, e := range all {
		if e.Found && (!hasFav || e.ID != fav.ID) {
			items = append(items, pickItem{Label: "  " + e.Name, Sub: kindOf(e), Data: e})
		}
	}
	if len(items) == 0 {
		kind := "editor"
		if terminalOnly {
			kind = "terminal editor"
		}
		a.errorf("no %s found - install one, or set a custom editor in Settings › General", kind)
		return
	}
	title := "Open with"
	switch chosen := a.cfg.FavouriteEditor; {
	case terminalOnly:
		title += " · terminal editors"
	case hasFav, chosen == askEveryTime:
	case chosen == "":
		title += " · no favourite yet: f in Settings › Integrations › Editors"
	default:
		title += " · the favourite, " + chosen + ", is not installed"
	}
	a.showPicker(title, items, func(it pickItem) {
		ed := it.Data.(editors.Editor)
		then(&ed)
	})
}

func kindOf(e editors.Editor) string {
	if e.Terminal {
		return "in this terminal"
	}
	return "in its own window"
}

// openNow opens a directory already on disk as it is: no fetch, no pull, no
// task log in between. Where it stands is in the lists, and p updates it
// first when that is wanted.
func (a *App) openNow(dir string, what session.Record, ed *editors.Editor) {
	go a.openEditor(dir, what, ed)
}

// openEditor gives a terminal editor the terminal. Neovim's record follows
// its server, so detaching its UI does not lose the directory or the buffers.
func (a *App) openEditor(dir string, what session.Record, ed *editors.Editor) {
	a.openEditorAt(dir, what, ed, "")
}

func (a *App) openEditorAt(dir string, what session.Record, ed *editors.Editor, file string) {
	a.openEditorIn(dir, what, ed, file, editorPlace{})
}

func (a *App) openEditorIn(dir string, what session.Record, ed *editors.Editor, file string, place editorPlace) {
	a.editorMu.Lock()
	defer a.editorMu.Unlock()
	if ed == nil {
		fav, ok := editors.Favourite(a.detectEditors(), a.cfg.FavouriteEditor)
		if !ok || place.client != nil && !fav.Terminal {
			a.tv.QueueUpdateDraw(func() {
				a.closeModal(pageTask)
				a.withEditorKind(true, place.client != nil, func(chosen *editors.Editor) { go a.openEditorIn(dir, what, chosen, file, place) })
			})
			return
		}
		ed = &fav
	}
	a.tv.QueueUpdateDraw(func() { a.closeModal(pageTask) })
	what.Dir, what.Editor = dir, ed.ID
	what.Branch, _ = workspace.WorktreeHead(dir)
	if place.client != nil {
		a.openMuxEditor(dir, what, *ed, place)
		return
	}
	if !ed.Terminal {
		a.zoxideAdd(dir)
		a.openWindowEditorAt(dir, what, *ed, file)
		return
	}
	if ed.ID == editors.Nvim {
		for _, running := range a.sessions.Running() {
			if sameDirectory(running.Dir, dir) {
				if file != "" {
					if err := editors.OpenFile(running.Launcher, running.Socket, file); err != nil {
						a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
						return
					}
				}
				a.attachEditorLocked(running, false)
				return
			}
		}
	}
	var cmd *exec.Cmd
	var err error
	if ed.CanDetach() {
		what.Launcher = ed.Where
		what.Socket, err = a.sessions.NewSocket()
		if err == nil {
			cmd, err = ed.BackgroundCommandAt(dir, what.Socket, file)
		}
	} else {
		cmd, err = ed.CommandAt(dir, file)
	}
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	if err := a.runInTerminal(dir, what, cmd); err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
		return
	}
	a.editorReturned(what)
}

// runInTerminal records the original directory for shell jumps while a
// program owns the terminal. A detachable editor keeps its own record.
func (a *App) runInTerminal(dir string, what session.Record, cmd *exec.Cmd) error {
	what.Dir = dir
	close, err := a.sessions.Add(what)
	if err != nil && what.Socket != "" {
		return fmt.Errorf("cannot record the editor: %w", err)
	}
	a.zoxideAdd(dir)
	err = a.runTerminalEditor(cmd, editors.Editor{Name: what.Editor})
	if what.Socket == "" {
		close()
	}
	a.refreshAfterTerminal(what)
	return err
}

func sameDirectory(left, right string) bool {
	// A symlink to the clone is still the same place, and must not get a
	// second Neovim fighting over its swap files.
	if real, err := filepath.EvalSymlinks(left); err == nil {
		left = real
	}
	if real, err := filepath.EvalSymlinks(right); err == nil {
		right = real
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func (a *App) attachEditor(r session.Record) {
	go func() {
		a.editorMu.Lock()
		defer a.editorMu.Unlock()
		a.attachEditorLocked(r, false)
	}()
}

func (a *App) attachEditorLocked(r session.Record, confirmClose bool) {
	if !editors.SocketAlive(r.Socket) {
		a.sessions.Remove(r)
		a.tv.QueueUpdateDraw(func() { a.refreshOpenEditors(); a.flash("editor has closed; open the directory again") })
		return
	}
	if !confirmClose {
		a.zoxideAdd(r.Dir)
	}
	cmd := editors.AttachCommand(r.Launcher, r.Socket, r.Dir)
	if confirmClose {
		a.runTerminalEditorConfirm(cmd, r)
	} else {
		a.runTerminalEditor(cmd, editors.Editor{Name: "Neovim"})
	}
	a.refreshAfterTerminal(r)
	a.editorReturned(r)
}

func (a *App) runTerminalEditor(cmd *exec.Cmd, ed editors.Editor) error {
	return a.runTerminalEditorWith(cmd, ed, nil)
}

func (a *App) runTerminalEditorWith(cmd *exec.Cmd, ed editors.Editor, started func()) error {
	var runErr error
	a.tv.Suspend(func() {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		err := cmd.Start()
		if err == nil {
			if started != nil {
				started()
			}
			err = cmd.Wait()
		}
		runErr = err
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s failed: %v\n", ed.Name, err)
			fmt.Fprintln(os.Stderr, "press enter to return to unagit")
			var s string
			fmt.Scanln(&s)
		}
	})
	return runErr
}

func (a *App) refreshAfterTerminal(r session.Record) {
	if r.Socket != "" && !editors.SocketAlive(r.Socket) {
		a.sessions.Remove(r)
	}
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
	})
}

func (a *App) editorReturned(r session.Record) {
	aside := editors.SocketAlive(r.Socket)
	a.tv.QueueUpdateDraw(func() {
		if aside {
			a.note("nvim aside: " + r.Label() + " · E lists the running editors")
		} else {
			a.done("opened " + r.Dir)
		}
	})
}

func (a *App) openWindowEditor(dir string, what session.Record, ed editors.Editor) {
	a.openWindowEditorAt(dir, what, ed, "")
}

func (a *App) openWindowEditorAt(dir string, what session.Record, ed editors.Editor, file string) {
	cmd, err := ed.CommandAt(dir, file)
	var out bytes.Buffer
	if err == nil {
		cmd.Stdout, cmd.Stderr = &out, &out
		err = cmd.Start()
	}
	if err != nil {
		a.tv.QueueUpdateDraw(func() { a.errorf("%s did not start: %v", ed.Name, err) })
		return
	}
	a.keepWindowSession(a.sessions.Open(what))
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		a.done(fmt.Sprintf("opened in %s: %s", ed.Name, dir))
	})
	// The launcher returns once the window is asked for; a failure there is
	// the only thing left to report.
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		a.tv.QueueUpdateDraw(func() { a.errorf("%s: %s", ed.Name, msg) })
	}
}

// windowSessions are the records of window editors, taken back when unagit
// exits.
var windowSessions struct {
	sync.Mutex
	close []func()
}

func (a *App) keepWindowSession(close func()) {
	windowSessions.Lock()
	defer windowSessions.Unlock()
	windowSessions.close = append(windowSessions.close, close)
}

func closeWindowSessions() {
	windowSessions.Lock()
	defer windowSessions.Unlock()
	for _, close := range windowSessions.close {
		close()
	}
	windowSessions.close = nil
}

func (a *App) attachEditorToClose(r session.Record) {
	go func() {
		a.editorMu.Lock()
		defer a.editorMu.Unlock()
		a.attachEditorLocked(r, true)
	}()
}

func (a *App) runTerminalEditorConfirm(cmd *exec.Cmd, r session.Record) {
	count, _ := editors.RemoteExpr(r.Launcher, r.Socket, "len(nvim_list_uis())")
	finished := make(chan struct{})
	defer close(finished)
	a.runTerminalEditorWith(cmd, editors.Editor{Name: "Neovim"}, func() {
		go editors.ConfirmCloseOnAttach(r.Launcher, r.Socket, count, finished)
	})
}
