package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
		rows = a.sessions.Running()
		for i := range rows {
			rows[i].Branch, _ = workspace.WorktreeHead(rows[i].Dir)
			mark := ""
			if dirty, err := editors.Modified(rows[i].Launcher, rows[i].Socket); err != nil {
				mark = "?"
			} else if dirty {
				mark = "modified"
			}
			modified = append(modified, mark)
		}
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

func (a *App) drawRunningEditors(rows []session.Record, modified []string) {
	items := make([]pickItem, len(rows))
	for i, r := range rows {
		items[i] = pickItem{About: r.Dir, Data: r}
	}
	var picker *livePicker
	label := func(items []pickItem, width int) {
		header := labelRunningEditors(items, rows, modified, width)
		if picker != nil {
			picker.setHeader(header)
		}
	}
	header := labelRunningEditors(items, rows, modified, 100)
	picker = a.showPickerWith("Running Editors", items, pickerOptions{
		wide: true, explain: true, header: header, enterHint: "attach", relabel: label,
		enterName: "Attach to Editor", enterAbout: "Return to this Neovim with its files and unsaved changes intact.",
		keys: []pickKey{{keys: "x", hint: "close editor", name: "Close Editor", about: "Close Neovim; with unsaved changes, attach and ask there.", run: func(it pickItem) { a.closeRunningEditor(it.Data.(session.Record)) }}},
	}, func(it pickItem) { a.attachEditor(it.Data.(session.Record)) })
}

func (a *App) closeRunningEditor(r session.Record) {
	attach := false
	a.loadThen("Closing editor", func(step func(string)) (string, error) {
		dirty, err := editors.Modified(r.Launcher, r.Socket)
		if err != nil {
			// A prompt or a busy editor is best dealt with in its own UI.
			attach = true
			return "", nil
		}
		if dirty {
			attach = true
			return "", nil
		}
		closed, err := editors.Close(r.Launcher, r.Socket)
		attach = !closed
		if closed {
			a.sessions.Remove(r)
		}
		return "", err
	}, func(string) {
		a.refreshOpenEditors()
		if attach {
			a.attachEditorToClose(r)
		} else {
			a.done("closed nvim: " + r.Label())
		}
	})
}

// The drawings use only this copy. Checking sockets belongs to a job, never
// to a row's draw function or the event loop.
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
	j := a.startJob("reading editors")
	go func() {
		open := map[string]session.Record{}
		for _, r := range a.sessions.InEditor(editors.Nvim) {
			open[filepath.Clean(r.Dir)] = r
		}
		a.tv.QueueUpdateDraw(func() {
			a.openReading = false
			a.endJob(j)
			if reflect.DeepEqual(open, a.openDirs) {
				return
			}
			a.openDirs = open
			a.projectsPane.reload()
			a.mrsPane.reload()
			a.worktreesPane.reload()
		})
	}()
}

func (a *App) watchEditors(stop <-chan struct{}) {
	dir := filepath.Join(a.cfg.Dir(), "sessions")
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			a.tv.QueueUpdateDraw(a.refreshOpenEditors)
		}
	}
}

func (a *App) editorMark(dir string) string {
	if _, ok := a.openDirs[filepath.Clean(dir)]; ok {
		return glyphEditor
	}
	return ""
}

func (a *App) mrEditorMark(disk mrDisk) string {
	if a.editorMark(disk.BranchDir) != "" || a.editorMark(disk.ReviewDir) != "" {
		return glyphEditor
	}
	return ""
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

// Marks keep the same place and width in every list, and take no room when
// nobody has one. The second cell leaves room for a future watched mark.
func editorColumn(rows []int, hidden bool, mark func(int) string) *listColumn {
	if !hidden {
		for _, idx := range rows {
			if mark(idx) != "" {
				return fixedColumn(2)
			}
		}
	}
	return &listColumn{}
}

func editorField(mark string, width int) field {
	return field{text: mark, width: width, colour: role("mark.editor")}
}

// A selection repaints the glyph in the band's ink. Keep its theme colour
// after the table, as the tags do, with enough contrast on that band.
func keepEditorMark(p *pane, row, x int, mark string, marked bool) {
	if mark == "" {
		return
	}
	style := styleSelected
	if marked {
		style = styleMarkedSelected
	}
	_, bg, _ := style.Decompose()
	ink := legibleOn([]tcell.Color{role("mark.editor")}, bg, colText)[0]
	markup := "[" + ink.String() + ":" + bg.String() + "]" + esc(mark) + "[-:-]"
	p.kept.keep(row, keptMarkup{x: x, markup: markup, width: cells(mark)})
}
