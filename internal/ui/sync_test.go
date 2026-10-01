package ui

import (
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
