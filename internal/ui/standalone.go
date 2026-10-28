package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/session"
)

// unagit standalone (-s) works on one repository and nothing else: no
// server, no passphrase, no lists. The repository's changes, its log, its
// branches and its shelf are tabs, and every git action unagit has for a
// worktree works on it. It runs on any repository, one unagit never cloned
// too, and leaves unagit's files as they were: the configuration is read
// for the theme and the editors, nothing is written back.
//
// The tabs are the dialogs the full unagit opens over its lists, made
// screens (embedding): the same keys, the same actions, the same look. The
// repository is the one row of the worktrees, under no server, so whatever
// takes a worktree takes it.

const (
	pageSoloChanges  = "solo-changes"
	pageSoloLog      = "solo-log"
	pageSoloBranches = "solo-branches"
	pageSoloShelf    = "solo-shelf"
)

// soloTabs are standalone's screens, numbered as the full unagit's are.
var soloTabs = []tab{
	{pageSoloChanges, '1', "Changes", "Changes"},
	{pageSoloLog, '2', "Log", "Log"},
	{pageSoloBranches, '3', "Branches", "Branches"},
	{pageSoloShelf, '4', "Shelf", "Shelf"},
}

// soloRepo is the repository standalone works on.
type soloRepo struct {
	dir    string // the top of its working tree
	name   string // its folder's name, which is what it is called here
	branch string // what is checked out, "(detached)" without a branch
	header *tview.TextView
	// stale are the tabs read before something changed the repository:
	// each is read again when it comes to the front, so a tab the user is
	// on keeps its cursor.
	stale map[string]bool
}

// NewStandalone builds the application for the repository dir is in,
// unlocked and without a vault: nothing it does needs a token. The
// configuration should be only read (config.LoadReadOnly).
func NewStandalone(cfg *config.Config, dir string) (*App, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	git := gitx.New("", nil)
	top, err := git.Run(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not in a git repository - give unagit -s the path of one", abs)
	}
	top = strings.TrimSpace(top)
	a := NewLocked(cfg)
	a.solo = &soloRepo{dir: top, name: filepath.Base(top), branch: soloBranch(git.CurrentBranch(top)), stale: map[string]bool{}}
	return a, nil
}

// soloBranch is how a worktree row names what is checked out.
func soloBranch(branch string) string {
	if branch == "" {
		return "(detached)"
	}
	return branch
}

// soloKey is the repository as the maps of repositories know it: under no
// server, by its folder's name.
func (a *App) soloKey() projectKey { return projectKey{"", a.solo.name} }

// isSolo tells whether a repository is standalone's own.
func (a *App) isSolo(instanceID, projectPath string) bool {
	return a.solo != nil && projectKey{instanceID, projectPath} == a.soloKey()
}

// soloRow is the repository as a worktree.
func (a *App) soloRow() worktreeRow {
	s := a.solo
	return worktreeRow{Path: s.name, Branch: s.branch, Dir: s.dir}
}

// buildStandalone lays standalone out: the repository on the top line,
// the tabs under it, the screen, and the status line at the bottom. The
// main screens' widgets are made all the same, for whatever reaches for
// them, but never shown.
func (a *App) buildStandalone() tview.Primitive {
	a.buildInterface()
	for _, t := range tabs {
		a.pages.RemovePage(t.page)
	}
	s := a.solo
	s.header = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	s.header.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		s.header.SetText(a.soloHeader())
		return x, y, w, h
	})
	a.helpHint.SetText(litHint("? help"))
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.header, 1, 0, false).
		AddItem(a.tabs, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.settingsLine, 1, 0, false)
	a.settingsLine.holdIn(layout)
	a.tab = pageSoloChanges
	a.drawTabs()
	return layout
}

// soloHeader is the top line: the repository, where it is, its branch and
// where that stands against origin, and what is not committed.
func (a *App) soloHeader() string {
	s := a.solo
	r := a.soloRow()
	parts := []string{
		tag(colTitle) + "[::b]" + esc(s.name) + "[::-]" + tagEnd,
		tag(colBranch) + esc(s.branch) + tagEnd,
	}
	plain, colour := a.worktreeRemoteWords(r)
	parts = append(parts, tag(colour)+esc(plain)+tagEnd)
	if plain, edits := editsMarkup(a.worktreeEdits(r)); plain != "" {
		parts = append(parts, edits+tag(colWarn)+" uncommitted"+tagEnd)
	}
	// Where it is comes last, the first to go when the line is too short.
	parts = append(parts, tag(colMuted)+esc(tildePath(s.dir))+tagEnd)
	return " " + strings.Join(parts, tag(colDim)+" · "+tagEnd)
}

// startStandalone reads the repository and opens the first tab.
func (a *App) startStandalone() {
	if a.themeProblem != "" {
		defer a.flash(a.themeProblem)
		a.themeProblem = ""
	}
	s := a.solo
	// Held in memory only: the state is never saved here.
	a.cfg.State.SetLocalRepository("", s.name, s.dir)
	a.readSolo(s.branch)
	for _, t := range soloTabs {
		a.openSoloTab(t.page)
	}
	a.switchTab(pageSoloChanges)
}

// refreshSolo is refreshDisk for standalone: what is checked out may have
// changed, so it is asked first, off the event loop.
func (a *App) refreshSolo() {
	dir := a.solo.dir
	go func() {
		branch := soloBranch(gitx.New("", nil).CurrentBranch(dir))
		a.tv.QueueUpdateDraw(func() { a.readSolo(branch) })
	}()
}

