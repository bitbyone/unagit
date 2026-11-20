// Package draft keeps the comments written in unagit and saved rather than
// published: each merge request's in a file of its own beside the indexes,
// until they are published, or, with Incomm on, moved into the Incomm
// store of a worktree of the merge request - from then on Incomm's copy is
// the comment, edited in the editor and published from there.
package draft

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tobola/unagit/internal/platform"
)

// Draft is a comment on a line of a merge request's latest version, not
// yet on the forge.
type Draft struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Line    int       `json:"line"`
	Body    string    `json:"body"`
	Created time.Time `json:"created"`
}

// Key names a merge request: its server, its repository and its number.
type Key struct {
	Instance, Project string
	IID               int
}

// Store is the drafts of every merge request, under one directory.
type Store struct{ Dir string }

// mu keeps one change to a file from overwriting another: the dialogs add
// and the background moves drafts into Incomm.
var mu sync.Mutex

func (s Store) file(k Key) string {
	return filepath.Join(s.Dir, url.PathEscape(k.Instance), url.PathEscape(k.Project), strconv.Itoa(k.IID)+".json")
}

// List is a merge request's drafts, oldest first.
func (s Store) List(k Key) ([]Draft, error) {
	mu.Lock()
	defer mu.Unlock()
	return s.read(k)
}

func (s Store) read(k Key) ([]Draft, error) {
	data, err := os.ReadFile(s.file(k))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var drafts []Draft
	if err := json.Unmarshal(data, &drafts); err != nil {
		return nil, fmt.Errorf("read the saved comments in %s: %w", s.file(k), err)
	}
	return drafts, nil
}

func (s Store) write(k Key, drafts []Draft) error {
	path := s.file(k)
	if len(drafts) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(drafts, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return platform.Replace(tmp, path)
}

// Add saves a draft, giving it an id and the time.
func (s Store) Add(k Key, d Draft) (Draft, error) {
	mu.Lock()
	defer mu.Unlock()
	drafts, err := s.read(k)
	if err != nil {
		return Draft{}, err
	}
	var id [6]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Draft{}, err
	}
	d.ID = hex.EncodeToString(id[:])
	if d.Created.IsZero() {
		d.Created = time.Now()
	}
	return d, s.write(k, append(drafts, d))
}

// Remove takes a draft away: published, moved into Incomm, or deleted.
func (s Store) Remove(k Key, id string) error {
	mu.Lock()
	defer mu.Unlock()
	drafts, err := s.read(k)
	if err != nil {
		return err
	}
	kept := drafts[:0]
	for _, d := range drafts {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	return s.write(k, kept)
}

// Counts is how many drafts each merge request has, for the lists.
func (s Store) Counts() map[Key]int {
	mu.Lock()
	defer mu.Unlock()
	out := map[Key]int{}
	_ = filepath.WalkDir(s.Dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		rel, err := filepath.Rel(s.Dir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 {
			return nil
		}
		instance, err1 := url.PathUnescape(parts[0])
		project, err2 := url.PathUnescape(parts[1])
		iid, err3 := strconv.Atoi(strings.TrimSuffix(parts[2], ".json"))
		if err1 != nil || err2 != nil || err3 != nil {
			return nil
		}
		k := Key{instance, project, iid}
		if drafts, err := s.read(k); err == nil && len(drafts) > 0 {
			out[k] = len(drafts)
		}
		return nil
	})
	return out
}
