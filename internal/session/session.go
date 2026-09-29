// Package session records what unagit currently has open in an editor, so
// another terminal can find its way there.
//
// Opening an editor does not end unagit: it suspends the interface and waits
// for the editor to exit. While that lasts, the running process knows which
// directory it handed over, and writes it down here. An editor that opens a
// window of its own does not keep unagit waiting; its record stays until
// unagit exits, since that is as long as anyone can vouch for it. Files are
// named after the process that wrote them, so two unagit windows never write
// to the same place and no locking is needed; a file whose process is gone is
// simply stale and gets swept up on the next read.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"syscall"
	"time"
)

// Modes a directory can be open in.
const (
	ModeRepository = "repository"
	ModeBranch     = "branch"
	ModeReview     = "review"
)

// Record is one directory currently open in an editor.
type Record struct {
	PID      int       `json:"pid"`
	Dir      string    `json:"dir"`
	Instance string    `json:"instance"`
	Server   string    `json:"server"`
	Project  string    `json:"project"`
	IID      int       `json:"iid,omitempty"`
	Title    string    `json:"title,omitempty"`
	Mode     string    `json:"mode"`
	Since    time.Time `json:"since"`
}

// Label is how the record reads in a list.
func (r Record) Label() string {
	if r.IID > 0 {
		return fmt.Sprintf("%s !%d", r.Project, r.IID)
	}
	return r.Project
}

// Store is where the records live, under the configuration directory.
type Store struct{ dir string }

// New returns a store rooted at the given directory.
func New(dir string) *Store { return &Store{dir: filepath.Join(dir, "sessions")} }

// Open writes the record down and returns the function that takes it back.
// A failure to write is not worth failing the open for: the editor still
// works, only another terminal cannot find it.
func (s *Store) Open(r Record) func() {
	r.PID = os.Getpid()
	r.Since = time.Now()
	path := s.path(r.PID, opened.Add(1))

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return func() {}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return func() {}
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return func() {}
	}
	return func() { os.Remove(path) }
}

// opened numbers the records of this process: several window editors can be
// open at once.
var opened atomic.Int64

func (s *Store) path(pid int, n int64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d-%d.json", pid, n))
}

// List returns what is open, newest first, after sweeping up the records of
// processes that are no longer running. A directory is listed once, however
// many records it has - two unagit windows, or one opening it twice - because
// what the list is for is going there.
func (s *Store) List() []Record {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []Record
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r Record
		if err := json.Unmarshal(b, &r); err != nil || r.Dir == "" {
			os.Remove(path)
			continue
		}
		if !alive(r.PID) {
			os.Remove(path)
			continue
		}
		if _, err := os.Stat(r.Dir); err != nil {
			// The worktree was deleted from under the editor.
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.After(out[j].Since) })
	seen := map[string]bool{}
	unique := out[:0]
	for _, r := range out {
		if dir := filepath.Clean(r.Dir); !seen[dir] {
			seen[dir] = true
			unique = append(unique, r)
		}
	}
	return unique
}

// alive reports whether a process is still running. Signal 0 asks the kernel
// that question without disturbing it.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
