// Package md renders markdown into tview markup.
//
// Comments on a merge request are markdown, and reading them as raw asterisks
// and backticks is miserable. This turns the structure into styling - bold,
// lists, quotes, code - and leaves the wrapping to the widget, so the text
// reflows with the pane instead of being hard wrapped at a fixed width.
package md

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Theme is the palette the renderer paints with, so the caller stays in
// charge of the look.
type Theme struct {
	Text    string // body text
	Heading string
	Code    string
	Quote   string
	Link    string
	Muted   string
}

// DefaultTheme is a muted set that suits a dark terminal.
var DefaultTheme = Theme{
	Text:    "#d0d0d0",
	Heading: "#87afaf",
	Code:    "#afaf87",
	Quote:   "#8a8a8a",
	Link:    "#87afaf",
	Muted:   "#8a8a8a",
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Render turns markdown into tview markup with the default theme.
func Render(source string) string { return RenderTheme(source, DefaultTheme) }

// RenderTheme turns markdown into tview markup.
func RenderTheme(source string, theme Theme) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	src := []byte(source)
	doc := parser.Parser().Parse(text.NewReader(src))

	r := &renderer{src: src, theme: theme}
	r.walk(doc, 0)
	out := strings.Trim(r.b.String(), "\n")
	if out == "" {
		return ""
	}
	return out
}

type renderer struct {
	b     strings.Builder
	src   []byte
	theme Theme
	// pending collects plain text so that it can be escaped as a whole.
	// goldmark splits text at every bracket it considered for a link, so
	// escaping chunk by chunk would miss the pairs that span a split.
	pending strings.Builder
	// listStack counts the items of the ordered lists being rendered.
	listStack []int
}

// flush writes the collected plain text, escaped so tview does not read a
// "[0]" in it as a colour tag.
func (r *renderer) flush() {
	if r.pending.Len() == 0 {
		return
	}
	r.b.WriteString(tview.Escape(r.pending.String()))
	r.pending.Reset()
}

func (r *renderer) write(s string)            { r.b.WriteString(s) }
func (r *renderer) writef(f string, a ...any) { fmt.Fprintf(&r.b, f, a...) }
func (r *renderer) colour(c, s string) string { return "[" + c + "]" + s + "[-]" }
func (r *renderer) indent(depth int) string   { return strings.Repeat("  ", depth) }
func (r *renderer) escape(s string) string    { return tview.Escape(s) }
func (r *renderer) lines(s string) []string   { return strings.Split(s, "\n") }
func (r *renderer) blank()                    { r.trimTrailingBlank(); r.write("\n\n") }

// trimTrailingBlank keeps paragraphs one blank line apart, however the source
// was spaced.
func (r *renderer) trimTrailingBlank() {
	s := strings.TrimRight(r.b.String(), "\n")
	r.b.Reset()
	r.b.WriteString(s)
}

// walk renders a node and its children at the given list depth.
func (r *renderer) walk(n ast.Node, depth int) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.node(c, depth)
	}
}

func (r *renderer) node(n ast.Node, depth int) {
	switch n := n.(type) {
	case *ast.Heading:
		r.write(r.indent(depth))
		r.write("[" + r.theme.Heading + "::b]")
		r.inline(n)
		r.write("[-:-:-]")
		r.blank()

	case *ast.Paragraph:
		r.write(r.indent(depth))
		r.inline(n)
		r.blank()

	case *ast.TextBlock: // paragraphs inside tight list items
		r.inline(n)
		r.write("\n")

	case *ast.List:
		r.listStack = append(r.listStack, n.Start)
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			r.listItem(n, item, depth)
		}
		r.listStack = r.listStack[:len(r.listStack)-1]
		if depth == 0 {
			r.blank()
		}

	case *ast.Blockquote:
		var inner renderer
		inner.src, inner.theme = r.src, r.theme
		inner.walk(n, 0)
		for _, line := range r.lines(strings.Trim(inner.b.String(), "\n")) {
			r.write(r.indent(depth) + r.colour(r.theme.Quote, "│ ") + line + "\n")
		}
		r.blank()

	case *ast.FencedCodeBlock:
		r.codeBlock(n.Lines(), depth, string(n.Language(r.src)))

	case *ast.CodeBlock:
		r.codeBlock(n.Lines(), depth, "")

	case *ast.ThematicBreak:
		r.write(r.indent(depth) + r.colour(r.theme.Muted, strings.Repeat("─", 24)))
		r.blank()

	case *ast.HTMLBlock:
		// Raw HTML in a comment is noise; keep the text, drop the tags.
		r.write(r.indent(depth) + r.colour(r.theme.Muted, r.escape(strings.TrimSpace(string(n.Lines().Value(r.src))))))
		r.blank()

	default:
		r.walk(n, depth)
	}
}

