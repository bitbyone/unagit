package ui

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
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
	all := a.detectEditors()
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
		a.errorf("no editor found - install one, or set a custom editor in Settings › General")
		return
	}
	title := "Open with"
	switch chosen := a.cfg.FavouriteEditor; {
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

// openEditor opens dir in the editor. A terminal editor gets the terminal:
// the TUI is suspended until it exits, and its directory is on record for
// exactly that long, so another terminal can find its way there. A window
// editor is only started - its launcher returns while the editor runs on - so
// its record stays until unagit exits, the longest anyone can vouch for it.
func (a *App) openEditor(dir string, what session.Record, ed *editors.Editor) {
	if ed == nil {
		fav, ok := editors.Favourite(a.detectEditors(), a.cfg.FavouriteEditor)
		if !ok {
			// The favourite went away while the task ran, or a task that was
			// not expected to open anything did: ask now, then carry on.
			a.tv.QueueUpdateDraw(func() {
				a.closeModal(pageTask)
				a.withEditor(true, func(chosen *editors.Editor) { go a.openEditor(dir, what, chosen) })
			})
			return
		}
		ed = &fav
	}
	a.tv.QueueUpdateDraw(func() { a.closeModal(pageTask) })
	what.Dir = dir
	if !ed.Terminal {
		a.openWindowEditor(dir, what, *ed)
		return
	}
	defer a.sessions.Open(what)()
	a.tv.Suspend(func() {
		// Nothing is printed on the way: it would pile up in the terminal's
		// scrollback and be all there is to see once unagit quits. Where
		// things are open is what unagit sessions and unagit cd are for.
		cmd, err := ed.Command(dir)
		if err == nil {
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			err = cmd.Run()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s failed: %v\n", ed.Name, err)
			fmt.Fprintln(os.Stderr, "press enter to return to unagit")
			var s string
			fmt.Scanln(&s)
		}
	})
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		a.done("opened " + dir)
	})
}

func (a *App) openWindowEditor(dir string, what session.Record, ed editors.Editor) {
	cmd, err := ed.Command(dir)
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
