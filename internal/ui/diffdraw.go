package ui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A diff is drawn as IntelliJ draws one: only the places that changed, a
// few lines of context round each, the old and the new line numbers in a
// gutter, an added line on a green fill and a deleted one on a red, and
// the code in the colours of its language. The colours are the theme's
// roles (diff.*, syntax.*), never a highlighter's own style.

// diffLine is one line of a diff as it is drawn.
type diffLine struct {
	kind     byte // ' ' context, '+' added, '-' deleted, '@' a hunk's start, '\\' a note
	old, new int  // line numbers, 0 where the side has none
	text     string
}

// diffLimit is how many lines of a diff are drawn; a file changed whole is
// read in an editor, not here.
const diffLimit = 4000

// parseDiff reads a unified diff of one file into its lines, the headers
// before the first hunk left out.
func parseDiff(diff string) []diffLine {
	var lines []diffLine
	old, new := 0, 0
	inHunk := false
	for _, raw := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(raw, "@@") {
			inHunk = true
			old, new = hunkStart(raw)
			lines = append(lines, diffLine{kind: '@', text: hunkContext(raw)})
			continue
		}
		if !inHunk || raw == "" {
			continue
		}
		switch raw[0] {
		case '+':
			lines = append(lines, diffLine{kind: '+', new: new, text: raw[1:]})
			new++
		case '-':
			lines = append(lines, diffLine{kind: '-', old: old, text: raw[1:]})
			old++
		case ' ':
			lines = append(lines, diffLine{kind: ' ', old: old, new: new, text: raw[1:]})
			old++
			new++
		case '\\':
			lines = append(lines, diffLine{kind: '\\', text: strings.TrimPrefix(raw, "\\ ")})
		}
	}
	return lines
}

// hunkStart is where a hunk begins on each side: "@@ -12,7 +12,8 @@".
func hunkStart(header string) (old, new int) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 1, 1
	}
	at := func(f string) int {
		n, _ := strconv.Atoi(strings.SplitN(strings.TrimLeft(f, "-+"), ",", 2)[0])
		return n
	}
	return at(fields[1]), at(fields[2])
}

// hunkContext is what git says a hunk is in - the function - after its
// header, if anything.
func hunkContext(header string) string {
	if i := strings.Index(header[2:], "@@"); i >= 0 {
		return strings.TrimSpace(header[i+4:])
	}
	return ""
}

// diffNote says why a diff has no lines to draw.
func diffNote(diff string) string {
	switch {
	case strings.Contains(diff, "Binary files"):
		return "a binary file: there are no lines to show"
	case strings.TrimSpace(diff) == "":
		return "nothing differs from the last commit"
	}
	return "no line changed: only the file's mode, or its name"
}

// drawDiff is a file's diff as markup for a text view that does not wrap,
// each line padded to width so its fill reaches the edge.
func drawDiff(path, diff string, width int) string {
	lines := parseDiff(diff)
	if len(lines) == 0 {
		return tag(colDim) + esc(diffNote(diff)) + tagEnd
	}
	more := 0
	if len(lines) > diffLimit {
		more = len(lines) - diffLimit
		lines = lines[:diffLimit]
	}
	numW := 1
	for _, l := range lines {
		numW = max(numW, len(strconv.Itoa(max(l.old, l.new))))
	}
	codes := highlightLines(path, lines)
	var b strings.Builder
	for i, l := range lines {
		number := func(n int) string {
			if n == 0 {
				return strings.Repeat(" ", numW)
			}
			return fmt.Sprintf("%*d", numW, n)
		}
		gutter := number(l.old) + " " + number(l.new) + " "
		used := len(gutter) + 2 + tview.TaggedStringWidth(codes[i])
		pad := strings.Repeat(" ", max(width-used, 0))
		switch l.kind {
		case '@':
			// The first part needs nothing to set it off.
			if i == 0 {
				continue
			}
			text := strings.Repeat(" ", len(gutter)) + "  " + l.text
			b.WriteString(tag(role("diff.hunk")) + esc(strings.Repeat("┄", 2)+" "+text) + tagEnd)
		case '\\':
			b.WriteString(tag(colDim) + esc(strings.Repeat(" ", len(gutter))+"  "+l.text) + tagEnd)
		case '+', '-':
			mark, fill := role("diff.added"), role("diff.added_fill")
			if l.kind == '-' {
				mark, fill = role("diff.deleted"), role("diff.deleted_fill")
			}
			b.WriteString(onFill(tag(role("diff.line_number"))+gutter+tagEnd+
				tag(mark)+string(l.kind)+" "+tagEnd+codes[i]+pad, fill))
		default:
			b.WriteString(tag(role("diff.line_number")) + gutter + tagEnd + "  " + codes[i])
		}
		b.WriteByte('\n')
	}
	if more > 0 {
		fmt.Fprintf(&b, "%s… %d more lines - open the file to read them%s\n", tag(colDim), more, tagEnd)
	}
	return b.String()
}

