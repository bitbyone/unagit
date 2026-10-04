package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestRemoteColumnAndUpdates: r fetches every clone and REMOTE says how far
// behind each is; p updates the one under the cursor, Alt-P every other.
func TestRemoteColumnAndUpdates(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/billing")
	gw := newRealProject(t, a, "acme/gateway")
	bl := newRealProject(t, a, "acme/billing")
	gw.rescan()
	waitFor(t, a, sc, "✓")

	for _, p := range []*realProject{gw, bl} {
		other := p.elsewhere("main")
		commitIn(t, other, "theirs.txt", "theirs")
		gitIn(t, other, "push", "-q", "origin", "main")
	}
	typeRunes(sc, "R")
	waitFor(t, a, sc, "↓1")
	// One row can show it while the other clone is still fetching, and a pull
	// that races its own fetch says something else than fast-forwarded.
	waitFetched(t, a)

	// The refresh may reorder the list; p takes the row under the cursor,
	// whichever that is, and only that one.
	typeRunes(sc, "p")
	waitFor(t, a, sc, ": fast-forwarded")
	synced := func(p *realProject) bool {
		return gitIn(t, p.clone, "rev-parse", "HEAD") == gitIn(t, p.origin, "rev-parse", "main")
	}
	pulled, left := gw, bl
	if synced(bl) {
		pulled, left = bl, gw
	}
	if !synced(pulled) || synced(left) {
		t.Fatal("p should bring exactly one repository up to origin")
	}

	sc.InjectKey(tcell.KeyRune, 'p', tcell.ModAlt)
	waitFor(t, a, sc, "1 updated · 1 up to date")
	if gitIn(t, left.clone, "rev-parse", "HEAD") != gitIn(t, left.origin, "rev-parse", "main") {
		t.Error("Alt-P did not bring the other repository up to origin")
	}
	waitFor(t, a, sc, "✓")
}

// TestRepositoriesShowTheCloneItself: EDITS counts what is not committed in
// the main clone, REMOTE names a rebase left half way, and the detail lists
// the files.
func TestRepositoriesShowTheCloneItself(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/billing")
	gw := newRealProject(t, a, "acme/gateway")
	must(t, os.WriteFile(filepath.Join(gw.clone, "a.txt"), []byte("edited\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(gw.clone, "new.txt"), []byte("new\n"), 0o644))
	gw.rescan()
	waitFor(t, a, sc, "EDITS")
	waitFor(t, a, sc, "✓")
	if row := rowWith(a, sc, "acme/gateway"); !containsField(row, "2") {
		t.Errorf("EDITS does not count the two files: %q", row)
	}

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "UNCOMMITTED · 2")
	waitFor(t, a, sc, "new.txt")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)

	// A rebase git stopped in the middle of, as a conflict leaves it.
	must(t, os.MkdirAll(filepath.Join(gw.clone, ".git", "rebase-merge"), 0o755))
	gw.rescan()
	waitFor(t, a, sc, "rebasing")
}

// TestOpenTakesTheCloneAsItIs: Ctrl-O opens a clone without fetching or
// pulling, however far origin has moved; p is what updates it.
func TestOpenTakesTheCloneAsItIs(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/billing")
	gw := newRealProject(t, a, "acme/gateway")
	other := gw.elsewhere("main")
	commitIn(t, other, "theirs.txt", "theirs")
	gitIn(t, other, "push", "-q", "origin", "main")
	before := gitIn(t, gw.clone, "rev-parse", "HEAD")
	gw.rescan()
	waitFor(t, a, sc, "✓")

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlO, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "opened "+tildePath(gw.clone)[:20])
	if gitIn(t, gw.clone, "rev-parse", "HEAD") != before {
		t.Error("Ctrl-O moved the clone")
	}
	if gitIn(t, gw.clone, "rev-parse", "origin/main") != before {
		t.Error("Ctrl-O fetched")
	}
}

// TestEditsShowOnFocusAndOnSwitchingTabs: what changed in another window shows
// when the terminal comes back to the front, and when a list is switched to.
func TestEditsShowOnFocusAndOnSwitchingTabs(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/billing")
	gw := newRealProject(t, a, "acme/gateway")
	gw.rescan()
	waitFor(t, a, sc, "✓")
	stale := func() { onLoop(a, func() bool { a.localRefreshed = time.Time{}; return true }) }

	must(t, os.WriteFile(filepath.Join(gw.clone, "one.txt"), []byte("1\n"), 0o644))
	stale()
	must(t, sc.PostEvent(tcell.NewEventFocus(true)))
	waitForRow(t, a, sc, "acme/gateway", "1")

	must(t, os.WriteFile(filepath.Join(gw.clone, "two.txt"), []byte("2\n"), 0o644))
	stale()
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Merge requests")
	typeRunes(sc, "1")
	waitForRow(t, a, sc, "acme/gateway", "2")
}

// waitForRow waits until the screen line holding text has want as a word.
func waitForRow(t *testing.T, a *App, sc tcell.SimulationScreen, text, want string) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if containsField(rowWith(a, sc, text), want) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("the row of %s never showed %q:\n%s", text, want, a.screenText(sc))
}

// waitFetched waits until no fetch is running in the background.
func waitFetched(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for onLoop(a, func() int { return a.fetching }) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the fetches never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
