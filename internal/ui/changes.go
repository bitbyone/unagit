package ui

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// The Changes dialog is IntelliJ's commit window: the files not committed
// on the left, in two groups - Changes, the versioned ones, and
// Unversioned Files - and on the right the diff of the one under the
// cursor, drawn as the cursor comes to it. A file's name is coloured by
// what happened to it; no letter says it.
//
// Two things are kept apart, as IntelliJ keeps its checkboxes apart from
// its selection. A file's box says whether it goes into the commit: x ticks
// it (the versioned ones start ticked, the unversioned not), and only c
// reads the boxes. Space marks rows, as it marks them in every list, and
// every other action - rollback, delete, ticking - acts on the marked rows,
// or on the row under the cursor when none is marked. So d never deletes
// what is merely ticked.

const pageChanges = "changes"

// changesPlace is the working tree whose changes are listed, and what an
// editor opened from it is recorded as.
type changesPlace struct {
	instance, path, dir string
	title               string
	what                session.Record
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
	// included are the files ticked for the commit, marked the rows marked
	// for an action, each by its path.
	included map[string]bool
	marked   map[string]bool
	folded   [2]bool
	rows     []changesRow
	table    *tview.Table
	// filter is the field / types into, and query what it holds: the list
	// shows the files whose path matches, the rest kept as they were.
	filter *tview.InputField
	query  string
	left   *tview.Flex
	diff   *tview.TextView
	hint   *tview.TextView
	frame  *tview.Flex
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

// changesKey is how a file is known in the ticks, the marks and the diffs.
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
		dir: a.projectDir(pr.Instance, pr.PathWithNamespace), title: pr.PathWithNamespace,
		what: session.Record{Instance: pr.Instance, Server: a.instanceLabel(pr.Instance), Project: pr.PathWithNamespace, Mode: session.ModeRepository}})
}

// showWorktreeChanges opens it on a worktree; a group has changes per
// repository, each from its block in the worktree view.
func (a *App) showWorktreeChanges(r worktreeRow) {
	if r.grouped() {
		a.flash("a group has its changes per repository - open its view with Enter and light one")
		return
	}
	a.showChanges(changesPlace{instance: r.Instance, path: r.Path, dir: r.Dir, title: r.Path + " · " + r.Branch,
		what: session.Record{Instance: r.Instance, Server: a.instanceLabel(r.Instance), Project: r.Path, Title: r.Branch, Mode: session.ModeBranch}})
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
		title: fmt.Sprintf("%s !%d · %s", project.PathWithNamespace, mr.IID, mr.SourceBranch),
		what:  a.sessionOf(mr, project.PathWithNamespace, session.ModeBranch)})
}

// showChanges builds the dialog and reads the changes into it.
func (a *App) showChanges(place changesPlace) {
	v := &changesView{place: place, included: map[string]bool{}, marked: map[string]bool{}, diffs: map[string]string{}}
	v.table = tview.NewTable().SetSelectable(true, false)
	v.table.SetSelectedStyle(styleSelected)
	box(v.table.Box, "Files")
	v.filter = filterField(tview.NewInputField())
	v.left = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(v.filter, 0, 0, false).
		AddItem(v.table, 0, 1, true)
	v.diff = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	v.diff.SetTextColor(colText)
	box(v.diff.Box, "")
	v.hint = tview.NewTextView().SetDynamicColors(true).SetTextColor(colDim)
	v.hint.SetWrap(true).SetWordWrap(true)
	panes := tview.NewFlex().
		AddItem(v.left, 30, 0, true).
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
		panes.ResizeItem(v.left, max(min(v.listWidth()+2, width*2/5), min(30, width/2)), 0)
		return x + 2, y + 1, width, max(0, h-2)
	})
	v.table.SetSelectionChangedFunc(func(int, int) {
		a.showChangeDiff(v)
		a.changesFocus(v)
	})
	v.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey { return a.changesListKeys(v, ev) })
	v.filter.SetChangedFunc(func(text string) {
		v.query = strings.TrimSpace(text)
		a.renderChanges(v)
	})
	v.filter.SetDoneFunc(func(key tcell.Key) {
		// Enter keeps what is typed and goes back to the list; Esc clears it.
		if key == tcell.KeyEscape {
			v.filter.SetText("")
		}
		a.showFilter(v, v.query != "")
		a.tv.SetFocus(v.table)
	})
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
// rollback, keeping the ticks of what is still there; the marks are done
// with.
func (a *App) reloadChanges() {
	if v := a.changes; v != nil {
		v.diffs = map[string]string{}
		a.readChanges(v, false)
	}
}

