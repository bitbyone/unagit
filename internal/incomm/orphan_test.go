package incomm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

// orphanRepo is a checkout on feature/review with a notes file holding content.
func orphanRepo(t *testing.T, content string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"symbolic-ref", "HEAD", "refs/heads/feature/review"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
	store := filepath.Join(dir, ".incomm")
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(store, "notes_feature_review.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

type orphanResult struct {
	Version int
	Branch  string
	Custom  string
	Notes   []struct {
		ID        string
		Content   string
		Orphaned  bool
		StartLine int
		Audience  string
		Source    *Source
		Replies   []struct{ Content string }
	}
}

func readOrphans(t *testing.T, path string) orphanResult {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result orphanResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOrphanPreservesBranchStore(t *testing.T) {
	dir, path := orphanRepo(t, `{"version":1,"custom":"keep","notes":[{"id":"existing","content":"local","replies":[{"content":"reply"}]}]}`)
	note := forge.Note{ID: 42, Thread: "th", Path: "gone.go", Line: 12}
	id, err := appendOrphan(context.Background(), dir, note, "imported", "Reviewer", "https://forge/mr/1#note_42")
	if err != nil {
		t.Fatal(err)
	}
	result := readOrphans(t, path)
	if result.Branch != "feature/review" || result.Custom != "keep" || len(result.Notes) != 2 ||
		result.Notes[0].Replies[0].Content != "reply" || !result.Notes[1].Orphaned || result.Notes[1].StartLine != 12 {
		t.Fatalf("store: %+v", result)
	}
	added := result.Notes[1]
	if added.ID != id || id == "" {
		t.Errorf("returned id %q, stored %q", id, added.ID)
	}
	if added.Audience != "agent+external" || added.Source == nil ||
		*added.Source != (Source{URL: "https://forge/mr/1#note_42", ID: 42, Thread: "th"}) {
		t.Errorf("audience/source: %+v %+v", added.Audience, added.Source)
	}
	// The appended comment uses fields of the new format, so the file says so.
	if result.Version != 2 {
		t.Errorf("version = %d, want 2", result.Version)
	}
}

func TestOrphanAppendsToAVersion2Store(t *testing.T) {
	dir, path := orphanRepo(t, `{"version":2,"notes":[{"id":"a","audience":"private","content":"mine"}]}`)
	if _, err := appendOrphan(context.Background(), dir, forge.Note{ID: 1, Path: "gone.go", Line: 3}, "x", "R", ""); err != nil {
		t.Fatal(err)
	}
	result := readOrphans(t, path)
	if result.Version != 2 || len(result.Notes) != 2 || result.Notes[0].Audience != "private" {
		t.Fatalf("store: %+v", result)
	}
	// Empty parts of the source are left out, as the CLI leaves them.
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"url"`) || strings.Contains(string(data), `"thread"`) {
		t.Errorf("empty source parts were written: %s", data)
	}
}

func TestOrphanRefusesANewerStore(t *testing.T) {
	dir, path := orphanRepo(t, `{"version":3,"notes":"a shape from the future"}`)
	before, _ := os.ReadFile(path)
	_, err := appendOrphan(context.Background(), dir, forge.Note{ID: 1, Path: "gone.go", Line: 3}, "x", "R", "")
	if err == nil || !strings.Contains(err.Error(), "unsupported incomm notes version") {
		t.Fatalf("err = %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("a store in a newer format must be left alone")
	}
}
