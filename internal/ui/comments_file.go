package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// A file opened from the conversation (Enter) is read whole, at a version
// of the merge request: its lines numbered, a mark beside each line with
// comments and how many, and the comments themselves under their line -
// s hides them and shows them again. The cursor goes over the lines and
// the comments alike: on a line c writes a comment there, on a comment r
// answers it, e edits it, d deletes it and x resolves its thread.
// Comments are on the version they were written on, so each version shows
// its own; new ones go on the latest.

// fileView is a file of the merge request on screen.
type fileView struct {
	d         *commentsDialog
	path, ref string
	latest    bool
	codes     []string
	// markFrom and markTo are the lines of the comment it was opened from.
	markFrom, markTo int
	// at is where the cursor is: a line, and a comment under it or 0.
	atLine, atNote int
	items          []fileItem
	view           *tview.TextView
	fit            func(func(int) string)
}

// fileItem is what the cursor can be on: a line of the file, or a comment
// under it, and the rows it takes.
type fileItem struct {
	line     int
	note     forge.Note
	root     forge.Note
	from, to int
}

// openFile is Enter on a comment: the file it is on, in the version it was
// written on or the latest - asked, when they differ - the cursor on the
// comment focus, or on its line when that is 0.
func (d *commentsDialog) openFile(root forge.Note, focus int) {
	if root.Path == "" {
		d.a.flash("this comment is on the merge request, not on a file")
		return
	}
	c := d.conv
	base, head, ok := c.versionOf(root)
	oldSide := root.To.New == 0 && (root.To.Old > 0 || root.Orphaned)
	ref, path := head, root.Path
	if oldSide {
		ref = base
		if root.OldPath != "" {
			path = root.OldPath
		}
	}
	from, to := sideLine(root.From, oldSide), sideLine(root.To, oldSide)
	if to == 0 {
		to = root.Line
	}
	if from == 0 || from > to {
		from = to
	}
	latest := c.head
	if latest == "" {
		latest = d.mr.SourceBranch
	}
	// The cursor starts on the line the comment hangs under, its last.
	if !ok || ref == "" || ref == latest {
		d.showFile(path, latest, true, from, to, to, focus)
		return
	}
	d.a.showPickerWith("Which version of "+root.Path, []pickItem{
		{Label: "The version the comment was written on", Sub: short(ref), Data: true},
		{Label: "The latest version", Sub: d.mr.SourceBranch},
	}, pickerOptions{pack: true}, func(it pickItem) {
		if it.Data == true {
			d.showFile(path, ref, false, from, to, to, focus)
			return
		}
		d.showFile(root.Path, latest, true, 0, 0, to, 0)
	})
}

// sideLine is a line's number on the side a comment is on.
func sideLine(r forge.LineRef, oldSide bool) int {
	if oldSide {
		return r.Old
	}
	return r.New
}

// short is a commit's short name.
func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// showFile reads a file at ref and shows it, the lines from to to marked
// (0 for none), the cursor on line - on its comment focus, when not 0.
func (d *commentsDialog) showFile(path, ref string, latest bool, from, to, line, focus int) {
	a := d.a
	client := a.client(d.mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(d.mr.Instance))
		return
	}
	var f fileText
	a.loadThen("Reading "+path, func(func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		f = d.fileAt(ctx, client, ref, path)
		if !f.ok {
			return "", fmt.Errorf("%s is not in %s", path, short(ref))
		}
		return "", nil
	}, func(string) {
		texts := strings.Split(strings.TrimRight(f.text, "\n"), "\n")
		lines := make([]diffLine, len(texts))
		for i, t := range texts {
			lines[i] = diffLine{kind: ' ', new: i + 1, text: strings.ReplaceAll(t, "\t", "    ")}
		}
		fv := &fileView{d: d, path: path, ref: ref, latest: latest, codes: highlightLines(path, lines),
			markFrom: from, markTo: to, atLine: max(1, min(line, len(texts))), atNote: focus}
		fv.show()
	})
}

