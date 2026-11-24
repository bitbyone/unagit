// Package board keeps the user's tasks: the Board's cards, in spaces of
// the user's own (plans/board.md). Nothing here draws anything or asks a
// server.
//
// The files are this machine's state, not configuration, laid out so a
// git repository could carry them between machines later without two of
// them ever writing the same file for different tasks:
//
//	spaces.json         the spaces and their order
//	board.lock          taken by whoever writes, every running unagit alike
//	<space>/<task>.json one file a task
package board

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/platform"
)

// The states a task is in, its board's columns from left to right.
const (
	Todo  = "todo"
	Doing = "doing"
	Done  = "done"
)

// States are the columns, in order.
var States = []string{Todo, Doing, Done}

// StateName is how a state is written on screen.
func StateName(state string) string {
	switch state {
	case Doing:
		return "In progress"
	case Done:
		return "Done"
	}
	return "To do"
}

// Colours are the colours a card can be given to stand out. They mean
// nothing to unagit: no priority, no order, no filter.
var Colours = []string{"red", "orange", "yellow", "green", "blue", "purple"}

// DefaultSpace is the space there is before the user makes any.
var DefaultSpace = Space{ID: "task-board", Name: "Task Board"}

// Space is a board of its own: the tasks of a customer, an employer, a
// project. ID is made once and never follows a rename.
type Space struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Order int    `json:"order"`
}

// Link kinds. Only a URL can be linked yet; the rest of what unagit knows
// comes in the next phase, and a task keeps whatever it does not know.
const KindURL = "url"

// Link is something a task points at. Label and URL are kept as they were
// when it was linked, for when what it points at is gone.
type Link struct {
	Kind  string    `json:"kind"`
	Key   string    `json:"key,omitempty"`
	Label string    `json:"label,omitempty"`
	URL   string    `json:"url,omitempty"`
	Path  string    `json:"path,omitempty"`
	Added time.Time `json:"added"`
}

// Name is what a link is called on screen: its label, or its address.
func (l Link) Name() string {
	if l.Label != "" {
		return l.Label
	}
	if l.URL != "" {
		return l.URL
	}
	return l.Key
}

// Comment is a note of the user's on a task.
type Comment struct {
	ID      string    `json:"id"`
	Body    string    `json:"body"`
	Written time.Time `json:"written"`
	Edited  time.Time `json:"edited,omitzero"`
}

// Event is a line of a task's story: what happened to it, and when.
type Event struct {
	At   time.Time `json:"at"`
	What string    `json:"what"`
}

