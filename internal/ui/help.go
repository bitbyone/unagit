package ui

import (
	"math/bits"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Help captures focus before opening its overlay; it describes the place the
// user came from, not the help table which now owns the keyboard.
type helpContext uint32

const (
	helpRepoList helpContext = 1 << iota
	helpRepoDetail
	helpMRList
	helpMRDetail
	helpWorktreeList
	helpWorktreeDetail
	helpSettingsList
	helpServers
	helpGroups
	helpGeneral
	helpIntegrations
	helpSecurity
	helpRepositories  = helpRepoList | helpRepoDetail
	helpMergeRequests = helpMRList | helpMRDetail
	helpWorktrees     = helpWorktreeList | helpWorktreeDetail
	helpLists         = helpRepositories | helpMergeRequests
	helpDetails       = helpRepoDetail | helpMRDetail | helpWorktreeDetail
	helpNavigation    = helpLists | helpWorktrees | helpSettingsList | helpServers | helpGroups | helpIntegrations | helpSecurity
)

func (l helpLine) in(scope helpContext) helpLine { l.scope = scope; return l }

func (a *App) helpContext() (helpContext, string) {
	switch a.currentTab() {
	case pageProjects:
		if a.projectsPane.detailFocused {
			return helpRepoDetail, "Repository detail"
		}
		return helpRepoList, "Repositories"
	case pageMRs:
		if a.mrsPane.detailFocused {
			return helpMRDetail, "Merge request detail"
		}
		return helpMRList, "Merge requests"
	case pageWorktrees:
		if a.worktreesPane.detailFocused {
			return helpWorktreeDetail, "Worktree detail"
		}
		return helpWorktreeList, "Worktrees"
	default:
		if !a.settings.contentFocused {
			return helpSettingsList, "Settings"
		}
		switch a.settings.current {
		case sectionGeneral:
			return helpGeneral, "General settings"
		case sectionGitLab, sectionGitHub:
			return helpServers, "Servers"
		case sectionGroups:
			return helpGroups, "Groups"
		case sectionIntegrations:
			return helpIntegrations, "Integrations"
		case sectionSecurity:
			return helpSecurity, "Security"
		}
	}
	return 0, "Help"
}

// Keep the most specific active sections first, without hiding the reference
// for other contexts. Stable ordering keeps equally relevant sections familiar.
func contextHelpRows(context helpContext) []helpLine {
	var groups [][]helpLine
	for _, line := range helpRows() {
		if line.section != "" {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], line)
	}
	rank := func(group []helpLine) int {
		scope := group[0].scope
		if scope&context == 0 {
			return 100
		}
		score := bits.OnesCount32(uint32(scope))
		for _, line := range group {
			if line.keys != "" {
				return score
			}
		}
		return 20 + score
	}

	sort.SliceStable(groups, func(i, j int) bool { return rank(groups[i]) < rank(groups[j]) })
	var rows []helpLine
	for _, group := range groups {
		rows = append(rows, group...)
	}
	return rows
}

// helpLine is one row of the help: a section heading, a key with what it
// does, or a paragraph of explanation.
type helpLine struct {
	scope   helpContext
	section string
	keys    string
	text    string
	note    string
}

func section(name string, scope helpContext) helpLine { return helpLine{section: name, scope: scope} }
func key(keys, text string) helpLine                  { return helpLine{keys: keys, text: text} }
func note(text string) helpLine                       { return helpLine{note: text} }
func blank() helpLine                                 { return helpLine{} }

