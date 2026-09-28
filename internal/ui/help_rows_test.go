package ui

import "testing"

// TestHelpIsKeysNotProse keeps the help a two column table that can be read at
// a glance. Paragraphs have crept in more than once and each time buried the
// keys under a wall of text, with rows that had nothing in the key column. So:
// every row is a heading, a blank, or a key with one short line about it. What
// needs more words goes in the README.
func TestHelpIsKeysNotProse(t *testing.T) {
	const (
		maxKey  = 15 // "Tab / Shift-Tab"; a longer key widens the column for every row
		maxText = 52 // one line next to the keys, once the help is 100 columns wide
	)
	for i, line := range helpRows() {
		switch {
		case line.section != "":
			if line.keys != "" || line.text != "" {
				t.Errorf("row %d: a heading with a key or text: %+v", i, line)
			}
		case line.keys == "" && line.text == "":
			// a blank between sections
		case line.keys == "":
			t.Errorf("row %d: %q has nothing in the key column", i, line.text)
		case line.text == "":
			t.Errorf("row %d: key %q says nothing about itself", i, line.keys)
		default:
			if n := len([]rune(line.keys)); n > maxKey {
				t.Errorf("row %d: key %q is %d wide, keep it to %d", i, line.keys, n, maxKey)
			}
			if n := len([]rune(line.text)); n > maxText {
				t.Errorf("row %d: %q is %d long, keep it to %d - the README is for explaining",
					i, line.text, n, maxText)
			}
		}
	}
}
