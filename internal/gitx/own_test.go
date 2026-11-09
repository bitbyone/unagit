package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnagitNotesWhatItMovesInAWatchedClone: in a clone watched for local
// changes, a branch and a tag unagit makes are noted as its own, and a
// commit made beside it is not; a clone nobody watches gets no note.
func TestUnagitNotesWhatItMovesInAWatchedClone(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	if _, err := g.Run(clone, "branch", "quiet"); err != nil {
		t.Fatal(err)
	}
	if OwnRefs(clone) != nil {
		t.Fatal("a clone nobody watches has a note")
	}
	common, _ := CommonDir(clone)
	if _, err := os.Stat(filepath.Join(common, ownFile)); err == nil {
		t.Fatal("the note was written for a clone nobody watches")
	}

	if err := Follow(clone, true); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(clone, "branch", "feat"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run(clone, "tag", "v1"); err != nil {
		t.Fatal(err)
	}
	commit(t, clone, "b.txt", "by an agent")
	refs, err := LocalRefs(clone)
	if err != nil {
		t.Fatal(err)
	}
	own := OwnRefs(clone)
	if own["refs/heads/feat"] != refs["refs/heads/feat"] || own["refs/tags/v1"] != refs["refs/tags/v1"] {
		t.Errorf("unagit's branch and tag are not noted as its own: %v", own)
	}
	if _, ok := own["refs/heads/main"]; ok {
		t.Errorf("the commit made beside unagit is noted as its own: %v", own)
	}
	if _, err := g.Run(clone, "branch", "-D", "feat"); err != nil {
		t.Fatal(err)
	}
	if to, ok := OwnRefs(clone)["refs/heads/feat"]; !ok || to != "" {
		t.Errorf("a branch unagit deleted is not noted: %q %v", to, ok)
	}

	n, by, subject, err := NewCommits(clone, refs["refs/heads/feat"], refs["refs/heads/main"])
	if err != nil || n != 1 || by != "test" || subject != "by an agent" {
		t.Errorf("new commits: %d %q %q %v", n, by, subject, err)
	}

	if err := Follow(clone, false); err != nil || OwnRefs(clone) != nil {
		t.Errorf("the note outlived the watch: %v", err)
	}
}
