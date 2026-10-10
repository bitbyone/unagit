package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCancelGoesBackThroughTheStack: what is opened from a list goes back
// to it when cancelled - a form by Esc or Cancel, a picker by Esc, a
// refusal by closing its message - and that list to its own, until the
// screen. A list opened again after something done from it still goes
// back where it did.
func TestCancelGoesBackThroughTheStack(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "c.txt", "Count requests")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")

	// The form: Esc stops typing, the next Esc cancels - back to the log.
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit Commit Message · ")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Edit Commit Message · ")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")

	// Its Cancel button goes the same way.
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit Commit Message · ")
	pressButton(t, a, sc, frontForm(a), "Cancel")
	waitGone(t, a, sc, "Edit Commit Message · ")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")

	// A picker opened from the log goes back to it, and the log, opened
	// again so, still closes to the screen.
	typeRunes(sc, "H")
	waitFor(t, a, sc, "nothing rewritten")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "u")
	waitFor(t, a, sc, "its changes wait to be committed again")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "H")
	waitFor(t, a, sc, "Rewrite History · acme/gateway")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Rewrite History")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")

	// A refusal over a bare screen: its message closed, the list is back.
	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	typeRunes(sc, "D")
	waitFor(t, a, sc, "main is the default branch - it is not deleted")
	closeMessage(t, a, sc)
	waitFor(t, a, sc, "Branches - acme/gateway")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Branches - acme/gateway")

	// A key on the screen forgets the way back: a form opened from there
	// cancels to the screen.
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Commit · ")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit · ")
	if onLoop(a, func() bool { return a.modalOpen() }) {
		t.Errorf("a dialog came back after a form opened from the screen:\n%s", a.screenText(sc))
	}
}
