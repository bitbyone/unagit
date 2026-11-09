package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// waitTitle waits for the terminal's title to be want.
func waitTitle(t *testing.T, a *App, sc tcell.SimulationScreen, want string) {
	t.Helper()
	eventually(t, a, "the title "+want, func() bool { return sc.GetTitle() == want })
}

// TestTheTabSaysWhereUnagitIs: the terminal's tab is named after the
// screen in front, the worktree open in its view, and nothing but Unagit
// once that is switched off in General.
func TestTheTabSaysWhereUnagitIs(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitTitle(t, a, sc, "Unagit · Repositories")
	changeOnLoop(a, func() { a.switchTab(pageMRs) })
	waitTitle(t, a, sc, "Unagit · Merge requests")
	changeOnLoop(a, func() { a.switchTab(pageSettings) })
	waitTitle(t, a, sc, "Unagit · Settings")

	changeOnLoop(a, func() { a.wtView = &wtView{row: worktreeRow{Path: "acme/gateway", Branch: "feat/rate"}} })
	waitTitle(t, a, sc, "Unagit · acme/gateway · feat/rate")
	changeOnLoop(a, func() { a.wtView = nil })
	waitTitle(t, a, sc, "Unagit · Settings")

	changeOnLoop(a, func() { a.cfg.SetTabTitle(false) })
	waitTitle(t, a, sc, "Unagit")
	changeOnLoop(a, func() { a.cfg.SetTabTitle(true) })
	waitTitle(t, a, sc, "Unagit · Settings")
}

// TestStandaloneNamesTheTabAfterItsRepository: unagit -s says which
// repository, on which branch.
func TestStandaloneNamesTheTabAfterItsRepository(t *testing.T) {
	t.Parallel()
	dir, _ := soloFixture(t)
	a, sc, _ := newSoloApp(t, dir)
	waitTitle(t, a, sc, "Unagit · toolbox · main")
}
