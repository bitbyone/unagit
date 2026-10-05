// Package index caches the GitLab project and merge request lists on disk so
// the TUI starts instantly and only hits the API on an explicit refresh.
package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// Version is the shape of the cached indexes. It goes up whenever a field is
// added that an older cache cannot have, so the interface can say a refresh
// would bring something new rather than leaving a column quietly empty.
const Version = 3

// Projects is the cached project index.
type Projects struct {
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	Items     []forge.Project `json:"items"`
}

// MergeRequests is the cached merge request index. Me is who the token of
// each server belongs to, by server, so "mine" and "to review" can be told.
type MergeRequests struct {
	Version   int                  `json:"version"`
	UpdatedAt time.Time            `json:"updated_at"`
	Items     []forge.MergeRequest `json:"items"`
	Me        map[string]string    `json:"me,omitempty"`
}

// Groups is the cached group tree.
type Groups struct {
	Version   int           `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
	Items     []forge.Group `json:"items"`
}

// Users is what the accounts of each server are called, by server and by
// username. GitHub's lists name an author by login alone; a name is asked
// for once and then kept for NameAge, since people seldom change theirs.
type Users struct {
	Items map[string]map[string]Person `json:"items"`
}

// Person is an account's name and when it was learnt. An empty name is
// kept too: the account has given none, and asking again would not help.
type Person struct {
	Name    string    `json:"name,omitempty"`
	Learned time.Time `json:"learned"`
}

// NameAge is how long a learnt name is trusted before it is asked for again.
const NameAge = 30 * 24 * time.Hour

// Clone is a copy that can be learnt into while the original is read.
func (u Users) Clone() Users {
	out := Users{Items: map[string]map[string]Person{}}
	for instance, people := range u.Items {
		out.Items[instance] = make(map[string]Person, len(people))
		for name, p := range people {
			out.Items[instance][name] = p
		}
	}
	return out
}

// Name is what the account is called, "" when that is not known.
func (u Users) Name(instance, username string) string {
	return u.Items[instance][username].Name
}

// Learn notes an account's name.
func (u *Users) Learn(instance, username, name string, at time.Time) {
	if username == "" {
		return
	}
	if u.Items == nil {
		u.Items = map[string]map[string]Person{}
	}
	if u.Items[instance] == nil {
		u.Items[instance] = map[string]Person{}
	}
	u.Items[instance][username] = Person{Name: name, Learned: at}
}

// Unknown is which of the usernames have no name learnt, or one learnt
// longer ago than NameAge, each once and in order.
func (u Users) Unknown(instance string, usernames []string, now time.Time) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range usernames {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if p, ok := u.Items[instance][name]; !ok || now.Sub(p.Learned) > NameAge {
			out = append(out, name)
		}
	}
	return out
}

// Stale reports whether a cache was written by a version that knew less than
// this one does.
func Stale(version, items int) bool { return items > 0 && version < Version }

// Load reads a JSON index file. A missing file is not an error: it yields the
// zero value so the TUI can start with an empty list.
func Load[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return v, nil
		}
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	return v, nil
}

// Save writes a JSON index file atomically.
func Save(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DedupeProjects removes projects seen through more than one selected group
// and sorts them by path.
func DedupeProjects(in []forge.Project) []forge.Project {
	seen := make(map[int]bool, len(in))
	out := make([]forge.Project, 0, len(in))
	for _, p := range in {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PathWithNamespace < out[j].PathWithNamespace })
	return out
}

// DedupeMergeRequests removes duplicates and sorts by most recently updated.
func DedupeMergeRequests(in []forge.MergeRequest) []forge.MergeRequest {
	seen := make(map[int]bool, len(in))
	out := make([]forge.MergeRequest, 0, len(in))
	for _, m := range in {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}
