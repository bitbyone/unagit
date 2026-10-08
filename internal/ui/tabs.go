package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// tab describes one of the top level views. short is its title where the
// terminal is too narrow for them all.
type tab struct {
	page         string
	key          rune
	title, short string
}

// The tabs are numbered: letters are the lists' own, and R refreshes.
var tabs = []tab{
	{pageProjects, '1', "Repositories", "Repos"},
	{pageMRs, '2', "Merge requests", "MRs"},
	{pageWorktrees, '3', "Worktrees", "Worktrees"},
	{pageAgents, '4', "Agents", "Agents"},
	{pageWatched, '5', "Watched", "Watched"},
	{pageSettings, '6', "Settings", "Settings"},
}

// settingsTab is how a message points at Settings.
const settingsTab = "[6] Settings"

// drawTabs renders the tab bar, highlighting the visible page. Where the
// titles do not fit, the short ones stand in; where even those do not, the
// keys lose their brackets.
func (a *App) drawTabs() {
	current := a.currentTab()
	label := func(t tab, short, tight bool) string {
		title := t.title
		if short {
			title = t.short
		}
		// The agents waiting for an answer are counted where they show from
		// every screen, and so are the watched changes nobody has seen.
		if t.page == pageAgents && a.agentsWaiting() > 0 {
			title += fmt.Sprintf(" %s%d", glyphManual, a.agentsWaiting())
		}
		// A watched pipeline under way turns its mark there.
		if t.page == pageWatched && a.watchesRunning() > 0 {
			title += fmt.Sprintf(" %s%d", ciFrame(), a.watchesRunning())
		}
		if t.page == pageWatched && a.watchUnseen() > 0 {
			title += fmt.Sprintf(" %s%d", glyphDot, a.watchUnseen())
		}
		if tight {
			return fmt.Sprintf(" %c %s ", t.key, title)
		}
		return fmt.Sprintf(" [%c] %s ", t.key, title)
	}
	short, tight := false, false
	fits := func(short, tight bool) bool {
		width := 1
		for _, t := range tabs {
			width += cells(label(t, short, tight)) + 1
		}
		return a.tabsWidth == 0 || width <= a.tabsWidth
	}
	switch {
	case fits(false, false):
	case fits(true, false):
		short = true
	default:
		short, tight = true, true
	}
	var parts []string
	for _, t := range tabs {
		// tview reads "[P]" as a colour tag, so the brackets have to be escaped.
		text := tview.Escape(label(t, short, tight))
		if t.page == current {
			parts = append(parts, fmt.Sprintf("[%s::br]%s[-:-:-]", colTabActive.String(), text))
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]%s[-]", colTabInactive.String(), text))
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
	a.agentsInFront.Store(page == pageAgents)
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
	case pageAgents:
		a.tv.SetFocus(a.agentsPane.focusTarget())
		a.agentsNowAsk()
	case pageWatched:
		a.tv.SetFocus(a.watchedPane.focusTarget())
		a.watchLooked = max(a.watchSnap.Seen, a.watchSeen.Load())
		a.watchedPane.reload()
		a.markWatchesSeen()
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
