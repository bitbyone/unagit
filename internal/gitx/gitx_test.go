package gitx

import (
	"strings"
	"testing"
)

// TestHintsExplainWhatToDo: git's own words are accurate but say nothing
// about the thing that would fix them.
func TestHintsExplainWhatToDo(t *testing.T) {
	cases := []struct {
		out  string
		want string
	}{
		{"git@gitlab.2n.cz: Permission denied (publickey,password).", "ssh-add"},
		{"Host key verification failed.", "known_hosts"},
		{"fatal: Authentication failed for 'https://gitlab.example.com/x.git'", "Settings"},
		{"fatal: could not read Username for 'https://github.com'", "Settings"},
		{"error: pathspec 'nope' did not match any file(s)", ""},
	}
	for _, c := range cases {
		got := hint(c.out)
		if c.want == "" {
			if got != "" {
				t.Errorf("%q got an unasked-for hint: %q", c.out, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%q: hint %q does not mention %q", c.out, got, c.want)
		}
	}
}

// TestBatchModeIsExplained: the hint has to say why it failed instead of
// asking, since unagit is what makes it fail.
func TestBatchModeIsExplained(t *testing.T) {
	got := hint("git@host: Permission denied (publickey).")
	if !strings.Contains(got, "batch mode") {
		t.Errorf("hint = %q", got)
	}
}
