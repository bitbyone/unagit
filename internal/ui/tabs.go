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

// The tabs are numbered: letters are the lists' own, and R refreshes.
var tabs = []tab{
	{pageProjects, '1', "Repositories"},
	{pageMRs, '2', "Merge requests"},
	{pageWorktrees, '3', "Worktrees"},
	{pageSettings, '4', "Settings"},
}

// drawTabs renders the tab bar, highlighting the visible page.
func (a *App) drawTabs() {
	current := a.currentTab()
	var parts []string
	for _, t := range tabs {
		// tview reads "[P]" as a colour tag, so the brackets have to be escaped.
		label := tview.Escape(fmt.Sprintf(" [%c] %s ", t.key, t.title))
		if t.page == current {
			parts = append(parts, fmt.Sprintf("[%s::br]%s[-:-:-]", colTabActive.String(), label))
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]%s[-]", colTabInactive.String(), label))
	}
	a.tabs.SetText(" " + strings.Join(parts, fmt.Sprintf("[%s]%s[-]", colTabSeparator.String(), glyphTabSeparator)))
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
	a.refreshZoxide()
	switch page {
	case pageProjects:
		a.tv.SetFocus(a.projectsPane.focusTarget())
		a.refreshLocal()
	case pageMRs:
		a.tv.SetFocus(a.mrsPane.focusTarget())
		a.refreshLocal()
	case pageWorktrees:
		a.tv.SetFocus(a.worktreesPane.focusTarget())
		a.refreshLocal()
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
	a.clearSaid()
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
