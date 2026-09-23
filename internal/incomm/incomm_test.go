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

func TestImport(t *testing.T) {
	bin, dir := t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\nif [ \"$4\" = list ]; then cat \"$2/existing.json\"; exit; fi\nprintf '%s\\n' \"$@\" >> \"$2/calls\"\n"
	if err := os.WriteFile(filepath.Join(bin, "incomm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	existing := map[string]any{"notes": []map[string]string{{"content": "already here\n\nSource: https://forge/mr/1 (comment 3)"}}}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(filepath.Join(dir, "existing.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	notes := []forge.Note{
		{ID: 1, Thread: "a", Body: "literal $(touch BAD)\nsecond line", Path: "code.go", Line: 2, Author: forge.User{Name: "Reviewer"}},
		{ID: 2, Thread: "a", Body: "reply"},
		{ID: 3, Body: "already here", Path: "code.go", Line: 1},
		{ID: 4, Body: "general"},
		{ID: 5, Body: "deleted file", Path: "gone.go", Line: 1},
		{ID: 6, Body: "past end", Path: "code.go", Line: 9},
		{ID: 7, Body: "system", Path: "code.go", Line: 1, System: true},
	}
	var logs []string
	err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, func(s string) { logs = append(logs, s) })
	if err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(calls)
	if strings.Count(got, "\nadd\n") != 2 || !strings.Contains(got, notes[0].Body) || !strings.Contains(got, "reply") || !strings.Contains(got, "Reviewer") {
		t.Fatalf("unexpected commands: %s", got)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "imported 2 orphaned") {
		t.Fatalf("logs: %v", logs)
	}
	// A failed write must propagate to the caller, which gates editor startup.
	if err := os.WriteFile(filepath.Join(bin, "incomm"), []byte("#!/bin/sh\nif [ \"$4\" = list ]; then echo '{\"notes\":[]}'; else exit 1; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Import(context.Background(), dir, forge.MergeRequest{}, notes[:1], nil); err == nil {
		t.Fatal("failed import succeeded")
	}
}

func TestImportMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Import(context.Background(), t.TempDir(), forge.MergeRequest{}, []forge.Note{{Path: "code.go", Line: 1}}, nil); err == nil {
		t.Fatal("missing binary accepted")
	}
}

// This also exercises branch detection and the CLI's actual JSON contract when
// Incomm is installed, without putting anything in the user's repositories.
func TestInstalledCLI(t *testing.T) {
	binary, err := exec.LookPath("incomm")
	if err != nil {
		t.Skip("incomm is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("one\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := []forge.Note{
		{ID: 1, Path: "code.go", Line: 1, Body: "review comment", Author: forge.User{Name: "Reviewer"}},
		{ID: 2, Path: "gone.go", Line: 5, Body: "deleted file"},
		{ID: 3, Path: "code.go", Line: 2, Body: "deleted line", Orphaned: true, Thread: "deleted"},
		{ID: 4, Body: "orphan reply", Thread: "deleted"},
		{ID: 5, Path: "code.go", Line: 99, Body: "past end"},
		{ID: 6, Body: "general"},
	}
	for range 2 {
		if err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, nil); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(binary, "--root", dir, "--json", "list")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Notes []struct {
			Content     string
			Orphaned    bool
			AuthorTitle string `json:"authorTitle"`
		}
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Notes) != 5 {
		t.Fatalf("unexpected notes: %s", out)
	}
	for _, n := range result.Notes {
		if strings.Contains(n.Content, "review comment") {
			if n.Orphaned || n.AuthorTitle != "Reviewer" {
				t.Fatalf("invalid current comment: %s", out)
			}
		} else if !n.Orphaned {
			t.Fatalf("lost orphan status after listing: %s", out)
		}
	}
	notes[0].Orphaned = true
	if err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, nil); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(binary, "--root", dir, "--json", "list")
	cmd.Dir = dir
	out, err = cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Notes) != 5 {
		t.Fatalf("reimport duplicated comments: %s", out)
	}
	for _, n := range result.Notes {
		if !n.Orphaned {
			t.Fatalf("existing comment was not orphaned: %s", out)
		}
	}
}

func TestGeneralCommentsDoNotNeedIncomm(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Import(context.Background(), t.TempDir(), forge.MergeRequest{}, []forge.Note{{Body: "general"}}, nil); err != nil {
		t.Fatal(err)
	}
}
