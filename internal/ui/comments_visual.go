package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A comment taller than the view is read a row at a time by j and k; to
// take some of it - a plan from Atlantis, a stack trace - v puts a cursor
// in the chosen comment itself. j k h l 0 $ move it, v starts a selection
// there and V one of whole lines, y copies what is selected and Y the
// whole comment as it was written; the mouse selects by dragging. Esc
// drops the selection, and then goes back to choosing comments.

// visual is the cursor in a comment and its selection.
type visual struct {
	on bool
	// note is the comment it is in.
	note int
	// line and col are the cursor: a row of the comment's text, a rune of
	// it. anchor is where the selection began, when one is under way.
	line, col int
	anchor    *[2]int
	lines     bool
	dragging  bool
}

// visualLine is a row of a comment's text: the row of the conversation it
// is drawn at, -1 while its card is clipped above or below it, the cell
// its text starts at, and the text.
type visualLine struct {
	row  int
	lead int
	text string
}

// plainRow is a row of markup as it reads, its spaces kept.
func plainRow(markup string) string {
	return styleTag.ReplaceAllStringFunc(markup, func(t string) string {
		if t == "[]" {
			return "]"
		}
		return ""
	})
}

// textCells is how wide a text is drawn, a wide character two cells.
func textCells(s string) int { return tview.TaggedStringWidth(tview.Escape(s)) }

// visualCard is the card the cursor is in.
func (d *commentsDialog) visualCard() (commentCard, bool) {
	for _, c := range d.cards {
		if c.note.ID == d.vis.note && !c.code {
			return c, len(c.body) > 0
		}
	}
	return commentCard{}, false
}

// visualLines are the rows of the chosen comment's text - all of them, a
// clipped card's too - each where its card shows it, if it does.
func (d *commentsDialog) visualLines() []visualLine {
	card, ok := d.visualCard()
	if !ok {
		return nil
	}
	shown := commentClip
	if len(card.body) <= commentClip {
		shown = len(card.body)
	}
	out := make([]visualLine, len(card.body))
	for i := range card.body {
		row := -1
		if i >= card.offset && i < card.offset+shown {
			row = card.bodyFrom + i - card.offset
		}
		out[i] = visualLine{row: row, lead: card.textLeft, text: card.plain[i]}
	}
	return out
}

// startVisual puts the cursor in the chosen comment.
func (d *commentsDialog) startVisual() {
	card, ok := d.chosen()
	if !ok || card.code || card.note.Changed != nil {
		d.a.flash("v works in a comment - choose one with j and k")
		return
	}
	d.vis = visual{on: true, note: card.note.ID, line: card.offset}
	if len(d.visualLines()) == 0 {
		d.vis = visual{}
		d.a.flash("this comment has no text to select")
		return
	}
	d.visualHint()
	d.revealVisual()
}

func (d *commentsDialog) stopVisual() {
	d.vis = visual{}
	d.hint()
}

// visualHint says in the footer what the cursor in a comment can do.
func (d *commentsDialog) visualHint() {
	d.footer.SetText(" " + tag(role("comments.heading")) + "[::b]VISUAL[::-]" + tagEnd + "  " +
		litHint("v select · V lines · y copy · Y copy all · Esc back"))
}

// revealVisual scrolls so that the cursor's row shows: a clipped comment's
// own window first, then the conversation.
func (d *commentsDialog) revealVisual() {
	card, ok := d.visualCard()
	if !ok {
		return
	}
	if len(card.body) > commentClip {
		offset := card.offset
		switch {
		case d.vis.line < offset:
			offset = d.vis.line
		case d.vis.line >= offset+commentClip:
			offset = d.vis.line - commentClip + 1
		}
		if offset != card.offset {
			if d.conv.offsets == nil {
				d.conv.offsets = map[int]int{}
			}
			d.conv.offsets[card.note.ID] = offset
			d.changed()
		}
	}
	lines := d.visualLines()
	if len(lines) == 0 {
		return
	}
	row := lines[d.vis.line].row
	if row < 0 {
		return
	}
	_, _, _, height := d.view.GetInnerRect()
	top, _ := d.view.GetScrollOffset()
	switch {
	case row < top:
		d.view.ScrollTo(row, 0)
	case height > 0 && row >= top+height:
		d.view.ScrollTo(row-height+1, 0)
	}
}

