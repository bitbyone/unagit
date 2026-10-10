package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// The shelf, as IntelliJ has it: changes put aside under a name, to come
// back to - git's stash underneath (gitx/shelf.go). Shelve Changes… puts
// aside what is not committed, or the files marked in the Changes dialog;
// Shelf… lists what is there, each to look at, unshelve into the checkout
// it was opened from, or delete - which Rewrite History can undo.

// shelfScope is the checkout the shelf is opened from: what is shelved
// comes from it, and what is unshelved goes into it.
type shelfScope struct {
	project forge.Project
	dir     string
	branch  string
	done    string
}

func (s shelfScope) label() string {
	if s.branch == "" {
		return s.project.PathWithNamespace
	}
	return s.project.PathWithNamespace + " (" + s.branch + ")"
}

// labelShelfName is the label of the shelve form's name.
const labelShelfName = "Name"

// shelveChanges asks for a name and puts paths aside - every change when
// none are given - then goes on with after.
func (a *App) shelveChanges(s shelfScope, paths []string, after func()) {
	form := tview.NewForm()
	styleForm(form)
	name := "Changes on " + s.branch
	if s.branch == "" {
		name = "Changes"
	}
	form.AddInputField(labelShelfName, name+" · "+time.Now().Format("Jan 2 15:04"), 0, nil, nil)
	what := "every file not committed, unversioned ones too"
	if len(paths) > 0 {
		what = counted(len(paths), "file", "files") + " marked"
	}
	form.AddTextView("Shelves", what, 0, 1, true, false)
	form.AddButton("Shelve", func() {
		text := strings.TrimSpace(form.GetFormItemByLabel(labelShelfName).(*tview.InputField).GetText())
		if text == "" {
			a.flash("enter a name for the shelf")
			return
		}
		a.closeModal(pageForm)
		git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
		go func() {
			err := git.Shelve(s.dir, text, paths)
			a.tv.QueueUpdateDraw(func() {
				switch {
				case errors.Is(err, gitx.ErrNothingToShelve):
					a.note(err.Error())
					return
				case err != nil:
					a.errorf("shelving: %v", err)
					return
				}
				a.afterGitChange()
				if after != nil {
					after()
				}
				a.done("shelved as " + text + " - Shelf… brings it back")
			})
		}()
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized("Shelve Changes · "+s.label(), form, 72, 9)
}

// shelfRow is a shelf as the list has it, with what it holds.
type shelfRow struct {
	gitx.Shelf
	files []gitx.FileStat
}

// showShelf reads the shelf off the loop and lists it, newest first.
func (a *App) showShelf(s shelfScope) {
	if s.dir == "" {
		a.flash(s.project.PathWithNamespace + " is not cloned - it has no shelf here")
		return
	}
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		shelves, err := git.Shelves(s.dir)
		rows := make([]shelfRow, len(shelves))
		for i, sh := range shelves {
			rows[i] = shelfRow{Shelf: sh}
			rows[i].files, _ = git.ShelfFiles(s.dir, sh.SHA)
		}
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("reading the shelf: %v", err)
				return
			}
			a.listShelf(s, rows)
		})
	}()
}

// listShelf shows the shelf.
func (a *App) listShelf(s shelfScope, rows []shelfRow) {
	if len(rows) == 0 {
		if back := a.peekBack(); back != nil {
			back()
		}
		a.note("the shelf of " + s.project.PathWithNamespace + " is empty - Shelve Changes… puts what is not committed there")
		return
	}
	items := make([]pickItem, len(rows))
	table := make([][]string, len(rows))
	for i, r := range rows {
		added, deleted := 0, 0
		for _, f := range r.files {
			added += f.Added
			deleted += f.Deleted
		}
		table[i] = []string{
			tag(role("activity.when")) + esc(eventTime(r.At)) + tagEnd,
			tag(role("activity.what")) + esc(trim(r.Name, 48)) + tagEnd,
			tag(role("column.branch")) + esc(r.Branch) + tagEnd,
			tag(colMuted) + counted(len(r.files), "file", "files") + tagEnd + " " +
				tag(role("files.lines_added")) + fmt.Sprintf("+%d", added) + tagEnd + " " +
				tag(role("files.lines_deleted")) + fmt.Sprintf("-%d", deleted) + tagEnd,
		}
		paths := make([]string, len(r.files))
		for j, f := range r.files {
			paths[j] = f.Path
		}
		items[i] = pickItem{About: r.At.Format("Mon 2006-01-02 15:04") + " · " + strings.Join(paths, ", "), Data: r}
	}
	header, labels := pickTable([]string{"WHEN", "NAME", "FROM", "CHANGES"}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	at := func(it pickItem) shelfRow { return it.Data.(shelfRow) }
	again := a.backOr(func() { a.showShelf(shelfScope{project: s.project, dir: s.dir, branch: s.branch}) })
	a.showPickerWith("Shelf · "+s.label(), items, pickerOptions{
		wide: true, explain: true, header: header, enterHint: "details",
		enterName: "Show Details", enterAbout: "What the shelf holds: each file with the lines it adds and deletes.",
		keys: []pickKey{
			{keys: "u", hint: "unshelve", name: "Unshelve", about: "Put the changes into this checkout and take them off the shelf; refused, nothing changes.",
				run: func(it pickItem) { a.unshelve(s, at(it), false, again) }},
			{keys: "a", hint: "apply", name: "Apply and Keep", about: "Put the changes into this checkout and keep them on the shelf, for another checkout too.",
				run: func(it pickItem) { a.unshelve(s, at(it), true, again) }},
			{keys: "D", hint: "diff", name: "Show Diff in Hunk", about: "The shelf's changes in Hunk.",
				run: func(it pickItem) { a.diffShelf(s, at(it), again) }},
			{keys: "d", hint: "delete", name: "Delete…", about: "Take it off the shelf, after asking; Rewrite History puts it back.",
				run: func(it pickItem) { a.deleteShelf(s, at(it), again) }},
		},
	}, func(it pickItem) { a.showShelfDetail(at(it), again) })
	if s.done != "" {
		a.done(s.done)
	}
}

// showShelfDetail is a shelf whole: its name, where it came from, its files.
func (a *App) showShelfDetail(r shelfRow, back func()) {
	d := &detailBuf{}
	d.title(r.Name)
	d.blank()
	d.kv("From", esc(r.Branch))
	d.kv("When", esc(r.At.Format("Mon 2006-01-02 15:04")+" · "+humanAge(r.At)))
	d.kv("Shelf", esc(r.Ref+" · "+r.SHA))
	files := make([]forge.FileChange, len(r.files))
	for i, f := range r.files {
		files[i] = forge.FileChange{Path: f.Path, Added: f.Added, Deleted: f.Deleted, Binary: f.Binary}
	}
	view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetScrollable(true)
	view.SetText(d.String() + "\n" + filesSection(files))
	box(view.Box, "Shelf · "+trim(r.Name, 40))
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q' || ev.Rune() == 'h' {
			a.closeModal(pageCommit)
			back()
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageCommit, modalPct(view, 70, 70), true, true)
	a.tv.SetFocus(view)
}

