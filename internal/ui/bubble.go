package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Comments are drawn as cards: each message a box filled with the card
// background, its frame in the comments' own colour, the author, the time
// and where it is anchored written into the top of the frame, as the
// sandboxes' cards carry their titles; replies nested under what they
// answer, in a quieter frame.

const (
	// defaultBubbleWidth is used until the view says how wide it really is.
	defaultBubbleWidth = 72
	minBubbleWidth     = 24
)

// bubble is one message.
type bubble struct {
	Name   string      // who wrote it, plain text
	Icon   string      // drawn before the name; the person glyph when empty
	Colour tcell.Color // the colour of the name
	Meta   string      // tview markup after the name: when, what state
	Where  string      // plain text: the file and line it is anchored to; on the frame when it fits, on its own row when not
	Body   string      // markdown
	Indent int         // cells of nesting, for a reply
	// Frame is the colour of the frame, its thread's; unset, the plain
	// comment's (comments.border).
	Frame tcell.Color
	// Selected is the comment chosen in the conversation, framed as a
	// focused box is.
	Selected bool
	// Clip shows no more than that many rows of the body, from Offset: a
	// comment never fills the view, and the rest is read with a cursor in
	// it. 0 shows it all.
	Clip, Offset int
}

// bubbleLayout is a card as drawn: its rows, the row its body starts at,
// and every row of the body, shown or not, as markup.
type bubbleLayout struct {
	text   string
	bodyAt int
	body   []string
}

// tview's own tag grammar: [foreground:background:attributes], any part may be
// left out, "-" resets it. "[]" is an escape, not a tag.
var styleTag = regexp.MustCompile(`\[([a-zA-Z]+|#[0-9a-fA-F]{6}|-)?(:([a-zA-Z]+|#[0-9a-fA-F]{6}|-)?(:([lbidrus]+|-)?)?)?\]`)

// ink is the style a run of text has reached. A wrapped line ends in the box's
// border, which has to be drawn in the border's colour and so cannot leave the
// style of the text in place; the next line puts it back.
type ink struct{ fg, bg, attr string }

func (s *ink) scan(text string) {
	for _, m := range styleTag.FindAllStringSubmatch(text, -1) {
		if m[0] == "[]" {
			continue
		}
		set := func(cur *string, v string) {
			switch {
			case v == "":
			case v == "-":
				*cur = ""
			default:
				*cur = v
			}
		}
		set(&s.fg, m[1])
		set(&s.bg, m[3])
		switch attr := m[5]; {
		case attr == "":
		case attr == "-":
			s.attr = ""
		default:
			for _, c := range attr {
				if !strings.ContainsRune(s.attr, c) {
					s.attr += string(c)
				}
			}
		}
	}
}

// restore puts the style back on a card whose background is bg.
func (s ink) restore(bg string) string {
	dash := func(v, none string) string {
		if v == "" {
			return none
		}
		return v
	}
	return "[" + dash(s.fg, "-") + ":" + dash(s.bg, bg) + ":" + dash(s.attr, "-") + "]"
}

const inkReset = "[-:-:-]"

// onCard keeps markup on a card: a tag that resets the background to the
// view's would punch a hole in the card's fill, so it resets to the card's.
func onCard(text, bg string) string {
	return styleTag.ReplaceAllStringFunc(text, func(t string) string {
		m := styleTag.FindStringSubmatch(t)
		if t == "[]" || m[3] != "-" {
			return t
		}
		return "[" + m[1] + ":" + bg + m[4] + "]"
	})
}

// renderBubble draws one message as a card that is width cells wide, the
// indent included.
func renderBubble(b bubble, width int) string { return layBubble(b, width).text }

