package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestSmartCheckoutTakesTheChangesAlong: Enter on a branch whose own
// version of an edited file is in the way asks, naming the file; Smart
// Checkout shelves the edit, switches, and puts it back on the branch.
func TestSmartCheckoutTakesTheChangesAlong(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	readme := filepath.Join(p.clone, "notes.txt")
	must(t, os.WriteFile(readme, []byte("a\nb\nc\nd\ne\n"), 0o644))
	gitIn(t, p.clone, "add", ".")
	gitIn(t, p.clone, "commit", "-qm", "notes")
	gitIn(t, p.clone, "checkout", "-q", "-b", "wide")
	must(t, os.WriteFile(readme, []byte("a\nb\nc\nd\ne\nf\n"), 0o644))
	gitIn(t, p.clone, "commit", "-qam", "more notes")
	gitIn(t, p.clone, "checkout", "-q", "main")
	must(t, os.WriteFile(readme, []byte("A\nb\nc\nd\ne\n"), 0o644))
	p.rescan()

	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	typeRunes(sc, "/wide")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "collides with wide")
	waitFor(t, a, sc, "notes.txt")
	assertLegible(t, a, sc, "the smart checkout question")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "your changes with it")
	if got := gitIn(t, p.clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "wide" {
		t.Errorf("the clone is on %s", got)
	}
	if got, _ := os.ReadFile(readme); string(got) != "A\nb\nc\nd\ne\nf\n" {
		t.Errorf("notes.txt = %q", got)
	}
	if got := gitIn(t, p.clone, "stash", "list"); got != "" {
		t.Errorf("left on the shelf: %q", got)
	}
}

// TestPushUpToHere: P on an older commit of those not pushed asks, saying
// what goes and what stays, and sends origin that commit alone.
func TestPushUpToHere(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Ready to go")
	ready := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "c.txt", "Still half done")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "j")
	waitFor(t, a, sc, "push to here")
	typeRunes(sc, "P")
	waitFor(t, a, sc, "Push 1 commit(s) of main to origin")
	waitFor(t, a, sc, "to "+ready[:8]+"?")
	waitFor(t, a, sc, "These stay here")
	if text := a.screenText(sc); !strings.Contains(text, "Still half done") {
		t.Errorf("what stays is not named:\n%s", text)
	}
	typeRunes(sc, "p")
	waitFor(t, a, sc, "pushed main up to "+ready[:8])
	if got := gitIn(t, p.origin, "rev-parse", "main"); got != ready {
		t.Errorf("origin's main is %s, want %s", got, ready)
	}
}
