//go:build integration

package mux_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/creack/pty"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/testenv"
)

// TestHerdrOpensRealCommands starts a herdr of its own, under the run's
// home, opens a tab running a command, and checks the command ran as given
// and that the pane closed with it.
func TestHerdrOpensRealCommands(t *testing.T) {
	bin := testenv.Require(t, "herdr")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(home, ".config", "herdr", "herdr.sock")
	// The server comes up with its first client, which needs a terminal.
	client := exec.Command(bin)
	client.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(client, &pty.Winsize{Rows: 36, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, terminal)
	t.Cleanup(func() {
		exec.Command(bin, "server", "stop").Run()
		terminal.Close()
		client.Process.Kill()
		client.Wait()
	})
	waitUntil(t, "herdr's server", func() bool { _, err := os.Stat(socket); return err == nil })

	c := mux.DetectHerdr(func(string) string { return "" }, exec.LookPath)
	if c == nil {
		t.Fatal("herdr was not found")
	}
	if c.Session != socket {
		t.Fatalf("herdr's socket is %s, not the run's own %s", c.Session, socket)
	}
	c.Self = buildUnagit(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	pane, err := c.Open(mux.Window, dir, "unagit live test", awkwardCommand(out))
	if err != nil {
		t.Fatal(err)
	}
	checkRan(t, out, dir)
	waitUntil(t, "the pane to close with its command", func() bool {
		live, err := c.Panes()
		return err == nil && !live[pane]
	})
}
