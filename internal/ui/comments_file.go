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
// h hides them and shows them again. The cursor goes over the lines and
// the comments alike: on a line c writes a comment there, on a comment r
// answers it, e edits it, d deletes it and x resolves its thread.
// Comments are on the version they were written on, so each version shows
// its own; new ones go on the latest.

// fileWidth is as wide as a file's dialog grows: code reads in longer
// lines than a conversation does.
const fileWidth = 180

// bandView is a text view with a band across the rows the cursor is on,
// laid over the text as it is drawn: moving the cursor draws the view
// again and no more, where setting its text anew would read every line of
// a long file again at every step.
type bandView struct {
	*tview.TextView
	band func() (bandArea, bool)
	// rows are its rows as the mouse finds them.
	rows func() *rowsUnder
}

func (v *bandView) mouseRows() *rowsUnder {
	if v.rows == nil {
		return nil
	}
	return v.rows()
}

// bandArea is what the band covers: rows of the text, and - for a comment's
// card - the columns from its left edge, width wide, its frame lit as the
// chosen comment's is; width 0 is the whole row.
type bandArea struct {
	from, to    int
	left, width int
	frame       bool
}

// frameRunes are what a card's frame is drawn with.
const frameRunes = "─│╭╮╰╯"

func (v *bandView) Draw(screen tcell.Screen) {
	v.TextView.Draw(screen)
	area, ok := v.band()
	if !ok {
		return
	}
	x, y, w, h := v.GetRect()
	inX, inY, _, inH := v.GetInnerRect()
	top, _ := v.GetScrollOffset()
	// The band is each cell's own background lifted - the code's, a
	// diff's fill, a comment's card - so what is under the cursor reads as
	// it does elsewhere; the terminal's own background, which cannot be
	// shifted, takes a chosen comment's.
	plain := lift(tview.Styles.PrimitiveBackgroundColor)
	if plain == tcell.ColorDefault {
		plain = role("comments.selected_fill")
	}
	// Inside the border, the padding too, so a line's band reaches the
	// frame; a card's covers the card alone.
	left, right := x+1, x+w-2
	if area.width > 0 {
		left, right = inX+area.left, inX+area.left+area.width-1
	}
	focus := role("comments.selected")
	for row := max(area.from, top); row <= area.to && row < top+inH; row++ {
		sy := inY + row - top
		if sy < y || sy >= y+h {
			continue
		}
		for sx := left; sx <= right && sx < x+w-1; sx++ {
			r, comb, style, _ := screen.GetContent(sx, sy)
			_, bg, _ := style.Decompose()
			fill := lift(bg)
			if fill == tcell.ColorDefault {
				fill = plain
			}
			style = style.Background(fill)
			edge := row == area.from || row == area.to || sx == left || sx == right
			if area.frame && edge && strings.ContainsRune(frameRunes, r) {
				style = style.Foreground(focus)
			}
			screen.SetContent(sx, sy, r, comb, style)
		}
	}
}

// fileView is a file of the merge request on screen.
type fileView struct {
	d         *commentsDialog
	path, ref string
	latest    bool
	// lines are the file's, codes the same in the colours of its language.
	lines []diffLine
	codes []string
	// markFrom and markTo are the lines of the comment it was opened from.
	markFrom, markTo int
	// at is where the cursor is: a line, and a comment under it or 0.
	atLine, atNote int
	items          []fileItem
	view           *tview.TextView
	band           *bandView
	fit            func(func(int) string)
}

// fileItem is what the cursor can be on: a line of the file, or a comment
// under it, and the rows it takes.
type fileItem struct {
	line     int
	note     forge.Note
	root     forge.Note
	from, to int
	// band is what the cursor's band covers on it.
	band bandArea
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
		fv := &fileView{d: d, path: path, ref: ref, latest: latest, lines: lines, codes: highlightLines(path, lines),
			markFrom: from, markTo: to, atLine: max(1, min(line, len(texts))), atNote: focus}
		fv.show()
	})
}