// Task is one card.
type Task struct {
	ID    string `json:"id"`
	Space string `json:"space"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	State string `json:"state"`
	// Rank is the task's place in its column; the lower first.
	Rank string `json:"rank"`
	// Colour is "" or one of Colours.
	Colour   string    `json:"colour,omitempty"`
	Created  time.Time `json:"created"`
	Moved    time.Time `json:"moved"`
	Done     time.Time `json:"done,omitzero"`
	Links    []Link    `json:"links,omitempty"`
	Comments []Comment `json:"comments,omitempty"`
	Story    []Event   `json:"story,omitempty"`
}

// Tell adds a line to the task's story.
func (t *Task) Tell(at time.Time, what string) {
	t.Story = append(t.Story, Event{At: at, What: what})
}

// SetState moves the task to a state, telling it in its story; done
// stamps when.
func (t *Task) SetState(state string, at time.Time) {
	if t.State == state {
		return
	}
	t.State, t.Moved = state, at
	t.Done = time.Time{}
	if state == Done {
		t.Done = at
	}
	t.Tell(at, "moved to "+StateName(state))
}

// HiddenAt says whether a task done is off the board at now: done longer
// ago than after. A negative after keeps every task done on the board.
func (t Task) HiddenAt(now time.Time, after time.Duration) bool {
	return t.State == Done && after >= 0 && now.Sub(t.Done) >= after
}

// NewID is an id for a task, a space or a comment: when, then chance,
// so ids made on two machines do not meet.
func NewID() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatInt(time.Now().UnixMilli(), 36) + hex.EncodeToString(b[:])
}

// Store is the board's files in dir.
type Store struct{ dir string }

// Open is the store in dir; nothing is made until something is written.
func Open(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}

// locked runs change under board.lock, which every writer takes.
func (s *Store) locked(change func() error) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path("board.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := platform.Lock(lock, true); err != nil {
		return err
	}
	defer platform.Unlock(lock)
	return change()
}

// Spaces are the spaces in order: the default one while there are none.
func (s *Store) Spaces() ([]Space, error) {
	var out []Space
	if err := readJSON(s.path("spaces.json"), &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return []Space{DefaultSpace}, nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

// ChangeSpaces changes the spaces under the lock. The last cannot go.
func (s *Store) ChangeSpaces(change func([]Space) ([]Space, error)) ([]Space, error) {
	var out []Space
	err := s.locked(func() error {
		spaces, err := s.Spaces()
		if err != nil {
			return err
		}
		if out, err = change(spaces); err != nil {
			return err
		}
		if len(out) == 0 {
			return errors.New("the last space cannot go")
		}
		for i := range out {
			out[i].Order = i
		}
		return writeJSON(s.path("spaces.json"), out)
	})
	return out, err
}

// Tasks are a space's tasks by state, each column in its order.
func (s *Store) Tasks(space string) ([]Task, error) {
	entries, err := os.ReadDir(s.path(space))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		var t Task
		if err := readJSON(s.path(space, e.Name()), &t); err != nil {
			return nil, err
		}
		if t.ID == "" {
			continue
		}
		t.Space = space
		out = append(out, t)
	}
	Sort(out)
	return out, nil
}

// Sort puts tasks in their board's order: by state, then by rank.
func Sort(tasks []Task) {
	order := map[string]int{Todo: 0, Doing: 1, Done: 2}
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := tasks[i], tasks[j]
		if a.State != b.State {
			return order[a.State] < order[b.State]
		}
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		return a.Created.Before(b.Created)
	})
}

// Stamp tells when a space last changed, for a reader to read it again
// only then: a task written is a file put in place in its directory.
func (s *Store) Stamp(space string) time.Time {
	var at time.Time
	for _, p := range []string{s.path(space), s.path("spaces.json")} {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(at) {
			at = fi.ModTime()
		}
	}
	return at
}

// Add writes a new task.
func (s *Store) Add(t Task) error {
	if t.ID == "" || t.Space == "" {
		return errors.New("a task needs an id and a space")
	}
	return s.locked(func() error { return s.write(t) })
}

// Change changes a task as it is on disk now, under the lock: another
// unagit's change made meanwhile is kept, not written over.
func (s *Store) Change(space, id string, change func(*Task) error) (Task, error) {
	var t Task
	err := s.locked(func() error {
		var err error
		if t, err = s.read(space, id); err != nil {
			return err
		}
		if err := change(&t); err != nil {
			return err
		}
		if t.Space != space {
			if err := s.write(t); err != nil {
				return err
			}
			return os.Remove(s.path(space, id+".json"))
		}
		return s.write(t)
	})
	return t, err
}

// Delete removes a task.
func (s *Store) Delete(space, id string) error {
	return s.locked(func() error {
		err := os.Remove(s.path(space, id+".json"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

// RemoveSpace takes a space away, its tasks moved to another first - to
// the bottom of each of its columns - or, with to empty, deleted.
func (s *Store) RemoveSpace(id, to string) error {
	tasks, err := s.Tasks(id)
	if err != nil {
		return err
	}
	if to != "" {
		there, err := s.Tasks(to)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			last := ""
			for _, o := range there {
				if o.State == t.State && o.Rank > last {
					last = o.Rank
				}
			}
			t.Space, t.Rank = to, Between(last, "")
			there = append(there, t)
			if _, err := s.Change(id, t.ID, func(m *Task) error { m.Space, m.Rank = t.Space, t.Rank; return nil }); err != nil {
				return err
			}
		}
	}
	if _, err := s.ChangeSpaces(func(spaces []Space) ([]Space, error) {
		out := spaces[:0]
		for _, sp := range spaces {
			if sp.ID != id {
				out = append(out, sp)
			}
		}
		return out, nil
	}); err != nil {
		return err
	}
	return os.RemoveAll(s.path(id))
}

func (s *Store) read(space, id string) (Task, error) {
	var t Task
	path := s.path(space, id+".json")
	if _, err := os.Stat(path); err != nil {
		return t, fmt.Errorf("the task is gone: %w", err)
	}
	err := readJSON(path, &t)
	t.Space = space
	return t, err
}

func (s *Store) write(t Task) error {
	if err := os.MkdirAll(s.path(t.Space), 0o700); err != nil {
		return err
	}
	return writeJSON(s.path(t.Space, t.ID+".json"), t)
}

// writeJSON puts a file in place whole, by a rename, so a reader never
// sees half of it.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
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
	return platform.Replace(f.Name(), path)
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
		return fmt.Errorf("%s is damaged: %v", path, err)
	}
	return nil
}

// ParseAfter reads how long a task done stays on the board: "30m", "12h",
// "1d", "7d", or "never" (-1). "" is a day.
func ParseAfter(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "":
		return 24 * time.Hour, nil
	case "never":
		return -1, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q is not a number of days", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a time: 30m, 12h, 1d or never", s)
	}
	return d, nil
}
