// Package watch keeps what the user watches - a merge request's pipeline, a
// branch's - and what was last seen of it, in files every running unagit
// shares. One of them, the poller, asks the servers and writes what it
// found; the others read it. Nothing here asks a server or draws anything.
//
// The files are machine-local state, not configuration: config.yaml is what
// one carries between machines, what one happens to wait for here is not.
//
//	watches.json   what is watched - written by any instance, under watches.lock
//	state.json     what was last seen - written by the poller alone
//	poller.lock    held by the instance that polls
//	present/<id>   each running instance: whether its terminal is in front
package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// KindPipeline is a watch on the pipelines of a merge request or a branch.
const KindPipeline = "pipeline"

// Watch is one thing watched: a merge request (IID) or a branch of a
// repository (Branch).
type Watch struct {
	Kind      string `json:"kind"`
	Instance  string `json:"instance"`
	Project   string `json:"project"`
	ProjectID int    `json:"project_id,omitempty"`
	IID       int    `json:"iid,omitempty"`
	Branch    string `json:"branch,omitempty"`
	// Title is the merge request's as it was when the watch began, for a
	// row to read before the first answer.
	Title string    `json:"title,omitempty"`
	Since time.Time `json:"since"`
}

// Key tells two watches apart: the same thing watched twice is one watch.
func (w Watch) Key() string {
	what := "@" + w.Branch
	if w.IID > 0 {
		what = "!" + strconv.Itoa(w.IID)
	}
	return strings.Join([]string{w.Kind, w.Instance, w.Project, what}, "\x00")
}

// Label is how a person names it: "acme/api !42", "acme/api · main".
func (w Watch) Label() string {
	if w.IID > 0 {
		return fmt.Sprintf("%s !%d", w.Project, w.IID)
	}
	return w.Project + " · " + w.Branch
}

// State is what was last read of a watch.
type State struct {
	// Pipeline is the newest pipeline's id, Status its status in GitLab's
	// words ("" for none yet), SHA the commit it ran for.
	Pipeline int    `json:"pipeline,omitempty"`
	Status   string `json:"status,omitempty"`
	SHA      string `json:"sha,omitempty"`
	WebURL   string `json:"web_url,omitempty"`
	// Failed is the first job that failed, once the pipeline has.
	Failed string `json:"failed,omitempty"`
	// User is whom the pipeline was started by, Started when.
	User    string    `json:"user,omitempty"`
	Started time.Time `json:"started,omitzero"`
	// Title and URL are the merge request's, for the row.
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
	// Changed is when the status last changed, Seq the write that changed
	// it: above Snapshot.Seen it has not been seen. The first reading of a
	// watch leaves Seq at zero - it is not news.
	Changed time.Time `json:"changed,omitzero"`
	Seq     uint64    `json:"seq,omitempty"`
	// Read is when it was last asked about, Error why that failed.
	Read  time.Time `json:"read,omitzero"`
	Error string    `json:"error,omitempty"`
}

// Event is one change worth saying: a pipeline failed, passed, began.
type Event struct {
	Seq uint64 `json:"seq"`
	Key string `json:"key"`
	// What names the watch, Line says what happened: "acme/api !42",
	// "pipeline failed · test:unit".
	What string `json:"what"`
	Line string `json:"line"`
	// News is an ending - a pipeline passed, failed, waits for a hand, a
	// merge request merged: it is counted until seen and notified. A start
	// is said on the status line and no more. Bad is a failure, Good a
	// success.
	News bool `json:"news,omitempty"`
	Bad  bool `json:"bad,omitempty"`
	Good bool `json:"good,omitempty"`
}

// keptEvents is how many of the newest events state.json keeps: enough for
// an instance that looked away for a while, not a history.
const keptEvents = 50

// Snapshot is state.json: every watch's state and the latest events.
type Snapshot struct {
	// Seq goes up with every write that has news; Seen is how far the user
	// has looked, in any instance.
	Seq  uint64 `json:"seq"`
	Seen uint64 `json:"seen"`
	// Poller is the instance that wrote it.
	Poller string           `json:"poller,omitempty"`
	States map[string]State `json:"states,omitempty"`
	Events []Event          `json:"events,omitempty"`
}

// Add records events under a new sequence number, keeping the newest.
func (s *Snapshot) Add(events ...Event) uint64 {
	if len(events) == 0 {
		return s.Seq
	}
	s.Seq++
	for _, e := range events {
		e.Seq = s.Seq
		s.Events = append(s.Events, e)
	}
	if over := len(s.Events) - keptEvents; over > 0 {
		s.Events = append([]Event(nil), s.Events[over:]...)
	}
	return s.Seq
}

