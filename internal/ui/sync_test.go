package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestRemoteColumnAndUpdates: r fetches every clone and REMOTE says how far
// behind each is; p updates the one under the cursor, Alt-P every other.
func TestRemoteColumnAndUpdates(t *testing.T) {
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
	typeRunes(sc, "r")
	waitFor(t, a, sc, "↓1")

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
