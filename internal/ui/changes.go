package ui

import (
	"fmt"
	"path"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/workspace"
)

// The Changes dialog is IntelliJ's commit window: the files not committed
// on the left, in two groups - Changes, the versioned ones, and
// Unversioned Files - and on the right the diff of the one under the
// cursor, drawn as the cursor comes to it. Files are picked for a commit
// with space (the versioned ones start picked, the unversioned not), and c
// opens the commit dialog with them. A file's name is coloured by what
// happened to it; no letter says it. Rollback and Delete act on the picked
// files when the cursor is on one of them, on the file under the cursor
// otherwise.

const pageChanges = "changes"

// changesPlace is the working tree whose changes are listed.
type changesPlace struct {
	instance, path, dir string
	title               string
}

// changesRow is a line of the list: a group's heading, or a file.
type changesRow struct {
	group  int // 0 the versioned files, 1 the unversioned
	change *gitx.Change
}

// The groups, by what IntelliJ calls them.
var changesGroups = [2]string{"Changes", "Unversioned Files"}

type changesView struct {
	place   changesPlace
	changes []gitx.Change
	picked  map[string]bool
	folded  [2]bool
	rows    []changesRow
	table   *tview.Table
	diff    *tview.TextView
	hint    *tview.TextView
	frame   *tview.Flex
	// diffs keeps what git said of each file, so going back to one is
	// instant; shown is the file in the diff pane, and reading counts the
	// reads so a slow answer cannot overwrite a newer one.
	diffs   map[string]string
	shown   string
	reading int
	loaded  bool
	// inDiff is whether the diff pane has the focus. It is noted by the
	// panes' focus functions rather than asked of them: a TextView answers
	// HasFocus under the lock its Focus holds while it calls them, and
	// asking from there hung unagit for good.
	inDiff bool
}

// changesKey is how a file is known in the picks and the diffs.
func changesKey(c gitx.Change) string { return c.Path }

// groupOf is the group a file is listed in.
func groupOf(c gitx.Change) int {
	if c.Versioned() {
		return 0
	}
	return 1
}

// showProjectChanges opens the Changes dialog on a clone.
func (a *App) showProjectChanges(pr forge.Project) {
	if !a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.flash(pr.PathWithNamespace + " is not cloned - there are no changes of yours")
		return
	}
	a.showChanges(changesPlace{instance: pr.Instance, path: pr.PathWithNamespace,
		dir: a.projectDir(pr.Instance, pr.PathWithNamespace), title: pr.PathWithNamespace})
}

// showWorktreeChanges opens it on a worktree; a group has changes per
// repository, each from its block in the worktree view.
func (a *App) showWorktreeChanges(r worktreeRow) {
	if r.grouped() {
		a.flash("a group has its changes per repository - open its view with Enter and light one")
		return
	}
	a.showChanges(changesPlace{instance: r.Instance, path: r.Path, dir: r.Dir, title: r.Path + " · " + r.Branch})
}

// showMRChanges opens it on a merge request's branch worktree; its review
// worktree holds the merge request's changes, which are not yours.
func (a *App) showMRChanges(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	dir := a.mrDir(mr.Instance, project.PathWithNamespace, mr.IID, mr.SourceBranch)
	if !workspace.Exists(dir) {
		a.flash(fmt.Sprintf("!%d has no branch worktree - Ctrl-O makes one; a review's changes are the merge request's", mr.IID))
		return
	}
	a.showChanges(changesPlace{instance: mr.Instance, path: project.PathWithNamespace, dir: dir,
		title: fmt.Sprintf("%s !%d · %s", project.PathWithNamespace, mr.IID, mr.SourceBranch)})
}

