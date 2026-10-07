package session

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/editortest"
)

func liveSocket(t *testing.T, socket string) net.Listener {
	t.Helper()
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l
}

func TestBackgroundRecordOutlivesItsWriter(t *testing.T) {
	t.Parallel()
	s := New(editortest.ShortDir(t))
	socket, err := s.NewSocket()
	if err != nil {
		t.Fatal(err)
	}
	liveSocket(t, socket)
	r := Record{PID: 4194303, Dir: t.TempDir(), Socket: socket, Editor: editors.Nvim, Since: time.Now().Add(-time.Hour)}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "dead-writer.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, store := range []*Store{s, New(filepath.Dir(s.dir))} {
		got := store.Running()
		if len(got) != 1 || got[0].Socket != socket {
			t.Fatalf("live server lost with its writer: %+v", got)
		}
	}
}

func TestDeadSocketIsSweptEvenWithALiveWriter(t *testing.T) {
	t.Parallel()
	s := New(editortest.ShortDir(t))
	socket, err := s.NewSocket()
	if err != nil {
		t.Fatal(err)
	}
	l := liveSocket(t, socket)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	_, err = s.Add(Record{Dir: t.TempDir(), Socket: socket, Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("dead socket kept: %+v", got)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatal("dead socket file was not swept")
	}
	entries, _ := os.ReadDir(s.dir)
	if len(entries) != 0 {
		t.Fatalf("stale records kept: %v", entries)
	}
}

func TestSocketDirectoryIsPrivateEvenWhenItAlreadyExists(t *testing.T) {
	t.Parallel()
	s := New(editortest.ShortDir(t))
	if err := os.Mkdir(s.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	socket, err := s.NewSocket()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("sessions mode = %o", info.Mode().Perm())
	}
	if len(socket) > 103 {
		t.Fatalf("socket too long: %s", socket)
	}
	_, err = New(filepath.Join(editortest.ShortDir(t), strings.Repeat("a", 100))).NewSocket()
	if err == nil {
		t.Fatal("an unusably long socket path was accepted")
	}
}

func TestWindowRecordDoesNotHideAnAttachableServer(t *testing.T) {
	t.Parallel()
	s := New(editortest.ShortDir(t))
	socket, err := s.NewSocket()
	if err != nil {
		t.Fatal(err)
	}
	liveSocket(t, socket)
	dir := t.TempDir()
	defer s.Open(Record{Dir: dir, Socket: socket, Editor: editors.Nvim})()
	defer s.Open(Record{Dir: dir, Editor: editors.Zed})()
	if got := s.Running(); len(got) != 1 || got[0].Socket != socket {
		t.Fatalf("window editor hides Neovim: %+v", got)
	}
	if got := s.InEditor(editors.Nvim); len(got) != 1 || got[0].Socket != socket {
		t.Fatalf("window editor hides Neovim's mark: %+v", got)
	}

}

func TestRemoveOnlyTakesBackTheClosedServer(t *testing.T) {
	t.Parallel()
	s := New(editortest.ShortDir(t))
	socket, err := s.NewSocket()
	if err != nil {
		t.Fatal(err)
	}
	r := Record{Dir: t.TempDir(), Socket: socket}
	_, err = s.Add(r)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Open(Record{Dir: t.TempDir(), Editor: editors.Zed})()
	s.Remove(r)
	if got := s.List(); len(got) != 1 || got[0].Editor != editors.Zed {
		t.Fatalf("removing server removed other sessions: %+v", got)
	}
}