// readChanges asks git what is not committed, off the event loop. The
// first read ticks every versioned file.
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
			included := map[string]bool{}
			for _, c := range changes {
				if first && c.Versioned() || v.included[changesKey(c)] {
					included[changesKey(c)] = true
				}
			}
			v.changes, v.included, v.marked, v.loaded = changes, included, map[string]bool{}, true
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
			if groupOf(v.changes[i]) == g && v.shows(v.changes[i]) {
				v.rows = append(v.rows, changesRow{group: g, change: &v.changes[i]})
			}
		}
	}
	// Where nothing is kept, the cursor starts on the first file.
	at := min(1, max(len(v.rows)-1, 0))
	for i, r := range v.rows {
		cell := tview.NewTableCell(v.rowText(r, counts[r.group])).SetExpansion(1)
		if r.change != nil && v.marked[changesKey(*r.change)] {
			bandMarked.paint(cell)
		}
		v.table.SetCell(i, 0, cell)
		switch {
		case r.change != nil && changesKey(*r.change) == keep:
			at = i
		case r.change == nil && r.group == keepGroup:
			at = i
		}
	}
	if len(v.rows) == 0 {
		word := "reading what is not committed …"
		switch {
		case v.loaded && v.query != "":
			word = "no file matches " + v.query
		case v.loaded:
			word = "nothing to commit: every file is as the last commit has it"
		}
		v.table.SetCell(0, 0, tview.NewTableCell(tag(colDim)+esc(word)+tagEnd).SetSelectable(false))
	}
	v.table.Select(at, 0)
	title := fmt.Sprintf(" %d of %d to commit ", len(v.includedChanges()), len(v.changes))
	if n := len(v.markedChanges()); n > 0 {
		title += fmt.Sprintf("· %d marked ", n)
	}
	if v.query != "" {
		title += "· / " + esc(v.query) + " "
	}
	v.table.SetTitle(title)
	a.showChangeDiff(v)
	a.changesFocus(v)
}

// rowText is a row as it is drawn: a group's fold, box and count, or a
// file's box, its name in the colour of what happened to it, and its
// folder after it, quieter.
func (v *changesView) rowText(r changesRow, count int) string {
	if r.change == nil {
		fold := glyphUnfolded
		if v.folded[r.group] {
			fold = glyphFolded
		}
		pick := glyphUnpicked
		if v.groupIncluded(r.group) {
			pick = glyphPicked
		}
		return esc(fold+" "+pick+" ") + "[::b]" + esc(changesGroups[r.group]) + "[::-]" +
			tag(colDim) + esc(fmt.Sprintf("  %d file%s", count, plural(count, "", "s"))) + tagEnd
	}
	c := *r.change
	pick := glyphUnpicked
	if v.included[changesKey(c)] {
		pick = glyphPicked
	}
	dir, name := path.Split(c.Path)
	text := "  " + esc(pick) + " " + tag(role(fileRole(c.Kind))) + esc(name) + tagEnd + lineCounts(c)
	if dir = strings.TrimSuffix(dir, "/"); dir != "" {
		text += "  " + tag(role("files.folder")) + esc(dir) + tagEnd
	}
	if c.From != "" {
		text += "  " + tag(role("files.folder")) + esc("← "+c.From) + tagEnd
	}
	return text
}

// lineCounts is how many lines of a file were added and deleted, each in
// its diff colour, quieter than the name: " +12 −3", a side with none left
// out, and "binary" for a file with no lines to count.
func lineCounts(c gitx.Change) string {
	switch {
	case c.Binary:
		return " " + tag(colDim) + "binary" + tagEnd
	case c.Added == 0 && c.Deleted == 0:
		return ""
	}
	out := ""
	if c.Added > 0 {
		out += " " + tag(iconShade(role("files.lines_added"))) + fmt.Sprintf("+%d", c.Added) + tagEnd
	}
	if c.Deleted > 0 {
		out += " " + tag(iconShade(role("files.lines_deleted"))) + fmt.Sprintf("−%d", c.Deleted) + tagEnd
	}
	return out
}

// shows reports whether the filter lets a file through.
func (v *changesView) shows(c gitx.Change) bool {
	if v.query == "" {
		return true
	}
	_, ok := fuzzy.Match(v.query, c.Path)
	return ok
}

