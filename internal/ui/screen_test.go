package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// countingScreen counts what reaches the terminal.
type countingScreen struct {
	tcell.SimulationScreen
	shows, syncs int
}

func (c *countingScreen) Show() { c.shows++; c.SimulationScreen.Show() }
func (c *countingScreen) Sync() { c.syncs++; c.SimulationScreen.Sync() }

// TestNothingIsDrawnWhileAnEditorHasTheTerminal: a draw between Suspend and
// Resume hung tcell for good, and unagit with it, once the editor closed.
func TestNothingIsDrawnWhileAnEditorHasTheTerminal(t *testing.T) {
	t.Parallel()
	inner := &countingScreen{SimulationScreen: tcell.NewSimulationScreen("UTF-8")}
	must(t, inner.Init())
	s := &quietScreen{Screen: inner}

	s.Show()
	must(t, s.Suspend())
	s.Show()
	s.Sync()
	if inner.shows != 1 || inner.syncs != 0 {
		t.Errorf("drawn while suspended: %d shows, %d syncs", inner.shows, inner.syncs)
	}
	must(t, s.Resume())
	s.Show()
	if inner.shows != 2 {
		t.Errorf("not drawn after resuming: %d shows", inner.shows)
	}
}
