package ui

import (
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
)

// quietScreen is the terminal, kept from being drawn on while an editor has it.
//
// tcell empties its cell buffer when it is suspended but keeps the size it had,
// and a Show in that state loops for ever over cells that are not there -
// holding the screen's lock, so the Resume after the editor then waits for
// ever as well. Anything that redrew while the editor ran - the lists filling
// in their state in the background - left unagit hanging the moment the editor
// closed. Nothing is drawn between Suspend and Resume; the redraw after Resume
// brings the screen back.
type quietScreen struct {
	tcell.Screen
	suspended atomic.Bool
}

func (s *quietScreen) Suspend() error {
	s.suspended.Store(true)
	return s.Screen.Suspend()
}

func (s *quietScreen) Resume() error {
	err := s.Screen.Resume()
	s.suspended.Store(false)
	return err
}

func (s *quietScreen) Show() {
	if !s.suspended.Load() {
		s.Screen.Show()
	}
}

func (s *quietScreen) Sync() {
	if !s.suspended.Load() {
		s.Screen.Sync()
	}
}

// SetScreen gives the app the screen to draw on; without one, Run opens the
// terminal.
func (a *App) SetScreen(s tcell.Screen) {
	a.screenGiven = true
	a.tv.SetScreen(&quietScreen{Screen: s})
}
