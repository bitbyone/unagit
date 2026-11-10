package ui

import (
	"math/bits"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/keymap"
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
	helpSandboxes
	helpStandalone
	helpKeys
	helpRepositories  = helpRepoList | helpRepoDetail
	helpMergeRequests = helpMRList | helpMRDetail
	helpWorktrees     = helpWorktreeList | helpWorktreeDetail
	helpLists         = helpRepositories | helpMergeRequests
	helpDetails       = helpRepoDetail | helpMRDetail | helpWorktreeDetail
	helpNavigation    = helpLists | helpWorktrees | helpActivity | helpSandboxes | helpSettingsList | helpServers | helpGroups | helpIntegrations | helpSecurity | helpTags | helpTheme | helpKeys
)

func (l helpLine) in(scope helpContext) helpLine { l.scope = scope; return l }

// at says where in keybinds.toml a row's keys are bound, when it is not
// where its heading's are: "in the log: ..." is the log's.
func (l helpLine) at(bind string) helpLine { l.bind = bind; return l }

const (
	helpHere   = ""
	helpNoKeys = "-"
)

func (a *App) helpContext() (helpContext, string) {
	if a.solo != nil {
		for _, t := range soloTabs {
			if t.page == a.currentTab() {
				return helpStandalone, "standalone · " + t.title
			}
		}
		return helpStandalone, "standalone"
	}
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
	case pageSandboxes:
		return helpSandboxes, "Sandboxes"
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
		case sectionKeys:
			return helpKeys, "Keys"
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

// helpBound is the help with the keys the user gave the actions in
// keybinds.toml, so it never names a key that does something else now. A
// row whose every key was taken away goes.
func (a *App) helpBound(rows []helpLine) []helpLine {
	if a.keys == nil {
		return rows
	}
	here := a.helpScope()
	var out []helpLine
	for _, line := range rows {
		bind := line.bind
		if bind == helpHere {
			bind = here
		}
		if line.keys != "" && bind != helpNoKeys {
			keys, kept := a.helpKeys(bind, line.keys)
			if !kept {
				continue
			}
			line.keys = keys
		}
		out = append(out, line)
	}
	return out
}

// helpScope is the section of keybinds.toml of the screen the help was
// opened over; where nothing is bound, global's keys are all it names.
func (a *App) helpScope() string {
	if a.solo != nil {
		switch a.currentTab() {
		case pageSoloChanges:
			return scopeChanges
		case pageSoloLog:
			return scopeLog
		case pageSoloBranches:
			return scopeBranches
		}
		return keymap.Global
	}
	switch a.currentTab() {
	case pageProjects:
		return scopeRepositories
	case pageMRs:
		return scopeMergeRequests
	case pageWorktrees:
		if a.wtView != nil {
			return scopeWorktreeView
		}
		return scopeWorktrees
	case pageActivity:
		return scopeActivity
	case pageSandboxes:
		return scopeSandboxes
	}
	return keymap.Global
}

// soloHelpRows keeps of the help what standalone has: the rest is about
// lists and servers it does not show.
func soloHelpRows(rows []helpLine) []helpLine {
	var out []helpLine
	keep := false
	for _, line := range rows {
		if line.section != "" {
			keep = line.scope&helpStandalone != 0
		}
		if keep {
			out = append(out, line)
		}
	}
	return out
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
	// bind is the section of keybinds.toml the row's keys are bound in:
	// a row takes its heading's, and a heading without one is about the
	// screen the help was opened over (helpHere). helpNoKeys is a part
	// whose keys nobody binds - Settings, a dialog without a section.
	bind string
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
		key("1 2 3 4 5", "Repos, MRs, Worktrees, Activity, Sandboxes"),
		key("S", "Settings, at the right end of the tabs"),
		key(".  Alt-Enter", "every action on the selection, with its key"),
		key(":", "every action of the screen, with its key"),
		key("+", "find any repository of yours and list it"),
		key("j  k", "move up and down"),
		key("g  G", "first · last").in(helpLists | helpWorktreeList),
		key("/", "filter: fuzzy, spaces separate terms").in(helpLists | helpWorktreeList),
		key("Esc", "clear the filter or selection, close the detail").in(helpRepoList | helpMRList | helpWorktreeList),
		key("Enter", "load it and jump in; it then follows the cursor").in(helpRepoList | helpMRList | helpWorktreeList),
		key("?", "this help").in(helpNavigation | helpGeneral),
		key("q", "quit"),
		blank(),

		section("The mouse  (Settings › General)", helpNavigation).at(helpNoKeys),
		key("click", "the cursor there; on the bar, that tab"),
		key("double click", "Enter"),
		key("right click", "every action on what it is on"),
		key("Ctrl-click", "mark or unmark the row; Alt-click too"),
		key("Shift-click", "mark from the cursor to the row"),
		key("drag", "mark the rows it passes over"),
		key("click beside", "close the dialog, as Esc does"),
		key("click "+glyphClose, "close the detail column"),
		blank(),

		section("The detail column", helpDetails).at(helpNoKeys),
		key("j k g G", "scroll"),
		key("Ctrl-F Ctrl-B", "page"),
		key("h  ←  Esc", "back to the list"),
		blank(),

		section("Standalone · unagit -s", helpStandalone),
		key("1 2 3 4", "Changes, Log, Branches, Shelf"),
		key(".  Alt-Enter", "every action on the selection, with its key"),
		key(":", "every action of the tab and the repository"),
		key("p  P", "pull · push"),
		key("C", "commit the changes to versioned files"),
		key("f", "fetch origin and read everything again"),
		key("R", "read the repository again, on every tab"),
		key("S", "settings: auto refresh"),
		key("o  O", "open in the editor · choose the editor and where"),
		key("D  B", "in Hunk: uncommitted · since the base"),
		key("Ctrl-R", "rebase onto the base"),
		key("q", "quit"),
		blank(),

		section("Dialogs", helpStandalone).at(keymap.Global),
		key("c  Esc", "cancel · go back"),
		key("letter", "the button whose letter is lit"),
		key("j k  Tab", "a form: from field to field, over the buttons"),
		key("i  Enter", "type into the field; Esc stops typing"),
		key(".  Alt-Enter", "a list: what can be done with the item"),
		key(":", "what can be done from anywhere: theme · editors"),
		key("Ctrl-D Ctrl-U", "a log or a commit: half a page down · up"),
		key("Ctrl-F Ctrl-B", "a log or a commit: a page down · up"),
		blank(),

		section("Every list", helpLists|helpWorktreeList),
		key("o", "open as it is on disk; clones only what is missing"),
		key("O", "open with what, and where: editor or agent"),
		key("d", "delete from disk; warns about unsaved work"),
		key("w", "open in the browser").in(helpLists),
		key("L", "commit log; Enter shows a commit's detail"),
		key("d  D", "in the log: the commit · from it to now, in Hunk").at(scopeLog),
		key("C  B  e", "in the log: checkout · back to branch · edit message").at(scopeLog),
		key("u", "in the log: undo the newest commit").at(scopeLog),
		key("P", "in the log: push up to the commit").at(scopeLog),
		key("space  s", "in the log: mark commits · squash the marked").at(scopeLog),
		key("H", "in the log: rewrite history, to undo a change").at(scopeLog),
		key("R", "in the log: the reflog, to go back anywhere").at(scopeLog),
		key("n  Ctrl-W", "in the log: a branch · a worktree at the commit").at(scopeLog),
		key("t  T", "in the log: a tag on the commit · its tags").at(scopeLog),
		key("w  y", "in the log: browser · copy id, link, reference").at(scopeLog),
		key("J", "in the log: the commit's pipelines").at(scopeLog),
		key("D  B", "Hunk: not committed · since the base").in(helpRepoList | helpWorktreeList),
		key("s", "changes: the files not committed, their diffs").in(helpRepoList | helpWorktreeList),
		key("space  x  a", "in changes: mark · tick for the commit · all").at(scopeChanges),
		key("c  R  d", "in changes: commit ticked · rollback · delete").at(scopeChanges),
		key("s", "in changes: shelve the marked files, or all").at(scopeChanges),
		key("A  I", "in changes: add to git · add to .gitignore").at(scopeChanges),
		key("/  y  D", "in changes: filter · copy paths or a patch · Hunk").at(scopeChanges),
		key("o", "in changes: open the repo, the file in front").at(scopeChanges),
		key("y", "copy the link, reference, branch or directory"),
		key("yy", "copy the link"),
		key("r", "refresh the row: fetch it, ask the server about it"),
		key("R", "refresh the whole list from the servers"),
		key("E", "running editors: Enter attach · a in… · x close"),
		key("Ctrl-Z", "in Neovim 0.12+: put aside and return to unagit").at(helpNoKeys),
		key(editorGlyph(editors.Nvim), "this directory is open in Neovim"),
		key(glyphAgent, "an agent works here; its colour, what it does"),
		key(glyphWatched, "its pipelines are watched; see Activity (4)"),
		blank(),

		section("Filters · shared by both lists", helpLists),
		key("x", "hide or show the repository and its merge requests").in(helpRepoList),
		key("X", "manage the hidden repositories").in(helpRepoList),
		key("z", "group: repositories by group, MRs by repository"),
		key("*", glyphFavourite+" star; starred lead a flat list unless v says not"),
		blank(),

		section("Repositories", helpRepositories).at(scopeRepositories),
		key("C", "clone without opening the editor"),
		key("e", "set the clone directory of an uncloned repository"),
		key("b", "branches: where each is; Enter switches the clone"),
		key("n", "in b: a new branch from the one under the cursor").at(scopeBranches),
		key("m", "in b: a merge request from the branch").at(scopeBranches),
		key("d  D", "in b: delete in the clone · everywhere").at(scopeBranches),
		key("d  D", "in b: a branch out in a worktree goes with it").at(scopeBranches),
		key("Ctrl-W", "in b: a worktree for the branch").at(scopeBranches),
		key("p", "pull; rebases your work; refuses on a conflict"),
		key("U", "the same for every clone origin has moved past"),
		key("c", "commit versioned files; push with it if you like"),
		key("P", "push what origin lacks; asks, offers force push"),
		key("n", "merge request from the clone's branch"),
		key("Ctrl-W", "a worktree for a branch; n for a new one"),
		key("space", "mark; Ctrl-W x H r p C y t * act on all"),
		key("m", "show only this repository's merge requests"),
		key("t", "tag the repository"),
		key("J", "pipeline of the clone's branch: log · run"),
		key("H", "hide its merge requests; it stays listed"),
		key("v", "view: order · server · visibility · columns"),
		key("f  F", "show only some tags · every tag again"),
		key("PATH", "clone directory; dim when not cloned yet"),
		key(glyphPrivate+" before forge", "private: visible to project or group members"),
		key("CI", "newest pipeline of the clone's branch"),
		key("RMT", glyphCheck+" up to date · "+glyphBehind+" behind · "+glyphAhead+" unpushed; r fetches"),
		key("MR", "merge requests with a worktree on disk"),
		key(glyphHidden+" in MR", "its merge requests are hidden (H, x in MRs)"),
		key("WT", "worktrees of branches, not of merge requests"),
		key("SIZE", "on disk: the clone and all its worktrees"),
		blank(),

		section("Worktrees", helpWorktrees).at(scopeWorktrees),
		key("p", "pull; unpushed: rebase onto the branch it came from"),
		key("U", "the same for every worktree"),
		key("Ctrl-R", "rebase onto its base; pushed: force push after"),
		key("c", "commit versioned files; one message, or one per repo"),
		key("J", "pipeline of the branch; a group asks which repo"),
		key("a  x", "a group: add a repository · take one out"),
		key("P", "push commits; asks, offers force push"),
		key("n", "open a merge request; a group, one in each, linked"),
		key("CI", "the branch's own pipeline; a group has none"),
		key("m", "go to the branch's merge request"),
		key("v", "view: order · server · columns"),
		key("RMT", "no upstream · in sync · "+glyphAhead+" unpushed · "+glyphBehind+" behind · gone"),
		key("MR", "the open merge request of the branch"),
		key(glyphWorktree, "a worktree of one repository"),
		key(glyphGroup, "grouped: several repositories in one folder"),
		key(glyphReviewWorktree, "review of a closed merge request, kept: d deletes"),
		key("REPOS", "how many repositories the worktree holds"),
		key(glyphEdits, "edits: versioned/unversioned; Repositories too"),
		key("COM", "Incomm comments; amber while some wait for P"),
		key("SIZE", "what it takes on disk; R measures again"),
		key("CREATED", "when the worktree was made"),
		blank(),

		section("Activity", helpActivity).at(scopeActivity),
		key("Enter", "a pipeline's jobs · go to the agent"),
		key("h j k l", "past a panel's edge: to the panel beside it"),
		key("Tab", "next panel: detail, log, editors, watching"),
		key("a  d  L  e", "the list · the detail · the log · the editors"),
		key("z", "the log in front, every event in full"),
		key("H", "the log: local history only, or everything"),
		key("W  E", "everything watched · every editor open"),
		key("x", "stop watching · close an agent in herdr"),
		key("w  m", "the pipeline in the browser · go to its row"),
		key("r  R", "read this one, or everything, now"),
		key("o", "open an agent's directory in the editor"),
		key(glyphNew, "new since your last visit; counted on the tab"),
		blank(),

		section("Sandboxes", helpSandboxes).at(scopeSandboxes),
		key("Tab", "next panel: environment, tasks, detail, …"),
		key("h j k l", "past a panel's edge: to the panel beside it"),
		key("B  t  N  L  E", "cards · tasks · envs · log · environment").at(helpNoKeys),
		key("Enter", "into the detail of what is chosen"),
		key("f", "a task: go to its agent where it is at work"),
		key("r", "a task: resume its agent; one at work is gone to"),
		key("a", "a task: resume with another of the sandbox's agents"),
		key("s", "a shell in the task or the sandbox"),
		key("n", "new task: a clone mounted, a worktree cloned"),
		key("i  +  o", "a task: instructions · add repo · open checkout"),
		key("e  y  w", "a task: rename · copy resume · repository page"),
		key("p  f  M", "a sandbox: ports · network · resources"),
		key("x  C  d", "a sandbox: stop · recreate · remove"),
		key("c  V  K  p", "an env: create sandbox · values · tokens · ports"),
		key("i  o", "an env: repository index · open its files"),
		key("u", "make it unibox's context"),
		key("U  D  T", "Unibox's settings · doctor · tokens in unagit"),
		key("Y", "your whole Unibox configuration"),
		key("I", "set unibox up, step by step"),
		key("c", "environment: clear the context"),
		key("y", "the log: copy the event's command"),
		key("R", "read everything from Unibox again"),
		key("STATE", "an env's missing values · running · an agent"),
		key("env changed", "an env: its sandbox older than it - C recreates"),
		key(glyphTabProblem+"2", "by the tab: how many things the sandboxes lack"),
		blank(),

		section("A worktree's view (Enter)", helpWorktreeDetail).at(scopeWorktreeView),
		key("j  k", "from block to block: the group, each repository"),
		key("p  P  C", "pull · push · commit what is lit"),
		key("w  c  L", "a repository's web page · comments · commits"),
		key("b", "the repository's branches: see, delete"),
		key("n  D", "merge request(s) · the changes in Hunk"),
		key("a  x", "add a repository · take the lit one out"),
		blank(),

		section("Merge requests", helpMergeRequests).at(scopeMergeRequests),
		key("Ctrl-R", "review: the whole change as unstaged edits"),
		key("Ctrl-R", "in the log: review from that commit to the head").at(scopeLog),
		key(glyphDot, "in the log: a commit new since your last review").at(scopeLog),
		key("C", "make the review worktree without opening it"),
		key("p", "pull the branch worktree; rebases your work"),
		key("c", "read the conversation, write a comment"),
		key("A", "approve; asks first"),
		key("M", "merge: now, or when the pipeline succeeds"),
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
		key("P", "in the jobs: earlier pipelines, ↺ attempts").at(helpNoKeys),
		key("H", "hide the author's merge requests (bots)"),
		key("x", "hide the repository's MRs; refreshes skip them"),
		key("space", "mark; x H r V y * then act on all"),
		key("v", "server · mine · review · drafts · age · columns"),
		blank(),

		section("Comments  (c)", 0).at(helpNoKeys),
		key("i", "write one"),
		key("Ctrl-S", "send it"),
		key("A", "approve"),
		key("r", "reload"),
		blank(),

		section("Settings  (S)", helpSettingsList).at(helpNoKeys),
		key("j  k", "move between the sections"),
		key("Enter  l", "edit the section"),
		blank(),
		section("Settings · servers", helpServers).at(helpNoKeys),
		key("a e t v d", "add · edit · token · verify · remove"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · groups", helpGroups).at(helpNoKeys),
		key("space", "off → this group → with subgroups (GitHub: on · off)"),
		key("a", "a repository on its own; space on it takes it back"),
		key("d", "clone directory of a group or a server"),
		key("t", "tags of a server or group; all below inherit"),
		key("i", "git name and email; all below inherit"),
		key("r  p  m", "reload groups · refresh projects · merge requests"),
		key("h  l", "fold · unfold; h on what is folded: back"),
		key("Esc", "back to the sections"),
		blank(),
		section("Settings · tags", helpTags).at(helpNoKeys),
		key("a e d", "add · edit · remove a tag"),
		key("s", "pill ends: rounded (Nerd Font) · circles · square"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · theme", helpTheme).at(helpNoKeys),
		key("Enter", "draw unagit in the theme under the cursor"),
		key("f", "fork into a file of yours; each save shows"),
		key("r", "read <config>/themes/*.json again"),
		key("Tab  l", "over to Display, Tab back to the themes"),
		key("h l", "Display: the value before · after"),
		key("n", "Nerd Font icons: from the terminal · on · off"),
		key("b", "background: the theme's · the terminal's own"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · security", helpSecurity).at(helpNoKeys),
		key("c", "change the passphrase"),
		key("k", "remember it in the macOS Keychain, or forget it"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · general", helpGeneral).at(helpNoKeys),
		key("s  r", "save · revert; Esc first while typing"),
		key("Tab / Shift-Tab", "next / previous field"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · keys", helpKeys).at(helpNoKeys),
		key("w", "keybinds.toml with every action, commented out"),
		key("u", "bring it up to this release; your lines stay"),
		key("e  r", "edit it · read it again, no restart needed"),
		key("Esc  h", "back to the sections"),
		blank(),
		section("Settings · integrations", helpIntegrations).at(helpNoKeys),
		key("e", "toggle the selected integration"),
		key("d", "on an editor: set it as default, or none"),
		key("o", "on Custom: its command, arguments, kind"),
		key("c", "check installation"),
		key("Tab / j k", "move between integrations"),
		key("Esc  h", "back to the sections"),
		blank(),

		section("Command line · unagit ...", 0).at(helpNoKeys),
		key("-s [DIR]", "standalone: one repository, git alone"),
		key("cd", "a shell where unagit has an editor open"),
		key("cd --print", "just that path, for cd \"$(...)\""),
		key("sessions", "what is open"),
		key("go", "a shell in anything on disk; --print too"),
		key("review URL", "open a merge request's link for review"),
		key("open URL", "open that link's branch worktree"),
		blank(),

		section("On disk", helpLists).at(helpNoKeys),
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
	bind := helpHere
	for i := range rows {
		if rows[i].section != "" {
			scope, bind = rows[i].scope, rows[i].bind
		}
		if rows[i].scope == 0 {
			rows[i].scope = scope
		}
		if rows[i].bind == helpHere {
			rows[i].bind = bind
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

	rows := a.helpBound(contextHelpRows(context))
	if a.solo != nil {
		rows = soloHelpRows(rows)
	}
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
