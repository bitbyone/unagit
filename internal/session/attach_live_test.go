package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/editortest"
)

func TestAttachFindsAServerLeftByAnotherProcess(t *testing.T) {
	bin, log := editortest.Install(t)
	home := editortest.ShortDir(t)
	dir := t.TempDir()
	s := New(home)
	socket, err := s.NewSocket()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "--listen", socket).CombinedOutput(); err != nil {
		t.Fatalf("start fake editor: %v: %s", err, out)
	}
	r := Record{PID: 4194303, Dir: dir, Project: "acme/api", Branch: "feat/background", Editor: editors.Nvim, Launcher: bin, Socket: socket, Since: time.Now().Add(-time.Hour)}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "previous-unagit.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(t.TempDir(), "unagit")
	if out, err := exec.Command("go", "build", "-o", command, "../../cmd/unagit").CombinedOutput(); err != nil {
		t.Fatalf("build unagit: %v: %s", err, out)
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(command, args...)
		cmd.Env = append(os.Environ(), "UNAGIT_CONFIG_DIR="+home, "_ZO_DATA_DIR="+filepath.Join(home, "zoxide"))
		return cmd.CombinedOutput()
	}
	out, err := run("sessions")
	if err != nil || !strings.Contains(string(out), dir) {
		t.Fatalf("background editor missing from sessions: %s, %v", out, err)
	}
	out, err = run("cd", "--print", "api")
	if err != nil || strings.TrimSpace(string(out)) != dir {
		t.Fatalf("background editor missing from cd: %s, %v", out, err)
	}
	out, err = run("attach", "feat/background")
	if err != nil {
		t.Fatalf("attach: %s, %v", out, err)
	}
	events, _ := os.ReadFile(log)
	if strings.Count(string(events), "start|") != 1 || !strings.Contains(string(events), "attach|"+socket) {
		t.Fatalf("attach started a different editor:\n%s", events)
	}
	real, _ := filepath.EvalSymlinks(dir)
	landed := false
	for _, line := range strings.Split(string(events), "\n") {
		if actual, ok := strings.CutPrefix(line, "directory|"); ok {
			resolved, _ := filepath.EvalSymlinks(actual)
			landed = resolved == real
		}
	}
	if !landed {
		t.Fatalf("remote UI in wrong directory:\n%s", events)
	}
	if out, err = run("attach", "missing"); err == nil {
		t.Fatalf("unmatched query attached: %s", out)
	}
	if !editors.SocketAlive(socket) {
		t.Fatal("returning from attach closed the server")
	}
}