// layBubble draws a card and says where its body is.
func layBubble(b bubble, width int) bubbleLayout {
	total := max(width-b.Indent, minBubbleWidth)
	inner := total - 4
	lead := strings.Repeat(" ", b.Indent)
	bg := role("comments.card").String()
	if b.Selected {
		bg = role("comments.selected_fill").String()
	}
	reset := "[-:" + bg + ":-]"
	frame := b.Frame
	switch {
	case b.Selected:
		frame = role("comments.selected")
	case frame == tcell.ColorDefault:
		frame = role("comments.border")
	}
	line := func(text string) string { return "[" + frame.String() + ":" + bg + ":-]" + text }
	edge := line(string(tview.Borders.Vertical))
	row := func(st *ink, text string) string {
		pad := strings.Repeat(" ", max(0, inner-tview.TaggedStringWidth(text)))
		out := lead + edge + reset + " " + st.restore(bg) + onCard(text, bg) + reset + pad + " " + edge + inkReset
		st.scan(text)
		return out
	}
	rule := func(n int) string { return strings.Repeat(string(tview.Borders.Horizontal), max(0, n)) }

	// The top of the frame carries the title, ╭─ icon name · meta ── where ─╮,
	// in what is left after the corners, a rule and a space each side.
	room := total - 6
	icon := b.Icon
	if icon == "" {
		icon = glyphUser
	}
	title := tag(role("comments.icon")) + icon + " " + tag(b.Colour) + "[::b]" + tview.Escape(trunc(b.Name, max(1, room-cells(icon)-1))) + "[::-]" + tagEnd
	if b.Meta != "" {
		if with := title + tag(role("comments.meta")) + " · " + tagEnd + b.Meta; tview.TaggedStringWidth(with) <= room {
			title = with
		}
	}
	// The place is the longest thing a title can carry, so it never costs the
	// time and the state their room: it goes to the right of the frame when
	// it fits and has a row of its own when it does not, cut from the left
	// because the end of a path is the part that says which file.
	var right, where string
	if b.Where != "" {
		place := tag(role("comments.where")) + tview.Escape(b.Where) + tagEnd
		if tview.TaggedStringWidth(title)+tview.TaggedStringWidth(place)+3 <= room {
			right = place
		} else {
			where = tag(role("comments.where")) + tview.Escape(truncLeft(b.Where, inner)) + tagEnd
		}
	}
	title = onCard(title, bg)
	tw := tview.TaggedStringWidth(title)
	top := lead + line(string(tview.Borders.TopLeft)+string(tview.Borders.Horizontal)+" ") + reset + title + reset + " "
	if right != "" {
		top += line(rule(room-tw-tview.TaggedStringWidth(right)-2)+" ") + reset + right + reset + " "
	} else {
		top += line(rule(room - tw))
	}
	top += line(string(tview.Borders.Horizontal)+string(tview.Borders.TopRight)) + inkReset

	rows := []string{top}
	if where != "" {
		rows = append(rows, row(&ink{}, where))
	}
	layout := bubbleLayout{bodyAt: len(rows)}
	if body := strings.TrimRight(renderMarkdown(b.Body, ""), "\n"); body != "" {
		for _, l := range strings.Split(body, "\n") {
			layout.body = append(layout.body, tview.WordWrap(l, inner)...)
		}
	}
	from, to := 0, len(layout.body)
	if b.Clip > 0 && len(layout.body) > b.Clip {
		from = max(0, min(b.Offset, len(layout.body)-b.Clip))
		to = from + b.Clip
	}
	// A row hidden above still sets the style the next ones are drawn in.
	st := &ink{}
	for i, part := range layout.body {
		if i < from || i >= to {
			st.scan(part)
			continue
		}
		rows = append(rows, row(st, part))
	}
	bottom := string(tview.Borders.BottomLeft) + rule(total-2) + string(tview.Borders.BottomRight)
	if from > 0 || to < len(layout.body) {
		// What is not shown is said in the middle of the frame under it.
		above, below := from, len(layout.body)-to
		var said string
		switch {
		case above > 0 && below > 0:
			said = fmt.Sprintf("%d above · %d more lines", above, below)
		case above > 0:
			said = fmt.Sprintf("%d lines above", above)
		default:
			said = fmt.Sprintf("%d more lines", below)
		}
		label := " " + glyphElided + " " + said + " " + glyphElided + " "
		if w := textCells(label); w < total-4 {
			left := (total - 2 - w) / 2
			rows = append(rows, lead+line(string(tview.Borders.BottomLeft)+rule(left))+reset+tag(role("comments.meta"))+esc(label)+tagEnd+
				line(rule(total-2-left-w)+string(tview.Borders.BottomRight))+inkReset)
			layout.text = strings.Join(rows, "\n")
			return layout
		}
	}
	rows = append(rows, lead+line(bottom)+inkReset)
	layout.text = strings.Join(rows, "\n")
	return layout
}

// renderBubbles draws a conversation, one box under the other.
func renderBubbles(bubbles []bubble, width int) string {
	parts := make([]string, len(bubbles))
	for i, b := range bubbles {
		parts[i] = renderBubble(b, width)
	}
	return strings.Join(parts, "\n")
}

// widthAware lets a bordered text view lay its content out to the width it is
// drawn at. The returned function takes a render func that is called with the
// inner width now, and again whenever the width changes (the terminal was
// resized, the columns stacked), keeping the scroll position. Passing nil goes
// back to plain text the caller sets itself.
//
// The view is expected to have a border and one cell of padding on each side, as
// every text view in the interface does; the draw func below returns the same
// content rectangle tview would have worked out.
func (a *App) widthAware(view *tview.TextView) func(render func(width int) string) {
	var render func(int) string
	width := -1
	apply := func(w int) {
		if render == nil {
			return
		}
		row, col := view.GetScrollOffset()
		view.SetText(render(w))
		view.ScrollTo(row, col)
	}
	view.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		inner := max(0, w-4)
		if inner != width {
			width = inner
			if render != nil {
				go a.tv.QueueUpdateDraw(func() { apply(inner) })
			}
		}
		return x + 2, y + 1, inner, max(0, h-2)
	})
	return func(r func(width int) string) {
		render = r
		switch {
		case r == nil:
		case width > 0:
			view.SetText(r(width))
		default:
			view.SetText(r(defaultBubbleWidth))
		}
	}
}

// truncLeft shortens s to at most n cells by cutting its beginning.
func truncLeft(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(n-1):])
}