// showChanges builds the dialog and reads the changes into it.
func (a *App) showChanges(place changesPlace) {
	v := &changesView{place: place, picked: map[string]bool{}, diffs: map[string]string{}}
	v.table = tview.NewTable().SetSelectable(true, false)
	v.table.SetSelectedStyle(styleSelected)
	box(v.table.Box, "Files")
	v.diff = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	v.diff.SetTextColor(colText)
	box(v.diff.Box, "")
	v.hint = tview.NewTextView().SetDynamicColors(true).SetTextColor(colDim)
	v.hint.SetWrap(true).SetWordWrap(true)
	panes := tview.NewFlex().
		AddItem(v.table, 30, 0, true).
		AddItem(v.diff, 0, 1, false)
	v.frame = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(panes, 0, 1, true).
		AddItem(v.hint, 1, 0, false)
	box(v.frame.Box, "Changes · "+place.title)
	// A Flex leaves what is under it: the inside is cleared, and the hint
	// made as tall as it needs at the width it got.
	v.frame.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		for row := y + 1; row < y+h-1; row++ {
			for col := x + 1; col < x+w-1; col++ {
				screen.SetContent(col, row, ' ', nil, baseStyle())
			}
		}
		width := max(1, w-4)
		v.frame.ResizeItem(v.hint, max(1, len(tview.WordWrap(v.hint.GetText(false), width))), 0)
		// The list as wide as its longest row, up to two fifths: the diff
		// is what is read.
		panes.ResizeItem(v.table, max(min(v.listWidth()+2, width*2/5), min(30, width/2)), 0)
		return x + 2, y + 1, width, max(0, h-2)
	})
	v.table.SetSelectionChangedFunc(func(int, int) { a.showChangeDiff(v) })
	v.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey { return a.changesListKeys(v, ev) })
	v.diff.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey { return a.changesDiffKeys(v, ev) })
	v.table.SetFocusFunc(func() { v.inDiff = false; a.changesFocus(v) })
	v.diff.SetFocusFunc(func() { v.inDiff = true; a.changesFocus(v) })
	a.changes = v
	a.pages.AddPage(pageChanges, modalPct(v.frame, 94, 92), true, true)
	a.tv.SetFocus(v.table)
	a.renderChanges(v)
	a.readChanges(v, true)
}

// closeChanges leaves the dialog.
func (a *App) closeChanges() {
	a.changes = nil
	a.closeModal(pageChanges)
}

// reloadChanges reads the dialog's changes again, after a commit or a
// rollback, keeping what is picked of what is still there.
func (a *App) reloadChanges() {
	if v := a.changes; v != nil {
		v.diffs = map[string]string{}
		a.readChanges(v, false)
	}
}

// readChanges asks git what is not committed, off the event loop. The
// first read picks every versioned file.
func (a *App) readChanges(v *changesView, first bool) {
	git := a.pathManager(v.place.instance, v.place.path).Git()
	go func() {
		changes, err := git.Changes(v.place.dir)
		a.tv.QueueUpdateDraw(func() {
			if a.changes != v {
				return
			}
			if err != nil {
				a.errorf("reading the changes of %s: %v", v.place.title, err)
				return
			}
			picked := map[string]bool{}
			for _, c := range changes {
				if first && c.Versioned() || v.picked[changesKey(c)] {
					picked[changesKey(c)] = true
				}
			}
			v.changes, v.picked, v.loaded = changes, picked, true
			a.renderChanges(v)
		})
	}()
}

// renderChanges lays the list out again, the cursor kept on its file.
func (a *App) renderChanges(v *changesView) {
	keep := ""
	if c := v.cursor(); c != nil {
		keep = changesKey(*c)
	}
	keepGroup := -1
	if r, ok := v.row(); ok && r.change == nil {
		keepGroup = r.group
	}
	v.rows = nil
	counts := [2]int{}
	for _, c := range v.changes {
		counts[groupOf(c)]++
	}
	v.table.Clear()
	for g := range changesGroups {
		if counts[g] == 0 {
			continue
		}
		v.rows = append(v.rows, changesRow{group: g})
		if v.folded[g] {
			continue
		}
		for i := range v.changes {
			if groupOf(v.changes[i]) == g {
				v.rows = append(v.rows, changesRow{group: g, change: &v.changes[i]})
			}
		}
	}
	// Where nothing is kept, the cursor starts on the first file.
	at := min(1, max(len(v.rows)-1, 0))
	for i, r := range v.rows {
		v.table.SetCell(i, 0, tview.NewTableCell(v.rowText(r, counts[r.group])).SetExpansion(1))
		switch {
		case r.change != nil && changesKey(*r.change) == keep:
			at = i
		case r.change == nil && r.group == keepGroup:
			at = i
		}
	}
	if len(v.rows) == 0 {
		word := "reading what is not committed …"
		if v.loaded {
			word = "nothing to commit: every file is as the last commit has it"
		}
		v.table.SetCell(0, 0, tview.NewTableCell(tag(colDim)+esc(word)+tagEnd).SetSelectable(false))
	}
	v.table.Select(at, 0)
	picked, total := 0, len(v.changes)
	for _, c := range v.changes {
		if v.picked[changesKey(c)] {
			picked++
		}
	}
	v.table.SetTitle(fmt.Sprintf(" %d of %d picked ", picked, total))
	a.showChangeDiff(v)
	a.changesFocus(v)
}

