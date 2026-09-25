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

// fakeScript stands in for the incomm CLI: it answers `version` and `list` (from
// agent.json / external.json in the worktree) and records every other command in
// `calls`, one block per command separated by a line of dashes. `add` answers
// with an id, like the real one does.
const fakeScript = `#!/bin/sh
root="$2"
view=agent
prev=""
for a in "$@"; do
  if [ "$prev" = --view ]; then view="$a"; fi
  if [ "$a" = version ]; then echo '{"version":"1.1.0","formatVersion":2}'; exit 0; fi
  prev="$a"
done
for a in "$@"; do
  if [ "$a" = list ]; then
    if [ -f "$root/$view.json" ]; then cat "$root/$view.json"; else echo '{"notes":[]}'; fi
    exit 0
  fi
done
printf '%s\n' "$@" >> "$root/calls"
printf -- '---\n' >> "$root/calls"
n=$(grep -c '^---$' "$root/calls")
echo "{\"id\":\"n$n\"}"
`

// fakeIncomm puts the fake first on PATH and returns a worktree to import into.
func fakeIncomm(t *testing.T) (bin, dir string) {
	t.Helper()
	bin, dir = t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(bin, "incomm"), []byte(fakeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// commands returns the recorded commands, each as its argument lines.
func commands(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, block := range strings.Split(string(data), "---\n") {
		if strings.TrimSpace(block) != "" {
			out = append(out, block)
		}
	}
	return out
}

func TestImportAddsCommentsWithAudienceAndSource(t *testing.T) {
	_, dir := fakeIncomm(t)
	notes := []forge.Note{
		{ID: 1, Thread: "a", Body: "literal $(touch BAD)\nsecond line", Path: "code.go", Line: 2,
			Author: forge.User{Name: "Reviewer"}, URL: "https://forge/mr/1#note_1"},
		{ID: 2, Thread: "a", Body: "a reply", Author: forge.User{Username: "dev"}},
		{ID: 4, Body: "general"},
		{ID: 7, Body: "system", Path: "code.go", Line: 1, System: true},
	}
	var logs []string
	if err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "BAD")); err == nil {
		t.Fatal("a comment text was run as a command")
	}
	calls := commands(t, dir)
	if len(calls) != 2 {
		t.Fatalf("want an add and a reply, got %d commands: %q", len(calls), calls)
	}
	add, reply := calls[0], calls[1]
	for _, want := range []string{"\nadd\n", "--content\nliteral $(touch BAD)\nsecond line\n", "--author\nuser\n",
		"--author-title\nReviewer\n", "--audience\nagent+external\n", "--source-id\n1\n",
		"--source-url\nhttps://forge/mr/1#note_1\n", "--source-thread\na\n", "--line\n2\n"} {
		if !strings.Contains(add, want) {
			t.Errorf("add is missing %q:\n%s", want, add)
		}
	}
	if strings.Contains(add, "Source: ") {
		t.Error("the origin must be in the source field, not in the text")
	}
	// The reply goes to the comment the add created, as a reply, not a new comment.
	for _, want := range []string{"\nreply\nn1\n", "--content\na reply\n", "--author-title\ndev\n",
		"--audience\nagent+external\n", "--source-id\n2\n", "--source-url\nhttps://forge/mr/1#note_2\n"} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply is missing %q:\n%s", want, reply)
		}
	}
	if strings.Contains(reply, "--source-thread") {
		t.Error("a reply has no thread of its own")
	}
	if !strings.Contains(strings.Join(logs, "\n"), "imported 1 comments") || !strings.Contains(strings.Join(logs, "\n"), "imported 1 replies") {
		t.Errorf("logs: %v", logs)
	}
}

