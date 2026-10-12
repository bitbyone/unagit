package mux_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/tobola/unagit/internal/editortest"
	"github.com/tobola/unagit/internal/mux"
)

// This owns a session and configuration of its own, never a user's panes.
// Environment isolation makes it serial; ordinary runs use the fake CLI.
func TestZellijOpensRealEditors(t *testing.T) {
	if os.Getenv("UNAGIT_ZELLIJ_TEST") != "1" {
		t.Skip("set UNAGIT_ZELLIJ_TEST=1 to exercise installed Zellij")
	}
	bin, err := exec.LookPath("zellij")
	if err != nil {
		t.Skip("Zellij is not installed")
	}
	dir := editortest.ShortDir(t)
	config := filepath.Join(dir, "config.kdl")
	mustWrite := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(config, "session_serialization false\nshow_release_notes false\nshow_startup_tips false\n")
	layout := filepath.Join(dir, "layout.kdl")
	mustWrite(layout, "layout {\n pane command=\"/bin/sh\" {\n args \"-c\" \"sleep 120\"\n close_on_exit true\n }\n}\n")
	for key, value := range map[string]string{
		"ZELLIJ_CONFIG_FILE": config, "ZELLIJ_CONFIG_DIR": filepath.Join(dir, "config"),
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"), "XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CACHE_HOME": filepath.Join(dir, "cache"),
		"TERM": "xterm-256color",
	} {
		t.Setenv(key, value)
	}
	for _, key := range []string{"ZELLIJ", "ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	name := filepath.Base(dir)
	server := exec.Command(bin, "--session", name, "--config", config, "--new-session-with-layout", layout)
	terminal, err := pty.StartWithSize(server, &pty.Winsize{Rows: 36, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, terminal)
	t.Cleanup(func() {
		exec.Command(bin, "kill-session", name).Run()
		terminal.Close()
		server.Process.Kill()
		server.Wait()
	})
	client := &mux.Client{Connection: mux.Connection{Kind: mux.Zellij, Binary: bin, Session: name}, SourcePane: "0"}
	wait := func(ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if ready() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("Zellij did not reach the expected state")
	}
	wait(func() bool { panes, err := client.Panes(); return err == nil && panes["terminal_0"] })
	// Zellij directs CLI focus actions to the last client that typed a key.
	// Opening the action picker in unagit already supplies that keystroke.
	if _, err := terminal.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_SESSION_NAME", name)
	t.Setenv("ZELLIJ_PANE_ID", "0")
	editorDir := filepath.Join(dir, "a folder's name")
	if err := os.Mkdir(editorDir, 0700); err != nil {
		t.Fatal(err)
	}
	realDir, err := filepath.EvalSymlinks(editorDir)
	if err != nil {
		t.Fatal(err)
	}
	for i, where := range []mux.Placement{mux.Tab, mux.Vertical, mux.Horizontal} {
		output := filepath.Join(dir, fmt.Sprintf("editor-%d", i))
		marker := fmt.Sprintf("%d quotes: ' \" \\ \n tab:\t é $(touch wrong)", i)
		// The command blocks on a file we create after its pane has been checked.
		gate := output + ".finish"
		script := `printf '%s\n%s' "$PWD" "$1" > "$2"; while [ ! -e "$3" ]; do sleep 0.02; done`
		command := exec.Command("/bin/sh", "-c", script, "editor", marker, output, gate)
		pane, err := client.Open(where, editorDir, "gateway !7", command)
		if err != nil {
			t.Fatal(err)
		}
		expected := []byte(realDir + "\n" + marker)
		wait(func() bool { body, _ := os.ReadFile(output); return bytes.Equal(body, expected) })
		alive, err := client.Panes()
		if err != nil || !alive[pane] {
			t.Fatalf("running editor absent: %v, %v", alive, err)
		}
		wait(func() bool {
			focused, err := exec.Command(bin, "--session", name, "action", "list-clients").Output()
			return err == nil && strings.Contains(string(focused), pane)
		})
		mustWrite(gate, "")
		wait(func() bool { alive, err := client.Panes(); return err == nil && !alive[pane] })
	}
}
