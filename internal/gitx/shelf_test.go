package gitx

import (
	"strings"
	"testing"
)

// TestShelveAndUnshelve: what is not committed - an unversioned file too -
// goes on the shelf under a name, the files back as the commit has them;
// unshelved, it is back and off the shelf.
func TestShelveAndUnshelve(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	write(t, clone, "a.txt", "an edit\n")
	write(t, clone, "new.txt", "new\n")
	must(t, g.Shelve(clone, "half done", nil))
	if got := sh(t, clone, "status", "--porcelain"); got != "" {
		t.Fatalf("not committed after shelving: %q", got)
	}
	shelves, err := g.Shelves(clone)
	must(t, err)
	if len(shelves) != 1 || shelves[0].Name != "half done" || shelves[0].Branch != "main" {
		t.Fatalf("shelf = %+v", shelves)
	}
	files, err := g.ShelfFiles(clone, shelves[0].SHA)
	must(t, err)
	if len(files) != 2 || files[0].Path != "a.txt" || files[1].Path != "new.txt" {
		t.Errorf("files = %+v", files)
	}
	if err := g.Shelve(clone, "empty", nil); err != ErrNothingToShelve {
		t.Errorf("shelving nothing: %v", err)
	}
	must(t, g.Unshelve(clone, shelves[0].SHA, false))
	if got := sh(t, clone, "status", "--porcelain"); got != "M a.txt\n?? new.txt" {
		t.Errorf("after unshelving: %q", got)
	}
	if left, _ := g.Shelves(clone); len(left) != 0 {
		t.Errorf("still on the shelf: %+v", left)
	}
}

// TestUnshelveRefusesWhatWouldCollide: a file edited here too, or a change
// the branch has since made to the same lines, leaves everything as it was.
func TestUnshelveRefusesWhatWouldCollide(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	write(t, clone, "a.txt", "shelved\n")
	must(t, g.Shelve(clone, "s", []string{"a.txt"}))
	sha := sh(t, clone, "rev-parse", "stash@{0}")

	write(t, clone, "a.txt", "here too\n")
	if why := g.UnshelveBlocked(clone, sha); !strings.Contains(why, "a.txt") {
		t.Errorf("an edit here too: %q", why)
	}
	sh(t, clone, "commit", "-q", "-am", "committed over it")
	if why := g.UnshelveBlocked(clone, sha); !strings.Contains(why, "conflict") {
		t.Errorf("a commit over the same lines: %q", why)
	}
	if err := g.Unshelve(clone, sha, false); err == nil {
		t.Error("it unshelved")
	}
	if got := sh(t, clone, "status", "--porcelain"); got != "" {
		t.Errorf("the refusal changed %q", got)
	}
}

// TestADeletedShelfComesBack: deleting is written down, and its undo puts
// the shelf back as it was.
func TestADeletedShelfComesBack(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	write(t, clone, "a.txt", "an edit\n")
	must(t, g.Shelve(clone, "keep me", nil))
	sha := sh(t, clone, "rev-parse", "stash@{0}")
	r, err := g.DeleteShelf(clone, sha)
	must(t, err)
	if left, _ := g.Shelves(clone); len(left) != 0 {
		t.Fatalf("still on the shelf: %+v", left)
	}
	undo, err := g.UndoRewrite(clone, r.ID)
	must(t, err)
	shelves, _ := g.Shelves(clone)
	if len(shelves) != 1 || shelves[0].SHA != sha || shelves[0].Name != "keep me" {
		t.Errorf("shelf = %+v", shelves)
	}
	if plan, err := g.PlanUndo(clone, undo.ID); err != nil || plan.Blocked == "" {
		t.Errorf("undoing the undo: %+v, %v", plan, err)
	}
}
