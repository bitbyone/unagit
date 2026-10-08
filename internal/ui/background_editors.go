package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func (a *App) runningEditorsAction(keys string) uiAction {
	return uiAction{name: "Running Editors…", about: "Return to a Neovim left aside, or close it; the editors survive unagit.", keys: keys, rank: 500, run: a.showRunningEditors}
}

func (a *App) showRunningEditors() {
	var rows []session.Record
	var modified []string
	a.loadThen("Reading running editors", func(step func(string)) (string, error) {
		rows, modified = a.readRunningEditors()
		return "", nil
	}, func(string) {
		if len(rows) == 0 {
			a.refreshOpenEditors()
			a.note("no running editors; open Neovim and use Ctrl-Z to put it aside")
			return
		}
		a.drawRunningEditors(rows, modified)
	})
}

// readRunningEditors lists the editors aside, each with whether it holds
// unsaved changes. It asks every editor, so it runs off the event loop.
func (a *App) readRunningEditors() ([]session.Record, []string) {
	rows := a.sessions.Running()
	modified := make([]string, len(rows))
	for i := range rows {
		rows[i].Branch, _ = workspace.WorktreeHead(rows[i].Dir)
		if dirty, err := editors.Modified(rows[i].Launcher, rows[i].Socket); err != nil {
			modified[i] = "?"
		} else if dirty {
			modified[i] = "modified"
		}
	}
	return rows, modified
}

// drawRunningEditors lists the editors aside. It stays open while editors
// are closed from it, one after another, and the user closes it.
func (a *App) drawRunningEditors(rows []session.Record, modified []string) {
	itemsOf := func() []pickItem {
		items := make([]pickItem, len(rows))
		for i, r := range rows {
			items[i] = pickItem{About: r.Dir, Data: r}
		}
		return items
	}
	var picker *livePicker
	width := 100
	label := func(items []pickItem, w int) {
		width = w
		header := labelRunningEditors(items, rows, modified, w)
		if picker != nil {
			picker.setHeader(header)
		}
	}
	put := func() {
		items := itemsOf()
		label(items, width)
		picker.set("Running Editors", items)
	}
	// reread puts the list again once an editor has closed, or come back
	// from being attached to close it: the closed one goes at once, so the
	// next x cannot land on it, and the rest are asked again.
	reread := func(gone session.Record) {
		for i, r := range rows {
			if r.Socket == gone.Socket && r.Dir == gone.Dir {
				rows = append(rows[:i:i], rows[i+1:]...)
				modified = append(modified[:i:i], modified[i+1:]...)
				put()
				break
			}
		}
		go func() {
			next, marks := a.readRunningEditors()
			a.tv.QueueUpdateDraw(func() {
				if !picker.open() {
					return
				}
				rows, modified = next, marks
				put()
			})
		}()
	}
	items := itemsOf()
	header := labelRunningEditors(items, rows, modified, width)
	picker = a.showPickerWith("Running Editors", items, pickerOptions{
		wide: true, explain: true, header: header, enterHint: "attach", relabel: label,
		enterName: "Attach to Editor…", enterAbout: "Return to this Neovim with its files and unsaved changes intact - here, or in a tab, split or window.",
		keys: []pickKey{{keys: "x", hint: "close editor", name: "Close Editor", about: "Close Neovim; with unsaved changes, attach and ask there.", stay: true,
			run: func(it pickItem) { a.closeRunningEditor(it.Data.(session.Record), reread) }}},
	}, func(it pickItem) { a.attachWhere(it.Data.(session.Record)) })
}

// attachWhere brings a Neovim put aside back where the user chooses: this
// terminal, or a tab, split or window of what is here. One in a pane goes
// to that pane, and with nowhere else to go there is nothing to ask.
func (a *App) attachWhere(r session.Record) {
	places := a.editorPlaces("Suspend unagit and bring Neovim back here; unagit returns when it is put aside or closed.")
	if r.Pane != "" || len(places) == 1 {
		a.attachEditor(r)
		return
	}
	a.pickPlace(placeOfAttach, "Attach "+r.Label()+" · where", places, func(place editorPlace) {
		if place.client == nil {
			a.attachEditor(r)
			return
		}
		go func() {
			a.editorMu.Lock()
			defer a.editorMu.Unlock()
			a.reachRunning(r, "", place)
		}()
	})
}

