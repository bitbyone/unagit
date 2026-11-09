package mux_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
	"github.com/tobola/unagit/internal/testenv"
)

func TestRexIsFoundInsideItAndFromOutside(t *testing.T) {
	t.Parallel()
	lookup := func(string) (string, error) { return "/usr/local/bin/rex", nil }
	inside := map[string]string{"REX_SESSION": "session:s1", "REX_BLOCK": "block:b1", "REX_SERVER": "studio:7100"}
	c := mux.DetectRex(func(k string) string { return inside[k] }, lookup, true)
	if c == nil || c.Kind != mux.Rex || c.Binary != "/usr/local/bin/rex" || c.Session != "studio:7100" || c.SourcePane != "block:b1" || c.Workspace != "session:s1" {
		t.Fatalf("inside Rex: %+v", c)
	}
	if !reflect.DeepEqual(c.Places(), []mux.Placement{mux.Tab, mux.Vertical, mux.Horizontal, mux.Window}) {
		t.Fatalf("places inside: %v", c.Places())
	}
	// In a multiplexer inside Rex, unagit's terminal is not Rex's block.
	c = mux.DetectRex(func(k string) string { return inside[k] }, lookup, false)
	if c == nil || c.SourcePane != "" || !reflect.DeepEqual(c.Places(), []mux.Placement{mux.Window}) {
		t.Fatalf("not here: %+v, %v", c, c.Places())
	}
	if c := mux.DetectRex(func(string) string { return "" }, func(string) (string, error) { return "", exec.ErrNotFound }, false); c != nil {
		t.Fatalf("rex not installed was found: %+v", c)
	}
	// The rex beside Rex's server is the one, whatever PATH holds first.
	binDir := t.TempDir()
	if err := testenv.WriteExecutable(filepath.Join(binDir, "rex"), []byte("#!/bin/sh\n")); err != nil {
		t.Fatal(err)
	}
	c = mux.DetectRex(func(k string) string { return map[string]string{"REX_BIN_DIR": binDir}[k] }, lookup, false)
	if c == nil || c.Binary != filepath.Join(binDir, "rex") {
		t.Fatalf("REX_BIN_DIR was passed over: %+v", c)
	}
}

// TestRexRunsTheCommandAsItsArguments: rex takes a command as separate
// arguments and no shell reads it, so awkward ones arrive whole; it runs
// with unagit's PATH, and each place is made as Rex makes it.
func TestRexRunsTheCommandAsItsArguments(t *testing.T) {
	t.Parallel()
	r := muxtest.NewRex(t)
	c := r.RexClient(true)
	dir := t.TempDir()
	command := exec.Command("/bin/editor", "--flag", "a file's.go", "$(touch wrong)")
	panes := map[string]bool{}
	for _, where := range []mux.Placement{mux.Tab, mux.Vertical, mux.Horizontal, mux.Window, mux.Window} {
		pane, err := c.Open(where, dir, "gateway !7", command)
		if err != nil || pane == "" {
			t.Fatalf("open %v: %q, %v", where, pane, err)
		}
		panes[pane] = true
	}
	if len(panes) != 5 {
		t.Fatalf("not a block each: %v", panes)
	}
	var calls []muxtest.RexCall
	r.Calls(t, &calls)
	var made []string
	want := append([]string{"--", "env", "PATH=" + os.Getenv("PATH")}, command.Args...)
	for _, call := range calls {
		if call.Server != "studio" {
			t.Fatalf("rex asked at %q", call.Server)
		}
		switch call.Args[0] {
		case "run", "split", "new":
			made = append(made, strings.Join(call.Args[:len(call.Args)-len(want)], " "))
			if got := call.Args[len(call.Args)-len(want):]; !reflect.DeepEqual(got, want) {
				t.Fatalf("ran %q", got)
			}
		}
	}
	wantMade := []string{
		"run -s session:here --cwd " + dir + " --label gateway !7 --json",
		"split -b block:self --split right --cwd " + dir + " --label gateway !7 --json",
		"split -b block:self --split below --cwd " + dir + " --label gateway !7 --json",
		// The first place of its own makes the session, the next is a tab of it.
		"new " + mux.RexSession + " --window gateway !7 --cwd " + dir + " --json",
		"run -s session:1 --cwd " + dir + " --label gateway !7 --json",
	}
	if !reflect.DeepEqual(made, wantMade) {
		t.Fatalf("made\n%s\nnot\n%s", strings.Join(made, "\n"), strings.Join(wantMade, "\n"))
	}
	live, err := c.Panes()
	if err != nil || len(live) != 5 {
		t.Fatalf("live blocks: %v, %v", live, err)
	}
	for pane := range panes {
		if !live[pane] {
			t.Errorf("%s is not live", pane)
		}
	}
}

// TestRexWithoutItsServer: from outside Rex only a session of its own can
// be opened, and a server that cannot be reached is an error, so the
// records of what runs there are kept rather than swept.
func TestRexWithoutItsServer(t *testing.T) {
	t.Parallel()
	r := muxtest.NewRex(t)
	c := r.RexClient(false)
	for _, where := range []mux.Placement{mux.Tab, mux.Vertical} {
		if _, err := c.Open(where, t.TempDir(), "x", exec.Command("nvim")); err == nil || !strings.Contains(err.Error(), "run unagit in Rex") {
			t.Fatalf("open %v from outside: %v", where, err)
		}
	}
	r.Fail(t, "4 block ls")
	if live, err := c.Panes(); err == nil || live != nil || !strings.Contains(err.Error(), "start Rex") {
		t.Fatalf("unreachable: %v, %v", live, err)
	}
	var calls []muxtest.RexCall
	r.Calls(t, &calls)
	if last := calls[len(calls)-1].Args; !reflect.DeepEqual(last, []string{"block", "ls", "-A", "-q", "--short=false", "--autostart=false"}) {
		t.Fatalf("asked %q", last)
	}
}
