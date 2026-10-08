package mux_test

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/muxtest"
)

func TestHerdrIsFoundInsideItAndFromOutside(t *testing.T) {
	t.Parallel()
	lookup := func(string) (string, error) { return "/bin/herdr", nil }
	inside := map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p2", "HERDR_WORKSPACE_ID": "w1", "HERDR_SOCKET_PATH": "/run/h.sock"}
	if c := mux.Detect(func(k string) string { return inside[k] }, lookup, true, false); c != nil {
		t.Fatalf("herdr turned off was used: %+v", c)
	}
	c := mux.Detect(func(k string) string { return inside[k] }, lookup, true, true)
	if c == nil || c.Kind != mux.Herdr || c.Session != "/run/h.sock" || c.SourcePane != "w1:p2" || c.Workspace != "w1" {
		t.Fatalf("inside herdr: %+v", c)
	}
	if !reflect.DeepEqual(c.Places(), []mux.Placement{mux.Tab, mux.Vertical, mux.Horizontal, mux.Window}) {
		t.Fatalf("places inside: %v", c.Places())
	}
	outside := mux.DetectHerdr(func(string) string { return "" }, lookup)
	if outside == nil || outside.SourcePane != "" || !strings.HasSuffix(outside.Session, "/.config/herdr/herdr.sock") {
		t.Fatalf("outside herdr: %+v", outside)
	}
	if !reflect.DeepEqual(outside.Places(), []mux.Placement{mux.Window}) {
		t.Fatalf("places outside: %v", outside.Places())
	}
	// Zellij inside herdr is the nearer of the two.
	both := map[string]string{"ZELLIJ": "0", "ZELLIJ_SESSION_NAME": "s", "HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p2", "HERDR_WORKSPACE_ID": "w1"}
	if c := mux.Detect(func(k string) string { return both[k] }, lookup, true, true); c == nil || c.Kind != mux.Zellij {
		t.Fatalf("zellij in herdr: %+v", c)
	}
}

// TestHerdrRunsTheCommandThroughTheLauncher: herdr types a line into a
// shell, so the line is constant and the command, awkward arguments and
// all, travels whole in the environment.
func TestHerdrRunsTheCommandThroughTheLauncher(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	c := h.HerdrClient(true)
	dir := t.TempDir()
	command := exec.Command("/bin/editor", "--flag", "a file's.go", "$(touch wrong)")
	for _, where := range []mux.Placement{mux.Tab, mux.Vertical, mux.Horizontal, mux.Window} {
		pane, err := c.Open(where, dir, "gateway !7", command)
		if err != nil || pane == "" {
			t.Fatalf("open %v: %q, %v", where, pane, err)
		}
	}
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	var made [][]string
	for _, call := range calls {
		if call.Socket != c.Session {
			t.Fatalf("herdr asked at %q, not %q", call.Socket, c.Session)
		}
		switch strings.Join(call.Args[:2], " ") {
		case "tab create", "pane split", "workspace create":
			made = append(made, call.Args)
			var launch struct {
				Dir  string
				Args []string
			}
			value, _ := strings.CutPrefix(call.Launch, mux.LaunchVariable+"=")
			if err := json.Unmarshal([]byte(value), &launch); err != nil || launch.Dir != dir || !reflect.DeepEqual(launch.Args, command.Args) {
				t.Fatalf("launch %q: %+v, %v", call.Launch, launch, err)
			}
		case "pane run":
			if call.Args[3] != "exec '/opt/un agit/unagit' launch" {
				t.Fatalf("typed %q", call.Args[3])
			}
		}
	}
	want := [][]string{
		{"tab", "create", "--workspace", "wU", "--cwd", dir, "--label", "gateway !7", "--focus"},
		{"pane", "split", "wU:p1", "--direction", "right", "--cwd", dir},
		{"pane", "split", "wU:p1", "--direction", "down", "--cwd", dir},
		{"workspace", "create", "--cwd", dir, "--label", "Unagit Agents", "--focus"},
	}
	for i, args := range want {
		if !reflect.DeepEqual(made[i][:len(args)], args) || made[i][len(args)] != "--env" {
			t.Fatalf("made %v, want %v --env", made[i], args)
		}
	}
	// A split does not take the focus in herdr; it is moved there.
	focused := 0
	for _, call := range calls {
		if strings.Join(call.Args[:2], " ") == "pane focus" {
			focused++
		}
	}
	if focused != 2 {
		t.Fatalf("focus moved %d times, want after each split", focused)
	}
}

