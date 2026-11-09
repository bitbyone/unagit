package ui

import (
	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/notify"
)

// followTitle names the terminal's tab after where unagit is - Ghostty,
// iTerm2, kitty and WezTerm name a tab by its window's title - and only
// when that changed, since a draw comes with every key. tcell keeps the
// title the shell had and puts it back on the way out, and gives this one
// again after an editor.
func (a *App) followTitle(screen tcell.Screen) {
	title := notify.WindowTitle
	if where := a.titleWhere(); where != "" && a.cfg.TabTitleOn() {
		title += " · " + where
	}
	if title == a.windowTitle {
		return
	}
	a.windowTitle = title
	screen.SetTitle(title)
}

// titleWhere is where unagit is, in a few words: the repository standalone
// works on, the worktree open in its view, or the screen in front. Locked,
// it is nowhere yet.
func (a *App) titleWhere() string {
	switch {
	case a.solo != nil:
		return a.solo.name + " · " + a.solo.branch
	case a.vault == nil:
		return ""
	case a.wtView != nil:
		r := a.wtView.row
		if r.Branch == "" {
			return r.Path
		}
		return r.Path + " · " + r.Branch
	}
	page := a.currentTab()
	for _, t := range tabs {
		if t.page == page {
			return t.title
		}
	}
	return ""
}

// titleNow is the title the window has, for a notification that took it
// for a moment to give back.
func (a *App) titleNow() string {
	if a.windowTitle == "" {
		return notify.WindowTitle
	}
	return a.windowTitle
}
