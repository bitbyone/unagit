package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/incomm"
)

const pendingNotes = `{"version":2,"notes":[
 {"id":"n1","file":"a.go","startLine":3,"author":"user","authorTitle":"Jan","content":"pending root","audience":"agent+external",
  "replies":[{"id":"r1","author":"agent","authorTitle":"Opus 5","content":"pending reply","audience":"external"},
             {"id":"r2","author":"user","content":"secret","audience":"private"}]},
 {"id":"n2","file":"b.go","startLine":1,"author":"user","content":"private","audience":"private"}
]}`

// gatewayWorktrees creates the two worktree directories of !7 with Incomm
// comments in them: two pending in the branch one, one in the review one.
func gatewayWorktrees(t *testing.T, a *App) (id string) {
	t.Helper()
	id = onLoop(a, func() string { return a.cfg.Instances[0].ID })
	dir := onLoop(a, func() string { return a.projectDir(id, "acme/gateway") })
	root := filepath.Join(filepath.Dir(dir), ".unagit", "gateway")
	for name, notes := range map[string]string{
		"7-feat-rate":        pendingNotes,
		"review-7-feat-rate": `{"version":2,"notes":[{"id":"x","file":"c.go","startLine":2,"author":"user","content":"marked","audience":"external"}]}`,
	} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, name, ".incomm"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, ".incomm", "notes_main.json"), []byte(notes), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// refreshLists redraws the way the application does after a task: the disk
// state again, then the list.
func refreshLists(a *App) {
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.mrsPane.reload()
		close(done)
	})
	<-done
}

func TestPendingCommentsAreCountedInThePubColumn(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	id := gatewayWorktrees(t, a)

	// With Incomm off nothing is read and there is no column.
	refreshLists(a)
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	if strings.Contains(a.screenText(sc), "PUB") {
		t.Fatal("the PUB column is shown although Incomm is off")
	}
	if got := onLoop(a, func() int { return a.diskOf(id, "acme/gateway").MRs[7].Pending }); got != 0 {
		t.Fatalf("pending = %d with Incomm off", got)
	}

	onLoop(a, func() bool { a.cfg.Integrations.Incomm = true; return true })
	refreshLists(a)
	waitFor(t, a, sc, "PUB")
	// The branch worktree has a pending comment and reply (the private ones do
	// not count) and the review worktree one more.
	if got := onLoop(a, func() int { return a.diskOf(id, "acme/gateway").MRs[7].Pending }); got != 3 {
		t.Fatalf("pending = %d, want 3", got)
	}
	// It reads as a number under the header, on the row of !7 and not on the others.
	lines := strings.Split(a.screenText(sc), "\n")
	header, row := "", ""
	for _, l := range lines {
		if strings.Contains(l, "PUB") {
			header = l
		}
		if strings.Contains(l, "Rate limiting") {
			row = l
		}
	}
	// Count in cells, not bytes: the row starts with a multi-byte mark.
	head, cells := []rune(header), []rune(row)
	at := len([]rune(header[:strings.Index(header, "PUB")]))
	if strings.Index(header, "PUB") < 0 || len(cells) < at+3 || strings.TrimSpace(string(cells[at:at+3])) != "3" {
		t.Errorf("PUB column does not line up:\n%s\n%s", string(head), row)
	}
}

func TestPublishAsksFirstAndSaysWhenThereIsNothing(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	gatewayWorktrees(t, a)
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	// Off: it says so instead of looking.
	typeRunes(sc, "P")
	waitFor(t, a, sc, "Incomm is off")

	onLoop(a, func() bool { a.cfg.Integrations.Incomm = true; return true })
	refreshLists(a)
	typeRunes(sc, "P")
	waitFor(t, a, sc, "Publish 3 comments to acme/gateway !7?")
	screen := a.screenText(sc)
	// The list names each item, marks the parent that only goes out because a
	// reply needs it (here the review comment is a root of its own), and keeps
	// the private one out.
	for _, want := range []string{"a.go:3", "reply by agent: pending reply", "c.go:2", "posted with your token"} {
		if !strings.Contains(screen, want) {
			t.Errorf("confirmation is missing %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "secret") || strings.Contains(screen, "b.go") {
		t.Errorf("a private comment is listed:\n%s", screen)
	}
	// Cancelling posts nothing and leaves the count.
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Publish 3 comments")

	// !8 has no worktrees at all.
	typeRunes(sc, "jj")
	typeRunes(sc, "P")
	waitFor(t, a, sc, "nothing to publish on !8")
}

func TestLocalCommentsShowWhatIsNotPublished(t *testing.T) {
	threads := incomm.ThreadsOf(worktreeOf(t, `{"version":2,"notes":[
 {"id":"a","file":"a.go","startLine":3,"author":"user","authorTitle":"Jan","content":"waiting **bold**","audience":"agent+external",
  "replies":[{"id":"a1","author":"agent","authorTitle":"Opus 5","content":"my answer","audience":"external"}]},
 {"id":"b","file":"b.go","startLine":4,"author":"agent","content":"only a note to self","audience":"agent"},
 {"id":"c","file":"c.go","startLine":5,"author":"user","content":"on the forge already","audience":"agent+external","source":{"id":4}},
 {"id":"d","file":"d.go","startLine":6,"author":"user","content":"hidden","audience":"private"},
 {"id":"e","file":"e.go","startLine":7,"author":"user","content":"answered later","audience":"agent+external","source":{"id":5},
  "replies":[{"id":"e1","author":"user","content":"new reply","audience":"agent+external"}]}
]}`))
	got := renderLocalThreads(threads)
	for _, want := range []string{"Local comments", "a.go:3", "not published", "Agent (Opus 5)", "my answer",
		"b.go:4", "local", "e.go:7", "new reply"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// What is on the forge and has nothing waiting is not shown again, and a
	// private comment never is.
	for _, unwanted := range []string{"c.go", "on the forge already", "d.go", "hidden"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q should not be shown:\n%s", unwanted, got)
		}
	}
	if renderLocalThreads(nil) != "" {
		t.Error("no threads, no section")
	}
}

func worktreeOf(t *testing.T, notes string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".incomm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".incomm", "notes_main.json"), []byte(notes), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPublishSummaryKeepsToOneScreen(t *testing.T) {
	var steps []incomm.Step
	for i := range 15 {
		steps = append(steps, incomm.Step{File: "a.go", Line: i + 1, IsRoot: true,
			Comment: incomm.Comment{Author: "user", Content: fmt.Sprintf("comment %d", i)}})
	}
	got := publishSummary("acme/gateway", forge.MergeRequest{IID: 7}, steps)
	if !strings.Contains(got, "Publish 15 comments to") || !strings.Contains(got, "and 3 more") || strings.Contains(got, "comment 14") {
		t.Errorf("summary:\n%s", got)
	}
	one := publishSummary("acme/gateway", forge.MergeRequest{IID: 7}, steps[:1])
	if !strings.Contains(one, "Publish 1 comment to") {
		t.Errorf("singular: %s", one)
	}
}
