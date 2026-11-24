package ui

import (
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/notify"
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
	// focused is whether the terminal is in front, as it last said; true
	// until it says otherwise - unagit was just started there - and for
	// ever in a terminal that never says.
	focused atomic.Bool
	// focusKnown is set by the first focus event: a terminal that never
	// sends one (or a multiplexer that keeps them) leaves focused saying
	// nothing.
	focusKnown atomic.Bool
	// onFocus runs, off the event loop, whenever the terminal regains focus.
	onFocus func()
	// dimmed is set once a modal has dimmed what is under it in the frame
	// being drawn, and cleared when the frame is shown (modalBox). under is
	// where the last modal drawn in the frame stands, which the next one
	// over it dims, and nothing else.
	dimmed bool
	under  struct{ x, y, w, h int }
	// held is a frame put off (drawNextFrame): tview cleared the cells
	// before asking, and shows them whatever the answer, so the terminal
	// went blank for a frame at every key held down. The cleared cells
	// are not shown; the next frame draws them all again.
	held bool
	// shown is what the terminal has, cell by cell, as last shown; nil
	// when that is not known (at the start, after a resize, an editor or a
	// Sync), and the next frame is sent whole.
	shown []shownCell
}

// shownCell is one cell as the terminal was sent it.
type shownCell struct {
	str   string
	style tcell.Style
	width int
}

// Init asks the terminal to report focus; most do (iTerm2, Ghostty, kitty,
// WezTerm), and the rest simply never say. It names the window unagit: a
// terminal's notification carries the window's title, which was otherwise
// the command the shell ran - an alias, with its flags. tcell keeps the
// title the shell had and puts it back on the way out, and sets unagit's
// again after an editor.
func (s *quietScreen) Init() error {
	if err := s.Screen.Init(); err != nil {
		return err
	}
	s.Screen.EnableFocus()
	s.Screen.SetTitle(notify.WindowTitle)
	return nil
}

// EnableMouse asks the terminal for the buttons and for a drag, never for
// the pointer's every move: unagit follows nothing but a drag, and a hand
// resting on the mouse sent a stream of events the event loop had to wade
// through before the next click.
func (s *quietScreen) EnableMouse(...tcell.MouseFlags) {
	s.Screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
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
		s.focused.Store(focus.Focused)
		s.focusKnown.Store(true)
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
	// The editor drew over everything.
	s.shown = nil
	return err
}

func (s *quietScreen) Show() {
	s.dimmed = false
	s.under.w = 0
	if s.held {
		s.held = false
		return
	}
	if !s.suspended.Load() {
		locked := s.lockUnchanged()
		s.Screen.Show()
		if locked {
			w, h := s.Screen.Size()
			s.Screen.LockRegion(0, 0, w, h, false)
		}
	}
}

// lockUnchanged locks the cells that are what the terminal already has, so
// that Show sends only the rest; it says whether it locked any.
//
// tview clears the screen before every frame and draws it all again, and
// tcell marks a cell changed whenever what is put in it differs from what
// it held a moment before - a blank, after the clear - not from what the
// terminal shows. So every frame went out whole: a step of the cursor in a
// file sent the terminal some 50 kB, and keys held down queued up behind
// the writes. What the terminal has is kept here instead, and a frame now
// sends the cells that changed.
func (s *quietScreen) lockUnchanged() bool {
	w, h := s.Screen.Size()
	if len(s.shown) != w*h {
		s.shown = make([]shownCell, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				str, style, width := s.Screen.Get(x, y)
				s.shown[y*w+x] = shownCell{str, style, width}
			}
		}
		return false
	}
	locked := false
	for y := 0; y < h; y++ {
		from := -1
		for x := 0; x <= w; x++ {
			same := false
			if x < w {
				str, style, width := s.Screen.Get(x, y)
				now := shownCell{str, style, width}
				if same = s.shown[y*w+x] == now; !same {
					s.shown[y*w+x] = now
				}
			}
			switch {
			case same && from < 0:
				from = x
			case !same && from >= 0:
				s.Screen.LockRegion(from, y, x-from, 1, true)
				locked, from = true, -1
			}
		}
	}
	return locked
}

func (s *quietScreen) Sync() {
	if !s.suspended.Load() {
		s.Screen.Sync()
		// Sync sent every cell; the next frame starts from what it sent.
		s.shown = nil
	}
}

// suspend hands the terminal to f, as tv.Suspend does, and counts it while
// f runs. tview's Run clears its screen without its lock as it ends, which
// races with a Suspend still under way; whatever stops the app waits for
// the count first (the tests do, before Stop).
func (a *App) suspend(f func()) {
	a.suspends.Add(1)
	defer a.suspends.Done()
	a.tv.Suspend(f)
}

// SetScreen gives the app the screen to draw on; without one, Run opens the
// terminal.
func (a *App) SetScreen(s tcell.Screen) {
	a.screenGiven = true
	a.quiet = &quietScreen{Screen: s, onFocus: func() {
		// Back from another window, where the files may have changed - and
		// where the terminal may have lost what it showed: a window that
		// slept, a display taken away, a multiplexer that drew over it.
		// tcell sends only the cells it thinks changed, so what the
		// terminal dropped stayed dropped, and the rest of the frame drew
		// over the remains. The whole screen is sent again.
		go func() {
			a.tv.Sync()
			a.tv.QueueUpdateDraw(func() {
				a.refreshLocal()
				a.reloadChanges()
				// A sandbox may have been made, a task started or the
				// context changed by unibox in another terminal; the
				// context rides on the tab bar from every screen.
				if a.solo == nil && a.uniboxOn() && a.ubx.read {
					a.visitSandboxes()
				}
			})
		}()
	}}
	a.quiet.focused.Store(true)
	a.tv.SetScreen(a.quiet)
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