// showFilter gives the filter its row above the list, or takes it away.
func (a *App) showFilter(v *changesView, on bool) {
	height := 0
	if on {
		height = 1
	}
	v.left.ResizeItem(v.filter, height, 0)
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

// groupIncluded reports whether every file of a group is ticked.
func (v *changesView) groupIncluded(group int) bool {
	any := false
	for _, c := range v.changes {
		if groupOf(c) == group {
			if !v.included[changesKey(c)] {
				return false
			}
			any = true
		}
	}
	return any
}

// includedChanges are the files ticked for the commit, in the list's order.
func (v *changesView) includedChanges() []gitx.Change { return v.inSet(v.included) }

// markedChanges are the files marked, in the list's order.
func (v *changesView) markedChanges() []gitx.Change { return v.inSet(v.marked) }

func (v *changesView) inSet(set map[string]bool) []gitx.Change {
	var out []gitx.Change
	for _, c := range v.changes {
		if set[changesKey(c)] {
			out = append(out, c)
		}
	}
	return out
}

// targets are what an action acts on: the marked files, or the row under
// the cursor when none is marked - a file, or every file of a group.
func (v *changesView) targets() []gitx.Change {
	if marked := v.markedChanges(); len(marked) > 0 {
		return marked
	}
	r, ok := v.row()
	switch {
	case !ok:
		return nil
	case r.change != nil:
		return []gitx.Change{*r.change}
	}
	return v.groupFiles(r.group)
}

// groupFiles are the files of a group the filter lets through.
func (v *changesView) groupFiles(group int) []gitx.Change {
	var out []gitx.Change
	for _, c := range v.changes {
		if groupOf(c) == group && v.shows(c) {
			out = append(out, c)
		}
	}
	return out
}

// targetsName says what the actions act on, for the title of their list.
func (v *changesView) targetsName() string {
	if n := len(v.markedChanges()); n > 0 {
		return fmt.Sprintf("%d marked file%s", n, plural(n, "", "s"))
	}
	if r, ok := v.row(); ok {
		if r.change != nil {
			return r.change.Path
		}
		return changesGroups[r.group]
	}
	return v.place.title
}

// split parts files into the versioned and the unversioned.
func split(changes []gitx.Change) (versioned, unversioned []gitx.Change) {
	for _, c := range changes {
		if c.Versioned() {
			versioned = append(versioned, c)
		} else {
			unversioned = append(unversioned, c)
		}
	}
	return versioned, unversioned
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
	v.hint.SetText(litHint(v.listHint()))
}

// listHint names what can be done with what the actions would act on now:
// a versioned file is rolled back, an unversioned one added or deleted.
func (v *changesView) listHint() string {
	versioned, unversioned := split(v.targets())
	parts := []string{"space mark", "x commit or not", "c commit"}
	if len(versioned) > 0 {
		parts = append(parts, "R rollback")
	}
	if len(unversioned) > 0 {
		parts = append(parts, "A add to git", "d delete")
	}
	return strings.Join(append(parts, "s shelve", "Ctrl-O open"), " · ")
}

// changesListKeys answers the list's keys: folding, the panes, then the
// actions.
func (a *App) changesListKeys(v *changesView, ev *tcell.EventKey) *tcell.EventKey {
	r, ok := v.row()
	switch {
	// Alt-Enter is an Enter too: the pickers come before the tree has it.
	case opensSelectionActions(ev) || opensScreenActions(ev):
		a.changesActionKeys(v, ev)
		return nil
	case ev.Key() == tcell.KeyEsc:
		// Marks go first, as in every list; then the dialog.
		switch {
		case len(v.markedChanges()) > 0:
			v.marked = map[string]bool{}
			a.renderChanges(v)
		case v.query != "":
			v.filter.SetText("")
			a.showFilter(v, false)
		default:
			a.closeChanges()
		}
		return nil
	case ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && ev.Rune() == '/':
		a.showFilter(v, true)
		a.tv.SetFocus(v.filter)
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
		a.toggleChangeMark(v)
		return nil
	}
	if ev.Key() == tcell.KeyRune && ev.Modifiers() == 0 && strings.ContainsRune("jkgG", ev.Rune()) ||
		ev.Key() == tcell.KeyUp || ev.Key() == tcell.KeyDown {
		return ev
	}
	a.changesActionKeys(v, ev)
	return nil
}

// changesActionKeys answers a key with the dialog's actions: Alt-Enter
// lists those of the marked rows or the row under the cursor, : the
// dialog's own.
func (a *App) changesActionKeys(v *changesView, ev *tcell.EventKey) {
	a.actionKeys(ev,
		func() (string, []uiAction) { return "Actions · " + v.targetsName(), a.changesSelectionActions(v) },
		func() (string, []uiAction) { return "Changes · " + v.place.title, a.changesScreenActions(v) })
}

// changesDiffKeys answers the diff pane: it scrolls, and goes back. h
// scrolls a diff moved right back to the left first, and goes back to the
// list only from there.
func (a *App) changesDiffKeys(v *changesView, ev *tcell.EventKey) *tcell.EventKey {
	switch {
	case opensSelectionActions(ev) || opensScreenActions(ev):
		a.changesActionKeys(v, ev)
		return nil
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

// changesSelectionActions are what can be done with the marked rows, or
// the row under the cursor when none is marked.
func (a *App) changesSelectionActions(v *changesView) []uiAction {
	// The pickers offer an action only where it can be done; its key runs
	// anyway, to say why not.
	versioned := func() bool { some, _ := split(v.targets()); return len(some) > 0 }
	unversioned := func() bool { _, some := split(v.targets()); return len(some) > 0 }
	someFile := func() bool { return len(v.targets()) > 0 }
	onDisk := func() bool { c := v.cursor(); return c != nil && c.Kind != gitx.Deleted }
	return []uiAction{
		{name: "Toggle Mark", about: "Mark the row for an action, or unmark it; on a group, every file of it.", keys: "space", rank: 10,
			run: func() { a.toggleChangeMark(v) }},
		{name: "Include or Exclude", about: "Tick the files for the commit, or untick them when every one is ticked: the marked ones, or the row under the cursor.", keys: "x", rank: 15, when: someFile,
			run: func() { a.toggleIncluded(v) }},
		{name: "Open", about: "Open the repository in your default editor with the file under the cursor in front.", keys: "Ctrl-O", rank: 25, when: onDisk,
			run: func() { a.openChangedFile(v, false) }},
		{name: "Open With…", about: "Choose the editor, then open the repository there with the file under the cursor in front.", keys: "Alt-O", rank: 26, when: onDisk,
			run: func() { a.openChangedFile(v, true) }},
		{name: "Rollback Changes…", about: "Put the marked versioned files - or the one under the cursor - back as the last commit has them; asks first.", keys: "R", rank: 30, when: versioned,
			run: func() { a.rollbackChanges(v) }},
		{name: "Copy…", about: "Copy the paths of the marked files - or the one under the cursor - or their folders, or their changes as a patch to apply elsewhere.", keys: "y", rank: 27, when: someFile,
			run: func() { a.yankChanges(v) }},
		{name: "Show Diff in Hunk", about: "The changes of the marked files - or the one under the cursor - in Hunk.", keys: "D", rank: 28, when: someFile,
			run: func() { a.changesInHunk(v) }},
		{name: "Add to .gitignore", about: "Have git ignore the marked unversioned files - or the one under the cursor - each by its own path.", keys: "I", rank: 37, when: unversioned,
			run: func() { a.ignoreUnversioned(v) }},
		{name: "Add to Git", about: "Put the marked unversioned files - the one under the cursor, or a whole group - under git: they join the changes as added files.", keys: "A", rank: 35, when: unversioned,
			run: func() { a.addUnversioned(v) }},
		{name: "Delete…", about: "Delete the marked unversioned files - or the one under the cursor - from disk; asks first.", keys: "d", rank: 40, when: unversioned,
			run: func() { a.deleteUnversioned(v) }},
	}
}

// changesScreenActions are what the dialog itself can do.
func (a *App) changesScreenActions(v *changesView) []uiAction {
	return []uiAction{
		{name: "Commit…", about: "Commit the ticked files as they are on disk, the unversioned among them added; the dialog asks for the message.", keys: "c", rank: 20,
			when: func() bool { return len(v.includedChanges()) > 0 },
			run:  func() { a.commitChanges(v) }},
		{name: "Include All or None", about: "Tick every file for the commit, versioned and unversioned; again, none.", keys: "a", rank: 22,
			run: func() { a.includeAllChanges(v) }},
		{name: "Shelve Changes…", about: "Put the marked files - every file when none is marked - aside under a name, the files back as the last commit has them; Shelf… brings them back.", keys: "s", rank: 30,
			when: func() bool { return len(v.changes) > 0 },
			run:  func() { a.shelveFromChanges(v) }},
		{name: "Refresh", about: "Read again what is not committed, keeping the ticks of the files still there.", keys: "r", rank: 50,
			run: a.reloadChanges},
	}
}

// openChangedFile opens the whole working tree in an editor - nil asks for
// none, ask chooses one first - with the file under the cursor in its
// buffer, and reads the changes again once a terminal editor is closed.
func (a *App) openChangedFile(v *changesView, ask bool) {
	c := v.cursor()
	switch {
	case c == nil:
		a.flash("no file is under the cursor - a group's heading has none to open")
		return
	case c.Kind == gitx.Deleted:
		a.flash(c.Path + " is deleted - there is nothing on disk to open")
		return
	}
	file := c.Path
	a.withEditor(ask, func(ed *editors.Editor) {
		go func() {
			a.openEditorAt(v.place.dir, v.place.what, ed, file)
			a.tv.QueueUpdateDraw(a.reloadChanges)
		}()
	})
}

// toggleChangeMark marks the file under the cursor and moves on, so a run
// of rows is marked by holding space - or marks a whole group, or unmarks.
func (a *App) toggleChangeMark(v *changesView) {
	r, ok := v.row()
	if !ok {
		return
	}
	if r.change != nil {
		key := changesKey(*r.change)
		v.marked[key] = !v.marked[key]
	} else {
		group := v.groupFiles(r.group)
		all := true
		for _, c := range group {
			if !v.marked[changesKey(c)] {
				all = false
			}
		}
		for _, c := range group {
			v.marked[changesKey(c)] = !all
		}
	}
	a.renderChanges(v)
	if at, _ := v.table.GetSelection(); r.change != nil && at+1 < len(v.rows) {
		v.table.Select(at+1, 0)
	}
}

// toggleIncluded ticks the files acted on for the commit, or unticks them
// when every one is ticked already.
func (a *App) toggleIncluded(v *changesView) {
	targets := v.targets()
	if len(targets) == 0 {
		return
	}
	on := false
	for _, c := range targets {
		if !v.included[changesKey(c)] {
			on = true
		}
	}
	for _, c := range targets {
		v.included[changesKey(c)] = on
	}
	a.renderChanges(v)
}

// includeAllChanges ticks every file, or none when every one is ticked.
func (a *App) includeAllChanges(v *changesView) {
	on := len(v.includedChanges()) < len(v.changes)
	for _, c := range v.changes {
		v.included[changesKey(c)] = on
	}
	a.renderChanges(v)
}

// commitChanges opens the commit dialog with the files ticked.
func (a *App) commitChanges(v *changesView) {
	included := v.includedChanges()
	if len(included) == 0 {
		a.flash("no file is ticked for the commit - x ticks the one under the cursor, a all of them")
		return
	}
	a.showCommitForm(v.place.title, false, []commitTarget{{instance: v.place.instance, path: v.place.path,
		dir: v.place.dir, name: v.place.path, edits: len(included), changes: included}})
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
	chosen, left := split(v.targets())
	if len(chosen) == 0 {
		if len(left) > 0 {
			a.flash("an unversioned file has nothing to roll back to - d deletes it")
		}
		return
	}
	body := fmt.Sprintf("Roll back %d file(s) to the last commit?\n\n%s\n\n"+
		"Your changes to them are lost. An added file stays on disk, unversioned.", len(chosen), changeList(chosen))
	if len(left) > 0 {
		body += fmt.Sprintf("\n\nThe %d unversioned file(s) marked have nothing to roll back to, and stay.", len(left))
	}
	a.confirmWith("Rollback changes", body, "Rollback", nil, func() {
		git := a.newManager(v.place.instance, v.place.path, nil).Git()
		a.runTaskThen("Rolling back "+v.place.title, func(log func(string)) (string, error) {
			return fmt.Sprintf("rolled back %d file(s)", len(chosen)), git.Rollback(v.place.dir, chosen)
		}, func(said string) {
			a.afterGitChange()
			a.done(said)
		})
	})
}

// addUnversioned puts unversioned files under git. They move to the
// Changes group as added files, ticked for the commit as the versioned
// ones start.
func (a *App) addUnversioned(v *changesView) {
	_, chosen := split(v.targets())
	if len(chosen) == 0 {
		a.flash("nothing unversioned is marked or under the cursor - git has it already")
		return
	}
	paths := make([]string, len(chosen))
	for i, c := range chosen {
		paths[i] = c.Path
		v.included[c.Path] = true
	}
	git := a.pathManager(v.place.instance, v.place.path).Git()
	go func() {
		err := git.AddFiles(v.place.dir, paths)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("adding to git: %v", err)
				return
			}
			a.afterGitChange()
			a.done(fmt.Sprintf("added %d file(s) to git", len(paths)))
		})
	}()
}