// rowText is a row as it is drawn: a group's fold, pick and count, or a
// file's pick, its name in the colour of what happened to it, and its
// folder after it, quieter.
func (v *changesView) rowText(r changesRow, count int) string {
	if r.change == nil {
		fold := glyphUnfolded
		if v.folded[r.group] {
			fold = glyphFolded
		}
		pick := glyphUnpicked
		if v.groupPicked(r.group) {
			pick = glyphPicked
		}
		return esc(fold+" "+pick+" ") + "[::b]" + esc(changesGroups[r.group]) + "[::-]" +
			tag(colDim) + esc(fmt.Sprintf("  %d file%s", count, plural(count, "", "s"))) + tagEnd
	}
	c := *r.change
	pick := glyphUnpicked
	if v.picked[changesKey(c)] {
		pick = glyphPicked
	}
	dir, name := path.Split(c.Path)
	text := "  " + esc(pick) + " " + tag(role(fileRole(c.Kind))) + esc(name) + tagEnd
	if dir = strings.TrimSuffix(dir, "/"); dir != "" {
		text += "  " + tag(role("files.folder")) + esc(dir) + tagEnd
	}
	if c.From != "" {
		text += "  " + tag(role("files.folder")) + esc("← "+c.From) + tagEnd
	}
	return text
}

// fileRole is the colour of a file's name, by what happened to it.
func fileRole(kind gitx.ChangeKind) string {
	switch kind {
	case gitx.Added:
		return "files.added"
	case gitx.Deleted:
		return "files.deleted"
	case gitx.Renamed:
		return "files.renamed"
	case gitx.Conflicted:
		return "files.conflicted"
	case gitx.Unversioned:
		return "files.unversioned"
	}
	return "files.changed"
}

// listWidth is how wide the longest row is drawn.
func (v *changesView) listWidth() int {
	w := 0
	for i := range v.rows {
		w = max(w, tview.TaggedStringWidth(v.table.GetCell(i, 0).Text))
	}
	return w
}

// row is the row under the cursor.
func (v *changesView) row() (changesRow, bool) {
	at, _ := v.table.GetSelection()
	if at < 0 || at >= len(v.rows) {
		return changesRow{}, false
	}
	return v.rows[at], true
}

// cursor is the file under the cursor, nil on a group's heading.
func (v *changesView) cursor() *gitx.Change {
	if r, ok := v.row(); ok {
		return r.change
	}
	return nil
}

// groupPicked reports whether every file of a group is picked.
func (v *changesView) groupPicked(group int) bool {
	any := false
	for _, c := range v.changes {
		if groupOf(c) == group {
			if !v.picked[changesKey(c)] {
				return false
			}
			any = true
		}
	}
	return any
}

// pickedChanges are the files picked, in the list's order.
func (v *changesView) pickedChanges() []gitx.Change {
	var out []gitx.Change
	for _, c := range v.changes {
		if v.picked[changesKey(c)] {
			out = append(out, c)
		}
	}
	return out
}

// chosen is what an action acts on: the picked files of a kind when the
// cursor is on one of them or on a group, the file under the cursor
// otherwise.
func (v *changesView) chosen(versioned bool) []gitx.Change {
	r, ok := v.row()
	if !ok {
		return nil
	}
	if r.change != nil && !v.picked[changesKey(*r.change)] {
		if r.change.Versioned() == versioned {
			return []gitx.Change{*r.change}
		}
		return nil
	}
	var out []gitx.Change
	for _, c := range v.pickedChanges() {
		if c.Versioned() == versioned && (r.change != nil || groupOf(c) == r.group) {
			out = append(out, c)
		}
	}
	return out
}

// showChangeDiff draws the diff of the file under the cursor, reading it
// first when it is not known yet.
func (a *App) showChangeDiff(v *changesView) {
	c := v.cursor()
	if c == nil {
		v.shown = ""
		v.diff.SetTitle("")
		v.diff.SetText("")
		return
	}
	key := changesKey(*c)
	v.diff.SetTitle(" " + c.Path + " ")
	if diff, ok := v.diffs[key]; ok {
		if v.shown != key {
			v.shown = key
			v.diff.SetText(drawDiff(c.Path, diff, diffWidth))
			v.diff.ScrollToBeginning()
		}
		return
	}
	v.shown = ""
	v.diff.SetText(tag(colDim) + "reading the diff …" + tagEnd)
	v.reading++
	ticket, change := v.reading, *c
	git := a.pathManager(v.place.instance, v.place.path).Git()
	go func() {
		diff, err := git.ChangeDiff(v.place.dir, change)
		if err != nil && diff == "" {
			diff = err.Error()
		}
		a.tv.QueueUpdateDraw(func() {
			if a.changes != v {
				return
			}
			v.diffs[key] = diff
			if ticket == v.reading {
				a.showChangeDiff(v)
			}
		})
	}()
}