func TestImportRecognisesWhatIsAlreadyThere(t *testing.T) {
	_, dir := fakeIncomm(t)
	// Comment 1 is in the agent view, comment 3 was made external-only by the human
	// and is only in the external view; comment 2 is a reply that was imported.
	writeJSON(t, filepath.Join(dir, "agent.json"), map[string]any{"notes": []map[string]any{
		{"id": "x1", "content": "one", "source": map[string]any{"id": 1}, "replies": []map[string]any{{"id": "r1", "source": map[string]any{"id": 2}}}},
	}})
	writeJSON(t, filepath.Join(dir, "external.json"), map[string]any{"notes": []map[string]any{
		{"id": "x3", "content": "three", "source": map[string]any{"id": 3}},
	}})
	notes := []forge.Note{
		{ID: 1, Thread: "a", Body: "one", Path: "code.go", Line: 1},
		{ID: 2, Thread: "a", Body: "reply"},
		{ID: 3, Thread: "b", Body: "three", Path: "code.go", Line: 2},
	}
	for range 2 {
		if err := Import(context.Background(), dir, forge.MergeRequest{}, notes, nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls := commands(t, dir); len(calls) != 0 {
		t.Fatalf("importing again changed something: %q", calls)
	}
}

func TestImportGivesAnEarlierImportItsSourceInsteadOfDuplicatingIt(t *testing.T) {
	_, dir := fakeIncomm(t)
	// What an earlier unagit wrote: the origin at the end of the text, no source.
	writeJSON(t, filepath.Join(dir, "agent.json"), map[string]any{"notes": []map[string]any{
		{"id": "old1", "content": "already here\n\nSource: https://forge/mr/1 (comment 3)"},
		{"id": "old2", "content": "a reply that was flattened\n\nSource: https://forge/mr/1 (comment 4)"},
	}})
	notes := []forge.Note{
		{ID: 3, Thread: "a", Body: "already here", Path: "code.go", Line: 1, URL: "https://forge/mr/1#note_3"},
		{ID: 4, Thread: "a", Body: "a reply that was flattened", URL: "https://forge/mr/1#note_4"},
	}
	var logs []string
	if err := Import(context.Background(), dir, forge.MergeRequest{WebURL: "https://forge/mr/1"}, notes, func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	calls := commands(t, dir)
	if len(calls) != 2 {
		t.Fatalf("want two set commands and nothing added, got %q", calls)
	}
	root, flat := calls[0], calls[1]
	for _, want := range []string{"\nset\nold1\n", "--audience\nagent+external\n", "--source-id\n3\n",
		"--source-url\nhttps://forge/mr/1#note_3\n", "--source-thread\na\n"} {
		if !strings.Contains(root, want) {
			t.Errorf("root set is missing %q:\n%s", want, root)
		}
	}
	if !strings.Contains(flat, "\nset\nold2\n") || !strings.Contains(flat, "--source-id\n4\n") || strings.Contains(flat, "--source-thread") {
		t.Errorf("reply set: %s", flat)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "recorded the origin of 2") {
		t.Errorf("logs: %v", logs)
	}
	// Running it again finds nothing left to do once the CLI reports the source.
}

func TestImportOrphansAndAttachesRepliesToThem(t *testing.T) {
	_, dir := fakeIncomm(t)
	notes := []forge.Note{
		{ID: 5, Thread: "g", Body: "deleted file", Path: "gone.go", Line: 1, URL: "https://forge/mr/1#note_5"},
		{ID: 6, Thread: "g", Body: "answer"},
		{ID: 8, Body: "past end", Path: "code.go", Line: 9},
	}
	var logs []string
	if err := Import(context.Background(), dir, forge.MergeRequest{}, notes, func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".incomm", "notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored orphanResult
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Notes) != 2 || stored.Version != 2 {
		t.Fatalf("stored: %s", data)
	}
	first := stored.Notes[0]
	if first.Audience != "agent+external" || first.Source == nil || first.Source.ID != 5 || first.Source.Thread != "g" {
		t.Errorf("orphan: %+v", first)
	}
	calls := commands(t, dir)
	if len(calls) != 1 || !strings.Contains(calls[0], "\nreply\n"+first.ID+"\n") || !strings.Contains(calls[0], "--source-id\n6\n") {
		t.Fatalf("the reply must go to the orphan %q: %q", first.ID, calls)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "imported 2 orphaned") {
		t.Errorf("logs: %v", logs)
	}
}

func TestImportOrphansACommentWhoseLineIsGone(t *testing.T) {
	_, dir := fakeIncomm(t)
	writeJSON(t, filepath.Join(dir, "agent.json"), map[string]any{"notes": []map[string]any{
		{"id": "x1", "content": "one", "source": map[string]any{"id": 1}},
	}})
	if err := Import(context.Background(), dir, forge.MergeRequest{}, []forge.Note{
		{ID: 1, Thread: "a", Body: "one", Path: "code.go", Line: 9},
	}, nil); err != nil {
		t.Fatal(err)
	}
	calls := commands(t, dir)
	if len(calls) != 1 || !strings.Contains(calls[0], "\nanchor\nset\nx1\n") || !strings.Contains(calls[0], "--orphaned\n") {
		t.Fatalf("calls: %q", calls)
	}
}

func TestImportFailureReachesTheCaller(t *testing.T) {
	bin, dir := fakeIncomm(t)
	// version and list work, everything else fails: a failed write must
	// propagate, because it gates starting the editor.
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = version ]; then echo '{\"formatVersion\":2}'; exit 0; fi\n  if [ \"$a\" = list ]; then echo '{\"notes\":[]}'; exit 0; fi\ndone\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "incomm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Import(context.Background(), dir, forge.MergeRequest{}, []forge.Note{{ID: 1, Thread: "a", Body: "x", Path: "code.go", Line: 1}}, nil)
	if err == nil {
		t.Fatal("failed import succeeded")
	}
}

func TestImportRefusesAnOldCLI(t *testing.T) {
	bin, dir := fakeIncomm(t)
	// A 1.0.x CLI has no version command, and would drop audience and source.
	if err := os.WriteFile(filepath.Join(bin, "incomm"), []byte("#!/bin/sh\necho 'unknown command' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Import(context.Background(), dir, forge.MergeRequest{}, []forge.Note{{ID: 1, Body: "x", Path: "code.go", Line: 1}}, nil)
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".incomm")); err == nil {
		t.Error("nothing may be written with an old CLI")
	}
}

func TestImportMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Import(context.Background(), t.TempDir(), forge.MergeRequest{}, []forge.Note{{Path: "code.go", Line: 1}}, nil); err == nil {
		t.Fatal("missing binary accepted")
	}
}

func TestImportThreadsGroupsAndLocates(t *testing.T) {
	threads := importThreads([]forge.Note{
		{ID: 1, Thread: "a", Body: "root", Path: "a.go", Line: 3},
		{ID: 2, Thread: "b", Body: "reply first, no location"},
		{ID: 3, Thread: "b", Body: "located reply", Path: "b.go", Line: 8, Orphaned: true},
		{ID: 4, Thread: "a", Body: "reply"},
		{ID: 5, Body: "general"},
		{ID: 6, Thread: "c", Body: "system", Path: "a.go", Line: 1, System: true},
		{ID: 7, Path: "c.go", Line: 2, Body: "alone"},
	})
	if len(threads) != 3 {
		t.Fatalf("threads = %+v", threads)
	}
	if threads[0].Root.ID != 1 || len(threads[0].Replies) != 1 || threads[0].Replies[0].ID != 4 {
		t.Errorf("a: %+v", threads[0])
	}
	// The conversation takes its location from the first comment that has one.
	if b := threads[1]; b.Root.ID != 2 || b.Root.Path != "b.go" || b.Root.Line != 8 || !b.Root.Orphaned {
		t.Errorf("b: %+v", b)
	}
	if threads[2].Root.ID != 7 || len(threads[2].Replies) != 0 {
		t.Errorf("standalone: %+v", threads[2])
	}
}

// This exercises the real CLI when one that understands audience and source is
// installed, without putting anything in the user's repositories.
func TestInstalledCLI(t *testing.T) {
	binary, err := CheckCLI(context.Background())
	if err != nil {
		t.Skip("no usable incomm installed: " + err.Error())
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

func TestGeneralCommentsDoNotNeedIncomm(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Import(context.Background(), t.TempDir(), forge.MergeRequest{}, []forge.Note{{Body: "general"}}, nil); err != nil {
		t.Fatal(err)
	}
}
