package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/muxtest"
)

func TestMuxRecordsFollowTheirPanesAfterTheWriterDies(t *testing.T) {
	t.Parallel()
	tool := muxtest.New(t)
	tool.SetPanes(t, []muxtest.Pane{{ID: 10}, {ID: 11, Exited: true}})
	s := New(t.TempDir())
	for _, pane := range []string{"terminal_10", "terminal_11"} {
		_, err := s.Add(Record{Dir: t.TempDir(), Editor: editors.Nvim, Pane: pane, Mux: tool.Client().Kind, MuxSession: tool.Client().Session, MuxLauncher: tool.Binary})
		if err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(s.dir)
		for _, entry := range entries {
			path := filepath.Join(s.dir, entry.Name())
			data, _ := os.ReadFile(path)
			var r Record
			json.Unmarshal(data, &r)
			r.PID = 4194303
			r.Since = time.Now().Add(-time.Hour)
			data, _ = json.Marshal(r)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := s.List()
	if len(got) != 1 || got[0].Pane != "terminal_10" {
		t.Fatalf("pane liveness followed writer pid: %+v", got)
	}
	if calls := tool.Calls(t); len(calls) != 1 {
		t.Fatalf("session was queried per record: %d calls", len(calls))
	}
	if got := New(filepath.Dir(s.dir)).InEditor(editors.Nvim); len(got) != 1 {
		t.Fatal("new store lost live pane marker")
	}
	if got := s.Running(); len(got) != 0 {
		t.Fatal("pane without an RPC socket was offered as an attachable server")
	}
	tool.SetPanes(t, nil)
	if got := s.List(); len(got) != 0 {
		t.Fatalf("closed pane retained: %+v", got)
	}
	entries, _ := os.ReadDir(s.dir)
	if len(entries) != 0 {
		t.Fatalf("closed pane records were not swept: %v", entries)
	}
}

func TestMuxReadFailureKeepsRecordsForARetry(t *testing.T) {
	t.Parallel()
	tool := muxtest.New(t)
	tool.SetPanes(t, []muxtest.Pane{{ID: 10}})
	s := New(t.TempDir())
	_, err := s.Add(Record{Dir: t.TempDir(), Pane: "terminal_10", Mux: tool.Client().Kind, MuxSession: tool.Client().Session, MuxLauncher: tool.Binary})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool.Failure, []byte("busy server"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatal("unverified pane was listed")
	}
	entries, _ := os.ReadDir(s.dir)
	if len(entries) != 1 {
		t.Fatal("failed query swept the editor record")
	}
	if err := os.Remove(tool.Failure); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 1 {
		t.Fatal("retry did not recover editor")
	}
}