func (fv *fileView) show() {
	a, d := fv.d.a, fv.d
	fv.view = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	fv.view.SetTextColor(colText)
	version := "latest"
	if !fv.latest {
		version = short(fv.ref)
	}
	box(fv.view.Box, esc(fmt.Sprintf("%s · %s · %s !%d", fv.path, version, a.projectPathOfMR(d.mr), d.mr.IID))).SetBorderPadding(0, 0, 1, 1)
	footer := tview.NewTextView().SetDynamicColors(true)
	hint := "c comment · r reply · e edit · d delete · x resolve · s toggle comments · v version"
	if !fv.latest {
		hint = "r reply · e edit · d delete · x resolve · s toggle comments · v version"
	}
	footer.SetText(" " + litHint(hint))
	frame := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(fv.view, 0, 1, true).AddItem(footer, 1, 0, false)
	fitFooter(frame, footer, 0)
	fv.fit = a.widthAware(fv.view)
	fv.view.SetInputCapture(fv.keys)
	// The conversation read again - a reply, a thread resolved, a comment
	// written here - is drawn here too.
	d.loaded = fv.draw
	// A file is a screen of its own: what is opened from it goes back to
	// it, not to the version picker it was chosen in.
	a.returnTo = nil
	a.pages.AddPage(pageCommit, modalFit(frame, func(w, h int) (int, int) {
		return min(w*90/100, commentsWidth+20), h * 85 / 100
	}), true, true)
	a.tv.SetFocus(fv.view)
	fv.draw()
	if i := fv.cursor(); i >= 0 {
		fv.view.ScrollTo(max(0, fv.items[i].from-codeBefore-2), 0)
	}
}

func (fv *fileView) close() {
	fv.d.loaded = nil
	fv.d.a.closeModal(pageCommit)
}

// threads are the threads on this file in this version, by line.
func (fv *fileView) threads() map[int][][]forge.Note {
	c := fv.d.conv
	out := map[int][][]forge.Note{}
	for _, thread := range groupIntoThreads(append(shown(c.notes), draftNotes(c.drafts)...)) {
		root := thread[0]
		if root.Changed != nil || root.Path == "" {
			continue
		}
		base, head, ok := c.versionOf(root)
		if !ok {
			continue
		}
		oldSide := root.To.New == 0 && (root.To.Old > 0 || root.Orphaned)
		line := sideLine(root.To, oldSide)
		if line == 0 {
			line = root.Line
		}
		switch {
		case !oldSide && head == fv.ref && root.Path == fv.path:
		case oldSide && base == fv.ref && (root.OldPath == fv.path || root.OldPath == "" && root.Path == fv.path):
		default:
			continue
		}
		out[line] = append(out[line], thread)
	}
	return out
}

// cursor is the index of the item the cursor is on.
func (fv *fileView) cursor() int {
	for i, it := range fv.items {
		if it.line == fv.atLine && it.note.ID == fv.atNote {
			return i
		}
	}
	for i, it := range fv.items {
		if it.line == fv.atLine && it.note.ID == 0 {
			return i
		}
	}
	return -1
}

func (fv *fileView) draw() {
	fv.fit(fv.render)
	fv.reveal()
}

// render lays the file out width wide: each line with its number and its
// comments' mark, and under it, while they are shown, its comments.
func (fv *fileView) render(width int) string {
	c := fv.d.conv
	at := fv.threads()
	numW := len(fmt.Sprint(len(fv.codes)))
	gutter := 1 + numW + 1 + 3 + 1
	inner := max(minBubbleWidth, width-gutter)
	pad := strings.Repeat(" ", gutter)
	var (
		rows  []string
		items []fileItem
	)
	for i, code := range fv.codes {
		line := i + 1
		threads := at[line]
		mark, number := " ", tag(role("diff.line_number"))+fmt.Sprintf("%*d", numW, line)+tagEnd
		if fv.markFrom > 0 && line >= fv.markFrom && line <= fv.markTo {
			mark = tag(role("diff.commented")) + glyphCommented + tagEnd
			number = tag(role("diff.commented")) + "[::b]" + fmt.Sprintf("%*d", numW, line) + "[::-]" + tagEnd
		}
		count := 0
		for _, t := range threads {
			for _, n := range t {
				if n.Changed == nil {
					count++
				}
			}
		}
		bubble := "   "
		if count > 0 {
			bubble = tag(role("comments.icon")) + glyphComment + fmt.Sprintf("%-2d", min(count, 99)) + tagEnd
		}
		row := mark + number + " " + bubble + " " + code
		if line == fv.atLine && fv.atNote == 0 {
			row = onFill(padMarkup(row, width), role("comments.selected_fill"))
		}
		items = append(items, fileItem{line: line, from: len(rows), to: len(rows)})
		rows = append(rows, row)
		if fv.d.hideInline {
			continue
		}
		for _, thread := range threads {
			root := thread[0]
			bubbles := bubblesOf(thread)
			for j, n := range thread {
				chosen := line == fv.atLine && n.ID == fv.atNote
				var text string
				if n.Changed != nil {
					text = c.drawChange(n, root, inner, chosen)
				} else {
					b := bubbles[j]
					b.Where, b.Selected = "", chosen
					text = renderBubble(b, inner)
				}
				from := len(rows)
				for _, r := range strings.Split(text, "\n") {
					rows = append(rows, pad+r)
				}
				items = append(items, fileItem{line: line, note: n, root: root, from: from, to: len(rows) - 1})
			}
		}
	}
	fv.items = items
	return strings.Join(rows, "\n")
}