func TestHerdrFromOutsideOpensOnlyAWorkspace(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	c := h.HerdrClient(false)
	for _, where := range []mux.Placement{mux.Tab, mux.Vertical} {
		if _, err := c.Open(where, t.TempDir(), "x", exec.Command("/bin/editor")); err == nil {
			t.Fatalf("placement %v opened from outside herdr", where)
		}
	}
	if _, err := c.OpenShell(mux.Window, t.TempDir(), "x"); err != nil {
		t.Fatal(err)
	}
}

func TestHerdrStartsAgentsAndToleratesOneThatAsksFirst(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	c := h.HerdrClient(false)
	pane, err := c.OpenShell(mux.Window, t.TempDir(), "gateway !7")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartAgent(pane, "gateway-7-ab12", "claude", nil); err != nil {
		t.Fatal(err)
	}
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	last := calls[len(calls)-1].Args
	if !reflect.DeepEqual(last, []string{"agent", "start", "gateway-7-ab12", "--kind", "claude", "--pane", pane}) {
		t.Fatalf("agent start: %v", last)
	}
	// Asking whether to trust the folder is a started agent waiting.
	h.Fail(t, "agent_not_ready agent start")
	if err := c.StartAgent(pane, "a", "claude", nil); err != nil {
		t.Fatalf("an agent asking first: %v", err)
	}
	h.Fail(t, "agent_pane_not_found agent start")
	if err := c.StartAgent(pane, "a", "claude", nil); err == nil || !strings.Contains(err.Error(), "agent_pane_not_found") && !strings.Contains(err.Error(), "fake") {
		t.Fatalf("a missing pane: %v", err)
	}
}

func TestHerdrPanesAndAServerThatIsGone(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	c := h.HerdrClient(false)
	pane, err := c.OpenShell(mux.Window, t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	live, err := c.Panes()
	if err != nil || !live[pane] || len(live) != 1 {
		t.Fatalf("panes: %v, %v", live, err)
	}
	h.Fail(t, "server_not_running")
	if live, err := c.Panes(); err != nil || len(live) != 0 {
		t.Fatalf("a server that is gone: %v, %v", live, err)
	}
	h.Fail(t, "internal_error")
	if _, err := c.Panes(); err == nil {
		t.Fatal("an unknown failure swept the panes")
	}
}

func TestALauncherPathThatNoShellReadsAlikeIsRefused(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	c := h.HerdrClient(true)
	c.Self = "/opt/it's/unagit"
	if _, err := c.Open(mux.Tab, t.TempDir(), "x", exec.Command("/bin/editor")); err == nil || !strings.Contains(err.Error(), "quote") {
		t.Fatalf("a quote in the path: %v", err)
	}
}

// TestPlacesOfTheirOwnShareOneWorkspace: the first opens the Unagit Agents
// workspace and names its tab, the next are tabs of it.
func TestPlacesOfTheirOwnShareOneWorkspace(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	c := h.HerdrClient(false)
	dir := t.TempDir()
	for _, name := range []string{"gateway !7", "billing"} {
		if _, err := c.OpenShell(mux.Window, dir, name); err != nil {
			t.Fatal(err)
		}
	}
	var calls []muxtest.HerdrCall
	h.Calls(t, &calls)
	var made [][]string
	for _, call := range calls {
		if call.Args[0] != "workspace" || call.Args[1] != "list" {
			made = append(made, call.Args)
		}
	}
	want := [][]string{
		{"workspace", "create", "--cwd", dir, "--label", "Unagit Agents", "--focus"},
		{"tab", "rename", "wN:t1", "gateway !7"},
		{"tab", "create", "--workspace", "wN", "--cwd", dir, "--label", "billing", "--focus"},
	}
	if !reflect.DeepEqual(made, want) {
		t.Fatalf("made %v\nwant %v", made, want)
	}
}

func TestHerdrAgentsSayWhereTheyRun(t *testing.T) {
	t.Parallel()
	h := muxtest.NewHerdr(t)
	h.SetAgents(t, []map[string]any{{"agent": "claude", "agent_status": "blocked", "cwd": "/w/a", "foreground_cwd": "/w/a/sub",
		"pane_id": "wA:p1", "tab_id": "wA:t1", "workspace_id": "wA", "terminal_title_stripped": "Fix the login"}})
	got, err := h.HerdrClient(false).Agents()
	want := []mux.HerdrAgent{{Kind: "claude", Status: "blocked", Title: "Fix the login", Dir: "/w/a/sub", Pane: "wA:p1", Workspace: "work", Tab: "gateway !7"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("agents: %+v, %v", got, err)
	}
	h.Fail(t, "server_not_running")
	if got, err := h.HerdrClient(false).Agents(); err != nil || len(got) != 0 {
		t.Fatalf("no server: %v, %v", got, err)
	}
}
