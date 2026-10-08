package mux_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// waitUntil polls for what a real terminal does in its own time.
func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
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

// TestHerdrOpensRealCommands uses the herdr server the user runs: it opens
// a tab in the Unagit Agents workspace, checks the command ran as given
// and that the pane closed with it, and puts herdr's focus back.
func TestHerdrOpensRealCommands(t *testing.T) {
	if os.Getenv("UNAGIT_HERDR_TEST") != "1" {
		t.Skip("set UNAGIT_HERDR_TEST=1 to exercise the running herdr")
	}
	c := mux.DetectHerdr(func(string) string { return "" }, exec.LookPath)
	if c == nil {
		t.Skip("herdr is not installed")
	}
	c.Self = buildUnagit(t)
	before := herdrWorkspaces(t, c)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	pane, err := c.Open(mux.Window, dir, "unagit live test", awkwardCommand(out))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !strings.Contains(before, mux.AgentsWorkspace) {
			for _, line := range strings.Split(herdrWorkspaces(t, c), "\n") {
				if id, label, _ := strings.Cut(line, " "); label == mux.AgentsWorkspace {
					exec.CommandContext(ctx, c.Binary, "workspace", "close", id).Run()
				}
			}
		}
		if back := os.Getenv("HERDR_WORKSPACE_ID"); back != "" {
			exec.CommandContext(ctx, c.Binary, "workspace", "focus", back).Run()
		}
	})
	checkRan(t, out, dir)
	waitUntil(t, "the pane to close with its command", func() bool {
		live, err := c.Panes()
		return err == nil && !live[pane]
	})
}

func herdrWorkspaces(t *testing.T, c *mux.Client) string {
	t.Helper()
	out, err := exec.Command(c.Binary, "workspace", "list").Output()
	if err != nil {
		t.Fatal(err)
	}
	// id label, one per line, without decoding more than is needed here.
	var lines []string
	for _, part := range strings.Split(string(out), `"workspace_id":"`)[1:] {
		id, _, _ := strings.Cut(part, `"`)
		lines = append(lines, id)
	}
	for i, part := range strings.Split(string(out), `"label":"`)[1:] {
		label, _, _ := strings.Cut(part, `"`)
		if i < len(lines) {
			lines[i] += " " + label
		}
	}
	return strings.Join(lines, "\n")
}

// TestGhosttyOpensRealCommands opens a Ghostty window running a command
// through the launcher, and checks the window's terminal is closed once
// the command ends - Ghostty itself would wait for a key.
func TestGhosttyOpensRealCommands(t *testing.T) {
	if os.Getenv("UNAGIT_GHOSTTY_TEST") != "1" {
		t.Skip("set UNAGIT_GHOSTTY_TEST=1 to open a real Ghostty window")
	}
	c := mux.DetectGhostty(os.Getenv, exec.LookPath, false)
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
