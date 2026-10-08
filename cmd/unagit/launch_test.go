package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
)

// TestLaunchRunsWhatItIsGivenAndClosesGhostty: `unagit launch` is what a
// herdr pane or a Ghostty terminal runs. It goes to the directory, runs the
// command with its arguments as they were, and in Ghostty closes the
// terminal afterwards, which Ghostty would keep open until a key.
func TestLaunchRunsWhatItIsGivenAndClosesGhostty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "unagit")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	work := filepath.Join(dir, "a dir")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	run := func(extra map[string]string) {
		t.Helper()
		spec := map[string]any{"dir": work, "args": []string{"sh", "-c", `printf '%s|%s' "$1" "$PWD" > "$2"`, "x", "a b $(no)", out}}
		for k, v := range extra {
			spec[k] = v
		}
		value, _ := json.Marshal(spec)
		cmd := exec.Command(bin, "launch")
		cmd.Env = append(os.Environ(), mux.LaunchVariable+"="+string(value))
		if text, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("launch: %v\n%s", err, text)
		}
		got, err := os.ReadFile(out)
		real, _ := filepath.EvalSymlinks(work)
		if err != nil || string(got) != "a b $(no)|"+real && string(got) != "a b $(no)|"+work {
			t.Fatalf("ran with %q, %v", got, err)
		}
	}
	run(nil)

	g := muxtest.NewGhostty(t)
	closeFile := filepath.Join(dir, "close")
	if err := os.WriteFile(closeFile, []byte("T-9"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(map[string]string{"close_ghostty": closeFile, "osascript": g.Binary})
	var calls []muxtest.GhosttyCall
	g.Calls(t, &calls)
	if len(calls) != 1 || calls[0].Kind != "close" || calls[0].Argv[0] != "T-9" {
		t.Fatalf("closing the terminal: %+v", calls)
	}
	if _, err := os.Stat(closeFile); !os.IsNotExist(err) {
		t.Error("the note of the terminal was left behind")
	}

	// Typed by hand, it says what it is for.
	if text, err := exec.Command(bin, "launch").CombinedOutput(); err == nil {
		t.Fatalf("launch without a command succeeded: %s", text)
	}
}
