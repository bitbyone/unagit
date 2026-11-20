package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// A merge request's conversation is drawn as GitLab draws it: a thread on
// code is one block, framed, the lines it is about at the top - a few
// before the commented ones and more after, since a comment goes on the
// first line of what it is about - and under them its comments as cards,
// and the forge's notes that a later version changed the line. Any other
// thread is its comments as cards. The code is the diff the comment was
// written on, which numbers its lines as the comment does; a later
// version of the merge request may number them differently.

// Lines of the diff a thread on code shows round what it is on.
const (
	codeBefore = 3
	codeAfter  = 4
)

// commentCard is where a comment stands in the drawn conversation: the
// rows of its card, the row to scroll to so that what it is read with -
// the code a thread is on - shows with it, and the comment.
type commentCard struct {
	top, from, to int
	note          forge.Note
}

// conversation is what the comments dialog draws from: the comments, the
// diffs they are read beside, and what the reader opened.
type conversation struct {
	notes []forge.Note
	// latest is the merge request's diff now; versions the diffs comments
	// were written on, by versionKey; changes the changes later versions
	// made to commented lines, by changeKey, nil while being read.
	latest   []forge.FileDiff
	versions map[string][]forge.FileDiff
	changes  map[string][]forge.FileDiff
	// expanded are the notes of a change whose change is shown.
	expanded map[int]bool
	selected int
}

func versionKey(n forge.Note) string { return n.Base + ".." + n.Head }

func changeKey(ch *forge.LineChange) string { return ch.From + ".." + ch.To }

// diffsOf is the diff a comment was written on, or the latest when that
// one is not known.
func (c *conversation) diffsOf(n forge.Note) []forge.FileDiff {
	if d, ok := c.versions[versionKey(n)]; ok && n.Base != "" {
		return d
	}
	return c.latest
}

// shown are the notes the conversation draws: what people wrote, and the
// forge's notes that a later version changed a commented line.
func shown(notes []forge.Note) []forge.Note {
	var out []forge.Note
	for _, n := range notes {
		switch {
		case n.Changed != nil:
			out = append(out, n)
		case !n.System && strings.TrimSpace(n.Body) != "":
			out = append(out, n)
		}
	}
	return out
}

// renderConversation lays the comments out as the threads they belong to,
// with no code to read them beside.
func renderConversation(notes []forge.Note, width int) string {
	text, _ := (&conversation{notes: notes}).draw(width)
	return text
}

// draw lays the conversation out, oldest thread first, and says where each
// comment's card is.
func (c *conversation) draw(width int) (string, []commentCard) {
	notes := shown(c.notes)
	if len(notes) == 0 {
		return tag(colMuted) + "No comments yet. Press c to write the first one." + tagEnd, nil
	}
	var (
		rows  []string
		cards []commentCard
	)
	add := func(text string) {
		rows = append(rows, strings.Split(text, "\n")...)
	}
	for _, thread := range groupIntoThreads(notes) {
		// A thread whose comment is gone is only the forge's note.
		if thread[0].Changed != nil {
			continue
		}
		if len(rows) > 0 {
			add("")
		}
		top := len(rows)
		if c.drawCodeThread(thread, width, &rows, &cards) {
			cards[len(cards)-len(thread)].top = top
			continue
		}
		for j, b := range threadBubbles(thread, false) {
			b.Selected = thread[j].ID == c.selected
			from := len(rows)
			add(renderBubble(b, width))
			cards = append(cards, commentCard{top: from, from: from, to: len(rows) - 1, note: thread[j]})
		}
		cards[len(cards)-len(thread)].top = top
	}
	return strings.Join(rows, "\n"), cards
}

