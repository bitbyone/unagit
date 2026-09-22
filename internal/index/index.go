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

// Projects is the cached project index.
type Projects struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Items     []forge.Project `json:"items"`
}

// MergeRequests is the cached merge request index.
type MergeRequests struct {
	UpdatedAt time.Time            `json:"updated_at"`
	Items     []forge.MergeRequest `json:"items"`
}

// Groups is the cached group tree.
type Groups struct {
	UpdatedAt time.Time     `json:"updated_at"`
	Items     []forge.Group `json:"items"`
}

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
