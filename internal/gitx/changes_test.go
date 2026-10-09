package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// changesFixture is a clone with one file of every kind: changed, added,
// deleted, renamed, unversioned in a folder of its own.
func changesFixture(t *testing.T) string {
	t.Helper()
	_, clone := repos(t)
	identify(t, clone)
	commit(t, clone, "gone.txt", "gone")
	commit(t, clone, "old.txt", "a file long enough to be known again after its rename\n")
	write(t, clone, "a.txt", "changed\n")
	write(t, clone, "added.txt", "added\n")
	sh(t, clone, "add", "added.txt")
	sh(t, clone, "rm", "-q", "gone.txt")
	sh(t, clone, "mv", "old.txt", "new.txt")
	must(t, os.MkdirAll(filepath.Join(clone, "dir"), 0o755))
	write(t, clone, "dir/loose.txt", "loose\n")
	write(t, clone, "*.txt", "a star\n")
	return clone
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestChangesNameEveryKind(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	changes, err := New("", nil).Changes(clone)
	must(t, err)
	var got []string
	for _, c := range changes {
		got = append(got, c.Path+"<"+c.From+">"+string("MADRCU"[c.Kind]))
	}
	want := "a.txt<>M added.txt<>A gone.txt<>D new.txt<old.txt>R *.txt<>U dir/loose.txt<>U"
	if strings.Join(got, " ") != want {
		t.Errorf("changes:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
}

func TestChangeDiff(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	g := New("", nil)
	diff, err := g.ChangeDiff(clone, Change{Path: "a.txt", Kind: Modified})
	if err != nil || !strings.Contains(diff, "-initial") || !strings.Contains(diff, "+changed") {
		t.Errorf("a change: %v\n%s", err, diff)
	}
	diff, err = g.ChangeDiff(clone, Change{Path: "dir/loose.txt", Kind: Unversioned})
	if err != nil || !strings.Contains(diff, "+loose") {
		t.Errorf("an unversioned file: %v\n%s", err, diff)
	}
	diff, err = g.ChangeDiff(clone, Change{Path: "new.txt", From: "old.txt", Kind: Renamed})
	if err != nil || !strings.Contains(diff, "rename from old.txt") {
		t.Errorf("a rename: %v\n%s", err, diff)
	}
}

// TestCommitPathsTakesWhatIsChosen: the chosen files go in as they are on
// disk, an unversioned one added on the way; the rest stay as they were.
func TestCommitPathsTakesWhatIsChosen(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	g := New("", nil)
	sha, err := g.CommitPaths(clone, "Take two", []Change{
		{Path: "new.txt", From: "old.txt", Kind: Renamed},
		{Path: "*.txt", Kind: Unversioned},
	})
	if err != nil || sha == "" {
		t.Fatalf("commit = %q, %v", sha, err)
	}
	if got := sh(t, clone, "show", "--name-status", "--format=", "-M", "HEAD"); got != "A\t*.txt\nR100\told.txt\tnew.txt" {
		t.Errorf("committed:\n%s", got)
	}
	if got := sh(t, clone, "status", "--porcelain", "--untracked-files=all"); got != "M a.txt\nA  added.txt\nD  gone.txt\n?? dir/loose.txt" {
		t.Errorf("left:\n%s", got)
	}
}

// TestRollbackPutsBackWhatHeadHas, and keeps every file that HEAD lacks on
// disk; an unversioned file is not git's to roll back.
func TestRollbackPutsBackWhatHeadHas(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	g := New("", nil)
	must(t, g.Rollback(clone, []Change{
		{Path: "a.txt", Kind: Modified},
		{Path: "added.txt", Kind: Added},
		{Path: "gone.txt", Kind: Deleted},
		{Path: "new.txt", From: "old.txt", Kind: Renamed},
	}))
	if got := sh(t, clone, "status", "--porcelain", "--untracked-files=all"); got != "?? *.txt\n?? added.txt\n?? dir/loose.txt\n?? new.txt" {
		t.Errorf("after the rollback:\n%s", got)
	}
	for _, name := range []string{"gone.txt", "old.txt"} {
		if _, err := os.Stat(filepath.Join(clone, name)); err != nil {
			t.Errorf("%s is not back: %v", name, err)
		}
	}
	if err := g.Rollback(clone, []Change{{Path: "dir/loose.txt", Kind: Unversioned}}); err == nil {
		t.Error("an unversioned file was rolled back")
	}
}

// TestDeleteUnversionedRefusesAVersionedFile.
func TestDeleteUnversionedRefusesAVersionedFile(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	g := New("", nil)
	if err := g.DeleteUnversioned(clone, []string{"dir/loose.txt", "a.txt"}); err == nil {
		t.Fatal("a versioned file was deleted")
	}
	if _, err := os.Stat(filepath.Join(clone, "dir/loose.txt")); err != nil {
		t.Error("a refusal still deleted something")
	}
	must(t, g.DeleteUnversioned(clone, []string{"dir/loose.txt", "*.txt"}))
	for _, name := range []string{"dir/loose.txt", "*.txt", "a.txt"} {
		_, err := os.Stat(filepath.Join(clone, name))
		if gone := os.IsNotExist(err); gone != (name != "a.txt") {
			t.Errorf("%s: gone %v", name, gone)
		}
	}
}
