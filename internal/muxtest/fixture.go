// Package muxtest supplies an isolated Zellij stand-in without changing PATH.
package muxtest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/tobola/unagit/internal/mux"
)

type Call struct {
	Args        []string
	Dir, Source string
}
type Pane struct {
	ID     int  `json:"id"`
	Plugin bool `json:"is_plugin"`
	Exited bool `json:"exited"`
	TabID  int  `json:"tab_id"`
}
type Tool struct{ Binary, Log, State, Failure, Gate string }

// Building a stand-in for every pane test would compete with short CLI
// deadlines across packages. Its bytes can be shared; its state cannot.
var buildTool = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "unagit-fake-zellij-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	_, here, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(here), "..", "mux", "testdata", "fake_zellij.go")
	bin := filepath.Join(dir, "zellij")
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build fake zellij: %w: %s", err, out)
	}
	return os.ReadFile(bin)
})

func New(t *testing.T) Tool {
	t.Helper()
	dir := t.TempDir()
	tool := Tool{filepath.Join(dir, "zellij"), filepath.Join(dir, "calls"), filepath.Join(dir, "panes"), filepath.Join(dir, "failure"), filepath.Join(dir, "gate")}
	bin, err := buildTool()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool.Binary, bin, 0700); err != nil {
		t.Fatal(err)
	}
	// The first start of a new executable is slow on macOS, which looks at
	// it before letting it run - under a loaded test run, longer than the
	// seconds unagit gives Zellij to answer. That start is had here, with
	// no deadline and nothing logged.
	warm := exec.Command(tool.Binary)
	warm.Env = append(os.Environ(), "UNAGIT_FAKE_ZELLIJ_WARM=1")
	if out, err := warm.CombinedOutput(); err != nil {
		t.Fatalf("start fake zellij: %v: %s", err, out)
	}
	tool.SetPanes(t, nil)
	return tool
}

func (tool Tool) Client() *mux.Client {
	return &mux.Client{Connection: mux.Connection{Kind: mux.Zellij, Binary: tool.Binary, Session: "test-session"}, SourcePane: "3"}
}

func (tool Tool) SetPanes(t *testing.T, panes []Pane) {
	t.Helper()
	if panes == nil {
		panes = []Pane{}
	}
	data, err := json.Marshal(panes)
	if err != nil {
		t.Fatal(err)
	}
	tmp := tool.State + ".next"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, tool.State); err != nil {
		t.Fatal(err)
	}
}

func (tool Tool) Calls(t *testing.T) []Call {
	t.Helper()
	file, err := os.Open(tool.Log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var calls []Call
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var call Call
		if err := decoder.Decode(&call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}