// diffShelf shows a shelf's changes in Hunk.
func (a *App) diffShelf(s shelfScope, r shelfRow, back func()) {
	bin, ok := a.hunkBinary()
	if !ok {
		back()
		return
	}
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		patch, err := git.ShelfPatch(s.dir, r.SHA)
		if err != nil {
			a.tv.QueueUpdateDraw(func() {
				back()
				a.errorf("reading the shelf: %v", err)
			})
			return
		}
		a.runHunkPatch(bin, s.dir, patch)
		a.tv.QueueUpdateDraw(back)
	}()
}

// unshelve puts a shelf's changes into the checkout, off the shelf unless
// keep; refused, it says why and the shelf comes back.
func (a *App) unshelve(s shelfScope, r shelfRow, keep bool, back func()) {
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		err := git.Unshelve(s.dir, r.SHA, keep)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				back()
				a.flash(firstLine(err.Error()))
				return
			}
			a.afterGitChange()
			word := "unshelved " + r.Name
			if keep {
				word = "applied " + r.Name + " - it stays on the shelf"
			}
			a.done(word)
		})
	}()
}

// deleteShelf takes a shelf off the shelf, after asking.
func (a *App) deleteShelf(s shelfScope, r shelfRow, back func()) {
	body := fmt.Sprintf("Delete the shelf [::b]%s[::-] - %s?\n\nRewrite History can put it back.",
		esc(r.Name), counted(len(r.files), "file", "files"))
	a.confirmChoicesBack("Delete shelf", body, nil, []choice{{"Delete", func() {
		since := time.Now()
		git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
		go func() {
			_, err := git.DeleteShelf(s.dir, r.SHA)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("deleting the shelf: %v", err)
					return
				}
				a.logRewrites(s.project, s.dir, since)
				next := s
				next.done = "deleted shelf " + r.Name
				a.showShelf(next)
			})
		}()
	}}}, back)
}

// worktreeShelf is the shelf scope of a worktree.
func (a *App) worktreeShelf(r worktreeRow) shelfScope {
	branch := r.Branch
	if branch == "(detached)" {
		branch = ""
	}
	return shelfScope{project: a.worktreeProject(r), dir: r.Dir, branch: branch}
}

// cloneShelf is the shelf scope of a clone.
func (a *App) cloneShelf(pr forge.Project) shelfScope {
	branch := a.diskOf(pr.Instance, pr.PathWithNamespace).Branch
	if strings.HasPrefix(branch, "@") {
		branch = ""
	}
	return shelfScope{project: pr, dir: a.projectDir(pr.Instance, pr.PathWithNamespace), branch: branch}
}

// shelveFromChanges shelves the files marked in the Changes dialog, or
// every file when none is.
func (a *App) shelveFromChanges(v *changesView) {
	if len(v.changes) == 0 {
		a.flash("nothing to shelve: every file is as the last commit has it")
		return
	}
	var paths []string
	for _, c := range v.markedChanges() {
		paths = append(paths, c.Path)
		if c.From != "" {
			paths = append(paths, c.From)
		}
	}
	pr, ok := a.projByKey[projectKey{v.place.instance, v.place.path}]
	if !ok {
		pr = forge.Project{Instance: v.place.instance, PathWithNamespace: v.place.path}
	}
	branch := a.pathManager(v.place.instance, v.place.path).Git().CurrentBranch(v.place.dir)
	a.shelveChanges(shelfScope{project: pr, dir: v.place.dir, branch: branch}, paths, a.reloadChanges)
}
