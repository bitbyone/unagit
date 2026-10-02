package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChangePatchReadsAsPartOfOne: the patch of a repository names its files
// under the prefix, new files included, measured from the base it is given.
func TestChangePatchReadsAsPartOfOne(t *testing.T) {
	f := newUpdateFixture(t)
	git(t, f.clone, "checkout", "-q", "-b", "feat/x")
	write(t, f.clone, "a.txt", "committed on the branch\n")
	git(t, f.clone, "commit", "-qam", "mine")
	write(t, f.clone, "b.txt", "not committed\n")
	write(t, f.clone, "new.txt", "brand new\n")

	from := f.m.ChangeBase(f.clone, "main")
	if from == "HEAD" {
		t.Fatal("the branch's base was not found")
	}
	patch, err := f.m.ChangePatch(f.clone, "api/", from)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+++ b/api/a.txt", "+++ b/api/b.txt", "+++ b/api/new.txt", "+committed on the branch", "+brand new"} {
		if !strings.Contains(patch, want) {
			t.Errorf("the patch lacks %q:\n%s", want, patch)
		}
	}
	if f.m.ChangeBase(f.clone, "") != "HEAD" {
		t.Error("without a base the change is what is not committed")
	}
}

// TestCommitAllTakesEverything: modified, new and deleted files go into one
// commit; a clean tree commits nothing.
func TestCommitAllTakesEverything(t *testing.T) {
	f := newUpdateFixture(t)
	write(t, f.clone, "a.txt", "changed\n")
	write(t, f.clone, "new.txt", "new\n")
	if err := os.Remove(filepath.Join(f.clone, "b.txt")); err != nil {
		t.Fatal(err)
	}
	sha, err := f.m.CommitAll(f.clone, "Do it all\n\nIn one go.")
	if err != nil || sha == "" {
		t.Fatalf("got %q, %v", sha, err)
	}
	if got := git(t, f.clone, "show", "--name-status", "--format=%s%n%b", "HEAD"); !strings.Contains(got, "Do it all") ||
		!strings.Contains(got, "In one go.") || !strings.Contains(got, "A\tnew.txt") || !strings.Contains(got, "M\ta.txt") || !strings.Contains(got, "D\tb.txt") {
		t.Errorf("the commit:\n%s", got)
	}
	if again, err := f.m.CommitAll(f.clone, "nothing"); err != nil || again != "" {
		t.Errorf("a clean tree: %q, %v", again, err)
	}
}