// helpRows is the whole of the help, in the order it is read. Keeping it as
// data rather than as one long string is what lets the keys line up in their
// own column.
func helpRows() []helpLine {
	rows := []helpLine{
		section("Getting around", helpNavigation),
		key("R  M  W  S", "Repositories · Merge requests · Worktrees · Settings"),
		key("j  k", "move up and down"),
		key("g  G", "first · last").in(helpLists | helpWorktreeList),
		key("/", "filter: fuzzy, spaces separate terms").in(helpLists | helpWorktreeList),
		key("Esc", "clear the filter or close the detail").in(helpRepoList | helpMRList | helpWorktreeList),
		key("Enter", "load it and jump in; it then follows the cursor").in(helpRepoList | helpMRList | helpWorktreeList),
		key("?", "this help").in(helpNavigation | helpGeneral),
		key("q", "quit"),
		blank(),

		section("The detail column", helpDetails),
		key("j k g G", "scroll"),
		key("Ctrl-F Ctrl-B", "page"),
		key("h  ←  Esc", "back to the list"),
		note("Repositories show statistics, languages, the latest pipeline, the " +
			"most recent commits and their open merge requests. Merge requests are " +
			"always fetched fresh."),
		blank(),

		section("Dialogs", 0),
		note("Modals and blocks with up to five actions show inline hints: c cancels, d deletes, a approves. " +
			"Forms use Alt + the shown letter while editing; on buttons the letter alone works too. Esc goes back."),
		blank(),

		section("Every list", helpLists|helpWorktreeList),
		key("Ctrl-O", "clone or update, then open the editor"),
		key("d", "delete from disk, warning about work that would be lost"),
		key("w", "open in the browser").in(helpLists),
		key("r", "refresh: the index from the server, or the worktrees from disk and origin"),
		blank(),

		section("Filters · shared by both lists", helpLists),
		key("C", "only the repositories you have cloned"),
		key("x", "hide the repository under the cursor, or bring it back"),
		key("X", "manage the hidden repositories"),
		key("o", "order: by activity, or by name"),
		key("Ctrl-G", "gather the merge requests under their repository").in(helpMergeRequests),
		note("Hiding a repository takes its merge requests with it. The header " +
			"under each list says what is being left out."),
		blank(),

		section("Repositories", helpRepositories),
		key("Ctrl-C", "clone to disk without opening the editor"),
		key("e", "set the exact clone directory of an uncloned repository"),
		key("b", "pick a branch and switch the main clone to it"),
		key("Ctrl-W", "pick a branch - or 'n' for a new one - and open it in its own worktree"),
		key("m", "show only the merge requests of this repository"),
		note("PATH shows the clone destination; dim paths are planned, not yet cloned."),
		note("d deletes the bare clone directly; with any worktree on top it opens a " +
			"list instead, so a single merge request or branch worktree can go on its " +
			"own, or the [main clone] entry for everything at once."),
		blank(),

		section("Worktrees", helpWorktrees),
		key("P", "push the branch, with -u when it has no upstream; never forced"),
		key("n", "open a merge request for it; offers to push first"),
		key("REMOTE", "no upstream · in sync · ↑ unpushed · ↓ behind · upstream gone").in(helpWorktrees),
		key("MR", "the open merge request the branch already has").in(helpWorktrees),
		note("Every worktree made with Ctrl-W in Repositories, whichever repository it " +
			"belongs to; Enter shows whether one is clean and pushed, and its latest " +
			"commits. Worktrees that belong to a merge request are on the Merge requests tab."),
		blank(),

		section("Merge requests", helpMergeRequests),
		key("Ctrl-R", "open for review: the whole change as pending edits"),
		key("c", "read the conversation, and write a comment"),
		key("A", "approve - it asks first (capital, like P for publish: both are seen by everyone)"),
		key("P", "publish the Incomm comments marked for the merge request, and resolve the threads you resolved - it lists them first"),
		key("f  F", "limit the list to one repository · clear that limit"),
		note("The COM column is how many comments a merge request has. GitLab " +
			"reports it on the listing; GitHub only on a single merge request, so " +
			"there it fills in once you have opened one."),
		note("With Incomm on, the PUB column counts the comments and replies in the " +
			"merge request's worktrees that are meant for the forge and have not gone " +
			"there yet. P posts them one by one, the conversation's first comment " +
			"before its replies, and writes each one's forge id back at once, so a " +
			"failure half way never posts anything twice. What the agent wrote is " +
			"marked as the agent's in the text, because the forge shows your name. " +
			"Nothing is ever published without P."),
		note("Incomm re-anchors the comments whenever a worktree is updated (Ctrl-O, " +
			"Ctrl-R), so each one is on the line its code is on; P posts the lines Incomm " +
			"has stored. One whose code is gone is marked orphaned and is posted on the " +
			"conversation with its file, not at a stale line. A thread you resolved in Incomm that is on the forge, and open " +
			"there, is listed as \"resolve thread\" and resolved after its posts " +
			"(GitLab only: GitHub's API cannot resolve threads, and unagit says so)."),
		note("Comments come in from the forge on Ctrl-R only, at the file and line " +
			"the forge gives; running it again adds the new ones and leaves the ones " +
			"already there where they are. Edits and deletions on the forge are not " +
			"synced. Resolved only moves one way: a thread the forge has resolved is " +
			"resolved in Incomm, but nothing is ever reopened, on either side."),
		blank(),

		section("Comments  (c)", 0),
		key("i", "write one, Ctrl-S sends it"),
		key("A", "approve"),
		key("r", "reload"),
		note("Oldest first, with the markdown rendered. The detail column keeps " +
			"the three newest."),
		blank(),

		section("Settings  (S)", helpSettingsList),
		key("j  k", "move between the sections"),
		key("Enter", "edit the section"),
		blank(),
		section("Settings · servers", helpServers),
		key("a e t v d", "add · edit · token · verify · remove"),
		key("Esc", "back to the sections"),
		blank(),
		section("Settings · groups", helpGroups),
		key("space", "in the group tree: a GitLab group cycles off → this group "+
			"only → including subgroups; a GitHub organisation is on or off"),
		key("d", "in the group tree: the clone directory of a group or a server"),
		key("r  p  m", "reload the groups · refresh projects · refresh merge requests"),
		key("Esc", "back to the sections"),
		blank(),
		section("Settings · security", helpSecurity),
		key("c", "change the passphrase"),
		key("Esc", "back to the sections"),
		blank(),
		section("Settings · general", helpGeneral),
		key("Alt-s / Alt-r", "save / revert; plain s / r also work on buttons"),
		key("Tab / Shift-Tab", "next / previous field"),
		key("Esc", "back to the sections"),
		blank(),
		section("Settings · integrations", helpIntegrations),
		key("e", "toggle the selected integration"),
		key("c", "check installation"),
		key("Tab / j k", "move between integrations"),
		key("Esc", "back to the sections"),
		blank(),

		section("Reviewing", helpMergeRequests),
		note("Opening a merge request (Ctrl-O or Ctrl-R) also asks the server about that one " +
			"request, so its row and its checkout are current; the rest of the list waits for r. " +
			"The detail column refreshes its row too, except the time the list is ordered by, " +
			"so the list does not shuffle while you move through it."),
		note("Ctrl-O gives you the branch: real commits, you can commit and push."),
		note("Ctrl-R gives you the review worktree: HEAD and the index sit on the " +
			"commit the merge request branched from while the working tree holds the " +
			"merge request, so the whole change is pending and unstaged. git diff, " +
			"gutter signs, ]c and diff views then work on it as one change."),
		note("Both record what they are: git config unagit.mr.base / .head / .iid " +
			"/ .target / .url / .mode."),
		blank(),

		section("From another terminal", 0),
		note("Opening an editor does not end unagit: it suspends itself and waits, so " +
			"it knows what you have open. unagit cd in another window starts a shell " +
			"there and exit comes back; it asks which when more than one is open, and " +
			"takes a search to narrow it. unagit cd --print writes the path instead, " +
			"for cd \"$(unagit cd --print)\". unagit sessions lists them."),
		blank(),

		section("On disk", helpLists),
		key("○ ● ◐ ◉", "nothing · branch worktree · review worktree · both"),
		key("⊘", "hidden from the lists"),
		note("<root>/<group>/<repo> is the main clone, where branch switching " +
			"happens. .unagit/<repo>/<iid>-<branch> is a branch worktree and " +
			".unagit/<repo>/review-<iid>-<branch> a review one; .unagit/<repo>/wt-<branch> " +
			"is a worktree for a plain branch, opened with Ctrl-W and counted in the " +
			"Repositories tab's WT column. They all share the main clone's objects, so " +
			"uncommitted changes survive switching between them. <root> comes from " +
			"Settings, unless the server or the group overrides it. The Repositories " +
			"tab shows it in the PATH column."),
	}
	var scope helpContext
	for i := range rows {
		if rows[i].section != "" {
			scope = rows[i].scope
		}
		if rows[i].scope == 0 {
			rows[i].scope = scope
		}
	}
	return rows

}

