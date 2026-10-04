package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// TestCommitLogs: Ctrl-L lists the commits of a clone from git, of a
// repository not yet cloned from the server, and of a merge request; the
// pane under the list says who made the commit under the cursor and the rest
// of its message.
func TestCommitLogs(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit log · acme/gateway (main on the server)")
	waitFor(t, a, sc, "Add rate limiting")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit log")

	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Count requests per client", "Clients are told apart by their token.")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit log · acme/gateway")
	waitFor(t, a, sc, "Count requests per client")
	waitFor(t, a, sc, "apart by their token.")
	waitFor(t, a, sc, "initial")
	assertLegible(t, a, sc, "a commit log")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit log")

	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit log · acme/gateway !7")
	waitFor(t, a, sc, "Token bucket")
	waitFor(t, a, sc, "jane ·")
}
