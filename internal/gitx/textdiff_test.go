package gitx

import (
	"strings"
	"testing"
)

func TestDiffTextsKeepsTheWholeFileAsContext(t *testing.T) {
	old := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"
	new := "one\ntwo\nthree\nfour\nFIVE\nsix\nseven\neight\n"
	diff, err := DiffTexts(old, new)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(diff, "@@ -1,8 +1,8 @@") {
		t.Fatalf("the diff should be one hunk of the whole file:\n%s", diff)
	}
	for _, want := range []string{" one\n", "-five\n", "+FIVE\n", " eight\n"} {
		if !strings.Contains(diff, want) {
			t.Errorf("no %q in:\n%s", want, diff)
		}
	}
	same, err := DiffTexts(old, old)
	if err != nil || !strings.HasPrefix(same, "@@ -1,8 +1,8 @@\n one\n") {
		t.Errorf("alike texts: %q, %v", same, err)
	}
	added, err := DiffTexts("", "a\nb\n")
	if err != nil || !strings.Contains(added, "+a\n+b\n") {
		t.Errorf("a file added: %q, %v", added, err)
	}
}
