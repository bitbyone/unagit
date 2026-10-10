package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitx"
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
	// origin/main is on a commit only origin has: its pill keeps the
	// remote's colour - not the text's, as a name that is no role once
	// made it.
	row := lineOf(text, "Bill them")
	line := strings.Split(text, "\n")[row]
	x := len([]rune(line[:strings.Index(line, "origin/main")]))
	_, style := cellAt(a, sc, x, row)
	_, bg, _ := style.Decompose()
	want := onLoop(a, func() tcell.Color { return quieter(role("log.ref_remote.fill")) })
	if bg.Hex() != want.Hex() {
		t.Errorf("origin/main on a commit only origin has is on %v, want %v", bg, want)
	}
}

// TestTheLogsRefsArePills: what points at a commit is drawn as pills - HEAD
// on main and origin/main at the same commit as one pill, half green, half
// purple, which keep their fill under the cursor; a local branch alone and
// a tag in pills of their own - and the id and the author in their colours.
func TestTheLogsRefsArePills(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 40)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Add the limiter")
	gitIn(t, p.clone, "tag", "v1.0")
	gitIn(t, p.clone, "branch", "feat/x")
	commitIn(t, p.clone, "c.txt", "Count requests")
	gitIn(t, p.clone, "push", "-q")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	waitFor(t, a, sc, "HEAD→main  origin")
	text := a.screenText(sc)
	for _, want := range []string{"feat/x", "v1.0"} {
		if row := strings.Split(text, "\n")[lineOf(text, "Add the limiter")]; !strings.Contains(row, want) {
			t.Errorf("%q is not on its commit's row: %q", want, row)
		}
	}
	assertLegible(t, a, sc, "a log with pills")

	// The cursor is on the newest commit: its pill keeps both fills.
	cellOf := func(needle string) (int, int) {
		text := a.screenText(sc)
		row := lineOf(text, needle)
		line := strings.Split(text, "\n")[row]
		return len([]rune(line[:strings.Index(line, needle)])), row
	}
	bgAt := func(x, y int) tcell.Color { _, style := cellAt(a, sc, x, y); _, bg, _ := style.Decompose(); return bg }
	fgAt := func(x, y int) tcell.Color { _, style := cellAt(a, sc, x, y); fg, _, _ := style.Decompose(); return fg }
	x, y := cellOf("HEAD→main")
	fills := onLoop(a, func() [2]tcell.Color {
		return [2]tcell.Color{quieter(role("log.ref_local.fill")), quieter(role("log.ref_remote.fill"))}
	})
	local, remote := fills[0], fills[1]
	if bg := bgAt(x, y); bg.Hex() != local.Hex() {
		t.Errorf("HEAD→main on the cursor is on %v, want the local fill %v", bg, local)
	}
	ox, oy := cellOf("  origin")
	if bg := bgAt(ox+2, oy); bg.Hex() != remote.Hex() {
		t.Errorf("origin on the cursor is on %v, want the remote fill %v", bg, remote)
	}
	sx, sy := cellOf("Add the limiter")
	sha := onLoop(a, func() tcell.Color { return role("log.sha") })
	if fg := fgAt(sx-10, sy); fg.Hex() != sha.Hex() {
		t.Errorf("the id is in %v, want %v", fg, sha)
	}
}

// TestPillsThatDoNotFitAreCounted: refs past the room left are a pill
// counting them, +2, never a pill cut in half; the pane under the list
// names every one.
func TestPillsThatDoNotFitAreCounted(t *testing.T) {
	t.Parallel()
	c := logCommit{LogEntry: gitx.LogEntry{
		Refs:     []string{"HEAD -> main", "feature/a-rather-long-name", "release/2026-10", "tag: v1.0"},
		RefKinds: []gitx.RefKind{gitx.RefHeadOn, gitx.RefLocal, gitx.RefLocal, gitx.RefTag},
	}}
	markup, width := refPills(c, config.TagEndsSquare, behindList, 30)
	plain := plainText(markup)
	if width > 30 || !strings.Contains(plain, "HEAD→main") || !strings.Contains(plain, "+3") && !strings.Contains(plain, "+2") {
		t.Errorf("in 30 cells: %q (%d)", plain, width)
	}
	if _, all := refPills(c, config.TagEndsSquare, behindList, 200); all <= 30 {
		t.Errorf("with room every pill is drawn: %d cells", all)
	}
	if about := commitAbout(c, logPlace{}); !strings.Contains(about, glyphRef+" release/2026-10") || !strings.Contains(about, glyphTag+" v1.0") {
		t.Errorf("the pane does not name every ref: %q", about)
	}
}