// moveVisual puts the cursor on a line and a column, within the text.
func (d *commentsDialog) moveVisual(line, col int) {
	lines := d.visualLines()
	if len(lines) == 0 {
		return
	}
	d.vis.line = max(0, min(line, len(lines)-1))
	d.vis.col = max(0, min(col, len([]rune(lines[d.vis.line].text))))
	d.revealVisual()
}

// selection is what is selected, from and to as line and column in order,
// the end's column past its last rune; ok is false while nothing is.
func (d *commentsDialog) selection() (from, to [2]int, ok bool) {
	if d.vis.anchor == nil {
		return from, to, false
	}
	a, b := *d.vis.anchor, [2]int{d.vis.line, d.vis.col}
	if b[0] < a[0] || b[0] == a[0] && b[1] < a[1] {
		a, b = b, a
	}
	if d.vis.lines {
		lines := d.visualLines()
		a[1], b[1] = 0, len([]rune(lines[b[0]].text))
	} else {
		b[1]++
	}
	return a, b, true
}

// selectedText is the selection as text, its lines one to a line; with
// nothing selected, the cursor's line.
func (d *commentsDialog) selectedText() (string, int) {
	lines := d.visualLines()
	from, to, ok := d.selection()
	if !ok {
		return strings.TrimRight(lines[d.vis.line].text, " "), 1
	}
	var out []string
	for i := from[0]; i <= to[0]; i++ {
		r := []rune(lines[i].text)
		start, end := 0, len(r)
		if i == from[0] {
			start = min(from[1], len(r))
		}
		if i == to[0] {
			end = min(to[1], len(r))
		}
		out = append(out, strings.TrimRight(string(r[start:max(start, end)]), " "))
	}
	return strings.Join(out, "\n"), len(out)
}

// copyVisual copies the selection, or the line under the cursor.
func (d *commentsDialog) copyVisual() {
	text, n := d.selectedText()
	if err := copyToClipboard(text); err != nil {
		d.a.errorf("cannot copy: %v", err)
		return
	}
	d.stopVisual()
	d.a.done(fmt.Sprintf("Copied %s", counted(n, "line", "lines")))
}

// copyComment copies the whole comment as it was written, its markdown.
func (d *commentsDialog) copyComment() {
	for _, c := range d.cards {
		if c.note.ID == d.vis.note {
			if err := copyToClipboard(c.note.Body); err != nil {
				d.a.errorf("cannot copy: %v", err)
				return
			}
			d.stopVisual()
			d.a.done("Copied the comment")
			return
		}
	}
}

// visualKeys answer the keys while the cursor is in a comment.
func (d *commentsDialog) visualKeys(ev *tcell.EventKey) *tcell.EventKey {
	lines := d.visualLines()
	if len(lines) == 0 {
		d.stopVisual()
		return nil
	}
	_, _, _, height := d.view.GetInnerRect()
	line, col := d.vis.line, d.vis.col
	switch {
	case ev.Key() == tcell.KeyEsc:
		if d.vis.anchor != nil {
			d.vis.anchor = nil
			return nil
		}
		d.stopVisual()
	case ev.Key() == tcell.KeyDown, ev.Key() == tcell.KeyRune && ev.Rune() == 'j':
		d.moveVisual(line+1, col)
	case ev.Key() == tcell.KeyUp, ev.Key() == tcell.KeyRune && ev.Rune() == 'k':
		d.moveVisual(line-1, col)
	case ev.Key() == tcell.KeyLeft, ev.Key() == tcell.KeyRune && ev.Rune() == 'h':
		d.moveVisual(line, col-1)
	case ev.Key() == tcell.KeyRight, ev.Key() == tcell.KeyRune && ev.Rune() == 'l':
		d.moveVisual(line, col+1)
	case ev.Key() == tcell.KeyCtrlD:
		d.moveVisual(line+max(1, height/2), col)
	case ev.Key() == tcell.KeyCtrlU:
		d.moveVisual(line-max(1, height/2), col)
	case ev.Key() == tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			d.stopVisual()
		case '0':
			d.moveVisual(line, 0)
		case '$':
			d.moveVisual(line, len([]rune(lines[line].text)))
		case 'g':
			d.moveVisual(0, col)
		case 'G':
			d.moveVisual(len(lines)-1, col)
		case 'v', 'V':
			whole := ev.Rune() == 'V'
			if d.vis.anchor != nil && d.vis.lines == whole {
				d.vis.anchor = nil
				return nil
			}
			if d.vis.anchor == nil {
				d.vis.anchor = &[2]int{line, col}
			}
			d.vis.lines = whole
		case 'y':
			d.copyVisual()
		case 'Y':
			d.copyComment()
		}
	}
	return nil
}