// Unseen counts the watches whose last change has not been seen.
func (s Snapshot) Unseen(watches []Watch) int {
	n := 0
	for _, w := range watches {
		if st, ok := s.States[w.Key()]; ok && st.Seq > s.Seen {
			n++
		}
	}
	return n
}

// After is the events newer than seq, oldest first.
func (s Snapshot) After(seq uint64) []Event {
	var out []Event
	for _, e := range s.Events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}

// Store is the files under one directory.
type Store struct{ dir string }

// Open is the store in dir; nothing is made until something is written.
func Open(dir string) *Store { return &Store{dir: dir} }

// Dir is where the files are.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// Watches is what is watched, nothing when the file is not there yet.
func (s *Store) Watches() ([]Watch, error) {
	var out []Watch
	err := readJSON(s.path("watches.json"), &out)
	return out, err
}

// Change changes what is watched under the file's own lock, so two
// instances adding a watch at the same moment both keep theirs.
func (s *Store) Change(change func([]Watch) []Watch) ([]Watch, error) {
	if err := s.secureDirectory(); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(s.path("watches.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	watches, err := s.Watches()
	if err != nil {
		return nil, err
	}
	watches = change(watches)
	sort.SliceStable(watches, func(i, j int) bool { return watches[i].Since.Before(watches[j].Since) })
	return watches, s.writeJSON("watches.json", watches)
}

// State is state.json, empty when it is not there yet.
func (s *Store) State() (Snapshot, error) {
	var snap Snapshot
	err := readJSON(s.path("state.json"), &snap)
	return snap, err
}

// WriteState puts state.json in place whole. Only the poller calls it.
func (s *Store) WriteState(snap Snapshot) error {
	if err := s.secureDirectory(); err != nil {
		return err
	}
	return s.writeJSON("state.json", snap)
}

// Stamp is when a file was last written, zero when it is not there: a
// reader looks at it every second and reads the file only when it moved.
func (s *Store) Stamp(name string) time.Time {
	fi, err := os.Stat(s.path(name))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// Poller is the lock on polling, held while it is open.
type Poller struct{ f *os.File }

// Release lets another instance poll.
func (p *Poller) Release() {
	if p != nil && p.f != nil {
		syscall.Flock(int(p.f.Fd()), syscall.LOCK_UN)
		p.f.Close()
		p.f = nil
	}
}

// TryPoll takes the poller's lock if no other instance holds it. The
// kernel lets it go when the process ends, however it ends, so there is
// nothing to clean up after a crash. Each call opens the file anew: two
// instances in one process - as tests have - are kept apart as two
// processes are.
func (s *Store) TryPoll() (*Poller, bool, error) {
	if err := s.secureDirectory(); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(s.path("poller.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Poller{f: f}, true, nil
}

// Presence is what one running instance says of itself: whether the user
// is looking at it, and how far they have looked at the watches.
type Presence struct {
	ID  string `json:"id"`
	PID int    `json:"pid"`
	// Focused is its terminal in front and not handed to an editor.
	Focused bool `json:"focused"`
	// Seen is the sequence number the user has seen there.
	Seen uint64 `json:"seen,omitempty"`
	// AskSeq goes up when the user asks for the watches to be read now:
	// those Ask names by key, every one when Ask is null.
	AskSeq uint64   `json:"ask_seq,omitempty"`
	Ask    []string `json:"ask"`
}

var instances atomic.Int64

// NewInstance is an id for one running unagit: its pid, and a count for
// the rare process that runs several.
func NewInstance() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), instances.Add(1))
}

// SetPresence writes what an instance says of itself.
func (s *Store) SetPresence(p Presence) error {
	if err := os.MkdirAll(s.path("present"), 0o700); err != nil {
		return err
	}
	return s.writeJSON(filepath.Join("present", p.ID), p)
}

// Leave takes an instance's presence away as it exits.
func (s *Store) Leave(id string) { os.Remove(s.path(filepath.Join("present", id))) }

// Presences is every running instance's word; one whose process is gone is
// removed, as a stale session is.
func (s *Store) Presences() []Presence {
	entries, _ := os.ReadDir(s.path("present"))
	var out []Presence
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := s.path(filepath.Join("present", e.Name()))
		var p Presence
		if readJSON(path, &p) != nil {
			continue
		}
		if !alive(p.PID) {
			os.Remove(path)
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Store) secureDirectory() error { return os.MkdirAll(s.dir, 0o700) }

// writeJSON puts a file in place whole, by a rename, so a reader never sees
// half of it.
func (s *Store) writeJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(name)
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s is damaged (%v); remove it and watch again", path, err)
	}
	return nil
}

// alive reports whether a process still runs; signal 0 asks without
// disturbing it.
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
