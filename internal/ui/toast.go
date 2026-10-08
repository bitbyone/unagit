package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A toast is news from the background - a watched pipeline that began,
// passed or failed - shown in the top right corner over whatever is in
// front, for a few seconds. It takes no key and no focus: it arrives while
// the user types into something else, and must not take the keyboard from
// them, which is why it is not a message box. Several stack, the newest on
// top. Its time runs only while the terminal is in front, so what came
// while the user was away is still there when they come back.

// toast is one on screen.
type toast struct {
	sev         severity
	title, body string
	// about is what the news is about - a merge request's title - on a
	// line of its own under the heading, muted.
	about string
	left  time.Duration
}

const (
	// toastTick is how often the toasts' time is counted down.
	toastTick = 250 * time.Millisecond
	// toastsKept is how many stack at most; an older one gives way.
	toastsKept = 4
	// toastWidth is the widest a toast is, its border included.
	toastWidth = 48
)

// toastMark is the first line's mark for a severity.
func toastMark(sev severity) string {
	switch sev {
	case sevSuccess:
		return glyphCheck
	case sevWarning:
		return "!"
	case sevError:
		return glyphCross
	}
	return "i"
}

// toastColour is a severity's colour on a toast: its border and its title.
func toastColour(sev severity) tcell.Color {
	switch sev {
	case sevSuccess:
		return role("toast.success")
	case sevWarning:
		return role("toast.warning")
	case sevError:
		return role("toast.danger")
	}
	return role("toast.info")
}

// showToast puts a toast up. It runs on the event loop.
func (a *App) showToast(sev severity, title, body string) { a.showToastAbout(sev, title, "", body) }

// showToastAbout is showToast with a line saying what it is about.
func (a *App) showToastAbout(sev severity, title, about, body string) {
	a.toasts = append(a.toasts, &toast{sev: sev, title: title, about: about, body: body, left: a.cfg.ToastLife()})
	if over := len(a.toasts) - toastsKept; over > 0 {
		a.toasts = append([]*toast(nil), a.toasts[over:]...)
	}
	if !a.toasting {
		a.toasting = true
		go a.countToasts()
	}
}

// countToasts takes the toasts down as their time runs out, and stops when
// none is left.
func (a *App) countToasts() {
	ticks, stopTicker := a.animationTicker(toastTick)
	defer stopTicker()
	last := time.Now()
	for range ticks {
		now := time.Now()
		gone := now.Sub(last)
		last = now
		front := a.terminalInFront()
		stop := make(chan bool, 1)
		a.tv.QueueUpdateDraw(func() {
			if front {
				kept := a.toasts[:0]
				for _, t := range a.toasts {
					if t.left -= gone; t.left > 0 {
						kept = append(kept, t)
					}
				}
				a.toasts = kept
			}
			if len(a.toasts) == 0 {
				a.toasting = false
			}
			stop <- !a.toasting
		})
		select {
		case done := <-stop:
			if done {
				return
			}
		case <-time.After(5 * time.Second):
			// The event loop is gone: unagit is closing.
			return
		}
	}
}

// drawToasts paints the toasts after everything else, so they stand over
// dialogs too, at the top right, inside the frame of the screen beneath.
func (a *App) drawToasts(screen tcell.Screen) {
	if len(a.toasts) == 0 {
		return
	}
	sw, sh := screen.Size()
	width := min(toastWidth, sw-4)
	if width < 16 {
		return
	}
	// Inside the frames of the screen under them, which stay whole.
	x, y := sw-width-2, 2
	fill := tcell.StyleDefault.Background(role("toast.background"))
	for i := len(a.toasts) - 1; i >= 0; i-- {
		t := a.toasts[i]
		lines := tview.WordWrap(t.body, width-4)
		if len(lines) > 3 {
			lines = lines[:3]
		}
		height := 3 + len(lines)
		if t.about != "" {
			height++
		}
		if y+height > sh-1 {
			return
		}
		colour := toastColour(t.sev)
		border := fill.Foreground(colour)
		for row := y; row < y+height; row++ {
			for col := x; col < x+width; col++ {
				screen.SetContent(col, row, ' ', nil, fill)
			}
		}
		for col := x + 1; col < x+width-1; col++ {
			screen.SetContent(col, y, tview.Borders.Horizontal, nil, border)
			screen.SetContent(col, y+height-1, tview.Borders.Horizontal, nil, border)
		}
		for row := y + 1; row < y+height-1; row++ {
			screen.SetContent(x, row, tview.Borders.Vertical, nil, border)
			screen.SetContent(x+width-1, row, tview.Borders.Vertical, nil, border)
		}
		screen.SetContent(x, y, tview.Borders.TopLeft, nil, border)
		screen.SetContent(x+width-1, y, tview.Borders.TopRight, nil, border)
		screen.SetContent(x, y+height-1, tview.Borders.BottomLeft, nil, border)
		screen.SetContent(x+width-1, y+height-1, tview.Borders.BottomRight, nil, border)
		head := tag(colour) + "[::b]" + esc(toastMark(t.sev)+" "+t.title) + "[::-]" + tagEnd
		tview.Print(screen, head, x+2, y+1, width-4, tview.AlignLeft, colour)
		first := y + 2
		if t.about != "" {
			tview.Print(screen, tag(role("toast.about"))+esc(trunc(t.about, width-4))+tagEnd, x+2, first, width-4, tview.AlignLeft, role("toast.about"))
			first++
		}
		for j, line := range lines {
			tview.Print(screen, tag(role("toast.text"))+esc(line)+tagEnd, x+2, first+j, width-4, tview.AlignLeft, role("toast.text"))
		}
		y += height
	}
}