// closeRunningEditor closes a Neovim aside, waiting in the edge of the
// dialog it was closed from, which stays. One with unsaved changes, or too
// busy to answer, is attached instead, so they are dealt with in its own UI.
// closed runs once it is gone or back from being attached.
func (a *App) closeRunningEditor(r session.Record, closed func(session.Record)) {
	attach := false
	a.waitInDialog("Closing "+r.Label(), func() error {
		dirty, err := editors.Modified(r.Launcher, r.Socket)
		if err != nil || dirty {
			attach = true
			return nil
		}
		gone, err := editors.Close(r.Launcher, r.Socket)
		attach = !gone
		if gone {
			a.sessions.Remove(r)
		}
		return err
	}, func(bool) {
		a.refreshOpenEditors()
		if attach {
			a.attachEditorToClose(r, func() { closed(r) })
			return
		}
		a.done("closed nvim: " + r.Label())
		closed(r)
	})
}

// The drawings use only this copy. Checking sockets and panes is done off
// the loop, never by a row's draw function. It is not a job: it runs every
// few seconds for as long as unagit does, and a spinner that never rests
// says nothing. The screen is drawn again only when what is open changed.
func (a *App) refreshOpenEditors() {
	// Before the first editor has ever opened, there is nothing to read or
	// animate. A missing directory after records existed still clears them.
	if _, err := os.Stat(filepath.Join(a.cfg.Dir(), "sessions")); os.IsNotExist(err) && len(a.openDirs) == 0 {
		return
	}
	if a.openReading {
		return
	}
	a.openReading = true
	go func() {
		open := map[string]session.Record{}
		for _, r := range a.sessions.InEditor(editors.Nvim) {
			open[filepath.Clean(r.Dir)] = r
		}
		a.openCount.Store(int64(len(open)))
		apply := func() {
			a.openReading = false
			if reflect.DeepEqual(open, a.openDirs) {
				return
			}
			a.openDirs = open
			a.projectsPane.reload()
			a.mrsPane.reload()
			a.worktreesPane.reload()
		}
		if seen := a.openSeen.Load(); seen != nil && reflect.DeepEqual(open, *seen) {
			a.tv.QueueUpdate(apply)
			return
		}
		a.openSeen.Store(&open)
		a.tv.QueueUpdateDraw(apply)
	}()
}

// editorsLookEvery is how often the sessions directory is looked at; a
// record written or removed - an editor opened, closed or put aside by any
// unagit, a Neovim's socket gone - changes its modification time.
// editorsAskEvery is how often the editors are asked whether they still
// run when nothing on disk changed: a Zellij pane closes without a trace in
// the directory.
const (
	editorsLookEvery = 3 * time.Second
	editorsAskEvery  = 15 * time.Second
)

func (a *App) watchEditors(stop <-chan struct{}) {
	dir := filepath.Join(a.cfg.Dir(), "sessions")
	ticker := time.NewTicker(editorsLookEvery)
	defer ticker.Stop()
	var seen time.Time
	asked := time.Now()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		fi, err := os.Stat(dir)
		if err != nil {
			continue
		}
		changed := !fi.ModTime().Equal(seen)
		due := a.openCount.Load() > 0 && time.Since(asked) >= editorsAskEvery
		if !changed && !due {
			continue
		}
		seen, asked = fi.ModTime(), time.Now()
		a.tv.QueueUpdate(a.refreshOpenEditors)
	}
}

func (a *App) editorMark(dir string) string {
	if r, ok := a.openDirs[filepath.Clean(dir)]; ok {
		return editorGlyph(r.Editor)
	}
	return ""
}

func (a *App) mrEditorMark(disk mrDisk) string {
	if mark := a.editorMark(disk.BranchDir); mark != "" {
		return mark
	}
	return a.editorMark(disk.ReviewDir)
}

// editorGlyph is the mark of a directory open in an editor: the editor's
// own icon when the theme has one, the plain mark otherwise.
func editorGlyph(id string) string {
	if id == editors.Nvim && glyphEditorNeovim != "" {
		return glyphEditorNeovim
	}
	return glyphEditor
}

