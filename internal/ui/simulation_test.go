package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// observedScreen keeps the real simulation screen, but lets a test wait for
// a frame instead of repeatedly reading thousands of unchanged cells. The
// frame and pending receipt belong to the event loop, just like the widgets.
type observedScreen struct {
	tcell.SimulationScreen
	t        *testing.T
	ready    chan struct{}
	stopped  chan struct{}
	next     chan struct{}
	text     string
	dirty    bool
	first    sync.Once
	last     sync.Once
	receipts sync.Map
	pending  chan struct{}
}

// A sequence longer than tcell's event queue must be delivered in order,
// including non-ASCII input, and its last draw must precede the assertion.
func TestTypedKeysAreHandledAndDrawnBeforeReturning(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Text", "", 0, nil, nil)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.tv.QueueUpdateDraw(func() { a.showFormModal("Input", form, 8) })
	text := strings.Repeat("čaj ", 6)
	typeRunes(sc, text)
	if got := onLoop(a, func() string {
		return form.GetFormItem(0).(*tview.InputField).GetText()
	}); got != text {
		t.Fatalf("typed %q, field contains %q", text, got)
	}
	if got := a.screenText(sc); !strings.Contains(got, text) {
		t.Fatalf("the last input was not drawn:\n%s", got)
	}
}

func newObservedScreen(t *testing.T) *observedScreen {
	return &observedScreen{
		SimulationScreen: tcell.NewSimulationScreen("UTF-8"),
		t:                t, ready: make(chan struct{}), stopped: make(chan struct{}), next: make(chan struct{}),
	}
}

func (s *observedScreen) Show() {
	s.SimulationScreen.Show()
	s.dirty = true
	close(s.next)
	s.next = make(chan struct{})
	s.first.Do(func() { close(s.ready) })
	if s.pending != nil {
		close(s.pending)
		s.pending = nil
	}
}

func (s *observedScreen) Fini() {
	s.SimulationScreen.Fini()
	s.last.Do(func() { close(s.stopped) })
}

// observeKeys acknowledges a sequence only after its last key has been
// handled and drawn. QueueUpdate alone can overtake keys on tcell's queue.
func (s *observedScreen) observeKeys(a *App) {
	select {
	case <-s.ready:
	case <-time.After(patience):
		s.t.Fatal("application did not draw its first frame")
	}
	onLoop(a, func() bool {
		capture := a.tv.GetInputCapture()
		a.tv.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if receipt, ok := s.receipts.LoadAndDelete(ev); ok {
				s.pending = receipt.(chan struct{})
			}
			return capture(ev)
		})
		return true
	})
}

func (s *observedScreen) typeKeys(text string) {
	var keys []*tcell.EventKey
	for _, r := range text {
		keys = append(keys, tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	s.sendKeys(keys...)
}

func (s *observedScreen) sendKeys(keys ...*tcell.EventKey) {
	if len(keys) == 0 {
		return
	}
	done := make(chan struct{})
	for i, ev := range keys {
		if i == len(keys)-1 {
			s.receipts.Store(ev, done)
		}
		s.PostEventWait(ev)
	}
	select {
	case <-done:
	case <-s.stopped:
	case <-time.After(patience):
		s.t.Fatal("keys were not handled")
	}
}

type screenFrame struct {
	text string
	next <-chan struct{}
}

func (s *observedScreen) frame() screenFrame {
	if s.dirty {
		s.text = dumpScreen(s.SimulationScreen)
		s.dirty = false
	}
	return screenFrame{s.text, s.next}
}

func waitScreenText(t *testing.T, a *App, sc tcell.SimulationScreen, want string, present bool) {
	t.Helper()
	timer := time.NewTimer(patience)
	defer timer.Stop()
	for {
		frame := onLoop(a, func() screenFrame {
			if s, ok := sc.(*observedScreen); ok {
				return s.frame()
			}
			return screenFrame{text: dumpScreen(sc)}
		})
		if strings.Contains(frame.text, want) == present {
			return
		}
		// Standalone picker tests have no observed screen.
		var poll <-chan time.Time
		if frame.next == nil {
			poll = time.After(20 * time.Millisecond)
		}
		select {
		case <-frame.next:
		case <-poll:
		case <-timer.C:
			t.Fatalf("screen contains %q should be %v:\n%s", want, present, frame.text)
		}
	}
}
