package ui

import (
	"regexp"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Comments are drawn the way Incomm draws them: one bordered box per message,
// the author and time in its first row, replies nested under what they answer.

const (
	// defaultBubbleWidth is used until the view says how wide it really is.
	defaultBubbleWidth = 72
	minBubbleWidth     = 24
)

// bubble is one message.
type bubble struct {
	Name   string      // who wrote it, plain text
	Colour tcell.Color // the colour of the name and of the border
	Meta   string      // tview markup after the name: when, what state
	Where  string      // plain text: the file and line it is anchored to; on the header when it fits, on its own row when not
	Body   string      // markdown
	Indent int         // cells of nesting, for a reply
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

func (s ink) restore() string {
	dash := func(v string) string {
		if v == "" {
			return "-"
		}
		return v
	}
	return "[" + dash(s.fg) + ":" + dash(s.bg) + ":" + dash(s.attr) + "]"
}

const inkReset = "[-:-:-]"

// renderBubble draws one message in a box that is width cells wide, the
// indent included.
func renderBubble(b bubble, width int) string {
	total := max(width-b.Indent, minBubbleWidth)
	inner := total - 4
	lead := strings.Repeat(" ", b.Indent)
	edge := tag(b.Colour) + "│" + inkReset
	row := func(st *ink, text string) string {
		pad := strings.Repeat(" ", max(0, inner-tview.TaggedStringWidth(text)))
		line := st.restore() + text + inkReset + pad
		st.scan(text)
		return lead + edge + " " + line + " " + edge
	}
	rule := func(l, r string) string {
		return lead + tag(b.Colour) + l + strings.Repeat("─", total-2) + r + inkReset
	}

	head := tag(b.Colour) + "[::b]" + tview.Escape(trunc(b.Name, inner)) + inkReset
	if b.Meta != "" {
		if with := head + "  " + b.Meta; tview.TaggedStringWidth(with) <= inner {
			head = with
		}
	}
	// The place is the longest thing a header can carry, so it never costs the
	// time and the state their room: it joins them when it fits and has a row
	// of its own when it does not, cut from the left because the end of a path
	// is the part that says which file.
	var where string
	if b.Where != "" {
		place := tag(colDim) + " · " + tview.Escape(b.Where) + tagEnd
		if with := head + place; tview.TaggedStringWidth(with) <= inner {
			head = with
		} else {
			where = tag(colDim) + tview.Escape(truncLeft(b.Where, inner)) + tagEnd
		}
	}

	rows := []string{rule("╭", "╮"), row(&ink{}, head)}
	if where != "" {
		rows = append(rows, row(&ink{}, where))
	}
	if body := strings.TrimRight(renderMarkdown(b.Body, ""), "\n"); body != "" {
		st := &ink{}
		for _, line := range strings.Split(body, "\n") {
			for _, part := range tview.WordWrap(line, inner) {
				rows = append(rows, row(st, part))
			}
		}
	}
	rows = append(rows, rule("╰", "╯"))
	return strings.Join(rows, "\n")
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
