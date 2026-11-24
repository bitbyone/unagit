package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/draft"
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
	// code is the code a thread is on, which the cursor stops at too, its
	// note the thread's first.
	code bool
	// band is what the cursor's band covers on it - a card, a line - and
	// block, for what is in a thread on code, the block's frame, lit while
	// the cursor is anywhere in it. They are laid over the text as drawn,
	// so a step of the cursor draws the conversation again and no more.
	band  bandArea
	block *bandArea
	// body is every row of a comment's text, shown or not; bodyFrom the
	// row its first shown one is drawn at, offset which one that is, and
	// textLeft the cell its text starts at.
	body             []string
	plain            []string
	bodyFrom, offset int
	textLeft         int
}

// commentClip is how many rows of a comment the conversation shows: a
// longer one is clipped, said under it, and read with a cursor in it.
const commentClip = 15

// laidKey is all a card's layout comes of.
type laidKey struct {
	b     bubble
	width int
}

// laidCard is a comment's card as the conversation draws it: clipped, from
// where the reader left it, and the card that says where its text is.
func (c *conversation) laidCard(b bubble, n forge.Note, width, from, left int) (string, commentCard) {
	b.Clip, b.Offset = commentClip, c.offsets[n.ID]
	key := laidKey{b, width}
	lay, ok := c.laid[key]
	if !ok {
		lay = layBubble(b, width)
		lay.plain = make([]string, len(lay.body))
		for i, part := range lay.body {
			lay.plain[i] = plainRow(part)
		}
		if c.laid == nil {
			c.laid = map[laidKey]bubbleLayout{}
		}
		c.laid[key] = lay
	}
	offset := 0
	if len(lay.body) > commentClip {
		offset = max(0, min(b.Offset, len(lay.body)-commentClip))
	}
	band := cardBand(from, from+strings.Count(lay.text, "\n"), left, b.Indent, width)
	return lay.text, commentCard{top: from, from: from, to: band.to, note: n, band: band,
		body: lay.body, plain: lay.plain, bodyFrom: from + lay.bodyAt, offset: offset, textLeft: band.left + 2}
}

// cardBand is the band of a card width wide, nested by indent, at left.
func cardBand(from, to, left, indent, width int) bandArea {
	return bandArea{from: from, to: to, left: left + indent, width: max(width-indent, minBubbleWidth), lit: true, frame: true}
}

// conversation is what the comments dialog draws from: the comments, the
// code they are read beside, and what the reader opened.
type conversation struct {
	notes []forge.Note
	// drafts are the comments saved in unagit, drawn as threads of their
	// own, not published.
	drafts []draft.Draft
	// base and head are the commits of the merge request's latest version;
	// latest its diff as the forge sends it, for a forge that names no
	// merge base (GitHub) and for a file whose versions cannot be read.
	base, head string
	latest     []forge.FileDiff
	// diffs are single files' diffs between two commits, worked out from
	// the file at each, by diffKey; reading those being worked out now.
	diffs   map[string]forge.FileDiff
	reading map[string]bool
	// expanded are the notes of a change whose change is shown.
	expanded map[int]bool
	selected int
	// noCode draws the threads on code as their comments alone, for
	// reading the conversation through.
	noCode bool
	// offsets are where the reader left a clipped comment, by its id.
	offsets map[int]int
	// onCode is whether the cursor is on the code of the chosen comment's
	// thread rather than on the comment.
	onCode bool
	// reading is whether code is still being read, which a thread
	// without its own yet says.
	pending bool
	// drawn are code blocks already coloured, by codeKey: colouring is the
	// slow part of a draw, and the cursor draws at every step. laid are
	// cards already laid out, by all that makes them: moving a clipped
	// comment's window lays out that one alone again.
	drawn map[string]string
	laid  map[laidKey]bubbleLayout
	// task draws the conversation as a task's on the Board: its comments
	// are the reader's own and unsigned, its story's events (System notes)
	// stand between them as quiet lines, which noCode hides as it hides
	// the code of a merge request's threads. top is drawn above it all,
	// laid out to the width: the task's description and links.
	task bool
	top  func(width int) string
}

// isChosen reports whether a card is the one the cursor is on.
func (c *conversation) isChosen(card commentCard) bool {
	return card.note.ID == c.selected && card.code == c.onCode
}

func diffKey(from, to, path string) string { return from + ".." + to + ":" + path }

