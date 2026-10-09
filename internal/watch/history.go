package watch

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// What happened to a watched thing - and to an agent - is kept as its
// history, a file of events apiece under history/, one JSON event a line,
// oldest first. state.json keeps only the newest events of all, enough for
// an instance that looked away; the Activity screen shows each thing's
// story and everything in time, which wants more and for longer.
//
// Any instance appends, under history.lock: the poller its watches' news,
// every instance the agents it sees change, which it writes once: an
// event that only says again what the file last said of its agent is
// dropped there, so two instances seeing the same agent wait write it once.

const (
	// historyKept is how many events a thing keeps, historyAge how long.
	historyKept = 200
	historyAge  = 30 * 24 * time.Hour
)

// historyName is the file of a key: a hash, since a key holds slashes and
// colons, and the key itself is in every event.
func historyName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:12]) + ".jsonl"
}

func (s *Store) historyDir() string { return s.path("history") }

// AppendHistory adds events to their things' histories, each stamped now
// when it has no time. With once, an event whose heading and line are the
// last its thing has is not added again - for what several instances see
// and each would write, an agent's state; the watches' news is the
// poller's alone, and the same words twice are two things that happened.
func (s *Store) AppendHistory(once bool, events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	if err := os.MkdirAll(s.historyDir(), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path("history.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	byKey := map[string][]Event{}
	var keys []string
	for _, e := range events {
		if e.At.IsZero() {
			e.At = time.Now()
		}
		if _, ok := byKey[e.Key]; !ok {
			keys = append(keys, e.Key)
		}
		byKey[e.Key] = append(byKey[e.Key], e)
	}
	for _, key := range keys {
		if err := s.appendTo(key, byKey[key], once); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) appendTo(key string, events []Event, once bool) error {
	path := filepath.Join(s.historyDir(), historyName(key))
	old := readEvents(path)
	var fresh []Event
	last := Event{}
	if len(old) > 0 {
		last = old[len(old)-1]
	}
	for _, e := range events {
		if once && e.Heading == last.Heading && e.Line == last.Line {
			continue
		}
		fresh = append(fresh, e)
		last = e
	}
	if len(fresh) == 0 {
		return nil
	}
	all := append(old, fresh...)
	cut := time.Now().Add(-historyAge)
	from := 0
	for from < len(all) && all[from].At.Before(cut) {
		from++
	}
	from = max(from, len(all)-historyKept)
	if from == 0 {
		// Nothing goes: the new lines are added at the end.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		_, err = f.Write(eventLines(fresh))
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		return err
	}
	return s.writeLines(path, eventLines(all[from:]))
}

// writeLines puts a history in place whole, by a rename.
func (s *Store) writeLines(path string, b []byte) error {
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

func eventLines(events []Event) []byte {
	var b bytes.Buffer
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// readEvents reads a history, oldest first; a line that cannot be read is
// passed over.
func readEvents(path string) []Event {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Event
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	for scan.Scan() {
		var e Event
		if json.Unmarshal(scan.Bytes(), &e) == nil && e.Key != "" {
			out = append(out, e)
		}
	}
	return out
}

// History is what happened to one thing, newest first.
func (s *Store) History(key string) []Event {
	events := readEvents(filepath.Join(s.historyDir(), historyName(key)))
	reverse(events)
	return events
}

// AllHistory is what happened to everything, newest first, at most limit
// events. A thing nothing happened to for longer than the history is kept
// is forgotten on the way.
func (s *Store) AllHistory(limit int) []Event {
	entries, err := os.ReadDir(s.historyDir())
	if err != nil {
		return nil
	}
	cut := time.Now().Add(-historyAge)
	var all []Event
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(s.historyDir(), e.Name())
		events := readEvents(path)
		if len(events) == 0 || events[len(events)-1].At.Before(cut) {
			os.Remove(path)
			continue
		}
		all = append(all, events...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.After(all[j].At) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// HistoryStamp is when any history last changed: the directory's time
// moves when a file is added or renamed in, and the newest file's when one
// is appended to.
func (s *Store) HistoryStamp() time.Time {
	entries, err := os.ReadDir(s.historyDir())
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	if fi, err := os.Stat(s.historyDir()); err == nil {
		newest = fi.ModTime()
	}
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

func reverse(events []Event) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}
