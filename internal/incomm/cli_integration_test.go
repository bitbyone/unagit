//go:build integration

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

// This exercises the real CLI when one that understands audience and source is
// installed, without putting anything in the user's repositories.
func TestInstalledCLI(t *testing.T) {
	binary, err := CheckCLI(context.Background())
	if err != nil {
		t.Fatal("the image's incomm: " + err.Error())
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("one\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := []forge.Note{
		{ID: 1, Path: "code.go", Line: 1, Body: "review comment", Author: forge.User{Name: "Reviewer"}, Thread: "t1"},
		{ID: 2, Path: "gone.go", Line: 5, Body: "deleted file", Thread: "t2"},
		{ID: 3, Path: "code.go", Line: 2, Body: "deleted line", Orphaned: true, Thread: "deleted"},
		{ID: 4, Body: "orphan reply", Thread: "deleted"},
		{ID: 5, Path: "code.go", Line: 99, Body: "past end", Thread: "t5"},
		{ID: 6, Body: "general"},
	}
	list := func() (result struct {
		Notes []struct {
			Content     string
			Orphaned    bool
			Audience    string
			AuthorTitle string `json:"authorTitle"`
			Source      *Source
			Replies     []struct{ Content string }
		}
	}) {
		cmd := exec.Command(binary, "--root", dir, "--json", "list")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for range 2 {
		if err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, nil); err != nil {
			t.Fatal(err)
		}
	}
	result := list()
	if len(result.Notes) != 4 {
		t.Fatalf("want 4 comments, importing twice must not duplicate: %+v", result)
	}
	for _, n := range result.Notes {
		if n.Audience != "agent+external" || n.Source == nil || n.Source.ID == 0 {
			t.Errorf("no audience/source: %+v", n)
		}
		switch {
		case strings.Contains(n.Content, "review comment"):
			if n.Orphaned || n.AuthorTitle != "Reviewer" {
				t.Errorf("invalid current comment: %+v", n)
			}
		case n.Content == "deleted line":
			if !n.Orphaned || len(n.Replies) != 1 || n.Replies[0].Content != "orphan reply" {
				t.Errorf("the orphan reply must be a reply: %+v", n)
			}
		default:
			if !n.Orphaned {
				t.Errorf("lost orphan status after listing: %+v", n)
			}
		}
	}
	notes[0].Orphaned = true
	if err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, nil); err != nil {
		t.Fatal(err)
	}
	result = list()
	if len(result.Notes) != 4 {
		t.Fatalf("reimport duplicated comments: %+v", result)
	}
	for _, n := range result.Notes {
		if !n.Orphaned {
			t.Fatalf("existing comment was not orphaned: %+v", n)
		}
	}
}

// TestInstalledCLIKeepsAGroupInOneStore: with the real incomm, two
// repositories' merge requests go into one store at the group's folder, and
// each repository reads back only its own.
func TestInstalledCLIKeepsAGroupInOneStore(t *testing.T) {
	if _, err := CheckCLI(context.Background()); err != nil {
		t.Fatal("the image's incomm: " + err.Error())
	}
	dir := t.TempDir()
	for _, repo := range []string{"gateway", "billing"} {
		if err := os.MkdirAll(filepath.Join(dir, repo), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, repo, "code.go"), []byte("one\ntwo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, repo := range []string{"gateway", "billing"} {
		notes := []forge.Note{{ID: 10 + i, Thread: repo, Body: "about " + repo, Path: "code.go", Line: 1}}
		for range 2 { // again: nothing is added twice
			if err := ImportAt(ctx, Place{Dir: dir, Prefix: repo + "/"}, forge.MergeRequest{}, notes, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, repo := range []string{"gateway", "billing"} {
		threads := ThreadsAt(Place{Dir: dir, Prefix: repo + "/"})
		if len(threads) != 1 || threads[0].File != "code.go" || threads[0].Root.Content != "about "+repo {
			t.Errorf("%s reads %+v", repo, threads)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".incomm", "notes*.json"))
	if len(files) != 1 {
		t.Errorf("the group should have one store, it has %v", files)
	}
}

func TestReanchorMakesThePlanFollowTheCode(t *testing.T) {
	binary, err := CheckCLI(context.Background())
	if err != nil {
		t.Fatal("the image's incomm: " + err.Error())
	}
	dir := t.TempDir()
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package main\n\nfunc a() {}\nfunc b() {}\nfunc c() {}\n")
	run := func(args ...string) []byte {
		cmd := exec.Command(binary, append([]string{"--root", dir, "--json"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("incomm %v: %v\n%s", args, err, out)
		}
		return out
	}
	run("add", "--file", "code.go", "--line", "4", "--content", "about b", "--author", "user",
		"--author-title", "Jan", "--audience", "external")
	line := func() (int, bool) {
		threads := ReadThreads(dir)
		if len(threads) != 1 {
			t.Fatalf("threads = %+v", threads)
		}
		return threads[0].Line, threads[0].Orphaned
	}
	if l, o := line(); l != 4 || o {
		t.Fatalf("before: %d %v", l, o)
	}

	// Lines land above the code: the stored line is now stale.
	write("// new\n// lines\npackage main\n\nfunc a() {}\nfunc b() {}\nfunc c() {}\n")
	if l, _ := line(); l != 4 {
		t.Fatalf("nothing has re-anchored yet, the line is still %d", l)
	}
	if err := Reanchor(context.Background(), dir, filepath.Join(dir, "no-incomm-here"), dir); err != nil {
		t.Fatal(err)
	}
	steps := Plan(ReadThreads(dir))
	if len(steps) != 1 || steps[0].Line != 6 || steps[0].Orphaned {
		t.Fatalf("the plan must follow the code to line 6: %+v", steps)
	}

	// The code is gone: the comment is orphaned, still listed, and marked.
	write("package main\n\nfunc a() {}\nfunc c() {}\n")
	if err := Reanchor(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	steps = Plan(ReadThreads(dir))
	if len(steps) != 1 || !steps[0].Orphaned || !strings.Contains(steps[0].Summary(), "orphaned") {
		t.Fatalf("an orphaned pending comment is listed and marked: %+v", steps)
	}

	// The comment was never touched by a view: it is still there, still external.
	var listed struct{ Notes []json.RawMessage }
	if err := json.Unmarshal(run("--view", "external", "list"), &listed); err != nil || len(listed.Notes) != 1 {
		t.Fatalf("external list: %v %v", listed, err)
	}
}
