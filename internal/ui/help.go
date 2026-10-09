package ui

import (
	"math/bits"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
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
	helpActivity
	helpSettingsList
	helpServers
	helpGroups
	helpGeneral
	helpIntegrations
	helpSecurity
	helpTags
	helpTheme
	helpRepositories  = helpRepoList | helpRepoDetail
	helpMergeRequests = helpMRList | helpMRDetail
	helpWorktrees     = helpWorktreeList | helpWorktreeDetail
	helpLists         = helpRepositories | helpMergeRequests
	helpDetails       = helpRepoDetail | helpMRDetail | helpWorktreeDetail
	helpNavigation    = helpLists | helpWorktrees | helpActivity | helpSettingsList | helpServers | helpGroups | helpIntegrations | helpSecurity | helpTags | helpTheme
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
		if a.wtView != nil {
			return helpWorktreeDetail, "Worktree view"
		}
		if a.worktreesPane.detailFocused {
			return helpWorktreeDetail, "Worktree detail"
		}
		return helpWorktreeList, "Worktrees"
	case pageActivity:
		return helpActivity, "Activity"
	default:
		if !a.settings.contentFocused {
			return helpSettingsList, "Settings"
		}
		switch a.settings.current {
		case sectionGeneral:
			return helpGeneral, "General settings"
		case sectionNotifications:
			return helpGeneral, "Notification settings"
		case sectionGitLab, sectionGitHub:
			return helpServers, "Servers"
		case sectionGroups:
			return helpGroups, "Groups"
		case sectionTags:
			return helpTags, "Tags"
		case sectionTheme:
			return helpTheme, "Theme"
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

// helpLine is one row of the help: a section heading, or a key with what it
// does. There are deliberately no paragraphs: the help is read at a glance,
// key by key, and prose buried the keys every time it was allowed in. The
// explanations belong in the README.
type helpLine struct {
	scope   helpContext
	section string
	keys    string
	text    string
}

func section(name string, scope helpContext) helpLine { return helpLine{section: name, scope: scope} }
func key(keys, text string) helpLine                  { return helpLine{keys: keys, text: text} }
func blank() helpLine                                 { return helpLine{} }

// helpRows is the whole of the help, in the order it is read. Keeping it as
// data rather than as one long string is what lets the keys line up in their
// own column.
func helpRows() []helpLine {
	// The rows show the theme's glyphs, so there has to be one.
	applyTheme()
	rows := []helpLine{
		section("Getting around", helpNavigation),
		key("1 2 3 4 5 6", "Repos, MRs, Worktrees, Agents, Watched, Settings"),
		key("Alt-Enter", "every action on the selection, with its key"),
		key("Ctrl-A", "the same, where the terminal keeps Alt-Enter"),
		key(":", "every action of the screen, with its key"),
		key("j  k", "move up and down"),
		key("g  G", "first · last").in(helpLists | helpWorktreeList),
		key("/", "filter: fuzzy, spaces separate terms").in(helpLists | helpWorktreeList),
		key("Esc", "clear the filter or selection, close the detail").in(helpRepoList | helpMRList | helpWorktreeList),
		key("Enter", "load it and jump in; it then follows the cursor").in(helpRepoList | helpMRList | helpWorktreeList),
		key("?", "this help").in(helpNavigation | helpGeneral),
		key("q", "quit"),
		blank(),

		section("The detail column", helpDetails),
		key("j k g G", "scroll"),
		key("Ctrl-F Ctrl-B", "page"),
		key("h  ←  Esc", "back to the list"),
		blank(),

		section("Dialogs", 0),
		key("c  Esc", "cancel · go back"),
		key("letter", "the button whose letter is lit"),
		key("j k  Tab", "a form: from field to field, over the buttons"),
		key("i  Enter", "type into the field; Esc stops typing"),
		key("Alt-Enter", "a list: what can be done with the item"),
		key(":", "what can be done from anywhere: theme · editors"),
		key("Ctrl-D Ctrl-U", "a log or a commit: half a page down · up"),
		key("Ctrl-F Ctrl-B", "a log or a commit: a page down · up"),
		blank(),

		section("Every list", helpLists|helpWorktreeList),
		key("Ctrl-O", "open as it is on disk; clones only what is missing"),
		key("Alt-O", "the same, in an editor you choose"),
		key("O", "open with what, and where: editor or agent"),
		key("d", "delete from disk; warns about unsaved work"),
		key("w", "open in the browser").in(helpLists),
		key("Ctrl-L", "commit log; Enter shows a commit's detail"),
		key("D  Alt-D", "in the log: the commit · from it to now, in Hunk"),
		key("C  B", "in the log: check out a commit · back to branch"),
		key("n  Ctrl-W", "in the log: a branch · a worktree at the commit"),
		key("w  y", "in the log: browser · copy id, link, reference"),
		key("J", "in the log: the commit's pipelines"),
		key("D  Alt-D", "Hunk: not committed · since the base"),
		key("y", "copy the link, reference, branch or directory"),
		key("yy", "copy the link"),
		key("r", "refresh the row: fetch it, ask the server about it"),
		key("R", "refresh the whole list from the servers"),
		key("E", "running editors: Enter attach · a in… · x close"),
		key("Alt-A", "running agents: Enter goes to one"),
		key("Ctrl-Z", "in Neovim 0.12+: put aside and return to unagit"),
		key(editorGlyph(editors.Nvim), "this directory is open in Neovim"),
		key(glyphAgent, "an agent works here; its colour, what it does"),
		key(glyphWatched, "its pipelines are watched; see Watched (5)"),
		blank(),

		section("Filters · shared by both lists", helpLists),
		key("L", "only the repositories you have cloned"),
		key("x", "hide or show the repository and its merge requests").in(helpRepoList),
		key("X", "manage the hidden repositories").in(helpRepoList),
		key("o", "order: activity, name, columns; visits for repos"),
		key("Ctrl-G", "group: repositories by group, MRs by repository"),
		key("Ctrl-F", glyphFavourite+" star; starred lead a flat list unless o says not"),
		blank(),

		section("Repositories", helpRepositories),
		key("C", "clone without opening the editor"),
		key("e", "set the clone directory of an uncloned repository"),
		key("b", "branches: where each is; Enter switches the clone"),
		key("n", "in b: a new branch from the one under the cursor"),
		key("m", "in b: a merge request from the branch"),
		key("d  D  Alt-D", "in b: delete in the clone · everywhere · on origin"),
		key("d  D", "in b: a branch out in a worktree goes with it"),
		key("Ctrl-W", "in b: a worktree for the branch"),
		key("p", "pull; rebases your work; refuses on a conflict"),
		key("Alt-P", "the same for every clone origin has moved past"),
		key("Ctrl-W", "a worktree for a branch; n for a new one"),
		key("space", "mark; Ctrl-W x H r p C y Ctrl-T/F act on all"),
		key("m", "show only this repository's merge requests"),
		key("Ctrl-T", "tag the repository"),
		key("J", "pipeline of the clone's branch: log · run"),
		key("H", "hide its merge requests; it stays listed"),
		key("v", "view: grouping · favourites first · columns"),
		key("f  F", "show only some tags · every tag again"),
		key("PATH", "clone directory; dim when not cloned yet"),
		key("CI", "newest pipeline of the clone's branch"),
		key("RMT", glyphCheck+" up to date · "+glyphBehind+" behind · "+glyphAhead+" unpushed; r fetches"),
		key("MR", "merge requests with a worktree on disk"),
		key(glyphHidden+" in MR", "its merge requests are hidden (H, x in MRs)"),
		key("WT", "worktrees of branches, not of merge requests"),
		key("SIZE", "on disk: the clone and all its worktrees"),
		blank(),

		section("Worktrees", helpWorktrees),
		key("p", "pull; unpushed: rebase onto the branch it came from"),
		key("Alt-P", "the same for every worktree"),
		key("Ctrl-R", "rebase onto its base; pushed: force push after"),
		key("c", "commit everything; one message, or one per repo"),
		key("J", "pipeline of the branch; a group asks which repo"),
		key("a  x", "a group: add a repository · take one out"),
		key("P", "push commits; -u when new; forced only after Ctrl-R"),
		key("n", "open a merge request; a group, one in each, linked"),
		key("CI", "the branch's own pipeline; a group has none"),
		key("m", "go to the branch's merge request"),
		key("v", "view: which columns are shown"),
		key("o", "order: activity, name, edits, or frecency").in(helpWorktreeList),
		key("RMT", "no upstream · in sync · "+glyphAhead+" unpushed · "+glyphBehind+" behind · gone"),
		key("MR", "the open merge request of the branch"),
		key(glyphWorktree, "a worktree of one repository"),
		key(glyphGroup, "grouped: several repositories in one folder"),
		key("REPOS", "how many repositories the worktree holds"),
		key(glyphEdits, "edits: files not committed; Repositories too"),
		key("COM", "Incomm comments; amber while some wait for P"),
		key("SIZE", "what it takes on disk; R measures again"),
		key("CREATED", "when the worktree was made"),
		blank(),

		section("Activity", helpActivity),
		key("Enter", "a pipeline's jobs · go to the agent"),
		key("h j k l", "past a panel's edge: to the panel beside it"),
		key("Tab", "next panel: detail, log, editors, watching"),
		key("a  d  L  e", "the list · the detail · the log · the editors"),
		key("z", "the log in front, every event in full"),
		key("W  E", "everything watched · every editor open"),
		key("x", "stop watching · close an agent in herdr"),
		key("w  m", "the pipeline in the browser · go to its row"),
		key("r  R", "read this one, or everything, now"),
		key("Ctrl-O", "open an agent's directory in the editor"),
		key(glyphNew, "new since your last visit; counted on the tab"),
		blank(),

		section("A worktree's view (Enter)", helpWorktreeDetail),
		key("j  k", "from block to block: the group, each repository"),
		key("p  P  C", "pull · push · commit what is lit"),
		key("w  c  Ctrl-L", "a repository's web page · comments · commits"),
		key("b", "the repository's branches: see, delete"),
		key("n  D", "merge request(s) · the changes in Hunk"),
		key("a  x", "add a repository · take the lit one out"),
		blank(),

		section("Merge requests", helpMergeRequests),
		key("Ctrl-R", "review: the whole change as unstaged edits"),
		key("Alt-R", "the same, in an editor you choose"),
		key("Ctrl-R", "in the log: review from that commit to the head"),
		key(glyphDot, "in the log: a commit new since your last review"),
		key("C", "make the review worktree without opening it"),
		key("p", "pull the branch worktree; rebases your work"),
		key("c", "read the conversation, write a comment"),
		key("A", "approve; asks first"),
		key("M", "merge: now, or when the pipeline succeeds"),
		key("Ctrl-D", "mark as a draft · mark ready"),
		key("a", "assignee: space assigns, x unassigns, Esc saves"),
		key("s", "reviewer: space asks, x withdraws, Esc saves"),
		key("t", "labels: space puts on or takes off, Esc saves"),
		key("P", "publish Incomm comments and resolved threads"),
		key("f  F", "limit to one repository · clear the limit"),
		key("COM", "2/4/9: threads open · resolved · comments"),
		key("PUB", "Incomm comments not yet published"),
		key("NEW", glyphDot+"2: commits pushed since your last review"),
		key("APPR", "1/2 of those asked, or 1 · "+glyphApproved+" one is yours"),
		key("CI", glyphCIDone+" passed/failed · "+ciFrame()+" running · "+glyphCIIdle+" waiting, skipped"),
		key("V", "mark as reviewed: NEW counts from the head now"),
		key("J", "pipeline jobs: Enter log · R run, retry · w web"),
		key("P", "in the jobs: earlier pipelines, ↺ attempts"),
		key("H", "hide the author's merge requests (bots)"),
		key("x", "hide the repository's MRs; refreshes skip them"),
		key("space", "mark; x H r V y Ctrl-F then act on all"),
		key("v", "view: mine · to review · drafts · authors · columns"),
		blank(),

		section("Comments  (c)", 0),
		key("i", "write one"),
		key("Ctrl-S", "send it"),
		key("A", "approve"),
		key("r", "reload"),
		blank(),

		section("Settings  (6)", helpSettingsList),
		key("j  k", "move between the sections"),
		key("Enter  l", "edit the section"),
		blank(),
		section("Settings · servers", helpServers),
		key("a e t v d", "add · edit · token · verify · remove"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · groups", helpGroups),
		key("space", "off → this group → with subgroups (GitHub: on · off)"),
		key("d", "clone directory of a group or a server"),
		key("t", "tags of a server or group; all below inherit"),
		key("r  p  m", "reload groups · refresh projects · merge requests"),
		key("h  l", "fold · unfold; h on what is folded: back"),
		key("Esc", "back to the sections"),
		blank(),
		section("Settings · tags", helpTags),
		key("a e d", "add · edit · remove a tag"),
		key("s", "pill ends: rounded (Nerd Font) · circles · square"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · theme", helpTheme),
		key("Enter", "draw unagit in the theme under the cursor"),
		key("f", "fork into a file of yours; each save shows"),
		key("n", "Nerd Font icons: from the terminal · on · off"),
		key("b", "background: the theme's · the terminal's own"),
		key("r", "read <config>/themes/*.json again"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · security", helpSecurity),
		key("c", "change the passphrase"),
		key("k", "remember it in the macOS Keychain, or forget it"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · general", helpGeneral),
		key("s  r", "save · revert; Esc first while typing"),
		key("Tab / Shift-Tab", "next / previous field"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · integrations", helpIntegrations),
		key("e", "toggle the selected integration"),
		key("f", "on an editor: the favourite, or none"),
		key("o", "on Custom: its command, arguments, kind"),
		key("c", "check installation"),
		key("Tab / j k", "move between integrations"),
		key("Esc  h", "back to the sections"),
		blank(),

		section("Command line · unagit ...", 0),
		key("cd", "a shell where unagit has an editor open"),
		key("cd --print", "just that path, for cd \"$(...)\""),
		key("sessions", "what is open"),
		key("go", "a shell in anything on disk; --print too"),
		key("review URL", "open a merge request's link for review"),
		key("open URL", "open that link's branch worktree"),
		blank(),

		section("On disk", helpLists),
		key(glyphDiskNone+" "+glyphDiskBranch+" "+glyphDiskReview+" "+glyphDiskBoth, "nothing · branch worktree · review worktree · both"),
		key(glyphHidden, "hidden from the lists"),
		key(glyphExternal+" Chezmoi", "chezmoi's checkout; worktrees still under the root"),
		key("clone", "<root>/<group>/<repo>"),
		key("branch", ".unagit/<repo>/<iid>-<branch>"),
		key("review", ".unagit/<repo>/review-<iid>-<branch>"),
		key("plain", ".unagit/<repo>/wt-<branch>"),
		key("grouped", "<root>/.unagit/groups/<folder>/<repo>"),
		blank(),
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

	a.pages.AddPage(pageHelp, modalPct(table, 82, 90), true, true)
	a.tv.SetFocus(table)
}
