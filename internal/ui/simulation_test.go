package ui

import (
	"reflect"
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

// tview uses tcell's older rune API for every cell, even a blank one. Its
// adapter allocates a string each time. Put reaches the same real buffer and
// locks, with an immutable string reused for the common single-byte cells.
func (s *observedScreen) SetContent(x, y int, main rune, combining []rune, style tcell.Style) {
	if main >= 0 && main < rune(len(asciiCells)) && len(combining) == 0 {
		s.SimulationScreen.Put(x, y, asciiCells[main], style)
		return
	}
	s.SimulationScreen.SetContent(x, y, main, combining, style)
}

var asciiCells = func() [128]string {
	var cells [128]string
	for r := range cells {
		cells[r] = string(rune(r))
	}
	return cells
}()

func TestObservedCellsMatchTheSimulationScreen(t *testing.T) {
	t.Parallel()
	actual := newObservedScreen(t)
	reference := tcell.NewSimulationScreen("UTF-8")
	for _, sc := range []tcell.SimulationScreen{actual, reference} {
		must(t, sc.Init())
		t.Cleanup(sc.Fini)
		sc.SetSize(12, 4)
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorRed).Background(tcell.ColorBlue).Bold(true)
	for frame, writes := range [][]struct {
		x, y int
		r    rune
		comb []rune
	}{
		{{0, 0, ' ', nil}, {1, 0, 'A', nil}, {2, 0, '0', nil}, {3, 0, 0, nil}, {4, 0, '\t', nil}},
		{{0, 1, 'č', nil}, {1, 1, 'e', []rune{'\u0301'}}, {3, 1, '界', nil}, {5, 1, '\U0001f469', []rune{'\u200d', '\U0001f4bb'}}, {11, 1, '界', nil}},
		{{3, 1, 'x', nil}, {5, 1, ' ', nil}, {-1, 0, 'a', nil}, {12, 0, 'b', nil}, {0, 4, 'c', nil}},
	} {
		for _, write := range writes {
			actual.SetContent(write.x, write.y, write.r, write.comb, style)
			reference.SetContent(write.x, write.y, write.r, write.comb, style)
		}
		actual.Show()
		reference.Show()
		got, w, h := actual.GetContents()
		want, rw, rh := reference.GetContents()
		if w != rw || h != rh || !reflect.DeepEqual(got, want) {
			t.Fatalf("frame %d differs from tcell's simulation", frame)
		}
		style = tcell.StyleDefault.Foreground(tcell.ColorNone).Background(tcell.ColorGreen)
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
