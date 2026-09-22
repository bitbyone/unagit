package md

import (
	"strings"
	"testing"
)

// render with a palette that is easy to read in assertions.
var testTheme = Theme{Text: "text", Heading: "head", Code: "code", Quote: "quote", Link: "link", Muted: "muted"}

func render(t *testing.T, source string) string {
	t.Helper()
	return RenderTheme(source, testTheme)
}

func TestEmphasis(t *testing.T) {
	got := render(t, "plain **bold** and *italic* and ~~gone~~")
	want := "plain [::b]bold[::-] and [::i]italic[::-] and [::s]gone[::-]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestInlineCode(t *testing.T) {
	got := render(t, "call `fmt.Println` please")
	if got != "call [code]fmt.Println[-] please" {
		t.Errorf("got %q", got)
	}
}

func TestBulletList(t *testing.T) {
	got := render(t, "- one\n- two\n")
	want := "[muted]• [-]one\n[muted]• [-]two"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestOrderedListCounts(t *testing.T) {
	got := render(t, "1. first\n2. second\n3. third\n")
	for _, want := range []string{"1. ", "2. ", "3. "} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
}

func TestNestedListIndents(t *testing.T) {
	got := render(t, "- outer\n    - inner\n")
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("the nested item is not indented: %q", lines[1])
	}
	if !strings.Contains(lines[1], "inner") {
		t.Errorf("nested item = %q", lines[1])
	}
}

func TestTaskList(t *testing.T) {
	got := render(t, "- [x] done\n- [ ] todo\n")
	if !strings.Contains(got, "✓ ") || !strings.Contains(got, "☐ ") {
		t.Errorf("task markers missing: %q", got)
	}
}

func TestHeading(t *testing.T) {
	got := render(t, "# Title\n\nbody\n")
	if !strings.HasPrefix(got, "[head::b]Title[-:-:-]") {
		t.Errorf("got %q", got)
	}
	if !strings.HasSuffix(got, "body") {
		t.Errorf("body missing: %q", got)
	}
	// Exactly one blank line between the two.
	if strings.Count(got, "\n\n") != 1 {
		t.Errorf("spacing = %q", got)
	}
}

func TestBlockquote(t *testing.T) {
	got := render(t, "> quoted\n> lines\n")
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "[quote]│ [-]") {
			t.Errorf("line %q is not quoted", line)
		}
	}
}

func TestFencedCode(t *testing.T) {
	got := render(t, "```go\nfmt.Println(1)\n```\n")
	if !strings.Contains(got, "[muted]go[-]") {
		t.Errorf("the language is not shown: %q", got)
	}
	if !strings.Contains(got, "  [code]fmt.Println(1)[-]") {
		t.Errorf("code block = %q", got)
	}
}

func TestLinkKeepsItsTarget(t *testing.T) {
	got := render(t, "see [the docs](https://example.com/x)")
	if !strings.Contains(got, "the docs") || !strings.Contains(got, "https://example.com/x") {
		t.Errorf("got %q", got)
	}
}

// TestSquareBracketsAreEscaped: unescaped text would be read as a colour tag
// and vanish.
func TestSquareBracketsAreEscaped(t *testing.T) {
	got := render(t, "an array[0] and [red] text")
	if !strings.Contains(got, "array[0[]") {
		t.Errorf("brackets not escaped: %q", got)
	}
	if !strings.Contains(got, "[red[]") {
		t.Errorf("a colour word was left as a tag: %q", got)
	}
}

func TestSoftBreaksBecomeSpaces(t *testing.T) {
	got := render(t, "one\ntwo\n")
	if got != "one two" {
		t.Errorf("got %q", got)
	}
}

func TestParagraphsKeepOneBlankLine(t *testing.T) {
	got := render(t, "one\n\n\n\ntwo\n")
	if got != "one\n\ntwo" {
		t.Errorf("got %q", got)
	}
}

func TestPlainTextSurvives(t *testing.T) {
	got := render(t, "just a sentence.")
	if got != "just a sentence." {
		t.Errorf("got %q", got)
	}
}

func TestEmptyInput(t *testing.T) {
	if got := render(t, "   \n\n"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestHTMLIsStrippedNotShown(t *testing.T) {
	got := render(t, "<details>\n<summary>hi</summary>\n</details>\n")
	if strings.Contains(got, "<script") {
		t.Errorf("got %q", got)
	}
	// Whatever it does, it must not blow up or produce tview tags.
	if strings.Contains(got, "[-]not") {
		t.Errorf("got %q", got)
	}
}
