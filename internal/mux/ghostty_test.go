package mux_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
)

// TestGhosttyOpensThroughTheLauncher: the values go to the script as
// arguments, the command line is the launcher's, and the launcher learns
// the terminal it is to close once its program ends.
func TestGhosttyOpensThroughTheLauncher(t *testing.T) {
	t.Parallel()
	g := muxtest.NewGhostty(t)
	c := g.GhosttyClient(false)
	dir := t.TempDir()
	command := exec.Command("/bin/agent", "a b", `"quoted"`)
	id, err := c.Open(mux.Window, dir, "gateway", command)
	if err != nil || id != "NEW-TERMINAL" {
		t.Fatalf("open: %q, %v", id, err)
	}
	var calls []muxtest.GhosttyCall
	g.Calls(t, &calls)
	argv := calls[0].Argv
	if calls[0].Kind != "open" || argv[0] != dir || argv[1] != "'/opt/un agit/unagit' launch" || argv[3] != "window" {
		t.Fatalf("open: %+v", calls[0])
	}
	var launch struct {
		Dir          string
		Args         []string
		CloseGhostty string `json:"close_ghostty"`
		Osascript    string
	}
	value, _ := strings.CutPrefix(argv[2], mux.LaunchVariable+"=")
	if err := json.Unmarshal([]byte(value), &launch); err != nil || !reflect.DeepEqual(launch.Args, command.Args) || launch.Osascript != g.Binary {
		t.Fatalf("launch: %+v, %v", launch, err)
	}
	written, err := os.ReadFile(launch.CloseGhostty)
	t.Cleanup(func() { os.Remove(launch.CloseGhostty) })
	if err != nil || string(written) != "NEW-TERMINAL" {
		t.Fatalf("the terminal to close: %q, %v", written, err)
	}
}

func TestGhosttySplitsOnlyBesideItself(t *testing.T) {
	t.Parallel()
	g := muxtest.NewGhostty(t)
	away := g.GhosttyClient(false)
	if !reflect.DeepEqual(away.Places(), []mux.Placement{mux.Window, mux.Tab}) {
		t.Fatalf("places away from Ghostty: %v", away.Places())
	}
	if _, err := away.Open(mux.Vertical, t.TempDir(), "x", exec.Command("/bin/agent")); err == nil {
		t.Fatal("split without a terminal of its own")
	}
	here := g.GhosttyClient(true)
	// Not yet found: a split waits for unagit to find itself.
	if _, err := here.Open(mux.Horizontal, t.TempDir(), "x", exec.Command("/bin/agent")); err == nil {
		t.Fatal("split before finding itself")
	}
	found, err := here.FindSelf("unagit-1-ab")
	if err != nil || found.SourcePane != "SELF-TERMINAL" {
		t.Fatalf("find self: %+v, %v", found, err)
	}
	if _, err := found.Open(mux.Horizontal, t.TempDir(), "x", exec.Command("/bin/agent")); err != nil {
		t.Fatal(err)
	}
	var calls []muxtest.GhosttyCall
	g.Calls(t, &calls)
	last := calls[len(calls)-1]
	if last.Argv[3] != "down" || last.Argv[4] != "SELF-TERMINAL" {
		t.Fatalf("split: %+v", last)
	}
	if calls[len(calls)-2].Kind != "named" || calls[len(calls)-2].Argv[0] != "unagit-1-ab" {
		t.Fatalf("finding itself: %+v", calls[len(calls)-2])
	}
}

func TestGhosttyTerminalsAndWhatToDoWhenRefused(t *testing.T) {
	t.Parallel()
	g := muxtest.NewGhostty(t)
	c := g.GhosttyClient(false)
	g.SetTerminals(t, "A-1", "B-2")
	live, err := c.Panes()
	if err != nil || !reflect.DeepEqual(live, map[string]bool{"A-1": true, "B-2": true}) {
		t.Fatalf("terminals: %v, %v", live, err)
	}
	g.Fail(t, "execution error: Not authorized to send Apple events to Ghostty. (-1743)")
	if _, err := c.Panes(); err == nil || !strings.Contains(err.Error(), "Automation") {
		t.Fatalf("refused: %v", err)
	}
	g.Fail(t, "execution error: Ghostty got an error: Can’t continue new tab. (-1708)")
	if _, err := c.Open(mux.Tab, t.TempDir(), "x", exec.Command("/bin/agent")); err == nil || !strings.Contains(err.Error(), "tabs may be turned off") {
		t.Fatalf("tabs off: %v", err)
	}
}
