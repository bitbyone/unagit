package gitx

import (
	"fmt"
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

// TestAddFilesMakesThemAdded: an unversioned file added is a change of the
// added kind, one name with a star in it no pattern.
func TestAddFilesMakesThemAdded(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	g := New("", nil)
	write(t, clone, "*.go", "a star\n")
	must(t, g.AddFiles(clone, []string{"dir/loose.txt", "*.txt"}))
	changes, err := g.Changes(clone)
	must(t, err)
	kinds := map[string]ChangeKind{}
	for _, c := range changes {
		kinds[c.Path] = c.Kind
	}
	for path, want := range map[string]ChangeKind{"dir/loose.txt": Added, "*.txt": Added, "*.go": Unversioned} {
		if kinds[path] != want {
			t.Errorf("%s is %d, want %d", path, kinds[path], want)
		}
	}
}

// TestChangesCountLines: each file says how many of its lines differ from
// HEAD - a rename by its moved content, an unversioned file all of it.
func TestChangesCountLines(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	write(t, clone, "a.txt", "changed\nand more\n")
	write(t, clone, "bin.dat", "a\x00b")
	changes, err := New("", nil).Changes(clone)
	must(t, err)
	got := map[string]string{}
	for _, c := range changes {
		got[c.Path] = fmt.Sprintf("+%d-%d %v", c.Added, c.Deleted, c.Binary)
	}
	for path, want := range map[string]string{
		"a.txt": "+2-1 false", "added.txt": "+1-0 false", "gone.txt": "+0-1 false",
		"new.txt": "+0-0 false", "dir/loose.txt": "+1-0 false", "bin.dat": "+0-0 true",
	} {
		if got[path] != want {
			t.Errorf("%s: %s, want %s", path, got[path], want)
		}
	}
}

// TestIgnoreAnchorsEachPathLiterally: a file is ignored by its own path
// alone, a star in its name no pattern, and what was there is kept.
func TestIgnoreAnchorsEachPathLiterally(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	write(t, clone, ".gitignore", "*.log")
	write(t, clone, "notes.md", "n\n")
	must(t, New("", nil).Ignore(clone, []string{"dir/loose.txt", "*.txt"}))
	if got, _ := os.ReadFile(filepath.Join(clone, ".gitignore")); string(got) != "*.log\n/dir/loose.txt\n/\\*.txt\n" {
		t.Errorf(".gitignore = %q", got)
	}
	status := sh(t, clone, "status", "--porcelain", "--untracked-files=all")
	for _, gone := range []string{"loose.txt", "*.txt"} {
		if strings.Contains(status, gone) {
			t.Errorf("%s is still listed:\n%s", gone, status)
		}
	}
	if !strings.Contains(status, "notes.md") {
		t.Errorf("an ignore by path took another file too:\n%s", status)
	}
}

// TestPatchAppliesElsewhere: the patch of a change, a rename and an
// unversioned file, applied to a fresh clone, makes the same files.
func TestPatchAppliesElsewhere(t *testing.T) {
	t.Parallel()
	clone := changesFixture(t)
	g := New("", nil)
	patch, err := g.Patch(clone, []Change{
		{Path: "a.txt", Kind: Modified},
		{Path: "new.txt", From: "old.txt", Kind: Renamed},
		{Path: "dir/loose.txt", Kind: Unversioned},
	})
	must(t, err)
	other := filepath.Join(t.TempDir(), "other")
	sh(t, clone, "worktree", "add", "-q", "--detach", other, "HEAD")
	file := filepath.Join(t.TempDir(), "change.patch")
	must(t, os.WriteFile(file, []byte(patch), 0o644))
	sh(t, other, "apply", file)
	for _, name := range []string{"a.txt", "new.txt", "dir/loose.txt"} {
		want, _ := os.ReadFile(filepath.Join(clone, name))
		got, err := os.ReadFile(filepath.Join(other, name))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: %q, want %q (%v)", name, got, want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(other, "old.txt")); !os.IsNotExist(err) {
		t.Error("the rename left the old name behind")
	}
}
