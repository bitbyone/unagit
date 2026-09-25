package incomm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func worktreeWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".incomm"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, ".incomm", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const mixedNotes = `{"version":2,"notes":[
 {"id":"n1","file":"a.go","startLine":3,"author":"user","authorTitle":"Jan","content":"pending root","audience":"agent+external",
  "replies":[
    {"id":"r1","author":"agent","authorTitle":"Opus 5","content":"pending reply","audience":"external"},
    {"id":"r2","author":"user","content":"secret aside","audience":"private"},
    {"id":"r3","author":"user","content":"already there","audience":"agent+external","source":{"id":9,"url":"https://f/9"}},
    {"id":"r4","author":"user","content":"agent only","audience":"agent"}
  ]},
 {"id":"n2","file":"b.go","startLine":1,"author":"user","content":"private","audience":"private",
  "replies":[{"id":"r5","author":"user","content":"stored as external","audience":"external"}]},
 {"id":"n3","file":"c.go","startLine":5,"author":"user","content":"imported","audience":"agent+external","source":{"id":4,"url":"https://f/4","thread":"th"},
  "replies":[{"id":"r6","author":"agent","content":"answer","audience":"agent+external"}]},
 {"id":"n4","file":"d.go","startLine":2,"author":"user","content":"future audience","audience":"team"},
 {"id":"n5","file":"e.go","startLine":7,"author":"user","content":"plain agent note"}
]}`

func TestReadThreadsIgnoresPrivateAndCountsWhatIsPending(t *testing.T) {
	dir := worktreeWith(t, map[string]string{"notes_main.json": mixedNotes})
	threads := ReadThreads(dir)
	byID := map[string]Thread{}
	for _, th := range threads {
		byID[th.Root.ID] = th
	}
	if _, ok := byID["n2"]; ok {
		t.Error("a private thread must not be read, whatever its replies say")
	}
	if _, ok := byID["n4"]; ok {
		t.Error("an audience this build does not know counts as private")
	}
	n1 := byID["n1"]
	if len(n1.Replies) != 3 {
		t.Fatalf("the private reply must be dropped: %+v", n1.Replies)
	}
	// The root and the external reply are pending; the published one and the
	// agent-only one are not.
	if n1.PendingCount() != 2 || !n1.Root.Pending() || !n1.Replies[0].Pending() || n1.Replies[1].Pending() || n1.Replies[2].Pending() {
		t.Errorf("n1 pending = %d: %+v", n1.PendingCount(), n1)
	}
	// n3 is on the forge already; only the agent's reply, marked for it, waits.
	if n3 := byID["n3"]; n3.PendingCount() != 1 || n3.Root.Pending() || !n3.Root.OnForge() || n3.Root.Source.Thread != "th" || !n3.Replies[0].Pending() {
		t.Errorf("n3: %+v", n3)
	}
	if n5 := byID["n5"]; n5.PendingCount() != 0 {
		t.Errorf("a note for the agent is not pending: %+v", n5)
	}
	if got := PendingIn(dir); got != 3 {
		t.Errorf("PendingIn = %d, want 3", got)
	}
}

func TestReadThreadsSkipsWhatItCannotUnderstand(t *testing.T) {
	dir := worktreeWith(t, map[string]string{
		"notes.json":        `{"version":3,"notes":"a shape from the future"}`,
		"notes_broken.json": `{"version":2,"notes":[`,
		"notes_ok.json":     `{"version":1,"notes":[{"id":"o","file":"a.go","startLine":1,"author":"user","content":"x","audience":"external"}]}`,
	})
	threads := ReadThreads(dir)
	if len(threads) != 1 || threads[0].Root.ID != "o" || PendingIn(dir) != 1 {
		t.Fatalf("threads = %+v", threads)
	}
	if got := PendingIn(t.TempDir()); got != 0 {
		t.Errorf("a worktree without notes has %d pending", got)
	}
}

func TestPlanPublishesOnlyWhatIsPendingParentFirst(t *testing.T) {
	dir := worktreeWith(t, map[string]string{"notes_main.json": `{"version":2,"notes":[
 {"id":"a","file":"a.go","startLine":3,"author":"user","content":"root","audience":"external","replies":[
   {"id":"a1","author":"agent","content":"reply","audience":"external"},
   {"id":"a2","author":"agent","content":"not marked","audience":"agent"}]},
 {"id":"b","file":"b.go","startLine":4,"author":"user","content":"agent-only root","audience":"agent","replies":[
   {"id":"b1","author":"user","content":"marked reply","audience":"external"}]},
 {"id":"c","file":"c.go","startLine":5,"author":"user","content":"on the forge","audience":"agent+external","source":{"id":1,"thread":"t"},"replies":[
   {"id":"c1","author":"user","content":"marked reply","audience":"agent+external"}]},
 {"id":"d","file":"d.go","startLine":6,"author":"user","content":"nothing to do","audience":"agent"}
]}`})
	steps := Plan(ReadThreads(dir))
	type shape struct {
		id, reply    string
		root, parent bool
	}
	var got []shape
	for _, s := range steps {
		sh := shape{id: s.Root.ID, root: s.IsRoot, parent: s.Parent}
		if !s.IsRoot {
			sh.reply = s.Comment.ID
		}
		got = append(got, sh)
	}
	want := []shape{
		{id: "a", root: true}, {id: "a", reply: "a1"},
		{id: "b", root: true, parent: true}, {id: "b", reply: "b1"},
		{id: "c", reply: "c1"},
	}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBodyNamesTheAgentAndLeavesPeopleAlone(t *testing.T) {
	if got := Body(Comment{Author: "user", Title: "Jan", Content: "as written"}); got != "as written" {
		t.Errorf("user body = %q", got)
	}
	if got := Body(Comment{Author: "agent", Title: "Opus 5", Content: "text"}); got != "**Agent (Opus 5):**\n\ntext" {
		t.Errorf("agent body = %q", got)
	}
	if got := Body(Comment{Author: "agent", Content: "text"}); got != "**Agent:**\n\ntext" {
		t.Errorf("untitled agent body = %q", got)
	}
}

func TestSummaryIsOneShortLine(t *testing.T) {
	s := Step{File: "a.go", Line: 3, IsRoot: true, Parent: true,
		Comment: Comment{Author: "user", Content: "first line\nsecond line and a good deal more text than fits on one line of a confirmation dialog"}}
	got := s.Summary()
	if !strings.HasPrefix(got, "a.go:3  comment, only because a reply needs it by you: first line second line") ||
		!strings.HasSuffix(got, "…") || strings.Contains(got, "\n") {
		t.Errorf("summary = %q", got)
	}
	if short := (Step{File: "b.go", Line: 1, Comment: Comment{Author: "agent", Content: "ok"}}).Summary(); short != "b.go:1  reply by agent: ok" {
		t.Errorf("short summary = %q", short)
	}
}
