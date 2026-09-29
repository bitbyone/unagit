package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/session"
)

var pickFixture = []session.Record{
	{Project: "my2n/ng/struct-context", IID: 123, Mode: session.ModeReview,
		Title: "MY2N-29863: Add LOC__DELETE permission to organization maintainer role",
		Dir:   "/Users/toby/unagit/my2n/ng/.unagit/struct-context/review-123-feature-MY2N-29863-loc-delete-permission"},
	{Project: "my2n/ng/struct-context", IID: 120, Mode: session.ModeBranch,
		Title: "MY2N-29678: Add structure-bindings persona migration plan",
		Dir:   "/Users/toby/unagit/my2n/ng/.unagit/struct-context/120-feature-MY2N-29678-structure-bindings"},
	{Project: "acme/gateway", Mode: session.ModeRepository, Dir: "/Users/toby/unagit/acme/gateway"},
}

// runPicker starts the picker on a simulation screen and hands it back with
// a channel for what it returns.
func runPicker(t *testing.T, w, h int) (tcell.SimulationScreen, chan session.Record) {
	t.Helper()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatal(err)
	}
	sc.SetSize(w, h)
	started := make(chan *tview.Application, 1)
	pickerStarted = func(app *tview.Application) { started <- app }
	t.Cleanup(func() { pickerStarted = nil })
	done := make(chan session.Record, 1)
	go func() {
		r, ok, err := pickSession(sc, pickFixture)
		if err != nil || !ok {
			r = session.Record{}
		}
		done <- r
	}()
	pickerApp = <-started
	// The application initialises the screen again as it starts, which
	// forgets the size; set it once it runs, and say so.
	waitScreen(t, sc, "Open in an editor")
	resize(sc, w, h)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, sw, _ := contentsOf(sc)
		// Drawn again at the new size: the frame is whole.
		if text := screenOf(sc); sw == w && strings.Contains(text, "╮") && strings.Count(text, "\n") == h {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	return sc, done
}

// pickerApp is the picker under test; its screen is read on its loop.
var pickerApp *tview.Application

func contentsOf(sc tcell.SimulationScreen) ([]tcell.SimCell, int, int) {
	type contents struct {
		cells []tcell.SimCell
		w, h  int
	}
	out := make(chan contents, 1)
	pickerApp.QueueUpdate(func() {
		cells, w, h := sc.GetContents()
		out <- contents{append([]tcell.SimCell(nil), cells...), w, h}
	})
	select {
	case c := <-out:
		return c.cells, c.w, c.h
	case <-time.After(2 * time.Second):
		return nil, 0, 0
	}
}

func screenOf(sc tcell.SimulationScreen) string {
	cells, w, h := contentsOf(sc)
	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if c := cells[y*w+x]; len(c.Runes) > 0 {
				b.WriteRune(c.Runes[0])
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func waitScreen(t *testing.T, sc tcell.SimulationScreen, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if text := screenOf(sc); strings.Contains(text, want) {
			return text
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never contained %q:\n%s", want, screenOf(sc))
	return ""
}

// TestPickSessionLooksLikeUnagit: the frame of the rest of unagit, not a
// double line; each entry on two lines inside it, the title and the path cut
// to fit rather than run past the edge; the terminal's own background.
func TestPickSessionLooksLikeUnagit(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 30}, {100, 20}, {70, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			sc, done := runPicker(t, size.w, size.h)
			text := waitScreen(t, sc, "Open in an editor")
			if strings.ContainsAny(text, "═║╔╗╚╝") {
				t.Errorf("a double frame:\n%s", text)
			}
			for _, want := range []string{"╭", "review", "struct-context !123", "branch", "!120",
				"repository", "acme/gateway", "Enter go there"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			// What does not fit is cut with a mark; at full width nothing is.
			if cut := strings.Contains(text, "…"); cut != (size.w < 120) {
				t.Errorf("cut = %v at %d columns:\n%s", cut, size.w, text)
			}
			// The frame's right edge is intact on every row it spans.
			lines := strings.Split(text, "\n")
			top := -1
			for i, l := range lines {
				if strings.Contains(l, "╭") {
					top = i
					break
				}
			}
			edge := strings.IndexRune(string([]rune(lines[top])), '╮')
			edge = len([]rune(lines[top][:edge]))
			for i := top + 1; i < len(lines) && !strings.Contains(lines[i], "╰"); i++ {
				if r := []rune(lines[i]); edge >= len(r) || r[edge] != '│' {
					t.Errorf("row %d: the frame's right edge is drawn over: %q", i, lines[i])
				}
			}
			if strings.Contains(text, "repositoryacme") {
				t.Errorf("the mode runs into the repository:\n%s", text)
			}
			// Nothing is drawn on a background of the picker's own.
			cells, w, _ := contentsOf(sc)
			_, bg, _ := cells[1*w+1].Style.Decompose()
			if bg != tcell.ColorDefault {
				t.Errorf("the picker paints its own background: %v", bg)
			}
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			<-done
		})
	}
}

func TestPickSessionReturnsTheChosenOne(t *testing.T) {
	sc, done := runPicker(t, 120, 20)
	waitScreen(t, sc, "Open in an editor")
	sc.InjectKey(tcell.KeyRune, 'j', tcell.ModNone)
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	select {
	case r := <-done:
		if r.IID != 120 {
			t.Errorf("chose %+v, want !120", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the picker did not return")
	}
}