// wrapText breaks a paragraph into lines that fit a width, on word
// boundaries. The table cannot wrap on its own, so the rows are made to fit.
func wrapText(text string, width int) []string {
	if width < 12 {
		width = 12
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// showHelp draws the help as a two column table: keys on the left, what they
// do on the right. A table rather than a block of text is what keeps the
// columns aligned however long a key combination is, and the rows are wrapped
// to the width the modal actually got.
func (a *App) showHelp() {
	table := tview.NewTable().SetSelectable(false, false)
	context, title := a.helpContext()
	box(table.Box, "unagit · keys · "+title).SetBorderPadding(0, 0, 2, 2)

	rows := contextHelpRows(context)
	keyWidth := 0
	for _, line := range rows {
		keyWidth = max(keyWidth, len([]rune(line.keys)))
	}

	fill := func(width int) {
		table.Clear()
		textWidth := width - keyWidth - 2
		row := 0
		put := func(keys, text string, keyColour, textColour tcell.Color, attrs tcell.AttrMask) {
			table.SetCell(row, 0, tview.NewTableCell(keys).
				SetTextColor(keyColour).
				SetAttributes(attrs).
				SetAlign(tview.AlignRight).
				SetSelectable(false))
			table.SetCell(row, 1, tview.NewTableCell(" "+text).
				SetTextColor(textColour).
				SetAttributes(attrs).
				SetExpansion(1).
				SetSelectable(false))
			row++
		}
		for _, line := range rows {
			keyColour, textColour, headingColour := colDim, colDim, colDim
			if line.scope&context != 0 {
				keyColour, textColour, headingColour = colText, colText, colTitle
			}
			switch {
			case line.section != "":
				put("", strings.ToUpper(line.section), colDim, headingColour, tcell.AttrBold)
			case line.keys != "":
				for i, text := range wrapText(line.text, textWidth) {
					if i == 0 {
						put(line.keys, text, keyColour, textColour, tcell.AttrNone)
						continue
					}
					put("", text, colDim, textColour, tcell.AttrNone)
				}
			case line.note != "":
				for _, text := range wrapText(line.note, textWidth) {
					put("", text, colDim, textColour, tcell.AttrNone)
				}
			default:
				put("", "", colDim, colDim, tcell.AttrNone)
			}
		}
	}
	fill(76)

	// The modal is a share of the terminal, so the width is only known once
	// it has been laid out.
	width := 0
	table.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		if inner := w - 6; inner != width {
			width = inner
			go a.tv.QueueUpdateDraw(func() { fill(inner) })
		}
		return x + 3, y + 1, w - 6, h - 2
	})

	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc, tcell.KeyEnter:
			a.closeModal(pageHelp)
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case '?', 'q':
				a.closeModal(pageHelp)
				return nil
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			case 'g':
				table.ScrollToBeginning()
				return nil
			case 'G':
				table.ScrollToEnd()
				return nil
			}
		}
		return ev
	})

	footer := tview.NewTextView().SetTextColor(colDim).SetText("j/k scroll · g/G first/last · Enter/Esc/?/q close")
	block := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(table, 0, 1, true).AddItem(footer, 1, 0, false)
	fitFooter(block, footer, 0)
	a.pages.AddPage(pageHelp, modalPct(block, 82, 90), true, true)
	a.tv.SetFocus(table)
}