func labelRunningEditors(items []pickItem, rows []session.Record, modified []string, width int) string {
	names, branches, paths := []int{}, []int{}, []string{}
	ageWidth := 3
	for _, r := range rows {
		names = append(names, cells(r.Project))
		branches = append(branches, cells(editorContext(r)))
		paths = append(paths, r.Dir)
		ageWidth = max(ageWidth, cells(humanAge(r.Since)))
	}
	repo := flexColumn("REPOSITORY", names, 12, 1.4)
	branch := flexColumn("BRANCH / MR", branches, 11, 1)
	path := gistColumn("DIRECTORY", paths, 9, 1)
	age := fixedColumn(ageWidth)
	edits := fixedColumn(5)
	layoutColumns(width-4, repo, branch, path, age, edits)
	table := make([][]string, len(rows))
	for i, r := range rows {
		mark := modified[i]
		if mark == "modified" {
			mark = glyphEdits
		}
		table[i] = []string{esc(shortenRepo(r.Project, repo.width)), esc(shortenBranch(editorContext(r), branch.width)), esc(shortPath(r.Dir, path.width)), humanAge(r.Since), mark}
	}
	header, labels := pickTable([]string{"REPOSITORY", "BRANCH / MR", "DIRECTORY", "AGE", "EDITS"}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	return header
}

func editorContext(r session.Record) string {
	if r.IID > 0 {
		return fmt.Sprintf("!%d · %s", r.IID, r.Mode)
	}
	return r.Branch
}

// openMark is one mark of something open in a row's directory: Neovim, or
// an agent unagit started there, each in a colour of its own.
type openMark struct {
	glyph  string
	colour tcell.Color
}

// openMarks are what is open in dir that unagit can tell still runs:
// Neovim, then every agent it started there. An agent waiting for an
// answer is in the warning colour and one at work in the accent, so it is
// seen in the lists and not only on the Agents tab. A window editor is not
// among them: nothing says when its window closes.
func (a *App) openMarks(dir string) []openMark {
	if dir == "" {
		return nil
	}
	dir = filepath.Clean(dir)
	var marks []openMark
	if r, ok := a.openDirs[dir]; ok {
		marks = append(marks, openMark{editorGlyph(r.Editor), role("mark.editor")})
	}
	for _, r := range a.agentRows {
		if r.Status == "ended" || filepath.Clean(r.Dir) != dir {
			continue
		}
		glyph := agentIcons[r.Kind]
		if glyph == "" {
			glyph = glyphAgent
		}
		colour := role("mark.agent")
		switch r.Status {
		case "blocked":
			colour = role("mark.agent_waiting")
		case "working":
			colour = role("mark.agent_working")
		}
		marks = append(marks, openMark{glyph, colour})
	}
	return marks
}

// mrOpenMarks are what is open in a merge request's worktrees, its branch's
// and its review's.
func (a *App) mrOpenMarks(disk mrDisk) []openMark {
	return append(a.openMarks(disk.BranchDir), a.openMarks(disk.ReviewDir)...)
}

// marksWidth is how many cells marks take, a space between each.
func marksWidth(marks []openMark) int {
	w := 0
	for i, m := range marks {
		if i > 0 {
			w++
		}
		w += cells(m.glyph)
	}
	return w
}

// marksMarkup draws marks in their colours, ink giving the colour each is
// drawn in.
func marksMarkup(marks []openMark, ink func(tcell.Color) string) string {
	var b strings.Builder
	for i, m := range marks {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("[" + ink(m.colour) + "]" + esc(m.glyph))
	}
	if len(marks) > 0 {
		b.WriteString("[-:-]")
	}
	return b.String()
}

// Marks keep the same place in every list, and take no room when nobody
// has one. They stand first in a row, so the column starts with the row's
// leading space; it is as wide as the most marks a row has.
func editorColumn(rows []int, hidden bool, marks func(int) []openMark) *listColumn {
	width := 0
	if !hidden {
		for _, idx := range rows {
			width = max(width, marksWidth(marks(idx)))
		}
	}
	if width == 0 {
		return &listColumn{}
	}
	return fixedColumn(1 + width)
}

func editorField(marks []openMark, width int) field {
	markup := marksMarkup(marks, func(c tcell.Color) string { return c.String() })
	return field{raw: " " + markup + strings.Repeat(" ", max(0, width-1-marksWidth(marks)))}
}

// A selection repaints the glyphs in the band's ink. Keep their colours
// after the table, as the tags do, with enough contrast on that band. x is
// where the marks' field starts; the glyphs are after its leading space.
func keepEditorMark(p *pane, row, x int, marks []openMark, marked bool) {
	if len(marks) == 0 {
		return
	}
	style := styleSelected
	if marked {
		style = styleMarkedSelected
	}
	_, bg, _ := style.Decompose()
	markup := marksMarkup(marks, func(c tcell.Color) string {
		return legibleOn([]tcell.Color{c}, bg, colText)[0].String() + ":" + bg.String()
	})
	p.kept.keep(row, keptMarkup{x: x + 1, markup: markup, width: marksWidth(marks)})
}
