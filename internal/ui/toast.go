package ui

import (
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A toast is news from the background - a watched pipeline that began,
// passed or failed, a merge request approved - shown in the bottom right
// corner over whatever is in front, for a few seconds. It takes no key and
// no focus: it arrives while the user types into something else, and must
// not take the keyboard from them, which is why it is not a message box.
// Several stack upwards, the newest in the corner. Its time runs only while
// the terminal is in front, so what came while the user was away is still
// there when they come back.
//
// Its severity is never written: it is the toast's colour - the whole of
// it filled, the border brighter - and the icon before its heading.

// toast is one on screen.
type toast struct {
	sev severity
	// title is what happened in a few words, bold; body the sentence of
	// where and how.
	title, body string
	// about is what the news is about - the repository, a merge request's
	// title - on a quieter line under it.
	about string
	left  time.Duration
}

const (
	// toastTick is how often the toasts' time is counted down.
	toastTick = 250 * time.Millisecond
	// toastsKept is how many stack at most; an older one gives way.
	toastsKept = 4
	// toastWidth is the widest a toast is, its border included.
	toastWidth = 56
	// toastBodyLines is as many lines as its sentence takes at most.
	toastBodyLines = 3
)

// toastIcons head a toast of each severity (setTheme).
var toastIcons map[severity]string

// toastLevel is a severity's name among the toast roles.
func toastLevel(sev severity) string {
	switch sev {
	case sevSuccess:
		return "success"
	case sevWarning:
		return "warning"
	case sevError:
		return "danger"
	}
	return "info"
}

// toastRole is a toast's colour of a severity: its background, border,
// text or about.
func toastRole(sev severity, part string) tcell.Color {
	return role("toast." + toastLevel(sev) + "." + part)
}

// showToast puts a toast up. It runs on the event loop.
func (a *App) showToast(sev severity, title, body string) { a.showToastAbout(sev, title, "", body) }

// showToastAbout is showToast with a line saying what it is about.
func (a *App) showToastAbout(sev severity, title, about, body string) {
	a.toasts = append(a.toasts, &toast{sev: sev, title: title, about: about, body: body, left: a.cfg.ToastLife(toastLevel(sev))})
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
// dialogs too, at the bottom right, inside the frame of the screen beneath
// and above its status line.
func (a *App) drawToasts(screen tcell.Screen) {
	if len(a.toasts) == 0 {
		return
	}
	sw, sh := screen.Size()
	width := min(toastWidth, sw-4)
	if width < 20 {
		return
	}
	// Inside the frames of the screen under them, which stay whole.
	x, bottom := sw-width-2, sh-3
	// The icon, then two cells, then the words.
	indent := 5
	inner := width - indent - 2
	for i := len(a.toasts) - 1; i >= 0; i-- {
		t := a.toasts[i]
		lines := tview.WordWrap(t.body, inner)
		if len(lines) > toastBodyLines {
			lines = lines[:toastBodyLines]
			lines[len(lines)-1] = trunc(lines[len(lines)-1]+" …", inner)
		}
		height := 3 + len(lines)
		if t.about != "" {
			height++
		}
		top := bottom - height + 1
		if top < 1 {
			return
		}
		fillColour, text := toastRole(t.sev, "background"), toastRole(t.sev, "text")
		fill := tcell.StyleDefault.Background(fillColour)
		border := fill.Foreground(toastRole(t.sev, "border"))
		for row := top; row <= bottom; row++ {
			for col := x; col < x+width; col++ {
				screen.SetContent(col, row, ' ', nil, fill)
			}
		}
		for col := x + 1; col < x+width-1; col++ {
			screen.SetContent(col, top, tview.Borders.Horizontal, nil, border)
			screen.SetContent(col, bottom, tview.Borders.Horizontal, nil, border)
		}
		for row := top + 1; row < bottom; row++ {
			screen.SetContent(x, row, tview.Borders.Vertical, nil, border)
			screen.SetContent(x+width-1, row, tview.Borders.Vertical, nil, border)
		}
		screen.SetContent(x, top, tview.Borders.TopLeft, nil, border)
		screen.SetContent(x+width-1, top, tview.Borders.TopRight, nil, border)
		screen.SetContent(x, bottom, tview.Borders.BottomLeft, nil, border)
		screen.SetContent(x+width-1, bottom, tview.Borders.BottomRight, nil, border)
		on := ":" + fillColour.String()
		icon := toastRole(t.sev, "border")
		tview.Print(screen, "["+icon.String()+on+":b]"+esc(toastIcons[t.sev])+"[-:-:-]", x+2, top+1, 2, tview.AlignLeft, icon)
		tview.Print(screen, "["+text.String()+on+":b]"+esc(trunc(t.title, inner))+"[-:-:-]", x+indent, top+1, inner, tview.AlignLeft, text)
		row := top + 2
		for _, line := range lines {
			tview.Print(screen, "["+text.String()+on+"]"+esc(line)+"[-:-:-]", x+indent, row, inner, tview.AlignLeft, text)
			row++
		}
		if t.about != "" {
			about := toastRole(t.sev, "about")
			tview.Print(screen, "["+about.String()+on+"]"+esc(trunc(t.about, inner))+"[-:-:-]", x+indent, row, inner, tview.AlignLeft, about)
		}
		bottom = top - 1
	}
}
