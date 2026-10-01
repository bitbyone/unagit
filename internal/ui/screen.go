package ui

import (
	"sync/atomic"
	"time"

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
//
// It also tells unagit when the terminal comes back to the front, so that what
// was changed in another window shows without asking for it.
type quietScreen struct {
	tcell.Screen
	suspended atomic.Bool
	// onFocus runs, off the event loop, whenever the terminal regains focus.
	onFocus func()
}

// Init asks the terminal to report focus; most do (iTerm2, Ghostty, kitty,
// WezTerm), and the rest simply never say.
func (s *quietScreen) Init() error {
	if err := s.Screen.Init(); err != nil {
		return err
	}
	s.Screen.EnableFocus()
	return nil
}

// PollEvent passes every event on but the focus ones, which tview would drop
// anyway; regaining focus calls onFocus.
func (s *quietScreen) PollEvent() tcell.Event {
	for {
		ev := s.Screen.PollEvent()
		focus, ok := ev.(*tcell.EventFocus)
		if !ok {
			return ev
		}
		if focus.Focused && s.onFocus != nil {
			s.onFocus()
		}
	}
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
	a.tv.SetScreen(&quietScreen{Screen: s, onFocus: func() {
		// Back from another window, where the files may have changed.
		go a.tv.QueueUpdateDraw(func() { a.refreshLocal() })
	}})
}

// localRefreshGap keeps switching tabs back and forth from starting a git per
// clone every time.
const localRefreshGap = 2 * time.Second

// refreshLocal reads the disk and the clones' state again - what is checked
// out, what is not committed, where each branch stands by the refs it has -
// without asking any server. Switching to a list and coming back to the
// terminal both do it, a moment apart at most once.
func (a *App) refreshLocal() {
	if time.Since(a.localRefreshed) < localRefreshGap {
		return
	}
	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
}