// reveal scrolls so that what the cursor is on shows.
func (fv *fileView) reveal() {
	i := fv.cursor()
	if i < 0 {
		return
	}
	it := fv.items[i]
	_, _, _, height := fv.view.GetInnerRect()
	top, _ := fv.view.GetScrollOffset()
	switch {
	case it.from < top:
		fv.view.ScrollTo(it.from, 0)
	case height > 0 && it.to >= top+height:
		fv.view.ScrollTo(max(it.from, it.to-height+1), 0)
	}
}

// move puts the cursor on the item at i.
func (fv *fileView) move(i int) {
	if len(fv.items) == 0 {
		return
	}
	it := fv.items[max(0, min(i, len(fv.items)-1))]
	fv.atLine, fv.atNote = it.line, it.note.ID
	fv.draw()
}

// moveRows moves the cursor by about rows rows: to the item there.
func (fv *fileView) moveRows(rows int) {
	i := fv.cursor()
	if i < 0 {
		return
	}
	target := fv.items[i].from + rows
	for j, it := range fv.items {
		if it.to >= target {
			fv.move(j)
			return
		}
	}
	fv.move(len(fv.items) - 1)
}

func (fv *fileView) keys(ev *tcell.EventKey) *tcell.EventKey {
	d, a := fv.d, fv.d.a
	_, _, _, height := fv.view.GetInnerRect()
	i := fv.cursor()
	onNote := func(run func(n, root forge.Note)) {
		if i < 0 || fv.items[i].note.ID == 0 {
			a.flash("no comment chosen - j and k go to one, s shows them")
			return
		}
		run(fv.items[i].note, fv.items[i].root)
	}
	switch ev.Key() {
	case tcell.KeyEsc:
		fv.close()
	case tcell.KeyDown:
		fv.move(i + 1)
	case tcell.KeyUp:
		fv.move(i - 1)
	case tcell.KeyCtrlD:
		fv.moveRows(max(1, height/2))
	case tcell.KeyCtrlU:
		fv.moveRows(-max(1, height/2))
	case tcell.KeyCtrlF, tcell.KeyPgDn:
		fv.moveRows(max(1, height))
	case tcell.KeyCtrlB, tcell.KeyPgUp:
		fv.moveRows(-max(1, height))
	case tcell.KeyEnter:
		onNote(func(n, root forge.Note) {
			if n.Changed != nil {
				d.compare(n, root)
			}
		})
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			fv.close()
		case 'j':
			fv.move(i + 1)
		case 'k':
			fv.move(i - 1)
		case 'g':
			fv.move(0)
		case 'G':
			fv.move(len(fv.items) - 1)
		case 's':
			d.hideInline = !d.hideInline
			if d.hideInline {
				fv.atNote = 0
			}
			fv.draw()
		case 'c':
			if !fv.latest {
				a.flash("a new comment goes on the latest version - v switches to it")
				return nil
			}
			a.showLineComposer(d.mr, fv.path, fv.atLine, func(posted *forge.Note) {
				if posted != nil {
					d.posted(posted)
					return
				}
				d.saved()
			})
		case 'r':
			onNote(func(n, _ forge.Note) { d.reply(&n) })
		case 'e':
			onNote(func(n, _ forge.Note) { d.edit(n) })
		case 'x':
			onNote(func(n, _ forge.Note) { d.resolve(n) })
		case 'd':
			onNote(func(n, _ forge.Note) { d.remove(n) })
		case 'v':
			fv.switchVersion()
		default:
			return ev
		}
	default:
		return ev
	}
	return nil
}

// switchVersion opens the file in the other version, the cursor on the
// same line number.
func (fv *fileView) switchVersion() {
	d := fv.d
	latest := d.conv.head
	if latest == "" {
		latest = d.mr.SourceBranch
	}
	items := []pickItem{{Label: "The latest version", Sub: d.mr.SourceBranch, Data: latest}}
	seen := map[string]bool{latest: true}
	for _, n := range append(shown(d.conv.notes), draftNotes(d.conv.drafts)...) {
		if _, head, ok := d.conv.versionOf(n); ok && n.Path == fv.path && !seen[head] {
			seen[head] = true
			items = append(items, pickItem{Label: "A version comments were written on", Sub: short(head), Data: head})
		}
	}
	if len(items) == 1 && fv.latest {
		d.a.flash("comments on this file are all on the latest version")
		return
	}
	line := fv.atLine
	d.a.showPickerWith("Which version of "+fv.path, items, pickerOptions{pack: true}, func(it pickItem) {
		ref := it.Data.(string)
		fv.close()
		d.showFile(fv.path, ref, ref == latest, 0, 0, line, 0)
	})
}
