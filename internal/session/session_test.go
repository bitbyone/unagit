package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndClose(t *testing.T) {
	s := New(t.TempDir())
	if got := s.List(); len(got) != 0 {
		t.Fatalf("a fresh store lists %+v", got)
	}

	done := s.Open(Record{Dir: t.TempDir(), Project: "acme/api", IID: 7, Mode: ModeReview})
	open := s.List()
	if len(open) != 1 {
		t.Fatalf("open = %+v", open)
	}
	if open[0].PID != os.Getpid() {
		t.Errorf("pid = %d, want this process", open[0].PID)
	}
	if open[0].Label() != "acme/api !7" {
		t.Errorf("label = %q", open[0].Label())
	}

	done()
	if got := s.List(); len(got) != 0 {
		t.Fatalf("still listed after closing: %+v", got)
	}
}

// TestRecordOfADeadProcessIsSweptUp: unagit can be killed with the editor
// open, and the leftover must not be offered.
func TestRecordOfADeadProcessIsSweptUp(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	// A pid that is certainly not running: pid 1 is init, so use a very large
	// one that no system will have handed out.
	stale := `{"pid":4194303,"dir":"` + dir + `","project":"acme/api","mode":"branch"}`
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sessions", "4194303.json")
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := s.List(); len(got) != 0 {
		t.Fatalf("a dead process is still listed: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the stale file was not removed")
	}
}

// TestRecordOfADeletedDirectoryIsSkipped
func TestRecordOfADeletedDirectoryIsSkipped(t *testing.T) {
	s := New(t.TempDir())
	gone := filepath.Join(t.TempDir(), "worktree")
	defer s.Open(Record{Dir: gone, Project: "acme/api"})()
	if got := s.List(); len(got) != 0 {
		t.Fatalf("a directory that is not there is offered: %+v", got)
	}
}

// TestTwoProcessesDoNotCollide: one file each, so no locking is needed.
func TestTwoProcessesDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	defer s.Open(Record{Dir: t.TempDir(), Project: "acme/api"})()

	// A second, live process: this test's own, under another name.
	other := Record{PID: os.Getppid(), Dir: t.TempDir(), Project: "acme/other"}
	b := []byte(`{"pid":` + itoa(other.PID) + `,"dir":"` + other.Dir + `","project":"acme/other"}`)
	if err := os.WriteFile(filepath.Join(dir, "sessions", itoa(other.PID)+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	if got := s.List(); len(got) != 2 {
		t.Fatalf("open = %+v", got)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
