package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A read from a server before a dialog can show it - a pipeline, a log,
// the branches for a form - has nothing worth keeping to say: the log that
// runTask keeps flashed up for a moment and went, and the dialog came after
// it. So a read waits in a small box instead, a spinner and the step under
// way, over whatever is in front, and the dialog opens once it has its
// data. Work whose log is worth reading - a clone, a push, a commit - keeps
// runTask. The box is on the task's page, so a read that puts up its own
// dialog closes it as a task does (closeModal(pageTask)).

// load runs fn behind the box; fn opens what it read itself.
func (a *App) load(title string, fn func(step func(string)) (string, error)) {
	a.loadThen(title, fn, nil)
}

// loadThen runs fn behind the box, then, if it went through and the box
// was not given up on, then on the event loop. What went wrong is a
// warning. Esc gives up waiting: what comes back later is dropped.
func (a *App) loadThen(title string, fn func(step func(string)) (string, error), then func(string)) {
	step := title
	view := tview.NewBox()
	view.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		// Handed the outer rectangle: inside the border only.
		for col := x + 1; col < x+w-1; col++ {
			screen.SetContent(col, y+1, ' ', nil, baseStyle())
		}
		text := tag(colAccent) + spinnerGlyph(a.spinFrame) + tagEnd + " " + tag(colMuted) + esc(step) + tagEnd
		tview.Print(screen, text, x+2, y+1, w-4, tview.AlignLeft, colMuted)
		return x, y, w, h
	})
	box(view, "")
	page := modalFit(view, func(w, h int) (int, int) {
		return min(w-4, max(40, tview.TaggedStringWidth(esc(step))+8)), 3
	})
	gone := func() bool { return a.pages.GetPage(pageTask) != page }
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc {
			a.closeModal(pageTask)
		}
		return nil
	})
	a.pages.AddPage(pageTask, page, true, true)
	a.tv.SetFocus(view)
	a.waits++
	a.keepSpinning()

	go func() {
		result, err := fn(func(line string) {
			a.tv.QueueUpdateDraw(func() {
				if line != "" {
					step = line
				}
			})
		})
		a.tv.QueueUpdateDraw(func() {
			a.waits--
			if gone() {
				// Given up on, or closed by fn for what it opened.
				return
			}
			a.closeModal(pageTask)
			a.resay(page)
			if err != nil {
				a.errorf("%v", err)
				return
			}
			if then != nil {
				then(result)
			}
		})
	}()
}

// resay puts a word said while the box was in front where the eye goes once
// it is gone: the dialog now in front, or the status line.
func (a *App) resay(was tview.Primitive) {
	if a.dialogSaid.on != was || a.dialogSaid.text == "" {
		return
	}
	text := a.dialogSaid.text
	a.dialogSaid = dialogWord{}
	if name, front := a.pages.GetFrontPage(); isModalPage(name) {
		a.dialogSaid = dialogWord{on: front, text: text}
		return
	}
	a.transient = text
	a.showJobs()
}
