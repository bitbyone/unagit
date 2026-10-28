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

// Without a token git asks the user's own credential helpers, as it would
// in a terminal; with one, unagit's helper alone answers.
func TestTheUsersCredentialHelpersAnswerWithoutAToken(t *testing.T) {
	dir := t.TempDir()
	sh(t, dir, "init", "-q")
	sh(t, dir, "config", "credential.helper", "store --file=mine")
	out, err := New("", nil).Run(dir, "config", "--get-all", "credential.helper")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out); got != "store --file=mine" {
		t.Errorf("without a token git's helpers are %q, not the user's own", got)
	}
	out, err = New("a-token", nil).Run(dir, "config", "--get-all", "credential.helper")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; last != credentialHelper {
		t.Errorf("with a token the last helper is %q, not unagit's", last)
	}
}