// drawCodeThread draws a thread on code as one block, when its code is in
// the diff it was written on.
func (c *conversation) drawCodeThread(thread []forge.Note, width int, rows *[]string, cards *[]commentCard) bool {
	root := thread[0]
	lines, from, to := commentedRange(root, c.diffsOf(root))
	if from < 0 {
		return false
	}
	total := max(width, minBubbleWidth)
	inner := total - 4
	frame := tag(role("comments.code_border"))
	edge := frame + string(tview.Borders.Vertical) + inkReset
	add := func(text string) { *rows = append(*rows, text) }
	inside := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			add(edge + " " + line + inkReset + " " + edge)
		}
	}

	add(frameTop(where(root), total, frame))
	start, end := codeWindow(lines, from, to)
	for _, line := range strings.Split(strings.TrimRight(drawDiffLines(root.Path, lines[start:end+1], total-2, from-start, to-start, true), "\n"), "\n") {
		add(edge + line + inkReset + edge)
	}
	add(frame + "├" + strings.Repeat(string(tview.Borders.Horizontal), total-2) + "┤" + inkReset)
	bubbles := threadBubbles(thread, false)
	for j, n := range thread {
		first := len(*rows)
		if n.Changed != nil {
			inside(c.drawChange(n, root, inner))
		} else {
			b := bubbles[j]
			b.Where, b.Selected = "", n.ID == c.selected
			inside(renderBubble(b, inner))
		}
		*cards = append(*cards, commentCard{top: first, from: first, to: len(*rows) - 1, note: n})
	}
	add(frame + string(tview.Borders.BottomLeft) + strings.Repeat(string(tview.Borders.Horizontal), total-2) + string(tview.Borders.BottomRight) + inkReset)
	return true
}

// where names the lines a comment is on: "path:117", or "path:112-117"
// for more than one.
func where(n forge.Note) string {
	number := func(r forge.LineRef) int {
		if r.New > 0 {
			return r.New
		}
		return r.Old
	}
	first, last := number(n.From), number(n.To)
	switch {
	case last == 0:
		return fmt.Sprintf("%s:%d", n.Path, n.Line)
	case first > 0 && first != last:
		return fmt.Sprintf("%s:%d-%d", n.Path, first, last)
	}
	return fmt.Sprintf("%s:%d", n.Path, last)
}

// frameTop is the top of a frame total wide, titled.
func frameTop(title string, total int, frame string) string {
	label := tag(role("comments.where")) + esc(truncLeft(title, total-6)) + tagEnd
	return frame + string(tview.Borders.TopLeft) + string(tview.Borders.Horizontal) + " " + tagEnd + label + frame + " " +
		strings.Repeat(string(tview.Borders.Horizontal), max(0, total-6-tview.TaggedStringWidth(label))) +
		string(tview.Borders.Horizontal) + string(tview.Borders.TopRight) + inkReset
}

// codeWindow is the lines shown round the commented ones, from to to:
// codeBefore before them and codeAfter after, within their hunk.
func codeWindow(lines []diffLine, from, to int) (start, end int) {
	start, end = from, to
	for start > 0 && from-start < codeBefore && lines[start-1].kind != '@' {
		start--
	}
	for end < len(lines)-1 && end-to < codeAfter && lines[end+1].kind != '@' {
		end++
	}
	return start, end
}

// linkInMarkdown is a link written in markdown, its text first.
var linkInMarkdown = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)

// drawChange is the forge's note that a later version changed the line a
// thread is on, one line, and under it the change while it is opened.
func (c *conversation) drawChange(n, root forge.Note, width int) string {
	fill := ""
	if n.ID == c.selected {
		fill = role("comments.selected_fill").String()
	}
	text := linkInMarkdown.ReplaceAllString(strings.TrimSpace(n.Body), "$1")
	open := c.expanded[n.ID]
	hint := "Enter compare"
	if open {
		hint = "Enter hide"
	}
	line := tag(role("comments.meta")) + glyphDot + tagEnd + " " + tag(colText) + "[::b]" + esc(personName(n.Author)) + "[::-]" + tagEnd + " " +
		tag(colMuted) + esc(text) + tagEnd + tag(role("comments.meta")) + " · " + esc(humanAge(n.CreatedAt)) + " · " + hint + tagEnd
	if w := tview.TaggedStringWidth(line); w > width {
		line = tag(role("comments.meta")) + glyphDot + tagEnd + " " + tag(colText) + "[::b]" + esc(trunc(personName(n.Author)+" "+text, width-2)) + "[::-]" + tagEnd
	}
	line += strings.Repeat(" ", max(0, width-tview.TaggedStringWidth(line)))
	if fill != "" {
		line = onFill(line, role("comments.selected_fill"))
	}
	if !open {
		return line
	}
	diffs, read := c.changes[changeKey(n.Changed)]
	switch {
	case !read || diffs == nil:
		return line + "\n" + padMarkup(tag(colDim)+"  Reading the change…"+tagEnd, width)
	}
	change := forge.Note{Path: root.Path, From: n.Changed.Line, To: n.Changed.Line}
	lines, from, to := commentedRange(change, diffs)
	if from < 0 {
		return line + "\n" + padMarkup(tag(colDim)+"  that change no longer has the line"+tagEnd, width)
	}
	start, end := codeWindow(lines, from, to)
	block := strings.TrimRight(drawDiffLines(root.Path, lines[start:end+1], width-2, from-start, to-start, true), "\n")
	var out []string
	for _, l := range strings.Split(block, "\n") {
		out = append(out, "  "+l+inkReset)
	}
	return line + "\n" + strings.Join(out, "\n")
}