func (fv *fileView) show() {
	a, d := fv.d.a, fv.d
	fv.view = tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	fv.view.SetTextColor(colText)
	fv.band = &bandView{TextView: fv.view, band: func() (bandArea, bool) {
		if i := fv.cursor(); i >= 0 {
			return fv.items[i].band, true
		}
		return bandArea{}, false
	}}
	// A click puts the cursor on a line or a comment, a double click is
	// Enter, the wheel moves it as j and k do.
	fv.band.rows = func() *rowsUnder {
		return &rowsUnder{
			focus: fv.band,
			at: func(x, y int) int {
				_, top, _, room := fv.view.GetInnerRect()
				if y < top || y >= top+room {
					return -1
				}
				scroll, _ := fv.view.GetScrollOffset()
				row := y - top + scroll
				for i, it := range fv.items {
					if row >= it.from && row <= it.to {
						return i
					}
				}
				return -1
			},
			cursor: fv.cursor,
			move:   fv.move,
			step:   func(dir int) { fv.move(fv.cursor() + dir) },
			edge:   func(int) int { return -1 },
		}
	}
	version := "latest"
	if !fv.latest {
		version = short(fv.ref)
	}
	box(fv.view.Box, esc(fmt.Sprintf("%s · %s · %s !%d", fv.path, version, a.projectPathOfMR(d.mr), d.mr.IID))).SetBorderPadding(0, 0, 1, 1)
	footer := tview.NewTextView().SetDynamicColors(true)
	hint := "c comment · r reply · e edit · d delete · x resolve · h toggle comments · v version"
	if !fv.latest {
		hint = "r reply · e edit · d delete · x resolve · h toggle comments · v version"
	}
	setHint := func() { footer.SetText(" " + litHint(hint)) }
	setHint()
	frame := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(fv.band, 0, 1, true).AddItem(footer, 1, 0, false)
	fitFooter(frame, footer, 0)
	fv.fit = a.widthAware(fv.view)
	fv.view.SetInputCapture(fv.keys)
	// Another theme put on with the file open: its code coloured again.
	a.onRepaint(fv.view, func() {
		fv.codes = highlightLines(fv.path, fv.lines)
		fv.draw()
	})
	a.onRepaint(footer, setHint)
	// The conversation read again - a reply, a thread resolved, a comment
	// written here - is drawn here too.
	d.loaded = fv.draw
	// A file is a screen of its own: what is opened from it goes back to
	// it, not to the version picker it was chosen in.
	a.returnTo = nil
	a.pages.AddPage(pageCommit, modalFit(frame, func(w, h int) (int, int) {
		return min(w*90/100, fileWidth), h * 85 / 100
	}), true, true)
	a.tv.SetFocus(fv.band)
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
		items = append(items, fileItem{line: line, from: len(rows), to: len(rows), band: bandArea{from: len(rows), to: len(rows)}})
		rows = append(rows, row)
		if fv.d.hideInline {
			continue
		}
		for _, thread := range threads {
			root := thread[0]
			bubbles := bubblesOf(thread)
			for j, n := range thread {
				var text string
				// The band covers the card alone, its frame lit; a note of a
				// change, its line.
				band := bandArea{left: gutter, width: inner}
				if n.Changed != nil {
					text = c.drawChange(n, root, inner, false)
				} else {
					b := bubbles[j]
					b.Where = ""
					text = renderBubble(b, inner)
					band.left, band.width, band.frame = gutter+b.Indent, max(inner-b.Indent, minBubbleWidth), true
				}
				from := len(rows)
				for _, r := range strings.Split(text, "\n") {
					rows = append(rows, pad+r)
				}
				band.from, band.to = from, len(rows)-1
				items = append(items, fileItem{line: line, note: n, root: root, from: from, to: len(rows) - 1, band: band})
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
	// What is drawn stays; the band follows the cursor.
	fv.reveal()
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

// actions are what can be done in the file, on the line or the comment
// the cursor is on; the keys and the picker of actions answer from them.
func (fv *fileView) actions() []uiAction {
	d, a := fv.d, fv.d.a
	i := fv.cursor()
	var it fileItem
	if i >= 0 {
		it = fv.items[i]
	}
	onComment := func() bool { return it.note.ID != 0 }
	note := func(run func(n, root forge.Note)) func() {
		return func() {
			if !onComment() {
				a.flash("no comment chosen - j and k go to one, h shows them")
				return
			}
			run(it.note, it.root)
		}
	}
	return []uiAction{
		{name: "Compare Change", about: "Show, or hide again, what a later version of the merge request changed on the line.",
			keys: "Enter", rank: 5, when: func() bool { return onComment() && it.note.Changed != nil },
			run: note(func(n, root forge.Note) { d.compare(n, root) })},
		{name: "Comment on Line…", about: "Write a comment on the line the cursor is on: publish it at once, or save it to publish later.",
			keys: "c", rank: 10, when: func() bool { return fv.latest },
			run: func() {
				if !fv.latest {
					a.flash("a new comment goes on the latest version - v switches to it")
					return
				}
				a.showLineComposer(d.mr, fv.path, fv.atLine, func(posted *forge.Note) {
					if posted != nil {
						d.posted(posted)
						return
					}
					d.saved()
				})
			}},
		{name: "Reply…", about: "Answer the chosen comment in its thread.", keys: "r", rank: 20,
			when: func() bool { return onComment() && it.note.ID >= 0 }, run: note(func(n, _ forge.Note) { d.reply(&n) })},
		{name: "Edit Comment…", about: "Put new text in a comment of your own, published or saved.", keys: "e", rank: 30,
			when: func() bool { return onComment() && d.own(it.note) }, run: note(func(n, _ forge.Note) { d.edit(n) })},
		{name: "Delete Comment…", about: "Take a comment of your own away, after asking.", keys: "d", rank: 40,
			when: func() bool { return onComment() && d.own(it.note) }, run: note(func(n, _ forge.Note) { d.remove(n) })},
		{name: "Resolve or Reopen Thread", about: "Resolve the chosen comment's thread on the forge, or open it again.", keys: "x", rank: 50,
			when: func() bool { return onComment() && it.root.Resolvable && it.root.ID >= 0 },
			run:  note(func(n, _ forge.Note) { d.resolve(n) })},
		{name: "Toggle Comments", about: "Hide the comments under their lines, or show them again; the marks beside the lines stay.", keys: "h", rank: 60,
			run: func() {
				d.hideInline = !d.hideInline
				if d.hideInline {
					fv.atNote = 0
				}
				fv.draw()
			}},
		{name: "Switch Version…", about: "Read the file at another version: the latest, or one comments were written on.", keys: "v", rank: 70,
			run: fv.switchVersion},
	}
}

func (fv *fileView) keys(ev *tcell.EventKey) *tcell.EventKey {
	a := fv.d.a
	_, _, _, height := fv.view.GetInnerRect()
	i := fv.cursor()
	switch {
	case ev.Key() == tcell.KeyEsc, ev.Key() == tcell.KeyRune && ev.Rune() == 'q':
		fv.close()
	case ev.Key() == tcell.KeyDown, ev.Key() == tcell.KeyRune && ev.Rune() == 'j':
		fv.move(i + 1)
	case ev.Key() == tcell.KeyUp, ev.Key() == tcell.KeyRune && ev.Rune() == 'k':
		fv.move(i - 1)
	case ev.Key() == tcell.KeyRune && ev.Rune() == 'g':
		fv.move(0)
	case ev.Key() == tcell.KeyRune && ev.Rune() == 'G':
		fv.move(len(fv.items) - 1)
	case ev.Key() == tcell.KeyCtrlD:
		fv.moveRows(max(1, height/2))
	case ev.Key() == tcell.KeyCtrlU:
		fv.moveRows(-max(1, height/2))
	case ev.Key() == tcell.KeyCtrlF, ev.Key() == tcell.KeyPgDn:
		fv.moveRows(max(1, height))
	case ev.Key() == tcell.KeyCtrlB, ev.Key() == tcell.KeyPgUp:
		fv.moveRows(-max(1, height))
	case a.opensSelectionActions(ev):
		a.showActions("Actions · "+fv.path, fv.actions())
	case ev.Key() == tcell.KeyEnter:
		runEnter(fv.actions())
	case runKey(fv.actions(), ev):
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