// versionOf is the commits of the diff a comment was written on: its own
// when the forge says them, else the latest version's, which a comment
// saved in unagit is on, and GitHub numbers its lines in.
func (c *conversation) versionOf(n forge.Note) (base, head string, ok bool) {
	if n.Base != "" && n.Head != "" {
		return n.Base, n.Head, true
	}
	return c.base, c.head, c.head != ""
}

// diffsOf is the diff a comment was written on, or the latest when that
// one could not be worked out.
func (c *conversation) diffsOf(n forge.Note) []forge.FileDiff {
	if base, head, ok := c.versionOf(n); ok && base != "" {
		if d, ok := c.diffs[diffKey(base, head, n.Path)]; ok {
			return []forge.FileDiff{d}
		}
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
	notes := append(shown(c.notes), draftNotes(c.drafts)...)
	if c.task {
		notes = c.taskNotes()
	}
	var (
		rows  []string
		cards []commentCard
	)
	add := func(text string) {
		rows = append(rows, strings.Split(text, "\n")...)
	}
	if c.top != nil {
		add(c.top(width))
	}
	if len(notes) == 0 {
		if len(rows) > 0 {
			add("")
		}
		add(tag(colMuted) + "No comments yet. Press c to write the first one." + tagEnd)
		return strings.Join(rows, "\n"), nil
	}
	// What is said of the merge request as a whole comes first, the threads
	// on its code after, each part headed when there are both.
	var general, onCode [][]forge.Note
	for _, thread := range groupIntoThreads(notes) {
		// A thread whose comment is gone is only the forge's note.
		switch {
		case thread[0].Changed != nil:
		case thread[0].Path == "":
			general = append(general, thread)
		default:
			onCode = append(onCode, thread)
		}
	}
	both := len(general) > 0 && len(onCode) > 0
	heading := func(title string) {
		if len(rows) > 0 {
			add("")
		}
		label := " " + title + " "
		add(tag(role("comments.heading")) + "[::b]" + strings.Repeat(string(tview.Borders.Horizontal), 2) + label + "[::-]" +
			strings.Repeat(string(tview.Borders.Horizontal), max(0, width-2-len([]rune(label)))) + tagEnd)
	}
	if both {
		heading("On the merge request")
	}
	if c.top != nil {
		heading("Comments")
	}
	story := false
	for _, thread := range general {
		if c.task && thread[0].System {
			// The story's lines go one under the other, a card's
			// width apart from the comments.
			if !story && len(rows) > 0 {
				add("")
			}
			story = true
			add(storyLine(thread[0], width))
			continue
		}
		story = false
		if len(rows) > 0 {
			add("")
		}
		top := len(rows)
		for j, b := range bubblesOf(thread) {
			if c.task {
				// Every comment is the reader's: the card says when.
				b.Name, b.Meta, b.Icon = humanAge(thread[j].CreatedAt), "", glyphComment
			}
			text, card := c.laidCard(b, thread[j], width, len(rows), 0)
			add(text)
			cards = append(cards, card)
		}
		cards[len(cards)-len(thread)].top = top
	}
	if both {
		heading("On the code")
	}
	for _, thread := range onCode {
		if len(rows) > 0 {
			add("")
		}
		top := len(rows)
		first := len(cards)
		if c.noCode {
			c.drawCards(thread, width, &rows, &cards)
		} else {
			c.drawCodeThread(thread, width, &rows, &cards)
		}
		cards[first].top = top
	}
	return strings.Join(rows, "\n"), cards
}

// taskNotes are a task's comments and, unless noCode hides them, the
// lines of its story.
func (c *conversation) taskNotes() []forge.Note {
	var out []forge.Note
	for _, n := range c.notes {
		if n.System && c.noCode || strings.TrimSpace(n.Body) == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

// storyLine is a line of a task's story: what happened and when, quiet.
func storyLine(n forge.Note, width int) string {
	text := glyphDot + " " + n.Body + " · " + humanAge(n.CreatedAt)
	return tag(colDim) + esc(trunc(text, max(1, width))) + tagEnd
}

// drawCodeThread draws a thread on code as one block: the code, when its
// lines are in the diff it was written on, then the comments.
func (c *conversation) drawCodeThread(thread []forge.Note, width int, rows *[]string, cards *[]commentCard) bool {
	root := thread[0]
	if root.Path == "" {
		return false
	}
	lines, from, to := commentedRange(root, c.diffsOf(root))
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

	codeFrom := len(*rows)
	add(frameTop(where(root), total, frame))
	switch {
	case from < 0 && c.pending:
		inside(padMarkup(tag(colDim)+"Reading the code…"+tagEnd, inner))
	case from < 0:
		inside(padMarkup(tag(colDim)+"the code is not in the diff it was written on"+tagEnd, inner))
	default:
		start, end := codeWindow(lines, from, to)
		key := fmt.Sprintf("%d/%d/%d/%d/%d/%d", root.ID, total, start, end, from, to)
		code, ok := c.drawn[key]
		if !ok {
			code = strings.TrimRight(drawDiffLines(root.Path, lines[start:end+1], total-2, from-start, to-start, true), "\n")
			if c.drawn == nil {
				c.drawn = map[string]string{}
			}
			c.drawn[key] = code
		}
		for _, line := range strings.Split(code, "\n") {
			add(edge + line + inkReset + edge)
		}
	}
	firstCard := len(*cards)
	*cards = append(*cards, commentCard{top: codeFrom, from: codeFrom, to: len(*rows) - 1, note: root, code: true})
	bubbles := bubblesOf(thread)
	for j, n := range thread {
		first := len(*rows)
		var band bandArea
		if n.Changed != nil {
			inside(c.drawChange(n, root, inner, false))
			band = bandArea{left: 2, width: inner, lit: true}
		} else {
			b := bubbles[j]
			b.Where = ""
			text, card := c.laidCard(b, n, inner, first, 2)
			inside(text)
			*cards = append(*cards, card)
			continue
		}
		band.from, band.to = first, len(*rows)-1
		*cards = append(*cards, commentCard{top: first, from: first, to: len(*rows) - 1, note: n, band: band})
	}
	add(frame + string(tview.Borders.BottomLeft) + strings.Repeat(string(tview.Borders.Horizontal), total-2) + string(tview.Borders.BottomRight) + inkReset)
	// The block is lit while the cursor is anywhere in it - on its code or
	// on any of its comments, which are part of it; on its code, that is
	// all there is to light.
	block := &bandArea{from: codeFrom, to: len(*rows) - 1, width: total, frame: true}
	for k := firstCard; k < len(*cards); k++ {
		(*cards)[k].block = block
	}
	(*cards)[firstCard].band = *block
	return true
}

// drawCards draws a thread as its comments alone, the place it is on in
// the first one's frame, and the forge's notes of a change as a line each.
func (c *conversation) drawCards(thread []forge.Note, width int, rows *[]string, cards *[]commentCard) {
	bubbles := bubblesOf(thread)
	for j, n := range thread {
		from := len(*rows)
		text := ""
		band := bandArea{width: width, lit: true}
		if n.Changed != nil {
			text = c.drawChange(n, thread[0], width, false)
		} else {
			b := bubbles[j]
			if j == 0 && b.Where != "" {
				b.Where = where(n)
			}
			text, card := c.laidCard(b, n, width, from, 0)
			*rows = append(*rows, strings.Split(text, "\n")...)
			*cards = append(*cards, card)
			continue
		}
		*rows = append(*rows, strings.Split(text, "\n")...)
		band.from, band.to = from, len(*rows)-1
		*cards = append(*cards, commentCard{top: from, from: from, to: len(*rows) - 1, note: n, band: band})
	}
}

// bubblesOf is a thread as cards, a comment saved in unagit saying so.
func bubblesOf(thread []forge.Note) []bubble {
	bubbles := threadBubbles(thread, false)
	for j, n := range thread {
		if n.ID < 0 {
			bubbles[j].Meta = tag(colWarn) + "saved · not published" + tagEnd
		}
	}
	return bubbles
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
func (c *conversation) drawChange(n, root forge.Note, width int, selected bool) string {
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
	if selected {
		line = onFill(line, role("comments.selected_fill"))
	}
	if !open {
		return line
	}
	key := diffKey(n.Changed.From, n.Changed.To, root.Path)
	diff, read := c.diffs[key]
	if !read {
		return line + "\n" + padMarkup(tag(colDim)+"  Reading the change…"+tagEnd, width)
	}
	change := forge.Note{Path: root.Path, From: n.Changed.Line, To: n.Changed.Line}
	lines, from, to := commentedRange(change, []forge.FileDiff{diff})
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
