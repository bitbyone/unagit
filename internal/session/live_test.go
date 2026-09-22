package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnotherProcessCanReadIt is the point of the whole thing: while one
// process holds a directory open, another one finds it - and lands there.
func TestAnotherProcessCanReadIt(t *testing.T) {
	home := t.TempDir()
	worktree := t.TempDir()
	s := New(home)
	defer s.Open(Record{Dir: worktree, Project: "my2n/ng/calling", IID: 19, Mode: ModeReview})()

	// A separate process, reading the same store through the command itself.
	binary := filepath.Join(t.TempDir(), "unagit")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/unagit")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the command: %v\n%s", err, out)
	}
	cmd := exec.Command(binary, "cd", "--print", "calling")
	cmd.Env = append(os.Environ(), "UNAGIT_CONFIG_DIR="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("unagit cd --print: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != worktree {
		t.Fatalf("unagit cd --print printed %q, want %q", got, worktree)
	}

	// And nothing else goes to standard output, or the command substitution
	// that this exists for would break.
	if strings.Count(strings.TrimSpace(string(out)), "\n") != 0 {
		t.Errorf("more than the directory was printed: %q", out)
	}

	// A search that matches nothing fails rather than printing something
	// unexpected into a cd.
	cmd = exec.Command(binary, "cd", "--print", "nonsense")
	cmd.Env = append(os.Environ(), "UNAGIT_CONFIG_DIR="+home)
	if out, err := cmd.Output(); err == nil {
		t.Errorf("a search matching nothing printed %q", out)
	}

	// Without --print it is the shell itself that ends up there, which is the
	// whole point: feed one a command and see where it thinks it is.
	cmd = exec.Command(binary, "cd", "calling")
	cmd.Env = append(os.Environ(), "UNAGIT_CONFIG_DIR="+home, "SHELL=/bin/sh")
	cmd.Stdin = strings.NewReader("pwd; echo \"$UNAGIT_CD\"\n")
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("unagit cd: %v", err)
	}
	want := worktree + "\n" + worktree + "\n"
	if string(out) != want {
		t.Errorf("the shell started in %q, want %q", out, want)
	}
}
