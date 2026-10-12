package mux_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/mux"
)

// buildUnagit is the binary herdr and Ghostty start as the launcher.
func buildUnagit(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "unagit")
	_, here, _, _ := runtime.Caller(0)
	if out, err := exec.Command("go", "build", "-o", bin, filepath.Join(filepath.Dir(here), "..", "..", "cmd", "unagit")).CombinedOutput(); err != nil {
		t.Fatalf("build unagit: %v\n%s", err, out)
	}
	return bin
}

// waitUntil polls for what a real terminal does in its own time, which
// beside the whole suite can be long.
func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// awkwardCommand writes where it ran and its argument, which no shell may
// have read, so a wrong quote shows.
func awkwardCommand(out string) *exec.Cmd {
	return exec.Command("sh", "-c", `printf '%s|%s' "$PWD" "$1" > "$2"; sleep 1`, "x", `a 'b' "c" $(no) ; \n`, out)
}

func checkRan(t *testing.T, out, dir string) {
	t.Helper()
	waitUntil(t, "the command to run", func() bool { b, _ := os.ReadFile(out); return len(b) > 0 })
	got, _ := os.ReadFile(out)
	real, _ := filepath.EvalSymlinks(dir)
	if string(got) != dir+`|a 'b' "c" $(no) ; \n` && string(got) != real+`|a 'b' "c" $(no) ; \n` {
		t.Fatalf("ran as %q", got)
	}
}

// TestGhosttyOpensRealCommands opens a Ghostty window running a command
// through the launcher, and checks the window's terminal is closed once
// the command ends - Ghostty itself would wait for a key.
func TestGhosttyOpensRealCommands(t *testing.T) {
	if os.Getenv("UNAGIT_GHOSTTY_TEST") != "1" {
		t.Skip("set UNAGIT_GHOSTTY_TEST=1 to open a real Ghostty window")
	}
	// AppleScript is the system's, outside the tests' PATH.
	c := mux.DetectGhostty(os.Getenv, func(name string) (string, error) {
		if name == "osascript" {
			return "/usr/bin/osascript", nil
		}
		return exec.LookPath(name)
	}, false)
	if c == nil {
		t.Skip("Ghostty is not installed")
	}
	c.Self = buildUnagit(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	id, err := c.Open(mux.Window, dir, "unagit live test", awkwardCommand(out))
	if err != nil {
		t.Fatal(err)
	}
	checkRan(t, out, dir)
	waitUntil(t, "the terminal to close with its command", func() bool {
		live, err := c.Panes()
		return err == nil && !live[id]
	})
}
