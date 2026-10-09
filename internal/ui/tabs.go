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
	{pageActivity, '4', "Activity", "Activity"},
	{pageSettings, '5', "Settings", "Settings"},
}

// settingsTab is how a message points at Settings.
const settingsTab = "Settings [5]"

// drawTabs renders the tab bar, highlighting the visible page. Where the
// titles do not fit, the short ones stand in; where even those do not, the
// keys lose their brackets.
func (a *App) drawTabs() {
	current := a.currentTab()
	// A tab reads as a panel's title does: its icon where the terminal has
	// a Nerd Font, its name, and the key that reaches it after the name, in
	// brackets and quieter - or bare where the bar is tight.
	tabParts := func(t tab, short, tight bool) (name, key string) {
		name = t.title
		if short {
			name = t.short
		}
		if icon := columnIcons[t.title]; icon != "" {
			name = icon + " " + name
		}
		if tight {
			return name, string(t.key)
		}
		return name, "[" + string(t.key) + "]"
	}
	label := func(t tab, short, tight bool) string {
		name, key := tabParts(t, short, tight)
		return " " + name + " " + key + " "
	}
	// What waits on the Activity screen is counted where it shows from
	// every screen, each count a still dot and its number in a colour of
	// its own after the tab's name: the agents waiting for an answer, the
	// watched pipelines under way, the changes nobody has seen. Still: a
	// mark turning there only looked broken.
	type badge struct {
		text   string
		colour string
	}
	var badges []badge
	if n := a.agentsWaiting(); n > 0 {
		badges = append(badges, badge{fmt.Sprintf("%s%d", glyphDot, n), "tabs.waiting"})
	}
	if n := a.watchesRunning(); n > 0 {
		badges = append(badges, badge{fmt.Sprintf("%s%d", glyphDot, n), "tabs.running"})
	}
	if n := a.watchUnseen(); n > 0 {
		badges = append(badges, badge{fmt.Sprintf("%s%d", glyphDot, n), "tabs.new"})
	}
	badgesWidth := 0
	for _, b := range badges {
		badgesWidth += cells(b.text) + 1
	}
	short, tight := false, false
	fits := func(short, tight bool) bool {
		width := 1 + badgesWidth
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
		var text string
		if t.page == current {
			text = fmt.Sprintf("[%s::br]%s[-:-:-]", colTabActive.String(), tview.Escape(label(t, short, tight)))
		} else {
			// The name in the tabs' accent, the key quieter after it: the
			// names are what is read.
			name, key := tabParts(t, short, tight)
			text = fmt.Sprintf(" [%s]%s[-] [%s]%s[-] ", role("tabs.name").String(), tview.Escape(name), role("tabs.key").String(), tview.Escape(key))
		}
		if t.page == pageActivity && len(badges) > 0 {
			// Outside the tab's own band, a cell off it and from each other,
			// each on the screen's background whatever the tab's style.
			for _, b := range badges {
				text += "[" + role(b.colour).String() + ":" + colBackground.String() + ":b] " + esc(b.text) + "[-:-:-]"
			}
			text += " "
		}
		parts = append(parts, text)
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
	a.agentsInFront.Store(page == pageActivity)
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
	case pageActivity:
		a.visitActivity()
		a.activity.focusAgain()
	case pageSettings:
		a.tv.SetFocus(a.settings.focusTarget())
	}
	a.drawTabs()
	if a.helpHint != nil {
		if page == pageSettings {
			a.helpHint.SetText(litHint("? help"))
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