// onFill puts markup on a background: every colour tag in it keeps its ink
// and takes the fill, so a highlighted word does not punch a hole in it.
func onFill(markup string, fill tcell.Color) string {
	bg := fill.String()
	var b strings.Builder
	b.WriteString("[:" + bg + "]")
	rest := markup
	for {
		i := strings.Index(rest, "[")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		j := strings.Index(rest[i:], "]")
		if j < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		inner := rest[i+1 : i+j]
		switch {
		case inner == "-":
			b.WriteString("[" + colText.String() + ":" + bg + "]")
		case isColourTag(inner):
			b.WriteString("[" + strings.SplitN(inner, ":", 2)[0] + ":" + bg + "]")
		default:
			b.WriteString(rest[i : i+j+1])
		}
		rest = rest[i+j+1:]
	}
	b.WriteString("[-:-:-]")
	return b.String()
}

// isColourTag tells a colour tag ("#aabbcc", "red", "-") from an escaped
// bracket ("[[]" leaves "["), which has none of a tag's shape.
func isColourTag(inner string) bool {
	if inner == "" || strings.HasSuffix(inner, "[") {
		return false
	}
	return !strings.ContainsAny(inner, " \"'")
}

// lexerCache holds a lexer per file name's extension: finding one walks
// every language there is.
var lexerCache sync.Map

// lexerFor is the language of a file, by its name; nil when none is known.
func lexerFor(path string) chroma.Lexer {
	if l, ok := lexerCache.Load(path); ok {
		lexer, _ := l.(chroma.Lexer)
		return lexer
	}
	lexer := lexers.Match(path)
	if lexer != nil {
		lexer = chroma.Coalesce(lexer)
	}
	lexerCache.Store(path, lexer)
	return lexer
}

// highlightLines colours the code of each line, as markup. Each hunk is
// read whole, so a string or a comment that spans lines is seen as one.
func highlightLines(path string, lines []diffLine) []string {
	out := make([]string, len(lines))
	lexer := lexerFor(path)
	start := 0
	flush := func(end int) {
		if start >= end {
			return
		}
		texts := make([]string, 0, end-start)
		for _, l := range lines[start:end] {
			texts = append(texts, strings.ReplaceAll(l.text, "\t", "    "))
		}
		for i, m := range highlight(lexer, texts) {
			out[start+i] = m
		}
	}
	for i, l := range lines {
		if l.kind == '@' || l.kind == '\\' {
			flush(i)
			start = i + 1
		}
	}
	flush(len(lines))
	return out
}

// highlight colours lines of code read as one text, back as one markup a
// line; plain where the language is not known.
func highlight(lexer chroma.Lexer, texts []string) []string {
	out := make([]string, len(texts))
	if lexer == nil {
		for i, t := range texts {
			out[i] = esc(t)
		}
		return out
	}
	it, err := lexer.Tokenise(nil, strings.Join(texts, "\n")+"\n")
	if err != nil {
		for i, t := range texts {
			out[i] = esc(t)
		}
		return out
	}
	line := 0
	var b strings.Builder
	for _, tok := range it.Tokens() {
		ink := syntaxRole(tok.Type)
		parts := strings.Split(tok.Value, "\n")
		for k, part := range parts {
			if k > 0 {
				if line < len(out) {
					out[line] = b.String()
				}
				line++
				b.Reset()
			}
			if part == "" {
				continue
			}
			if ink == "" {
				b.WriteString(esc(part))
			} else {
				b.WriteString(tag(role(ink)) + esc(part) + "[-]")
			}
		}
	}
	if line < len(out) && b.Len() > 0 {
		out[line] = b.String()
	}
	return out
}

// syntaxRole is the role a kind of token is drawn in, "" for the text's
// own colour.
func syntaxRole(t chroma.TokenType) string {
	switch {
	case t.InCategory(chroma.Comment):
		return "syntax.comment"
	case t == chroma.KeywordType || t == chroma.NameClass || t == chroma.NameBuiltin:
		return "syntax.type"
	case t.InCategory(chroma.Keyword):
		return "syntax.keyword"
	case t.InSubCategory(chroma.LiteralString):
		return "syntax.string"
	case t.InSubCategory(chroma.LiteralNumber):
		return "syntax.number"
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return "syntax.function"
	}
	return ""
}