// diffWidth is how far a line's fill reaches: past any terminal, since the
// pane does not wrap and cuts what is beyond its edge.
const diffWidth = 400

// changesFocus marks the pane with the focus and says its keys. It runs
// inside a pane's Focus, so it touches only what takes no lock of the
// panes: their borders, and the hint.
func (a *App) changesFocus(v *changesView) {
	for _, pane := range []*tview.Box{v.table.Box, v.diff.Box} {
		pane.SetBorderColor(colBorder).SetTitleColor(colTitle)
	}
	if v.inDiff {
		v.diff.SetBorderColor(colBorderFocus).SetTitleColor(colBorderFocus)
		v.hint.SetText(litHint("c commit"))
		return
	}
	v.table.SetBorderColor(colBorderFocus).SetTitleColor(colBorderFocus)
	v.hint.SetText(litHint("space pick · a all · c commit · u roll back · d delete"))
}

// changesListKeys answers the list's keys: folding, the panes, then the
// actions.
func (a *App) changesListKeys(v *changesView, ev *tcell.EventKey) *tcell.EventKey {
	r, ok := v.row()
	switch {
	case ev.Key() == tcell.KeyEsc:
		a.closeChanges()
		return nil
	case ev.Key() == tcell.KeyTab:
		a.tv.SetFocus(v.diff)
		return nil
	case ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && (ev.Rune() == 'l' || ev.Rune() == 'h'):
		if !ok {
			return nil
		}
		// h and l are the tree's while it has a use for them - a group to
		// fold or unfold, a file's group to go up to - and otherwise l goes
		// on to the diff, as h and l go between panels everywhere.
		enter := ev.Key() == tcell.KeyEnter
		right := !enter && ev.Rune() == 'l'
		left := !enter && ev.Rune() == 'h'
		switch {
		case r.change == nil && (enter || right && v.folded[r.group] || left && !v.folded[r.group]):
			v.folded[r.group] = !v.folded[r.group]
			a.renderChanges(v)
		case r.change != nil && left:
			at, _ := v.table.GetSelection()
			for at > 0 && v.rows[at].change != nil {
				at--
			}
			v.table.Select(at, 0)
		case enter || right:
			a.tv.SetFocus(v.diff)
		}
		return nil
	case ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && ev.Rune() == 'q':
		a.closeChanges()
		return nil
	case ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && ev.Rune() == ' ':
		a.toggleChangePick(v)
		return nil
	}
	if ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && strings.ContainsRune("jkgG", ev.Rune()) ||
		ev.Key() == tcell.KeyUp || ev.Key() == tcell.KeyDown {
		return ev
	}
	a.actionKeys(ev,
		func() (string, []uiAction) { return "Actions · " + v.place.title, a.changesActions(v) },
		func() (string, []uiAction) { return "Changes", a.changesActions(v) })
	return nil
}

// changesDiffKeys answers the diff pane: it scrolls, and goes back. h
// scrolls a diff moved right back to the left first, and goes back to the
// list only from there.
func (a *App) changesDiffKeys(v *changesView, ev *tcell.EventKey) *tcell.EventKey {
	switch {
	case ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && ev.Rune() == 'h':
		if _, column := v.diff.GetScrollOffset(); column > 0 {
			return ev
		}
		a.tv.SetFocus(v.table)
		return nil
	case ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyBacktab:
		a.tv.SetFocus(v.table)
		return nil
	case ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && ev.Rune() == 'c':
		a.commitChanges(v)
		return nil
	}
	return ev
}

// changesActions are what can be done in the dialog, on the row under the
// cursor or the files picked.
func (a *App) changesActions(v *changesView) []uiAction {
	return []uiAction{
		{name: "Pick", about: "Put the file into the commit or take it out; on a group, every file of it.", keys: "space", rank: 10,
			run: func() { a.toggleChangePick(v) }},
		{name: "Pick All or None", about: "Pick every file, versioned and unversioned; again, none.", keys: "a", rank: 15,
			run: func() { a.pickAllChanges(v) }},
		{name: "Commit…", about: "Commit the picked files as they are on disk, the unversioned among them added; the dialog asks for the message.", keys: "c", rank: 20,
			run: func() { a.commitChanges(v) }},
		{name: "Rollback Changes…", about: "Put the picked versioned files - or the one under the cursor - back as the last commit has them; asks first.", keys: "u", rank: 30,
			run: func() { a.rollbackChanges(v) }},
		{name: "Delete…", about: "Delete the picked unversioned files - or the one under the cursor - from disk; asks first.", keys: "d", rank: 40,
			run: func() { a.deleteUnversioned(v) }},
		{name: "Refresh", about: "Read again what is not committed, keeping the picks of the files still there.", keys: "r", rank: 50,
			run: a.reloadChanges},
	}
}

