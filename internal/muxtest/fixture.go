// Package muxtest supplies an isolated Zellij stand-in without changing PATH.
package muxtest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// Stand is a herdr or osascript stand-in, kept to the directory it was put
// in: what it was called with goes to calls, what it is to fail with is
// written to failure.
type Stand struct{ Binary, Log, Failure, dir string }

// HerdrCall is one call of the herdr stand-in, with the socket it was
// pointed at and the --env it was given.
type HerdrCall struct {
	Args           []string
	Socket, Launch string
}

// GhosttyCall is one script run through the osascript stand-in.
type GhosttyCall struct {
	Kind string
	Argv []string
}

var buildStands sync.Map

func standBytes(source string) ([]byte, error) {
	once, _ := buildStands.LoadOrStore(source, sync.OnceValues(func() ([]byte, error) {
		dir, err := os.MkdirTemp("", "unagit-fake-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		_, here, _, _ := runtime.Caller(0)
		bin := filepath.Join(dir, "fake")
		src := filepath.Join(filepath.Dir(here), "..", "mux", "testdata", source)
		if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build %s: %w: %s", source, err, out)
		}
		return os.ReadFile(bin)
	}))
	return once.(func() ([]byte, error))()
}

func newStand(t *testing.T, source, name, warm string) Stand {
	t.Helper()
	dir := t.TempDir()
	s := Stand{Binary: filepath.Join(dir, name), Log: filepath.Join(dir, "calls"), Failure: filepath.Join(dir, "failure"), dir: dir}
	bin, err := standBytes(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Binary, bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// The first start of a new executable is slow on macOS; had here.
	cmd := exec.Command(s.Binary)
	cmd.Env = append(os.Environ(), warm+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start %s: %v: %s", name, err, out)
	}
	return s
}

// NewHerdr is a herdr stand-in.
func NewHerdr(t *testing.T) Stand {
	return newStand(t, "fake_herdr.go", "herdr", "UNAGIT_FAKE_HERDR_WARM")
}

// NewGhostty is an osascript stand-in answering for Ghostty.
func NewGhostty(t *testing.T) Stand {
	return newStand(t, "fake_osascript.go", "osascript", "UNAGIT_FAKE_OSASCRIPT_WARM")
}

// HerdrClient is a client of the stand-in as unagit inside herdr has it;
// without inside, as from outside herdr.
func (s Stand) HerdrClient(inside bool) *mux.Client {
	c := &mux.Client{Connection: mux.Connection{Kind: mux.Herdr, Binary: s.Binary, Session: filepath.Join(s.dir, "herdr.sock")}, Self: "/opt/un agit/unagit"}
	if inside {
		c.SourcePane, c.Workspace = "wU:p1", "wU"
	}
	return c
}

// GhosttyClient is a client of the stand-in; here is whether unagit runs
// in a Ghostty terminal of its own.
func (s Stand) GhosttyClient(here bool) *mux.Client {
	c := &mux.Client{Connection: mux.Connection{Kind: mux.Ghostty, Binary: s.Binary}, Self: "/opt/un agit/unagit"}
	if here {
		c.SourcePane = "self"
	}
	return c
}

// SetTerminals says which Ghostty terminals are open.
func (s Stand) SetTerminals(t *testing.T, ids ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.dir, "terminals"), []byte(strings.Join(ids, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Fail makes the stand-in fail: herdr with an error code, on one command
// ("pane list") or on all; osascript with what it prints.
func (s Stand) Fail(t *testing.T, failure string) {
	t.Helper()
	if err := os.WriteFile(s.Failure, []byte(failure), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Calls decodes what the stand-in was called with into out, a pointer to
// a slice of HerdrCall or GhosttyCall.
func (s Stand) Calls(t *testing.T, out any) {
	t.Helper()
	data, err := os.ReadFile(s.Log)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := "[" + strings.Join(strings.Split(strings.TrimSpace(string(data)), "\n"), ",") + "]"
	if err := json.Unmarshal([]byte(lines), out); err != nil {
		t.Fatal(err)
	}
}

// SetAgents says which agents herdr knows, as herdr's agent list gives them.
func (s Stand) SetAgents(t *testing.T, agents []map[string]any) {
	t.Helper()
	data, err := json.Marshal(agents)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "agents"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
