//go:build !windows

package ui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
)

// TestAFrameSendsTheTerminalOnlyWhatChanged: tview clears the screen and
// draws it all again for every frame, and tcell counts a cell changed when
// it differs from the blank the clear left - so every frame went to the
// terminal whole. A frame that changes one cell sends about that cell.
func TestAFrameSendsTheTerminalOnlyWhatChanged(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	master, slave, err := pty.Open()
	if err != nil {
		t.Skip("no pty:", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 50, Cols: 200}); err != nil {
		t.Fatal(err)
	}
	var sent atomic.Int64
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := master.Read(buf)
			sent.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()
	tty, err := tcell.NewDevTtyFromDev(slave.Name())
	if err != nil {
		t.Fatal(err)
	}
	inner, err := tcell.NewTerminfoScreenFromTty(tty)
	if err != nil {
		t.Fatal(err)
	}
	s := &quietScreen{Screen: inner}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()

	line := strings.Repeat("lorem ipsum ", 20)
	frame := func(mark rune) {
		// as tview draws a frame: everything cleared, everything drawn
		s.Clear()
		w, h := s.Size()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r := rune(line[(x+y)%len(line)])
				style := tcell.StyleDefault.Foreground(tcell.NewRGBColor(int32(x), int32(y), 99))
				if x == 5 && y == 5 {
					r = mark
				}
				s.SetContent(x, y, r, nil, style)
			}
		}
		s.Show()
	}
	settle := func() int64 {
		for last := int64(-1); ; {
			time.Sleep(50 * time.Millisecond)
			if now := sent.Load(); now == last {
				return now
			} else {
				last = now
			}
		}
	}
	frame('a')
	whole := settle()
	frame('b')
	one := settle() - whole
	if whole < 50000 {
		t.Fatalf("the first frame sent %d bytes: the screen is not drawn", whole)
	}
	if one > 200 {
		t.Fatalf("a frame changing one cell sent %d bytes, the whole screen was %d", one, whole)
	}
}