// toggleChangePick picks the file under the cursor, or a whole group, or
// takes it out.
func (a *App) toggleChangePick(v *changesView) {
	r, ok := v.row()
	if !ok {
		return
	}
	if r.change != nil {
		key := changesKey(*r.change)
		v.picked[key] = !v.picked[key]
	} else {
		on := !v.groupPicked(r.group)
		for _, c := range v.changes {
			if groupOf(c) == r.group {
				v.picked[changesKey(c)] = on
			}
		}
	}
	a.renderChanges(v)
	if at, _ := v.table.GetSelection(); r.change != nil && at+1 < len(v.rows) {
		v.table.Select(at+1, 0)
	}
}

// pickAllChanges picks every file, or none when every one is picked.
func (a *App) pickAllChanges(v *changesView) {
	on := len(v.pickedChanges()) < len(v.changes)
	for _, c := range v.changes {
		v.picked[changesKey(c)] = on
	}
	a.renderChanges(v)
}

// commitChanges opens the commit dialog with the files picked.
func (a *App) commitChanges(v *changesView) {
	picked := v.pickedChanges()
	if len(picked) == 0 {
		a.flash("no file is picked - space picks the one under the cursor, a all of them")
		return
	}
	a.showCommitForm(v.place.title, false, []commitTarget{{instance: v.place.instance, path: v.place.path,
		dir: v.place.dir, name: v.place.path, edits: len(picked), changes: picked}})
}

// changeList names files for a question, one a line, a long list cut.
func changeList(changes []gitx.Change) string {
	const most = 12
	var lines []string
	for i, c := range changes {
		if i == most {
			lines = append(lines, fmt.Sprintf("… and %d more", len(changes)-most))
			break
		}
		lines = append(lines, "  "+tag(role(fileRole(c.Kind)))+esc(c.Path)+tagEnd)
	}
	return strings.Join(lines, "\n")
}

// rollbackChanges puts versioned files back as HEAD has them, after asking.
func (a *App) rollbackChanges(v *changesView) {
	chosen := v.chosen(true)
	if len(chosen) == 0 {
		if c := v.cursor(); c != nil && !c.Versioned() {
			a.flash("an unversioned file has nothing to roll back to - d deletes it")
		} else {
			a.flash("no versioned file is picked or under the cursor")
		}
		return
	}
	body := fmt.Sprintf("Roll back %d file(s) to the last commit?\n\n%s\n\n"+
		"Your changes to them are lost. An added file stays on disk, unversioned.", len(chosen), changeList(chosen))
	a.confirmWith("Rollback changes", body, "Roll back", nil, func() {
		git := a.newManager(v.place.instance, v.place.path, nil).Git()
		a.runTaskThen("Rolling back "+v.place.title, func(log func(string)) (string, error) {
			return fmt.Sprintf("rolled back %d file(s)", len(chosen)), git.Rollback(v.place.dir, chosen)
		}, func(said string) {
			a.afterGitChange()
			a.done(said)
		})
	})
}

// deleteUnversioned deletes unversioned files from disk, after asking.
func (a *App) deleteUnversioned(v *changesView) {
	chosen := v.chosen(false)
	if len(chosen) == 0 {
		if c := v.cursor(); c != nil && c.Versioned() {
			a.flash("a versioned file is rolled back, not deleted - u rolls it back")
		} else {
			a.flash("no unversioned file is picked or under the cursor")
		}
		return
	}
	paths := make([]string, len(chosen))
	for i, c := range chosen {
		paths[i] = c.Path
	}
	body := fmt.Sprintf("Delete %d unversioned file(s) from disk?\n\n%s\n\ngit has no copy of them: they cannot be brought back.",
		len(chosen), changeList(chosen))
	a.confirmWith("Delete files", body, "Delete", nil, func() {
		git := a.pathManager(v.place.instance, v.place.path).Git()
		if err := git.DeleteUnversioned(v.place.dir, paths); err != nil {
			a.errorf("%v", err)
			return
		}
		a.afterGitChange()
		a.done(fmt.Sprintf("deleted %d file(s)", len(chosen)))
	})
}
