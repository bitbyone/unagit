package ui

import (
	"os"
	"testing"
)

// TestReviewStartListsCommitsWithoutCloning: v draws its list from the forge;
// the repository is cloned only once a commit is chosen, so a
// large one does not hold the list up.
func TestReviewStartListsCommitsWithoutCloning(t *testing.T) {
	a, sc := newTestApp(t)
	openMRDetail(t, a, sc)

	typeRunes(sc, "v")
	waitFor(t, a, sc, "Review !7 from a commit to the head")
	waitFor(t, a, sc, "beef1230  Token bucket")

	root := onLoop(a, func() string { return a.cfg.RootDir })
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("listing the commits put %s under the clone root", entries[0].Name())
	}
}
