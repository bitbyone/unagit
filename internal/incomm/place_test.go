package incomm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

const groupNotes = `{"version":2,"notes":[
 {"id":"g1","file":"gateway/src/a.go","startLine":3,"author":"user","content":"on the gateway","audience":"external"},
 {"id":"b1","file":"billing/b.go","startLine":1,"author":"user","content":"on billing","audience":"external"},
 {"id":"b2","file":"billing/c.go","startLine":2,"author":"user","content":"published","audience":"agent+external","source":{"id":4}},
 {"id":"x1","file":"gatewayish/d.go","startLine":5,"author":"user","content":"another folder","audience":"external"}
]}`

// TestAGroupsCommentsGoToTheirRepository: in a grouped worktree's one store,
// each repository sees only the comments on its own files, named as it names
// them, and they stay where they were read from.
func TestAGroupsCommentsGoToTheirRepository(t *testing.T) {
	dir := worktreeWith(t, map[string]string{"notes.json": groupNotes})

	gateway := ThreadsAt(Place{Dir: dir, Prefix: "gateway/"})
	if len(gateway) != 1 || gateway[0].Root.ID != "g1" || gateway[0].File != "src/a.go" || gateway[0].Dir != dir {
		t.Fatalf("gateway's threads: %+v", gateway)
	}
	billing := ThreadsAt(Place{Dir: dir, Prefix: "billing/"})
	if len(billing) != 2 || billing[0].File != "b.go" || billing[1].File != "c.go" {
		t.Fatalf("billing's threads: %+v", billing)
	}
	if n := PendingAt(Place{Dir: dir, Prefix: "billing/"}); n != 1 {
		t.Errorf("billing has %d pending, want the one not published", n)
	}
	if n := len(ThreadsAt(Place{Dir: dir})); n != 4 {
		t.Errorf("without a prefix every thread is read: %d", n)
	}
}

// TestImportIntoAGroupPutsCommentsUnderTheRepository: a merge request's
// comments land in the group's store on the repository's folder.
func TestImportIntoAGroupPutsCommentsUnderTheRepository(t *testing.T) {
	_, dir := fakeIncomm(t)
	if err := os.MkdirAll(filepath.Join(dir, "gateway"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway", "code.go"), []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := []forge.Note{{ID: 1, Thread: "a", Body: "look here", Path: "code.go", Line: 2}}
	if err := ImportAt(context.Background(), Place{Dir: dir, Prefix: "gateway/"}, forge.MergeRequest{}, notes, nil); err != nil {
		t.Fatal(err)
	}
	calls := commands(t, dir)
	if len(calls) != 1 || !strings.Contains(calls[0], "--file\ngateway/code.go\n") {
		t.Fatalf("the comment was not put on the repository's folder: %q", calls)
	}
}

// TestInstalledCLIKeepsAGroupInOneStore: with the real incomm, two
// repositories' merge requests go into one store at the group's folder, and
// each repository reads back only its own.
func TestInstalledCLIKeepsAGroupInOneStore(t *testing.T) {
	if _, err := CheckCLI(context.Background()); err != nil {
		t.Skip("no usable incomm installed: " + err.Error())
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
