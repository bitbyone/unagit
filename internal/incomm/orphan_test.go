package incomm

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

func TestOrphanPreservesBranchStore(t *testing.T) {
	dir := t.TempDir()
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
	path := filepath.Join(store, "notes_feature_review.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"custom":"keep","notes":[{"id":"existing","content":"local","replies":[{"content":"reply"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appendOrphan(context.Background(), dir, forge.Note{Path: "gone.go", Line: 12}, "imported", "Reviewer"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Branch string
		Custom string
		Notes  []struct {
			ID        string
			Content   string
			Orphaned  bool
			StartLine int
			Replies   []struct{ Content string }
		}
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Branch != "feature/review" || result.Custom != "keep" || len(result.Notes) != 2 || result.Notes[0].Replies[0].Content != "reply" || !result.Notes[1].Orphaned || result.Notes[1].StartLine != 12 {
		t.Fatalf("store: %s", data)
	}
}
