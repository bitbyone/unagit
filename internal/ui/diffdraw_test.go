package ui

import (
	"strconv"
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/x.go b/x.go
index 1111111..2222222 100644
--- a/x.go
+++ b/x.go
@@ -3,3 +3,4 @@ func A() {
 	a := 1
-	b := 2
+	b := 3
+	c := 4
 	return
@@ -20,2 +21,2 @@ func B() {
-	old
+	new
\ No newline at end of file
`

// TestParseDiffNumbersEachSide: a context line has both numbers, an added
// one the new side's, a deleted one the old side's, each hunk from where
// its header says.
func TestParseDiffNumbersEachSide(t *testing.T) {
	t.Parallel()
	var got []string
	for _, l := range parseDiff(sampleDiff) {
		got = append(got, string(l.kind)+" "+strconv.Itoa(l.old)+"/"+strconv.Itoa(l.new)+" "+strings.TrimSpace(l.text))
	}
	want := []string{
		"@ 0/0 func A() {",
		"  3/3 a := 1",
		"- 4/0 b := 2",
		"+ 0/4 b := 3",
		"+ 0/5 c := 4",
		"  5/6 return",
		"@ 0/0 func B() {",
		"- 20/0 old",
		"+ 0/21 new",
		"\\ 0/0 No newline at end of file",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestOnFillKeepsInkAndTakesTheFill: a highlighted word on an added line
// keeps its colour and the line's background, and an escaped bracket in
// the code stays text.
func TestOnFillKeepsInkAndTakesTheFill(t *testing.T) {
	t.Parallel()
	got := onFill("[#ff0000]func[-] x"+esc("[a]"), colText)
	bg := colText.String()
	for _, want := range []string{"[#ff0000:" + bg + "]func", "[" + colText.String() + ":" + bg + "] x", esc("[a]")} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}

// TestDrawDiffSaysWhyThereAreNoLines.
func TestDrawDiffSaysWhyThereAreNoLines(t *testing.T) {
	t.Parallel()
	if got := drawDiff("a.png", "diff --git a/a.png b/a.png\nBinary files a/a.png and b/a.png differ\n", 80); !strings.Contains(got, "binary file") {
		t.Errorf("a binary file: %q", got)
	}
}