// ignoreUnversioned writes unversioned files into the .gitignore at the
// repository's root, which then shows among the changes itself.
func (a *App) ignoreUnversioned(v *changesView) {
	_, chosen := split(v.targets())
	if len(chosen) == 0 {
		a.flash("nothing unversioned is marked or under the cursor - a versioned file is not ignored")
		return
	}
	paths := make([]string, len(chosen))
	for i, c := range chosen {
		paths[i] = c.Path
	}
	if err := a.pathManager(v.place.instance, v.place.path).Git().Ignore(v.place.dir, paths); err != nil {
		a.errorf("writing .gitignore: %v", err)
		return
	}
	a.afterGitChange()
	a.done(fmt.Sprintf("ignored %d file(s) in .gitignore", len(paths)))
}

// yankChanges copies what the actions would act on: each file's path from
// the repository's root or in full, the folders they are in, or their
// changes as a patch another checkout can apply.
func (a *App) yankChanges(v *changesView) {
	targets := v.targets()
	if len(targets) == 0 {
		return
	}
	git := a.pathManager(v.place.instance, v.place.path).Git()
	go func() {
		patch, err := git.Patch(v.place.dir, targets)
		a.tv.QueueUpdateDraw(func() {
			var paths, full, folders, fullFolders []string
			seen := map[string]bool{}
			for _, c := range targets {
				paths = append(paths, c.Path)
				full = append(full, filepath.Join(v.place.dir, c.Path))
				folder := path.Dir(c.Path)
				if !seen[folder] {
					seen[folder] = true
					folders = append(folders, folder)
					fullFolders = append(fullFolders, filepath.Join(v.place.dir, folder))
				}
			}
			many := len(targets) > 1
			items := []yankItem{
				{pluralWord(many, "Path", "Paths") + " from the repository root", strings.Join(paths, "\n")},
				{pluralWord(many, "Absolute path", "Absolute paths"), strings.Join(full, "\n")},
				{pluralWord(len(folders) > 1, "Folder", "Folders") + " from the repository root", strings.Join(folders, "\n")},
				{pluralWord(len(folders) > 1, "Absolute folder", "Absolute folders"), strings.Join(fullFolders, "\n")},
			}
			if err == nil && patch != "" {
				items = append(items, yankItem{"Patch", patch})
			}
			a.showYank("Copy "+v.targetsName(), items)
		})
	}()
}

