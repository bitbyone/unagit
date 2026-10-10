package ui

import (
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestReviewStartListsCommitsWithoutCloning: the log draws its list from the forge;
// the repository is cloned only once a commit is chosen, so a
// large one does not hold the list up.
func TestReviewStartListsCommitsWithoutCloning(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	openMRDetail(t, a, sc)

	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway !7")
	waitFor(t, a, sc, "beef1230  "+glyphCommit+" Token bucket")

	root := onLoop(a, func() string { return a.cfg.RootDir })
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("listing the commits put %s under the clone root", entries[0].Name())
	}
}