// readSolo takes in what is checked out and reads the rest again: where the
// branch stands, the changes, and every tab but the one in front once it
// comes to the front.
func (a *App) readSolo(branch string) {
	s := a.solo
	s.branch = branch
	a.disk = map[projectKey]diskInfo{a.soloKey(): {Cloned: true, Branch: branch}}
	a.worktrees = []worktreeRow{a.soloRow()}
	a.loadWorktreeRemotes()
	a.reloadChanges()
	for _, t := range soloTabs {
		if t.page != a.currentTab() && t.page != pageSoloChanges {
			s.stale[t.page] = true
		}
	}
}

// switchSoloTab brings a tab to the front, reading it again first when the
// repository changed since it was read.
func (a *App) switchSoloTab(page string) {
	a.tab = page
	a.pages.SwitchToPage(page)
	a.drawTabs()
	a.clearSaid()
	if a.solo.stale[page] {
		delete(a.solo.stale, page)
		a.openSoloTab(page)
	}
	if page == pageSoloChanges {
		a.reloadChanges()
	}
	a.restoreFocus()
}

// openSoloTab reads a tab and puts it on its page.
func (a *App) openSoloTab(page string) {
	r := a.soloRow()
	pr := a.worktreeProject(r)
	embed := a.soloEmbedding(page)
	switch page {
	case pageSoloChanges:
		a.showChanges(changesPlace{path: r.Path, dir: r.Dir, title: r.Path + " · " + r.Branch, embed: embed,
			what: session.Record{Project: r.Path, Title: r.Branch, Mode: session.ModeRepository}})
	case pageSoloLog:
		a.soloLog("", "")
	case pageSoloBranches:
		a.showBranchManager(branchScope{project: pr, checkout: true, focus: r.Branch, embed: embed})
	case pageSoloShelf:
		branch := r.Branch
		if branch == "(detached)" {
			branch = ""
		}
		a.showShelf(shelfScope{project: pr, dir: r.Dir, branch: branch, embed: embed})
	}
}

// soloLog reads the log of what is checked out now, which an action of the
// log itself - a checkout - may just have changed.
func (a *App) soloLog(focus, done string) {
	dir := a.solo.dir
	go func() {
		branch := soloBranch(gitx.New("", nil).CurrentBranch(dir))
		a.tv.QueueUpdateDraw(func() {
			r := a.soloRow()
			r.Branch = branch
			a.localLog(logPlace{title: "Commit Log · " + branch, project: a.worktreeProject(r),
				dir: r.Dir, branch: branch, checkout: true, branches: true,
				embed: a.soloEmbedding(pageSoloLog), reload: a.soloLog}, focus, done)
		})
	}()
}

// soloEmbedding makes a dialog the screen of a tab.
func (a *App) soloEmbedding(page string) *embedding {
	return &embedding{
		page:   page,
		screen: func() (string, []uiAction) { return a.solo.name, a.soloScreenActions() },
		shown:  func() bool { return a.currentTab() == page },
	}
}

// soloLeavesOut are the worktree's actions standalone has no use for: what
// needs a server, and what a tab of its own already is.
var soloLeavesOut = map[string]bool{
	"Changes…": true, "Branches…": true, "Shelf…": true,
	"New Merge Request…": true, "New Linked Merge Requests…": true, "Go to Merge Request": true,
	"Show Pipeline…": true, "Add Repository…": true, "Watch Branch…": true,
}

// soloScreenActions are what can be done from every tab: what the full
// unagit offers for a worktree, the tabs and leaving.
func (a *App) soloScreenActions() []uiAction {
	r := a.soloRow()
	open := func(ask bool) {
		a.withEditor(ask, func(ed *editors.Editor) { a.openWorktree(a.soloRow(), ed) })
	}
	var acts []uiAction
	for _, act := range a.worktreeActions(r, open, "C") {
		if soloLeavesOut[act.name] || strings.HasPrefix(act.name, "Watch ") || strings.HasPrefix(act.name, "Stop Watching ") {
			continue
		}
		acts = append(acts, act)
	}
	for i, t := range soloTabs {
		acts = append(acts, uiAction{name: "Go to " + t.title, about: "Bring the " + t.title + " tab to the front.",
			keys: string(t.key), rank: 80 + i, run: func() { a.switchTab(t.page) }})
	}
	return append(acts,
		uiAction{name: "Fetch", about: "Fetch origin and read the repository again: where the branch stands, the changes, the tabs.", keys: "f", rank: 8,
			run: a.fetchSolo},
		uiAction{name: "Refresh All", about: "Read the repository again without asking origin: the branch, the changes, every tab.", keys: "R", rank: 9,
			run: func() {
				a.refreshSolo()
				a.solo.stale[a.currentTab()] = true
				a.switchTab(a.currentTab())
			}},
		uiAction{name: "Help", about: "Every key of standalone, those of the tab in front lit.", keys: "?", rank: 950, run: a.showHelp},
		uiAction{name: "Quit", about: "Leave unagit.", keys: "q", rank: 990, run: a.tv.Stop},
	)
}

// fetchSolo fetches origin, then reads everything again.
func (a *App) fetchSolo() {
	r := a.soloRow()
	a.runTask("Fetching "+r.Path, func(log func(string)) (string, error) {
		return "", a.newManager(r.Instance, r.Path, log).Git().Fetch(r.Dir)
	})
}

// keysFreeOf is acts without the keys that taken already have: a screen's
// action under a dialog's that answers the same key is still listed, keyless,
// so that the key never does one thing while the picker says another.
func keysFreeOf(acts, taken []uiAction) []uiAction {
	used := map[string]bool{}
	for _, t := range taken {
		if t.keys != "" {
			used[t.keys] = true
		}
	}
	out := make([]uiAction, len(acts))
	for i, act := range acts {
		if used[act.keys] {
			act.keys = ""
		}
		out[i] = act
	}
	return out
}
