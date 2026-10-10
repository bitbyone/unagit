package ui

import (
	"regexp"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// ruleWords finds the lines between a log's sections, not a frame's border.
var ruleWords = regexp.MustCompile(`── [a-z]`)

// inOrder fails unless every text is on screen, each on a line below the
// one before it.
func inOrder(t *testing.T, text string, want ...string) {
	t.Helper()
	last := -1
	for _, w := range want {
		at := lineOf(text, w)
		if at < 0 {
			t.Fatalf("%q is not on screen:\n%s", w, text)
		}
		if at <= last {
			t.Fatalf("%q is not below %q:\n%s", w, want[0], text)
		}
		last = at
	}
}

// TestTheLogShowsWhereOriginStands: a branch two commits ahead has one line
// under them, where origin's copy is, which the cursor steps over and a
// filter hides; once pushed commits are rewritten with plain git, the log
// has what is only here, what only on origin, and what both have.
func TestTheLogShowsWhereOriginStands(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Add the limiter", "About the limiter.")
	gitIn(t, p.clone, "push", "-q")
	commitIn(t, p.clone, "c.txt", "Count requests", "About counting.")
	commitIn(t, p.clone, "d.txt", "Bill them", "About billing.")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	waitFor(t, a, sc, "About billing.")
	text := a.screenText(sc)
	inOrder(t, text, "Bill them", "Count requests", "── origin/main", "Add the limiter")
	if n := len(ruleWords.FindAllString(text, -1)); n != 1 {
		t.Errorf("%d lines part the log, want one:\n%s", n, text)
	}
	assertLegible(t, a, sc, "a log with a line where origin is")

	// The cursor steps over the line both ways, and g and G land on commits.
	typeRunes(sc, "j")
	waitFor(t, a, sc, "About counting.")
	typeRunes(sc, "j")
	waitFor(t, a, sc, "About the limiter.")
	typeRunes(sc, "k")
	waitFor(t, a, sc, "About counting.")
	sc.InjectKey(tcell.KeyDown, 0, tcell.ModNone)
	waitFor(t, a, sc, "About the limiter.")
	typeRunes(sc, "g")
	waitFor(t, a, sc, "About billing.")

	typeRunes(sc, "/limiter")
	waitGone(t, a, sc, "── origin/main")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")

	// Pushed, then the two squashed into one with plain git.
	gitIn(t, p.clone, "push", "-q")
	gitIn(t, p.clone, "reset", "-q", "--soft", "HEAD~2")
	gitIn(t, p.clone, "commit", "-q", "-m", "Count and bill requests")
	p.rescan()
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Count and bill requests")
	text = a.screenText(sc)
	inOrder(t, text, "── only here", "Count and bill requests", "── only on origin",
		"Bill them", "Count requests", "── shared", "Add the limiter")
	assertLegible(t, a, sc, "a log parted from origin")
}