// padMarkup pads markup with spaces to width cells.
func padMarkup(markup string, width int) string {
	return markup + strings.Repeat(" ", max(0, width-tview.TaggedStringWidth(markup)))
}

// commentedRange is the diff of the file a comment is on and the indexes
// there of the first and the last line it is on, -1 when they are not in
// it.
func commentedRange(n forge.Note, diffs []forge.FileDiff) ([]diffLine, int, int) {
	if n.Path == "" {
		return nil, -1, -1
	}
	first, last := n.From, n.To
	if last == (forge.LineRef{}) {
		if n.Line <= 0 {
			return nil, -1, -1
		}
		last = forge.LineRef{New: n.Line}
		if n.Orphaned {
			last = forge.LineRef{Old: n.Line}
		}
	}
	if first == (forge.LineRef{}) {
		first = last
	}
	for _, d := range diffs {
		if d.Path != n.Path && d.OldPath != n.Path {
			continue
		}
		lines := parseDiff(d.Diff)
		to := findLine(lines, last)
		if to < 0 {
			return lines, -1, -1
		}
		from := findLine(lines, first)
		if from < 0 || from > to {
			from = to
		}
		return lines, from, to
	}
	return nil, -1, -1
}

// findLine is the index of a line in a diff: the one with both numbers when
// both are named, else the line after on the new side, else before.
func findLine(lines []diffLine, r forge.LineRef) int {
	content := func(l diffLine) bool { return l.kind != '@' && l.kind != '\\' }
	if r.Old > 0 && r.New > 0 {
		for i, l := range lines {
			if content(l) && l.old == r.Old && l.new == r.New {
				return i
			}
		}
	}
	if r.New > 0 {
		for i, l := range lines {
			if content(l) && l.kind != '-' && l.new == r.New {
				return i
			}
		}
	}
	if r.Old > 0 {
		for i, l := range lines {
			if content(l) && l.kind != '+' && l.old == r.Old {
				return i
			}
		}
	}
	return -1
}

// drawFile is a whole file to read, its lines numbered and in the colours
// of its language, the lines from to to (counted from 1) marked.
func drawFile(path, text string, from, to int) string {
	texts := strings.Split(strings.TrimRight(text, "\n"), "\n")
	lines := make([]diffLine, len(texts))
	for i, t := range texts {
		lines[i] = diffLine{kind: ' ', new: i + 1, text: strings.ReplaceAll(t, "\t", "    ")}
	}
	codes := highlightLines(path, lines)
	numW := len(fmt.Sprint(len(lines)))
	var b strings.Builder
	for i, code := range codes {
		number, mark := tag(role("diff.line_number"))+fmt.Sprintf("%*d", numW, i+1)+tagEnd, " "
		if i+1 >= from && i+1 <= to {
			number = tag(role("diff.commented")) + "[::b]" + fmt.Sprintf("%*d", numW, i+1) + "[::-]" + tagEnd
			mark = tag(role("diff.commented")) + glyphCommented + tagEnd
		}
		b.WriteString(mark + number + "  " + code + "\n")
	}
	return b.String()
}