// pluralWord is one of two words, by whether there are several.
func pluralWord(many bool, one, several string) string {
	if many {
		return several
	}
	return one
}

// changesInHunk shows the changes of what the actions act on in Hunk.
func (a *App) changesInHunk(v *changesView) {
	targets := v.targets()
	bin, ok := a.hunkBinary()
	if !ok || len(targets) == 0 {
		return
	}
	git := a.pathManager(v.place.instance, v.place.path).Git()
	go func() {
		patch, err := git.Patch(v.place.dir, targets)
		if err != nil {
			a.tv.QueueUpdateDraw(func() { a.errorf("%v", err) })
			return
		}
		a.runHunkPatch(bin, v.place.dir, patch)
	}()
}

// deleteUnversioned deletes unversioned files from disk, after asking.
func (a *App) deleteUnversioned(v *changesView) {
	left, chosen := split(v.targets())
	if len(chosen) == 0 {
		if len(left) > 0 {
			a.flash("a versioned file is rolled back, not deleted - R rolls it back")
		}
		return
	}
	paths := make([]string, len(chosen))
	for i, c := range chosen {
		paths[i] = c.Path
	}
	body := fmt.Sprintf("Delete %d unversioned file(s) from disk?\n\n%s\n\ngit has no copy of them: they cannot be brought back.",
		len(chosen), changeList(chosen))
	if len(left) > 0 {
		body += fmt.Sprintf("\n\nThe %d versioned file(s) marked are not deleted: R rolls them back.", len(left))
	}
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