// visualAt is the line and column of the comment's text at a cell, ok
// false outside it.
func (d *commentsDialog) visualAt(x, y int) (int, int, bool) {
	inX, top, _, room := d.view.GetInnerRect()
	if y < top || y >= top+room {
		return 0, 0, false
	}
	scroll, _ := d.view.GetScrollOffset()
	row := y - top + scroll
	for i, l := range d.visualLines() {
		if l.row != row {
			continue
		}
		at := x - inX - l.lead
		if at < 0 {
			return i, 0, true
		}
		w := 0
		for c, r := range []rune(l.text) {
			w += textCells(string(r))
			if w > at {
				return i, c, true
			}
		}
		return i, len([]rune(l.text)), true
	}
	return 0, 0, false
}

// visualMouse selects by dragging over the comment's text.
func (d *commentsDialog) visualMouse(action tview.MouseAction, ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	line, col, in := d.visualAt(x, y)
	switch action {
	case tview.MouseLeftDown:
		if !in {
			return false
		}
		d.vis.line, d.vis.col = line, col
		d.vis.anchor, d.vis.lines, d.vis.dragging = &[2]int{line, col}, false, true
	case tview.MouseMove:
		if d.vis.dragging && in {
			d.vis.line, d.vis.col = line, col
		}
	case tview.MouseLeftUp:
		d.vis.dragging = false
		if a := d.vis.anchor; a != nil && a[0] == d.vis.line && a[1] == d.vis.col {
			// A click and no drag: the cursor goes there, nothing is
			// selected.
			d.vis.anchor = nil
		}
	default:
		return in
	}
	return true
}

// drawVisual lays the cursor and the selection over the comment as drawn.
func (d *commentsDialog) drawVisual(screen tcell.Screen) {
	lines := d.visualLines()
	if len(lines) == 0 {
		return
	}
	inX, top, inW, room := d.view.GetInnerRect()
	scroll, _ := d.view.GetScrollOffset()
	from, to, selecting := d.selection()
	fill := role("comments.visual_selection")
	paint := func(i, col int, style func(tcell.Style) tcell.Style) {
		l := lines[i]
		if l.row < 0 {
			return
		}
		y := top + l.row - scroll
		if y < top || y >= top+room {
			return
		}
		x := inX + l.lead
		r := []rune(l.text)
		for c := 0; c < col && c < len(r); c++ {
			x += textCells(string(r[c]))
		}
		if x >= inX+inW {
			return
		}
		ch, comb, st, _ := screen.GetContent(x, y)
		screen.SetContent(x, y, ch, comb, style(st))
	}
	if selecting {
		for i := from[0]; i <= to[0]; i++ {
			start, end := 0, len([]rune(lines[i].text))
			if i == from[0] {
				start = from[1]
			}
			if i == to[0] {
				end = min(to[1], end)
			}
			for c := start; c < end; c++ {
				paint(i, c, func(st tcell.Style) tcell.Style { return st.Background(fill) })
			}
		}
	}
	paint(d.vis.line, d.vis.col, func(st tcell.Style) tcell.Style { return st.Reverse(true) })
}
