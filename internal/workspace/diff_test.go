package workspace

import (
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
