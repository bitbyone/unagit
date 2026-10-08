package mux_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
)

func TestDetectionRequiresZellijAndItsSession(t *testing.T) {
	t.Parallel()
	lookup := func(string) (string, error) { return "/bin/zellij", nil }
	for _, env := range []map[string]string{{}, {"TMUX": "inside"}, {"ZELLIJ": "1"}, {"ZELLIJ_SESSION_NAME": "test"}} {
		if found := mux.Detect(func(k string) string { return env[k] }, lookup, true, true); found != nil {
			t.Fatalf("outside Zellij: %+v", found)
		}
	}
	env := map[string]string{"ZELLIJ": "1", "ZELLIJ_SESSION_NAME": "test", "ZELLIJ_PANE_ID": "4"}
	found := mux.Detect(func(k string) string { return env[k] }, lookup, true, true)
	if found == nil || found.Session != "test" || found.SourcePane != "4" {
		t.Fatalf("detection: %+v", found)
	}
	if found := mux.Detect(func(k string) string { return env[k] }, func(string) (string, error) { return "", exec.ErrNotFound }, true, true); found != nil {
		t.Fatal("missing launcher was detected")
	}
}

func TestOpenKeepsArgumentsAndMapsVimSplitDirections(t *testing.T) {
	t.Parallel()
	tool := muxtest.New(t)
	dir := t.TempDir()
	command := exec.Command("/bin/editor", "--flag", "a file's.go", "$(touch wrong)")
	for _, where := range []mux.Placement{mux.Vertical, mux.Horizontal, mux.Tab} {
		id, err := tool.Client().Open(where, dir, "gateway !7", command)
		if err != nil || !strings.HasPrefix(id, "terminal_") {
			t.Fatalf("open: %q, %v", id, err)
		}
	}
	var opened []muxtest.Call
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-tab" || call.Args[3] == "new-pane" {
			opened = append(opened, call)
		}
	}
	for i, direction := range []string{"right", "down"} {
		want := append([]string{"--session", "test-session", "action", "new-pane", "--direction", direction, "--cwd", dir, "--close-on-exit", "--near-current-pane", "--"}, command.Args...)
		if !reflect.DeepEqual(opened[i].Args, want) || realDir(opened[i].Dir) != realDir(dir) || opened[i].Source != "3" {
			t.Fatalf("split: %+v, want %v", opened[i], want)
		}
	}
	tab := opened[2].Args
	if !reflect.DeepEqual(tab[:10], []string{"--session", "test-session", "action", "new-tab", "--cwd", dir, "--name", "gateway !7", "--layout-string", tab[9]}) {
		t.Fatalf("tab: %v", tab)
	}
	for _, part := range []string{`command="/bin/editor"`, `args "--flag" "a file's.go" "$(touch wrong)"`, `close_on_exit true`} {
		if !strings.Contains(tab[9], part) {
			t.Fatalf("layout lost %q: %s", part, tab[9])
		}
	}
}

func TestOpeningWaitsForAVisiblePane(t *testing.T) {
	t.Parallel()
	tool := muxtest.New(t)
	if err := os.WriteFile(tool.Gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tool.Gate) })
	finished := make(chan error, 1)
	dir := t.TempDir()
	go func() {
		_, err := tool.Client().Open(mux.Vertical, dir, "test", exec.Command("/bin/editor"))
		finished <- err
	}()
	deadline := time.After(5 * time.Second)
	for {
		ready := false
		for _, call := range tool.Calls(t) {
			ready = ready || call.Args[3] == "list-panes"
		}
		if ready {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no pane query")
		case <-time.After(20 * time.Millisecond):
		}
	}
	select {
	case err := <-finished:
		t.Fatalf("returned before its pane exists: %v", err)
	default:
	}
	if err := os.Remove(tool.Gate); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-deadline:
		t.Fatal("pane never opened")
	}
}

func TestAnAlreadyFocusedPaneCountsAsOpened(t *testing.T) {
	t.Parallel()
	tool := muxtest.New(t)
	if err := os.WriteFile(tool.Failure, []byte("focused"), 0600); err != nil {
		t.Fatal(err)
	}
	pane, err := tool.Client().Open(mux.Tab, t.TempDir(), "test", exec.Command("/bin/editor"))
	if err != nil || pane == "" {
		t.Fatalf("already focused pane: %q, %v", pane, err)
	}
}

func TestPaneSnapshotsDistinguishDeadPanesAndUnavailableServers(t *testing.T) {
	t.Parallel()
	tool := muxtest.New(t)
	tool.SetPanes(t, []muxtest.Pane{{ID: 1}, {ID: 2, Plugin: true}, {ID: 3, Exited: true}})
	live, err := tool.Client().Panes()
	if err != nil || !reflect.DeepEqual(live, map[string]bool{"terminal_1": true}) {
		t.Fatalf("panes: %v, %v", live, err)
	}
	for _, failure := range []string{"missing", "There is no active session!", "unavailable"} {
		if err := os.WriteFile(tool.Failure, []byte(failure), 0600); err != nil {
			t.Fatal(err)
		}
		live, err := tool.Client().Panes()
		if failure == "unavailable" {
			if err == nil {
				t.Fatal("unknown failure swept panes")
			}
		} else if err != nil || len(live) != 0 {
			t.Fatalf("absent session: %v, %v", live, err)
		}
	}
}

func realDir(dir string) string {
	real, err := filepath.EvalSymlinks(dir)
	if err == nil {
		return real
	}
	return dir
}
