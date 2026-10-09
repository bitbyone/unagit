// Package session records what unagit currently has open in an editor, so
// another terminal can find its way there.
//
// A terminal editor's record lasts until it closes. A Neovim with a socket
// lives independently of the unagit that opened it, so its record follows
// the listener instead of the writer's pid. Multiplexer records follow their
// pane in its original session, and a Neovim put aside from its pane then
// follows its listener like any other. Window editors keep the pid rule:
// nothing tells unagit when their windows close. Each opening gets a file of
// its own, and readers sweep away those whose editor is gone.
package session

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/mux"
)

// Modes a directory can be open in.
const (
	ModeRepository = "repository"
	ModeBranch     = "branch"
	ModeReview     = "review"
	// ModeGroup is a grouped worktree: several repositories in one directory.
	ModeGroup = "group"
)

// Record is one directory currently open in an editor.
type Record struct {
	PID         int       `json:"pid"`
	Dir         string    `json:"dir"`
	Instance    string    `json:"instance"`
	Server      string    `json:"server"`
	Project     string    `json:"project"`
	IID         int       `json:"iid,omitempty"`
	Title       string    `json:"title,omitempty"`
	Mode        string    `json:"mode"`
	Since       time.Time `json:"since"`
	Editor      string    `json:"editor,omitempty"`
	Launcher    string    `json:"launcher,omitempty"`
	Socket      string    `json:"socket,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Mux         string    `json:"mux,omitempty"`
	Pane        string    `json:"pane,omitempty"`
	MuxSession  string    `json:"mux_session,omitempty"`
	MuxLauncher string    `json:"mux_launcher,omitempty"`
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
	close, _ := s.Add(r)
	return close
}

// Add reports failures for a background editor: losing its record would
// leave a running server that the next unagit cannot find.
func (s *Store) Add(r Record) (func(), error) {
	noop := func() {}
	r.PID = os.Getpid()
	if r.Since.IsZero() {
		r.Since = time.Now()
	}
	path := s.path(r.PID, opened.Add(1))

	if err := s.secureDirectory(); err != nil {
		return noop, err
	}
	if err := s.write(path, r); err != nil {
		return noop, err
	}
	return func() { os.Remove(path) }, nil
}

// write puts a record in place whole, so a reader never sees half of one.
func (s *Store) write(path string, r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".record-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (s *Store) secureDirectory() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(s.dir, 0o700)
}

// NewSocket keeps the name independent of repository names and short enough
// for macOS, where even a normal temporary directory can be too long.
func (s *Store) NewSocket() (string, error) {
	if err := s.secureDirectory(); err != nil {
		return "", err
	}
	var id [6]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	path := filepath.Join(s.dir, fmt.Sprintf("%x.sock", id))
	if len(path) > 103 {
		return "", fmt.Errorf("editor socket path is too long; use a shorter UNAGIT_CONFIG_DIR")
	}
	return path, nil
}

// Remove takes back a closed server's record, even when another unagit
// created it. Only sockets in this store are ours to remove.
func (s *Store) Remove(r Record) {
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		b, err := os.ReadFile(path)
		var saved Record
		if err == nil && json.Unmarshal(b, &saved) == nil && saved.Socket == r.Socket && r.Socket != "" {
			os.Remove(path)
		}
	}
	if filepath.Dir(r.Socket) == s.dir && !editors.SocketAlive(r.Socket) {
		os.Remove(r.Socket)
	}
}

// opened numbers the records of this process: several window editors can be
// open at once.
var opened atomic.Int64

func (s *Store) path(pid int, n int64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d-%d.json", pid, n))
}

// List returns what is open, newest first, after sweeping up the records of
// editors that are no longer running. A directory is listed once, however
// many records it has - two unagit windows, or one opening it twice - because
// what the list is for is going there.
func (s *Store) List() []Record {
	return unique(s.records(false))
}

// Running excludes PID-only records before deduplication, so opening a
// window editor beside Neovim does not hide a server that can be attached.
func (s *Store) Running() []Record {
	return unique(s.records(true))
}

// InEditor filters before deduplication: a newer window editor beside
// Neovim does not take away the mark of the Neovim still running there.
func (s *Store) InEditor(id string) []Record {
	var out []Record
	for _, r := range s.records(false) {
		if r.Editor == id {
			out = append(out, r)
		}
	}
	return unique(out)
}

// Where is what is open and keep says yes to, filtered before
// deduplication as InEditor is.
func (s *Store) Where(keep func(Record) bool) []Record {
	var out []Record
	for _, r := range s.records(false) {
		if keep(r) {
			out = append(out, r)
		}
	}
	return unique(out)
}

func (s *Store) records(backgroundOnly bool) []Record {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	type paneRead struct {
		live map[string]bool
		err  error
	}
	panes := map[mux.Connection]paneRead{}
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
		live := alive(r.PID)
		if r.Pane != "" {
			connection := mux.Connection{Kind: r.Mux, Binary: r.MuxLauncher, Session: r.MuxSession}
			read, ok := panes[connection]
			if !ok {
				read.live, read.err = connection.Panes()
				panes[connection] = read
			}
			switch {
			case read.err != nil && r.Socket == "":
				// Nothing else can tell; ask again later.
				continue
			case read.err != nil:
				live = editors.SocketAlive(r.Socket)
			case read.live[r.Pane]:
				live = true
			case r.Socket != "" && editors.SocketAlive(r.Socket):
				// Put aside from its pane with Ctrl-Z: the pane closed and
				// the server runs on. From now on it is an editor aside like
				// any other, and its record says so for every reader.
				r.Pane, r.Mux, r.MuxSession, r.MuxLauncher = "", "", "", ""
				_ = s.write(path, r)
				live = true
			default:
				live = false
			}
		} else if r.Socket != "" {
			live = editors.SocketAlive(r.Socket)
			// The record is written before the first UI starts listening.
			// Another unagit must not sweep it in that short interval.
			if !live && alive(r.PID) && time.Since(r.Since) < 5*time.Second {
				continue
			}
		}
		if !live {
			os.Remove(path)
			if r.Socket != "" && filepath.Dir(r.Socket) == s.dir {
				os.Remove(r.Socket)
			}
			continue
		}
		if backgroundOnly && r.Socket == "" {
			continue
		}
		if _, err := os.Stat(r.Dir); err != nil {
			// The worktree was deleted from under the editor.
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.After(out[j].Since) })
	return out
}

func unique(out []Record) []Record {
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