func (r *renderer) listItem(list *ast.List, item ast.Node, depth int) {
	marker := "• "
	if list.IsOrdered() {
		n := len(r.listStack) - 1
		marker = fmt.Sprintf("%d. ", r.listStack[n])
		r.listStack[n]++
	}
	if li, ok := item.(*ast.ListItem); ok {
		if check, ok := li.FirstChild().(*ast.TextBlock); ok && check.FirstChild() != nil {
			if box, ok := check.FirstChild().(*east.TaskCheckBox); ok {
				marker = "☐ "
				if box.IsChecked {
					marker = "✓ "
				}
			}
		}
	}

	var inner renderer
	inner.src, inner.theme = r.src, r.theme
	inner.walk(item, 0)
	body := strings.Trim(inner.b.String(), "\n")
	if body == "" {
		return
	}
	for i, line := range r.lines(body) {
		prefix := r.indent(depth) + r.colour(r.theme.Muted, marker)
		if i > 0 {
			prefix = r.indent(depth) + strings.Repeat(" ", len([]rune(marker)))
		}
		r.write(prefix + line + "\n")
	}
}

func (r *renderer) codeBlock(lines *text.Segments, depth int, lang string) {
	if lang != "" {
		r.write(r.indent(depth+1) + r.colour(r.theme.Muted, r.escape(lang)) + "\n")
	}
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		text := strings.TrimRight(string(line.Value(r.src)), "\n")
		r.write(r.indent(depth+1) + r.colour(r.theme.Code, r.escape(text)) + "\n")
	}
	r.blank()
}

// inline renders the text of a node with its emphasis.
func (r *renderer) inline(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			r.pending.Write(c.Segment.Value(r.src))
			if c.SoftLineBreak() {
				r.pending.WriteString(" ")
			}
			if c.HardLineBreak() {
				r.pending.WriteString("\n")
			}

		case *ast.String:
			r.pending.Write(c.Value)

		case *ast.Emphasis:
			r.flush()
			style := "[::i]"
			if c.Level == 2 {
				style = "[::b]"
			}
			r.write(style)
			r.inline(c)
			r.write("[::-]")

		case *east.Strikethrough:
			r.flush()
			r.write("[::s]")
			r.inline(c)
			r.write("[::-]")

		case *ast.CodeSpan:
			r.flush()
			r.write("[" + r.theme.Code + "]")
			r.inline(c)
			r.write("[-]")

		case *ast.Link:
			r.flush()
			// Colour only: tview's "u" flag sets tcell's underline style and
			// then overwrites the attribute mask it lives beside, so the flag
			// that would turn it off never sees it again and the underline
			// runs to the end of the text.
			r.write("[" + r.theme.Link + "]")
			r.inline(c)
			r.write("[-]")
			if dest := string(c.Destination); dest != "" {
				r.write(" " + r.colour(r.theme.Muted, r.escape(dest)))
			}

		case *ast.AutoLink:
			r.flush()
			r.write(r.colour(r.theme.Link, r.escape(string(c.URL(r.src)))))

		case *ast.Image:
			r.flush()
			r.write(r.colour(r.theme.Muted, "[image[] "))
			r.inline(c)

		case *east.TaskCheckBox:
			// The marker is drawn by the list item.

		case *ast.RawHTML:
			// Dropped: the text around it is what matters.

		default:
			r.inline(c)
		}
	}
	r.flush()
}
