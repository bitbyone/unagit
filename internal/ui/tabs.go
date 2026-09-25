package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// tab describes one of the top level views.
type tab struct {
	page  string
	key   rune
	title string
}

var tabs = []tab{
	{pageProjects, 'R', "Repositories"},
	{pageMRs, 'M', "Merge requests"},
	{pageWorktrees, 'W', "Worktrees"},
	{pageSettings, 'S', "Settings"},
}

// drawTabs renders the tab bar, highlighting the visible page.
func (a *App) drawTabs() {
	current := a.currentTab()
	var parts []string
	for _, t := range tabs {
		// tview reads "[P]" as a colour tag, so the brackets have to be escaped.
		label := tview.Escape(fmt.Sprintf(" %s [%c] ", t.title, t.key))
		if t.page == current {
			parts = append(parts, fmt.Sprintf("[%s::br]%s[-:-:-]", colTabActive.String(), label))
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]%s[-]", colMuted.String(), label))
	}
	a.tabs.SetText(" " + strings.Join(parts, fmt.Sprintf("[%s]│[-]", colDim.String())))
}

// currentTab returns the page name of the visible tab, ignoring modals.
func (a *App) currentTab() string {
	if a.tab == "" {
		return pageProjects
	}
	return a.tab
}

// switchTab shows one of the main pages.
func (a *App) switchTab(page string) {
	a.tab = page
	a.pages.SwitchToPage(page)
	switch page {
	case pageProjects:
		a.tv.SetFocus(a.projectsPane.focusTarget())
	case pageMRs:
		a.tv.SetFocus(a.mrsPane.focusTarget())
	case pageWorktrees:
		a.tv.SetFocus(a.worktreesPane.focusTarget())
	case pageSettings:
		a.tv.SetFocus(a.settings.focusTarget())
	}
	a.drawTabs()
	if a.helpHint != nil {
		if page == pageSettings {
			a.helpHint.SetText(tag(colDim) + "? help" + tagEnd)
		} else {
			a.helpHint.SetText("")
		}
	}
	a.setStatus("")
}

// tabKey switches tabs when the event is one of the tab shortcuts.
func (a *App) tabKey(r rune) bool {
	for _, t := range tabs {
		if t.key == r {
			a.switchTab(t.page)
			return true
		}
	}
	return false
}
