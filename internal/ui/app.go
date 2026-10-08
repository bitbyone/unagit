// Package ui implements the unagit terminal interface.
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/chezmoi"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/github"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/incomm"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/mux"
	"github.com/tobola/unagit/internal/secret"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
	"github.com/tobola/unagit/internal/zoxide"
)

// page names
const (
	pageProjects  = "projects"
	pageMRs       = "mrs"
	pageWorktrees = "worktrees"
	pageAgents    = "agents"
	pageSettings  = "settings"
	pageHelp      = "help"
	pageTask      = "task"
	pageConfirm   = "confirm"
	pagePicker    = "picker"
	pageUnlock    = "unlock"
	pageForm      = "form"
	pageComments  = "comments"
	pageToggles   = "toggles"
	pageMessage   = "message"
	pageCommit    = "commit"
	// pageActions is an action picker over a dialog, on a page of its own
	// so the dialog stays under it.
	pageActions = "actions"
)

// mrDisk records which worktrees a merge request has on disk.
type mrDisk struct {
	Branch bool // a real branch, can be committed and pushed
	Review bool // the whole change pending on the merge base
	// BranchDir and ReviewDir are where those worktrees are.
	BranchDir, ReviewDir string
	// Pending counts the Incomm comments and replies in both worktrees that are
	// meant for the merge request and have not been published yet.
	Pending int
}

// diskInfo is the cached on-disk state of one project.
type diskInfo struct {
	Cloned    bool
	Branch    string
	MRs       map[int]mrDisk
	Worktrees int // plain branch worktrees, not tied to any merge request
}

// projectKey identifies a project across instances: two servers can host the
// same path.
type projectKey struct {
	Instance string
	Path     string
}

// App is the running TUI.
type App struct {
	tv       *tview.Application
	pages    *tview.Pages
	tabs     *tview.TextView
	status   *tview.TextView
	helpHint *tview.TextView
	// dialogSaid is the note or success last said over a dialog.
	dialogSaid dialogWord
	// transient is the note or success last said, at the right-hand end of
	// the status line until something else is (say).
	transient string
	// settingsLine is Settings' status line, which also says what runs
	// behind the interface.
	settingsLine *statusLine
	tab          string

	cfg      *config.Config
	sessions *session.Store
	editorMu sync.Mutex
	// Visits are recorded by editor and removal workers; their switch and
	// cached client are shared, while score snapshots belong to the loop.
	zoxideOnce    sync.Once
	zoxideClient  atomic.Pointer[zoxide.Client]
	zoxideEnabled atomic.Bool
	zoxideScores  map[string]float64
	zoxideParents map[string]float64 // zoxideByParent's, built on demand
	zoxideGen     int
	openDirs      map[string]session.Record
	openReading   bool
	// openSeen and openCount are what the last read of the editors found,
	// kept by its goroutine: whether to draw, and whether to ask again.
	openSeen  atomic.Pointer[map[string]session.Record]
	openCount atomic.Int64
	vault     tokenVault
	clients   map[string]forge.Provider
	// logins maps an instance to the account its token belongs to, filled in
	// when a token is verified.
	logins map[string]string

	projects []forge.Project
	mrs      []forge.MergeRequest
	// me is who the token of each server belongs to, as the last refresh
	// of the merge requests found out.
	me map[string]string
	// people is what the accounts of each server are called (people.go).
	people index.Users
	// ciEvery is how often a running pipeline is read again; 0 is the
	// usual five seconds, and tests make it short.
	ciEvery time.Duration
	// mrFresh counts the commits pushed to a merge request since its review
	// last checked out its head, -1 when there are some not yet on disk;
	// freshGen numbers its loads.
	mrFresh  map[mrKey]int
	freshGen int
	// seen is the heads marked as reviewed without a review on disk, by
	// seenKey; afterFresh runs once the next count of NEW is in.
	seen        map[string]string
	afterFresh  func()
	groups      []forge.Group
	projByKey   map[projectKey]forge.Project
	projUpdated time.Time
	mrsUpdated  time.Time
	// stale marks an index written before unagit knew about a field it shows
	// now, so a column would be empty until it is refreshed.
	staleProjects bool
	staleMRs      bool

	disk map[projectKey]diskInfo

	// chezmoi is the checkout chezmoi keeps and the repository it is, when
	// that integration is on; chezmoiProblem says why none was found.
	// findChezmoi asks chezmoi, and is replaced in tests.
	chezmoi        atomic.Pointer[chezmoiState]
	chezmoiProblem string
	findChezmoi    func() (chezmoi.Checkout, error)

	projectsPane  *pane
	mrsPane       *pane
	worktreesPane *pane
	settings      *settingsView

	// worktrees are the plain branch worktrees on disk, made from Repositories
	// rather than for a merge request; refreshDisk fills them in.
	worktrees []worktreeRow
	// wtRemote is where each worktree's branch stands against origin, by
	// directory, and wtGen numbers the loads so a slow one cannot overwrite a
	// newer answer.
	wtRemote map[string]remoteState
	wtGen    int
	// wtSize is what each worktree takes on disk, by directory, measured in
	// the background; wtSizing holds the ones being measured now.
	wtSize   map[string]int64
	wtSizing map[string]bool
	// themes are the themes there are, built-in and the user's;
	// nerdWhy says whether Nerd Font icons are drawn and how that is known.
	nerdWhy string
	// themeProblem says why the chosen one is not on, until it is said.
	// jobs are the jobs under way behind the interface (jobs.go), and
	// spinFrame turns their spinner; refreshing guards each refresh against
	// starting twice.
	jobs      []*bgJob
	spinFrame int
	// waits counts the dialogs waiting on a request (waitInDialog), which
	// keep the spinner turning as jobs do; spinning is whether it turns.
	waits                        int
	spinning                     bool
	refreshingMRs, refreshingPrj bool

	themes       themeSet
	themeProblem string
	// themeFile is the file of the theme in use, "" for a built-in one;
	// watchTheme reads it from its own goroutine.
	themeFile atomic.Value
	// themeWatchEvery is how often watchTheme looks; zero is
	// themeWatchInterval.
	themeWatchEvery time.Duration
	// themeStat lets the watcher announce a real file read in tests, so a
	// save can follow its baseline rather than guessing when a tick ran.
	// nil reads through os.Stat.
	themeStat func(string) (os.FileInfo, error)
	// newAnimationTicker lets a rendering test advance decoration without
	// changing the clocks that load data or debounce input. Nil uses real time.
	newAnimationTicker func(time.Duration) (<-chan time.Time, func())
	// findExecutable keeps a fake integration local to its app instead of
	// changing PATH for every app in the test process. Nil searches PATH.
	findExecutable func(string) (string, error)
	// yaziInitPath lets tests inspect an isolated Yazi configuration.
	yaziInitPath func() string
	// findMux keeps each fixture independent of the terminal running its tests.
	findMux     func() *mux.Client
	multiplexer *mux.Client
	// findHerdr and findGhostty do the same for herdr's server and Ghostty,
	// which agents and editors can be opened in from outside them too.
	findHerdr   func() *mux.Client
	findGhostty func() *mux.Client
	// The Agents tab: the rows as last read, whether a read is under way,
	// what went wrong with the last, whether the tab is in front - read
	// from the watcher's goroutine - and a nudge for a read now.
	agentsPane *pane
	// tabsWidth is the terminal's width the tab bar was last laid out for.
	tabsWidth     int
	agentRows     []agentRow
	agentsReading bool
	agentsError   string
	agentsInFront atomic.Bool
	agentsNow     chan struct{}
	// repoSync is where each main clone's branch stands against origin, read
	// from the refs on disk; r fetches first. fetchFailed says why a fetch did
	// not get through, and fetching counts the fetches still running.
	repoSync    map[projectKey]remoteState
	fetchFailed map[projectKey]string
	fetching    int
	// ciWatching is set while running pipelines are followed, and
	// ciAskEvery is how often they are asked about; 0 is the usual (cipoll.go).
	ciWatching bool
	ciAskEvery time.Duration
	// branchStatus is the newest pipeline of each branch the lists show that
	// has no merge request, and ciAsking those being asked about
	// (branchci.go).
	branchStatus map[branchKey]string
	ciAsking     map[branchKey]bool
	// starred is the starred repositories that were cloned (starred.go).
	starred []forge.Project
	// repoSize is what each cloned repository takes on disk, its worktrees
	// included, and repoSizing those being measured (reposize.go).
	repoSize   map[projectKey]int64
	repoSizing map[projectKey]bool
	// fetchJob is the fetches under way as one job on the status line, and
	// freshJob the counting of new commits.
	fetchJob, freshJob *bgJob

	mrProjectScope projectKey // the project the merge request list is limited to

	// sortHold keeps a merge request where the list has it, when the detail found
	// it newer than the index: the row shows the fresh time, but the order waits
	// for a refresh or for the request to be opened. Without it, a row would jump
	// away from under the cursor while the detail follows it down the list.
	sortHold map[mrKey]time.Time

	// goal is what the app was started to open, pursued once the vault is
	// open; nil for a plain start.
	goal *Goal

	// wtView is the worktree view while it is open.
	wtView *wtView

	// formModes is the NORMAL or INSERT mode of each form on screen.
	formModes map[*tview.Form]*formMode

	// screenGiven is set when a screen was handed in, as tests do.
	screenGiven bool
	// localRefreshed is when the disk was last looked at.
	localRefreshed time.Time

	// screen is the terminal, kept from the last draw so the clipboard can be
	// set through it when the system has no program for that.
	screen tcell.Screen
}

// mrKey identifies a merge request across servers and projects.
type mrKey struct {
	Instance  string
	ProjectID int
	IID       int
}

func keyOfMR(mr forge.MergeRequest) mrKey { return mrKey{mr.Instance, mr.ProjectID, mr.IID} }

// mrSortTime is the time a merge request is ordered by.
func (a *App) mrSortTime(mr forge.MergeRequest) time.Time {
	if held, ok := a.sortHold[keyOfMR(mr)]; ok {
		return held
	}
	return mr.UpdatedAt
}

// New builds the application with an already open vault, for tests and for
// callers that unlocked it themselves.
func New(cfg *config.Config, vault *secret.Vault) *App {
	if vault == nil {
		return newApp(cfg, nil)
	}
	return newApp(cfg, vault)
}

// tokenVault keeps the interface independent of how an already unlocked
// vault is held. Unlocking still goes through secret.OpenOrCreate.
type tokenVault interface {
	Token(string) string
	Has(string) bool
	Set(string, string)
	Remove(string)
	IDs() []string
	Save(string) error
	Rekey([]byte) error
}

func newApp(cfg *config.Config, vault tokenVault) *App {
	// tview's widgets copy its styles when they are made, so the theme comes
	// first - and once per process, which also orders it before the widgets
	// of every other application made in parallel.
	applyTheme()
	a := &App{
		tv:        tview.NewApplication(),
		pages:     tview.NewPages(),
		cfg:       cfg,
		sessions:  session.New(cfg.Dir()),
		disk:      map[projectKey]diskInfo{},
		agentsNow: make(chan struct{}, 1),
	}
	a.vault = vault
	a.rebuildClients()
	return a
}

// NewLocked builds the application with the tokens still encrypted; the
// passphrase is asked for in a modal once the interface is up.
func NewLocked(cfg *config.Config) *App {
	applyTheme()
	return &App{
		tv:        tview.NewApplication(),
		pages:     tview.NewPages(),
		cfg:       cfg,
		sessions:  session.New(cfg.Dir()),
		disk:      map[projectKey]diskInfo{},
		agentsNow: make(chan struct{}, 1),
	}
}

// setVault wires up everything that needs the decrypted tokens.
func (a *App) setVault(v *secret.Vault) {
	a.vault = v
	a.rebuildClients()
}

// rebuildClients refreshes the per-instance API clients after the
// configuration or the tokens changed.
func (a *App) rebuildClients() {
	a.clients = make(map[string]forge.Provider, len(a.cfg.Instances))
	if a.vault == nil {
		return
	}
	for _, inst := range a.cfg.Instances {
		token := a.vault.Token(inst.ID)
		if token == "" {
			continue
		}
		if inst.IsGitHub() {
			a.clients[inst.ID] = github.New(token)
			continue
		}
		a.clients[inst.ID] = gitlab.New(inst.URL, token)
	}
}

// client returns the API client of an instance, or nil when it has no token.
func (a *App) client(instanceID string) forge.Provider { return a.clients[instanceID] }

// rememberLogin records who a token belongs to, so the settings can show the
// account behind a GitHub entry.
func (a *App) rememberLogin(instanceID, login string) {
	if a.logins == nil {
		a.logins = map[string]string{}
	}
	a.logins[instanceID] = login
}

// githubLogin is the account a GitHub token belongs to, once it has been
// verified.
func (a *App) githubLogin(instanceID string) string {
	if login := a.logins[instanceID]; login != "" {
		return "github.com/" + login
	}
	return "github.com"
}

// forgeGroup turns a selected group back into what a provider takes.
func forgeGroup(g config.Group) forge.Group {
	return forge.Group{ID: g.ID, Name: g.Name, Path: g.Name, FullPath: g.FullPath}
}

// Run builds the interface and starts the event loop.
func (a *App) Run() error {
	a.detectMultiplexer()
	applyTheme()
	a.chooseTheme()
	layout := a.buildInterface()

	a.tv.SetInputCapture(a.globalKeys)
	a.tv.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		a.screen = screen
		if w, _ := screen.Size(); w != a.tabsWidth && a.tabs != nil {
			a.tabsWidth = w
			a.drawTabs()
		}
		return false
	})
	// A form in NORMAL types nothing, so no cursor blinks in its field.
	a.tv.SetAfterDrawFunc(func(screen tcell.Screen) {
		a.markFocusedField(screen)
		a.drawDialogWord(screen)
	})

	if !a.screenGiven {
		screen, err := tcell.NewScreen()
		if err != nil {
			return err
		}
		a.SetScreen(screen)
	}
	if a.vault == nil {
		a.showUnlock()
	} else {
		a.start()
	}
	stopWatching := make(chan struct{})
	go a.watchTheme(stopWatching)
	go a.watchEditors(stopWatching)
	go a.watchAgents(stopWatching)
	err := a.tv.SetRoot(layout, true).EnableMouse(false).Run()
	close(stopWatching)
	// Window editors outlive unagit, but nothing vouches for them any more.
	closeWindowSessions()
	return err
}

// buildInterface makes every widget of the main screens. tview's widgets
// copy the theme's colours when they are made, so a new theme is put on by
// making them again (switchTheme).
func (a *App) buildInterface() tview.Primitive {
	a.pages = tview.NewPages()
	a.tabs = tview.NewTextView().SetDynamicColors(true)
	a.status = tview.NewTextView().SetDynamicColors(true)
	a.helpHint = tview.NewTextView().SetDynamicColors(true).SetText(tag(colDim) + "? help" + tagEnd).SetTextAlign(tview.AlignRight)

	a.projectsPane = a.newProjectsPane()
	a.mrsPane = a.newMRsPane()
	a.worktreesPane = a.newWorktreesPane()
	a.agentsPane = a.newAgentsPane()
	a.settings = a.newSettingsView()

	a.pages.AddPage(pageProjects, a.projectsPane.root, true, true)
	a.pages.AddPage(pageMRs, a.mrsPane.root, true, false)
	a.pages.AddPage(pageWorktrees, a.worktreesPane.root, true, false)
	a.pages.AddPage(pageAgents, a.agentsPane.root, true, false)
	a.tab = pageProjects
	a.drawTabs()

	a.settingsLine = newStatusLine(a.status, a.helpHint)
	settingsLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.settings.root, 0, 1, true).
		AddItem(a.settingsLine, 1, 0, false)
	a.settingsLine.holdIn(settingsLayout)
	a.pages.AddPage(pageSettings, settingsLayout, true, false)

	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.tabs, 1, 0, false).
		AddItem(a.pages, 0, 1, true)
}

// start loads the cached indexes and shows the first tab. It runs once the
// vault is open.
func (a *App) start() {
	if a.themeProblem != "" {
		defer a.flash(a.themeProblem)
		a.themeProblem = ""
	}
	a.loadIndexes()
	a.detectChezmoi()
	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
	a.settings.reload()
	a.switchTab(pageProjects)

	switch {
	case a.staleProjects || a.staleMRs:
		a.flash("The cached index is from an older unagit - press R on each tab to fill in what it did not know")
	case len(a.cfg.Instances) == 0:
		a.switchTab(pageSettings)
		a.settings.selectSection(sectionGitLab)
		a.flash("Add your first GitLab server: press a - GitHub is the section below")
	case len(a.selectedGroups()) == 0:
		a.switchTab(pageSettings)
		a.settings.selectSection(sectionGroups)
		a.flash("Pick the groups you work with, then refresh the indexes with p and m")
	}
	a.pursueGoal()
}

// selectedGroups counts every group selected across all instances.
func (a *App) selectedGroups() []config.Group {
	var all []config.Group
	for _, inst := range a.cfg.Instances {
		all = append(all, inst.Groups...)
	}
	return all
}

// globalKeys handles the keys that work on every page.
func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if ev, handled := a.openSelectKeys(ev); handled {
		return ev
	}
	// Ctrl-C ends unagit, as it ends any program in a terminal; C clones.
	if ev.Key() == tcell.KeyCtrlC {
		a.tv.Stop()
		return nil
	}
	// : in a dialog lists what can be done from anywhere. A main screen
	// answers : itself, its own actions first, and so does the worktree
	// view; what only a main screen can do is not offered over a dialog.
	if opensScreenActions(ev) && a.dialogTakesColon() {
		a.showActions("Actions", a.globalActions())
		return nil
	}
	return ev
}

// dialogTakesColon reports whether : over what is in front is the global
// actions: a dialog is, nothing is being typed into, and it is not one that
// answers : itself, holds the keys (a warning) or must be got through first
// (the passphrase).
func (a *App) dialogTakesColon() bool {
	name, _ := a.pages.GetFrontPage()
	switch {
	case !isModalPage(name):
		return false
	case name == pageWorktree, name == pageActions, name == pageMessage, name == pageUnlock:
		return false
	}
	switch a.tv.GetFocus().(type) {
	case *tview.InputField, *tview.TextArea:
		// A form in NORMAL types nothing; a filter or INSERT does.
		if form, mode := a.focusedForm(); form != nil && !mode.insert {
			return true
		}
		return false
	}
	return true
}

// closeModal removes a modal page and gives the keyboard back to whatever was
// underneath it. Without this, closing a dialog would leave nothing focused.
func (a *App) closeModal(page string) {
	// The form of the page being closed - not whatever is in front - is gone.
	if name, prim := a.pages.GetFrontPage(); name == page && prim != nil {
		if box, ok := prim.(*modalBox); ok {
			if form, ok := box.content.(*tview.Form); ok {
				a.forgetForm(form)
			}
		}
	}
	a.pages.RemovePage(page)
	// Modals stack: closing one can leave another underneath.
	if name, prim := a.pages.GetFrontPage(); isModalPage(name) {
		a.tv.SetFocus(prim)
		return
	}
	a.restoreFocus()
}

// isModalPage reports whether a page name is one of the overlays.
func isModalPage(name string) bool {
	switch name {
	case pageTask, pageConfirm, pageHelp, pagePicker, pageActions, pageUnlock, pageForm, pageComments, pageToggles, pageWorktree,
		pageMessage, pageCommit:
		return true
	}
	return false
}

// restoreFocus focuses the visible tab again.
func (a *App) restoreFocus() {
	if a.modalOpen() {
		return
	}
	switch a.currentTab() {
	case pageProjects:
		a.tv.SetFocus(a.projectsPane.focusTarget())
	case pageMRs:
		a.tv.SetFocus(a.mrsPane.focusTarget())
	case pageWorktrees:
		a.tv.SetFocus(a.worktreesPane.focusTarget())
	case pageAgents:
		a.tv.SetFocus(a.agentsPane.focusTarget())
	case pageSettings:
		a.tv.SetFocus(a.settings.focusTarget())
	}
}

// modalOpen reports whether a modal page covers the current tab.
func (a *App) modalOpen() bool {
	name, _ := a.pages.GetFrontPage()
	return isModalPage(name)
}

// ---------------------------------------------------------------- status bar

// clearSaid takes the word last said off the status line: it belonged to
// what was in front before.
func (a *App) clearSaid() {
	if a.transient == "" {
		return
	}
	a.transient = ""
	a.showJobs()
}

// tildePath shortens a path under the home directory for display.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home); ok {
		return "~" + rest
	}
	return p
}

// flash, note, done and errorf tell the user something: a warning, a word on
// what is under way or how the view changed, what was done, a failure. A
// warning and a failure ask for attention, so they come up in a box of their
// own over whatever is in front (showMessage) and stay until Esc. A note and
// a success are a passing word: on a main screen they go to the right-hand
// end of the status line, beside the jobs under way, until something else is
// said; while a dialog is in front the status line is out of the eye's way,
// under the dimmed screen, so there they come up over the dialog too.
func (a *App) flash(msg string) { a.say(msg, sevWarning) }
func (a *App) note(msg string)  { a.say(msg, sevInfo) }
func (a *App) done(msg string)  { a.say(msg, sevSuccess) }
func (a *App) errorf(f string, v ...any) {
	a.say(fmt.Sprintf(f, v...), sevError)
}

func (a *App) say(msg string, sev severity) {
	if sev == sevWarning || sev == sevError {
		a.showMessage(msg, sev)
		return
	}
	colour := sev.colour()
	if sev == sevInfo {
		colour = colMuted
	}
	said := tag(colour) + tview.Escape(msg) + tagEnd
	if name, front := a.pages.GetFrontPage(); isModalPage(name) {
		a.dialogSaid = dialogWord{on: front, text: said}
		return
	}
	a.transient = said
	a.showJobs()
}

// dialogWord is a note or a success said while a dialog was in front, and
// the dialog: the word is drawn in that dialog's bottom edge, at its right,
// as the main screens have it at the right of their status line - read in
// passing, in nobody's way. It goes with the dialog.
type dialogWord struct {
	on   tview.Primitive
	text string
	// waiting is a word of waitInDialog's: what the dialog waits for, with
	// the spinner before it.
	waiting bool
}

// waitInDialog runs fn off the event loop while the dialog in front says in
// its bottom edge what it waits for, a spinner turning before it. A short
// request made from a dialog - starting a job - needs no log over it that
// flashes up and goes. then runs on the event loop once fn went through,
// told whether that dialog is still the one in front; what went wrong is a
// warning.
func (a *App) waitInDialog(text string, fn func() error, then func(stillOpen bool)) {
	_, front := a.pages.GetFrontPage()
	a.dialogSaid = dialogWord{on: front, text: tag(colMuted) + esc(text) + tagEnd, waiting: true}
	a.waits++
	a.keepSpinning()
	go func() {
		err := fn()
		a.tv.QueueUpdateDraw(func() {
			a.waits--
			if a.dialogSaid.waiting && a.dialogSaid.on == front {
				a.dialogSaid = dialogWord{}
			}
			if err != nil {
				a.errorf("%v", err)
				return
			}
			_, now := a.pages.GetFrontPage()
			then(now == front)
		})
	}()
}

// drawDialogWord draws the word last said in the bottom edge of the dialog
// in front, when it was said there.
func (a *App) drawDialogWord(screen tcell.Screen) {
	name, front := a.pages.GetFrontPage()
	if !isModalPage(name) || front == nil || front != a.dialogSaid.on || a.dialogSaid.text == "" {
		return
	}
	box, ok := front.(*modalBox)
	if !ok {
		return
	}
	x, y, w, h := box.content.GetRect()
	if w < 8 || h < 2 {
		return
	}
	// On the edge's own background, so the word sits in the line as a
	// title sits in the top one.
	row, end := y+h-1, x+w-2
	text := " " + a.dialogSaid.text + " "
	if a.dialogSaid.waiting {
		text = " " + tag(colAccent) + spinnerGlyph(a.spinFrame) + tagEnd + text
	}
	width := min(tview.TaggedStringWidth(text), w-4)
	tview.Print(screen, text, end-width, row, width, tview.AlignLeft, colMuted)
}

// ------------------------------------------------------------------- indexes

func (a *App) loadIndexes() {
	if p, err := index.Load[index.Projects](a.cfg.IndexPath("projects")); err == nil {
		a.projects, a.projUpdated = p.Items, p.UpdatedAt
		a.staleProjects = index.Stale(p.Version, len(p.Items))
	}
	if m, err := index.Load[index.MergeRequests](a.cfg.IndexPath("mrs")); err == nil {
		a.mrs, a.mrsUpdated, a.me = m.Items, m.UpdatedAt, m.Me
		a.loadSeen()
		a.staleMRs = index.Stale(m.Version, len(m.Items))
	}
	if g, err := index.Load[index.Groups](a.cfg.IndexPath("groups")); err == nil {
		a.groups = g.Items
	}
	if u, err := index.Load[index.Users](a.cfg.IndexPath("users")); err == nil {
		a.people = u
	}
	a.loadBranchCI()
	a.loadStarred()
	a.adoptLegacyIndex()
	a.reindexProjects()
}

// adoptLegacyIndex labels items cached before unagit knew about instances.
func (a *App) adoptLegacyIndex() {
	if len(a.cfg.Instances) == 0 {
		return
	}
	first := a.cfg.Instances[0].ID
	for i := range a.projects {
		if a.projects[i].Instance == "" {
			a.projects[i].Instance = first
		}
	}
	for i := range a.mrs {
		if a.mrs[i].Instance == "" {
			a.mrs[i].Instance = first
		}
	}
	for i := range a.groups {
		if a.groups[i].Instance == "" {
			a.groups[i].Instance = first
		}
	}
}

func (a *App) reindexProjects() {
	a.mergeStarred()
	a.projByKey = make(map[projectKey]forge.Project, len(a.projects))
	for _, p := range a.projects {
		a.projByKey[projectKey{p.Instance, p.PathWithNamespace}] = p
	}
	a.pairChezmoi()
}

// instanceOf returns the configured instance an item came from.
func (a *App) instanceOf(id string) *config.Instance { return a.cfg.Instance(id) }

// instanceLabel names an instance for display.
func (a *App) instanceLabel(id string) string {
	if inst := a.cfg.Instance(id); inst != nil {
		return inst.Label()
	}
	return id
}

// multiInstance reports whether the lists have to say where a row came from.
func (a *App) multiInstance() bool { return len(a.cfg.Instances) > 1 }

// projectPathOfMR resolves the target project path of a merge request, falling
// back to the "group/project!iid" reference GitLab returns.
func (a *App) projectPathOfMR(mr forge.MergeRequest) string {
	if mr.ProjectPath != "" {
		return mr.ProjectPath
	}
	return resolveMRPath(mr, nil)
}

// resolveMRPath falls back to the project index when a provider could not say
// which repository a merge request belongs to.
func resolveMRPath(mr forge.MergeRequest, byID map[int]string) string {
	if mr.ProjectPath != "" {
		return mr.ProjectPath
	}
	if path, ok := byID[mr.ProjectID]; ok {
		return path
	}
	return ""
}

// ------------------------------------------------------------ roots and dirs

// rootFor is where a project of an instance is cloned.
func (a *App) rootFor(instanceID, projectPath string) string {
	return a.cfg.RootFor(a.cfg.Instance(instanceID), projectPath)
}

// projectDir is where a repository's main clone is: chezmoi's checkout when
// chezmoi keeps it, else where unagit clones it.
func (a *App) projectDir(instanceID, projectPath string) string {
	if dir := a.managedDir(instanceID, projectPath); dir != "" {
		return dir
	}
	return a.cloneDir(instanceID, projectPath)
}

// cloneDir is where unagit clones a repository, and where its worktrees hang
// even when chezmoi keeps the clone.
func (a *App) cloneDir(instanceID, projectPath string) string {
	return a.cfg.ProjectDir(a.cfg.Instance(instanceID), projectPath)
}

func (a *App) mrDir(instanceID, projectPath string, iid int, branch string) string {
	return a.pathManager(instanceID, projectPath).MRDir(projectPath, iid, branch)
}

func (a *App) reviewDir(instanceID, projectPath string, iid int, branch string) string {
	return a.pathManager(instanceID, projectPath).ReviewDir(projectPath, iid, branch)
}

// Path lookups do not need credentials or an API client.
func (a *App) pathManager(instanceID, projectPath string) *workspace.Manager {
	return workspace.New(workspace.Options{Root: a.rootFor(instanceID, projectPath),
		ProjectDirectory: a.cloneDir(instanceID, projectPath), ManagedDirectory: a.managedDir(instanceID, projectPath), OnRemoved: a.zoxideRemove}, nil)
}

// newManager builds a workspace manager for one project, with that project's
// root, its server's URL and token, and the way that forge publishes merge
// request heads.
func (a *App) newManager(instanceID, projectPath string, log func(string)) *workspace.Manager {
	opts := workspace.Options{
		Root:             a.rootFor(instanceID, projectPath),
		OnRemoved:        a.zoxideRemove,
		ManagedDirectory: a.managedDir(instanceID, projectPath),
	}
	if inst := a.cfg.Instance(instanceID); inst != nil {
		opts.ProjectDirectory = inst.ProjectDirs[projectPath]
		opts.GitLabURL = inst.URL
		opts.CloneProtocol = inst.Protocol()
	}
	if a.vault != nil {
		opts.Token = a.vault.Token(instanceID)
	}
	if client := a.client(instanceID); client != nil {
		opts.GitUser = client.GitUser()
		opts.HeadRefFormat = strings.Replace(client.HeadRef(0), "0", "%d", 1)
	}
	return workspace.New(opts, log)
}

// ------------------------------------------------------------------ refresh

// instancesWithTokens returns the instances unagit can actually talk to.
func (a *App) instancesWithTokens() ([]config.Instance, error) {
	var ready []config.Instance
	var missing []string
	for _, inst := range a.cfg.Instances {
		if a.client(inst.ID) == nil {
			missing = append(missing, inst.Label())
			continue
		}
		if len(inst.Groups) == 0 {
			continue
		}
		ready = append(ready, inst)
	}
	if len(ready) == 0 {
		if len(missing) > 0 {
			return nil, fmt.Errorf("no token for %s - set one in [4] Settings", strings.Join(missing, ", "))
		}
		return nil, fmt.Errorf("no groups selected - open [4] Settings first")
	}
	return ready, nil
}

// refreshFanOut is how many groups are asked about at once. Each one is a
// separate conversation with a server, and GitHub adds a request per
// repository on top of that.
const refreshFanOut = 6

// groupJob is one selected group on one server, with the client to ask.
type groupJob struct {
	inst   config.Instance
	group  config.Group
	client forge.Provider
	// projects, when set, are the only repositories of the group whose
	// merge requests are asked for - the ones the list shows - and carried
	// what the others had when last read (narrowMRJobs).
	projects []forge.Project
	carried  []forge.MergeRequest
}

// narrowLimit is how many shown repositories a group may have for its
// merge requests to be asked for repository by repository; past it, one
// listing of the whole group is quicker.
const narrowLimit = 60

// narrowMRJobs makes a refresh of merge requests ask about what the list
// shows: in a group where repositories, or their merge requests, are
// hidden, the shown repositories are asked one by one, and the hidden
// keep what they had - a group's listing of every merge request was most
// of a refresh's wait, for rows nobody looks at. It runs on the event loop.
func (a *App) narrowMRJobs(jobs []groupJob) {
	for i, job := range jobs {
		var shown []forge.Project
		hidden := map[string]bool{}
		for _, pr := range a.projects {
			if pr.Instance != job.inst.ID || !job.group.Owns(pr.PathWithNamespace) {
				continue
			}
			if a.passesFilters(pr.Instance, pr.PathWithNamespace) && !a.cfg.Filters.HidesMRsOf(pr.Instance, pr.PathWithNamespace) {
				shown = append(shown, pr)
			} else {
				hidden[pr.PathWithNamespace] = true
			}
		}
		if len(hidden) == 0 || len(shown) > narrowLimit {
			continue
		}
		jobs[i].projects = shown
		if jobs[i].projects == nil {
			jobs[i].projects = []forge.Project{}
		}
		// Every merge request of the group not asked about is carried: the
		// hidden repositories', and those of a repository the index does
		// not know yet - left out, they would look closed, and their
		// worktrees would be tidied away.
		asked := map[string]bool{}
		for _, pr := range shown {
			asked[pr.PathWithNamespace] = true
		}
		for _, mr := range a.mrs {
			path := a.projectPathOfMR(mr)
			if mr.Instance == job.inst.ID && job.group.Owns(path) && !asked[path] {
				jobs[i].carried = append(jobs[i].carried, mr)
			}
		}
	}
}

// projectsMRs asks for the open merge requests of each repository, a few
// at a time.
func projectsMRs(ctx context.Context, client forge.Provider, projects []forge.Project) ([]forge.MergeRequest, error) {
	var (
		mu       sync.Mutex
		out      []forge.MergeRequest
		firstErr error
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, extrasFanOut)
	for _, pr := range projects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ms, err := client.ProjectMergeRequests(ctx, pr)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for i := range ms {
				ms[i].ProjectID = pr.ID
				if ms[i].ProjectPath == "" {
					ms[i].ProjectPath = pr.PathWithNamespace
				}
			}
			out = append(out, ms...)
		}()
	}
	wg.Wait()
	return out, firstErr
}

// groupJobs pairs every selected group with its client, on the event loop, so
// the workers never touch shared state.
func (a *App) groupJobs(instances []config.Instance) []groupJob {
	var jobs []groupJob
	for _, inst := range instances {
		client := a.client(inst.ID)
		if client == nil {
			continue
		}
		for _, g := range inst.Groups {
			jobs = append(jobs, groupJob{inst: inst, group: g, client: client})
		}
	}
	return jobs
}

// fanOut runs the jobs a few at a time and gathers what they return. The first
// failure cancels the rest: a half refreshed index is worse than none.
func fanOut[T any](ctx context.Context, jobs []groupJob, work func(context.Context, groupJob) ([]T, error)) ([]T, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu       sync.Mutex
		out      []T
		firstErr error
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, refreshFanOut)
	for _, job := range jobs {
		wg.Add(1)
		go func(job groupJob) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			items, err := work(ctx, job)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil && ctx.Err() == nil {
					firstErr = fmt.Errorf("%s · %s: %w", job.inst.Label(), job.group.FullPath, err)
					cancel()
				}
				return
			}
			out = append(out, items...)
		}(job)
	}
	wg.Wait()
	return out, firstErr
}

// refreshProjects re-reads every selected group's project list from the API.
func (a *App) refreshProjects() {
	instances, err := a.instancesWithTokens()
	if err != nil {
		a.errorf("%v", err)
		return
	}
	jobs := a.groupJobs(instances)
	var all []forge.Project
	var idx index.Projects
	a.runInBackground("refreshing repositories", &a.refreshingPrj, func(progress func(string)) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var asked atomic.Int32
		var err error
		all, err = fanOut(ctx, jobs, func(ctx context.Context, job groupJob) ([]forge.Project, error) {
			ps, err := job.client.GroupProjects(ctx, forgeGroup(job.group), job.group.IncludesSubgroups())
			if err != nil {
				return nil, err
			}
			for i := range ps {
				ps[i].Instance = job.inst.ID
			}
			progress(fmt.Sprintf("groups %d/%d", asked.Add(1), len(jobs)))
			return ps, nil
		})
		if err != nil {
			return err
		}
		all = index.DedupeProjects(all)
		idx = index.Projects{Version: index.Version, UpdatedAt: time.Now(), Items: all}
		return index.Save(a.cfg.IndexPath("projects"), idx)
	}, func() {
		a.projects, a.projUpdated, a.staleProjects = all, idx.UpdatedAt, false
		// The marks point into the old list.
		a.projectsPane.marks = nil
		a.reindexProjects()
		a.refreshDisk()
		a.loadRepoSync(true)
		// What each takes on disk is measured again last: it is the slowest
		// and the least pressing.
		a.loadRepoSizes(true)
		a.projectsPane.reload()
		a.mrsPane.reload()
		a.settings.reload()
		a.done(fmt.Sprintf("%d repositories indexed", len(all)))
		a.askBranchCI(a.repositoryCITargets(), "reading pipelines")
	})
}

// refreshMRs re-reads every selected group's open merge requests from the API.
func (a *App) refreshMRs() {
	instances, err := a.instancesWithTokens()
	if err != nil {
		a.errorf("%v", err)
		return
	}
	jobs := a.groupJobs(instances)
	a.narrowMRJobs(jobs)
	before := map[mrKey]bool{}
	for _, mr := range a.mrs {
		before[keyOfMR(mr)] = true
	}
	clients := map[string]forge.Provider{}
	for _, j := range jobs {
		clients[j.inst.ID] = j.client
	}
	paths := a.snapshotProjectPaths()
	var all []forge.MergeRequest
	var idx index.MergeRequests
	people := a.people.Clone()
	a.runInBackground("refreshing merge requests", &a.refreshingMRs, func(progress func(string)) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var asked atomic.Int32
		var err error
		all, err = fanOut(ctx, jobs, func(ctx context.Context, job groupJob) ([]forge.MergeRequest, error) {
			if job.projects != nil {
				ms, err := projectsMRs(ctx, job.client, job.projects)
				if err != nil {
					return nil, err
				}
				for i := range ms {
					ms[i].Instance = job.inst.ID
				}
				progress(fmt.Sprintf("groups %d/%d", asked.Add(1), len(jobs)))
				return append(ms, job.carried...), nil
			}
			ms, err := job.client.GroupMergeRequests(ctx, forgeGroup(job.group), job.group.IncludesSubgroups())
			if err != nil {
				return nil, err
			}
			// GitLab's group endpoint always descends into subgroups, so a
			// group selected on its own is narrowed down here.
			kept := ms[:0]
			for _, mr := range ms {
				mr.Instance = job.inst.ID
				mr.ProjectPath = resolveMRPath(mr, paths[job.inst.ID])
				if job.group.Owns(mr.ProjectPath) {
					kept = append(kept, mr)
				}
			}
			progress(fmt.Sprintf("groups %d/%d", asked.Add(1), len(jobs)))
			return kept, nil
		})
		if err != nil {
			return err
		}
		all = index.DedupeMergeRequests(all)

		// What the list does not show is not asked about: it keeps what the
		// last refresh found, and the requests go to what is seen.
		var shown map[mrKey]bool
		var before map[mrKey]forge.MergeRequest
		if !a.onLoopWait(ctx, func() {
			shown, before = map[mrKey]bool{}, map[mrKey]forge.MergeRequest{}
			for _, mr := range all {
				shown[keyOfMR(mr)] = a.passesFilters(mr.Instance, mr.ProjectPath) && a.passesMRFilters(mr)
			}
			for _, mr := range a.mrs {
				before[keyOfMR(mr)] = mr
			}
		}) {
			return ctx.Err()
		}
		var ask []int
		for i := range all {
			if shown[keyOfMR(all[i])] {
				ask = append(ask, i)
				continue
			}
			if old, ok := before[keyOfMR(all[i])]; ok {
				keepExtras(&all[i], old)
			}
		}
		mrExtras(ctx, all, ask, clients, func(done int) {
			progress(fmt.Sprintf("details %d/%d", done, len(ask)))
		})
		me := map[string]string{}
		for id, client := range clients {
			if u, err := client.CurrentUser(ctx); err == nil && u != nil {
				me[id] = u.Username
			}
		}
		learnNames(ctx, &people, all, clients, func(done, of int) {
			progress(fmt.Sprintf("names %d/%d", done, of))
		})
		// The names are a convenience; one that could not be saved is
		// asked for again next time.
		_ = index.Save(a.cfg.IndexPath("users"), people)
		idx = index.MergeRequests{Version: index.Version, UpdatedAt: time.Now(), Items: all, Me: me}
		return index.Save(a.cfg.IndexPath("mrs"), idx)
	}, func() {
		a.mrs, a.mrsUpdated, a.staleMRs, a.me = all, idx.UpdatedAt, false, idx.Me
		// The marks point into the list that was.
		a.mrsPane.marks = nil
		a.people = people
		a.sortHold = nil
		a.forgetClosedFavourites(instances, all)
		asked := map[string]bool{}
		for _, inst := range instances {
			asked[inst.ID] = true
		}
		a.pruneSeen(asked, all)
		a.refreshDisk()
		a.cleanUpClosed(instances, all, func(removed, kept []string) {
			// The count of NEW comes once the disk is looked at again;
			// the summary waits for it.
			a.afterFresh = func() { a.sayRefreshed(before, all, removed, kept) }
			a.refreshDisk()
		})
		a.mrsPane.reload()
		a.worktreesPane.reload()
		a.projectsPane.reload()
		a.settings.reload()
	})
}

// onLoopWait runs fn on the event loop and waits for it, for a job behind
// the interface that needs to read what the interface holds. It reports
// false when ctx ends first.
func (a *App) onLoopWait(ctx context.Context, fn func()) bool {
	done := make(chan struct{})
	a.tv.QueueUpdate(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// keepExtras carries over what a refresh did not ask again about a merge
// request: its pipeline, approvals and threads as last read.
func keepExtras(mr *forge.MergeRequest, old forge.MergeRequest) {
	mr.Pipeline = old.Pipeline
	mr.ApprovedBy, mr.ApprovalsRequired = old.ApprovedBy, old.ApprovalsRequired
	mr.Unresolved, mr.Resolved, mr.UnresolvedKnown = old.Unresolved, old.Resolved, old.UnresolvedKnown
}

// mrExtras fills in what the listings leave out - the pipeline, the
// approvals, the threads not resolved - for the merge requests at those
// indexes, several at a time and the three questions of each at once. What
// cannot be read is left out: it is a mark in a column, not a reason to fail
// the refresh. done hears how many are finished. It runs off the event loop.
func mrExtras(ctx context.Context, mrs []forge.MergeRequest, which []int, clients map[string]forge.Provider, done func(int)) {
	sem := make(chan struct{}, extrasFanOut)
	var wg sync.WaitGroup
	var finished atomic.Int32
	for _, i := range which {
		client := clients[mrs[i].Instance]
		if client == nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(mr *forge.MergeRequest) {
			defer func() {
				<-sem
				if done != nil {
					done(int(finished.Add(1)))
				}
				wg.Done()
			}()
			// Each question gets the same copy, and the answers are written
			// back once all three are in.
			asked := *mr
			var each sync.WaitGroup
			each.Add(3)
			pipeline := asked.Pipeline
			go func() {
				defer each.Done()
				if p, err := client.MergeRequestPipeline(ctx, asked); err == nil && p != nil {
					pipeline = p.Status
				}
			}()
			approvedBy, required := asked.ApprovedBy, asked.ApprovalsRequired
			go func() {
				defer each.Done()
				if ap, err := client.MergeRequestApprovals(ctx, asked); err == nil && ap != nil {
					approvedBy, required = ap.ApprovedBy, ap.Required
				}
			}()
			unresolved, resolved, known := asked.Unresolved, asked.Resolved, asked.UnresolvedKnown
			go func() {
				defer each.Done()
				if open, done, k, err := client.Threads(ctx, asked); err == nil {
					unresolved, resolved, known = open, done, k
				}
			}()
			each.Wait()
			mr.Pipeline = pipeline
			mr.ApprovedBy, mr.ApprovalsRequired = approvedBy, required
			mr.Unresolved, mr.Resolved, mr.UnresolvedKnown = unresolved, resolved, known
		}(&mrs[i])
	}
	wg.Wait()
}

// extrasFanOut is how many merge requests are asked about at once, each with
// three questions in flight.
const extrasFanOut = 8

// snapshotProjectPaths copies the project id to path mapping per instance, for
// background use.
func (a *App) snapshotProjectPaths() map[string]map[int]string {
	m := make(map[string]map[int]string, len(a.cfg.Instances))
	for _, p := range a.projects {
		if m[p.Instance] == nil {
			m[p.Instance] = map[int]string{}
		}
		m[p.Instance][p.ID] = p.PathWithNamespace
	}
	return m
}

// orgExplainer is implemented by providers that can say why a group listing
// came back thinner than expected.
type orgExplainer interface {
	ExplainMissingOrgs(ctx context.Context) string
}

// countOrgs counts the groups that are not the account itself. GitHub's own
// account is listed with a negative id, which no real organisation has.
func countOrgs(groups []forge.Group) int {
	n := 0
	for _, g := range groups {
		if g.ID > 0 {
			n++
		}
	}
	return n
}

// refreshGroups re-reads the group trees from every server that has a token,
// all of them at once.
func (a *App) refreshGroups() {
	type serverJob struct {
		inst   config.Instance
		client forge.Provider
	}
	var jobs []serverJob
	for _, inst := range a.cfg.Instances {
		if client := a.client(inst.ID); client != nil {
			jobs = append(jobs, serverJob{inst, client})
		}
	}
	if len(jobs) == 0 {
		a.errorf("no server with a token yet - add one in Settings")
		return
	}
	a.load("Refreshing groups", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var (
			mu       sync.Mutex
			all      []forge.Group
			firstErr error
			wg       sync.WaitGroup
		)
		for _, job := range jobs {
			wg.Add(1)
			go func(job serverJob) {
				defer wg.Done()
				log(fmt.Sprintf("%s: fetching the groups you are a member of …", job.inst.Label()))
				gs, err := job.client.Groups(ctx)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", job.inst.Label(), err)
					}
					return
				}
				for i := range gs {
					gs[i].Instance = job.inst.ID
				}
				log(fmt.Sprintf("%s: %d group(s)", job.inst.Label(), len(gs)))
				// GitHub answers an unreadable organisation list with silence
				// rather than an error, which looks exactly like having none.
				if explainer, ok := job.client.(orgExplainer); ok && countOrgs(gs) == 0 {
					log("! " + job.inst.Label() + ": " + explainer.ExplainMissingOrgs(ctx))
				}
				all = append(all, gs...)
			}(job)
		}
		wg.Wait()
		if firstErr != nil {
			return "", firstErr
		}

		sort.Slice(all, func(i, j int) bool {
			if all[i].Instance != all[j].Instance {
				return all[i].Instance < all[j].Instance
			}
			return all[i].FullPath < all[j].FullPath
		})
		if err := index.Save(a.cfg.IndexPath("groups"), index.Groups{Version: index.Version, UpdatedAt: time.Now(), Items: all}); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.groups = all
			a.settings.reload()
		})
		log(fmt.Sprintf("Done: %d group(s).", len(all)))
		return "", nil
	})
}

// ---------------------------------------------------------------- disk state

// refreshDisk rebuilds the cached on-disk state by looking at the filesystem.
// It reads .git/HEAD directly instead of shelling out to git, so it stays fast
// even with hundreds of projects.
func (a *App) refreshDisk() {
	a.refreshOpenEditors()
	// Counting what waits to be published reads a small file per worktree, and
	// only means something when Incomm is in use.
	a.disk, a.worktrees = a.scanDisk(a.cfg.Integrations.Incomm)
	a.localRefreshed = time.Now()
	a.loadWorktreeRemotes()
	a.loadWorktreeSizes(false)
	a.loadRepoSizes(false)
	a.loadRepoSync(false)
	a.loadMRFresh()
	a.reloadWorktreeView()
	if a.worktreesPane != nil && a.worktreesPane.reload != nil {
		a.worktreesPane.reload()
	}
}

// scanDisk reads what is on disk for every repository of the lists - the
// clone, the merge requests' worktrees, the other worktrees - and the grouped
// worktrees. It changes nothing, so unagit go can read the same without an
// interface around it.
func (a *App) scanDisk(countPending bool) (map[projectKey]diskInfo, []worktreeRow) {
	disk := make(map[projectKey]diskInfo, len(a.projects))
	seen := map[projectKey]bool{}
	var worktrees []worktreeRow

	inspect := func(key projectKey) {
		if key.Path == "" || seen[key] {
			return
		}
		seen[key] = true
		dir := a.projectDir(key.Instance, key.Path)
		info := diskInfo{MRs: map[int]mrDisk{}}
		if head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD")); err == nil {
			info.Cloned = true
			head := strings.TrimSpace(string(head))
			if branch, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
				info.Branch = branch
			} else {
				// Detached: the commit stands where a branch would.
				info.Branch = detachedLabel(head)
			}
		} else if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && !fi.IsDir() {
			info.Cloned = true
		}
		mgr := a.pathManager(key.Instance, key.Path)
		home, legacyReviews := mgr.MRRoot(key.Path), a.cloneDir(key.Instance, key.Path)+".reviews"
		for _, root := range mgr.WorktreeRoots(key.Path) {
			entries, _ := os.ReadDir(root)
			for _, e := range entries {
				if !e.IsDir() || !workspace.Exists(filepath.Join(root, e.Name())) {
					continue
				}
				name := e.Name()
				if root == home && strings.HasPrefix(name, "wt-") {
					info.Worktrees++
					wtDir := filepath.Join(root, name)
					branch, moved := workspace.WorktreeHead(wtDir)
					worktrees = append(worktrees, worktreeRow{
						Instance: key.Instance, Path: key.Path, Branch: branch, Dir: wtDir, Moved: moved,
						Created: workspace.WorktreeCreated(wtDir)})
					continue
				}
				review := root == legacyReviews
				if root == home && strings.HasPrefix(name, "review-") {
					review = true
					name = strings.TrimPrefix(name, "review-")
				}
				num, _, _ := strings.Cut(name, "-")
				iid, err := strconv.Atoi(num)
				if err != nil {
					continue
				}
				d := info.MRs[iid]
				if review {
					d.Review, d.ReviewDir = true, filepath.Join(root, e.Name())
				} else {
					d.Branch, d.BranchDir = true, filepath.Join(root, e.Name())
				}
				if countPending {
					d.Pending += incomm.PendingIn(filepath.Join(root, e.Name()))
				}
				info.MRs[iid] = d
			}
		}

		if info.Cloned || len(info.MRs) > 0 || info.Worktrees > 0 {
			disk[key] = info
		}
	}

	for _, p := range a.projects {
		inspect(projectKey{p.Instance, p.PathWithNamespace})
	}
	for _, m := range a.mrs {
		inspect(projectKey{m.Instance, a.projectPathOfMR(m)})
	}
	// A grouped worktree is one row, and each of its members also counts as a
	// worktree of its own repository.
	for _, g := range workspace.ListGroups(a.cfg.Root()) {
		row := a.groupRow(g)
		for _, m := range row.Members {
			key := projectKey{m.Instance, m.Path}
			info, ok := disk[key]
			if !ok {
				info = diskInfo{MRs: map[int]mrDisk{}}
			}
			info.Worktrees++
			// The group's comments on this repository wait for its merge request.
			if mr, open := a.openMRFor(m); open && countPending {
				d := info.MRs[mr.IID]
				d.Pending += incomm.PendingAt(incomm.Place{Dir: row.Dir, Prefix: filepath.Base(m.Dir) + "/"})
				info.MRs[mr.IID] = d
			}
			disk[key] = info
		}
		worktrees = append(worktrees, row)
	}
	return disk, worktrees
}

// diskOf returns the cached state of one project.
func (a *App) diskOf(instanceID, projectPath string) diskInfo {
	return a.disk[projectKey{instanceID, projectPath}]
}

// --------------------------------------------------------------- long tasks

// runTask shows a log modal and runs fn on a background goroutine. When fn
// returns a directory, the TUI is suspended and the editor is opened there.
func (a *App) runTask(title string, fn func(log func(string)) (string, error)) {
	a.runTaskOpening(title, session.Record{}, nil, fn)
}

// runTaskOpening is runTask for the tasks that end in an editor: what they
// are opening is written down while it is open, so another terminal can find
// the directory. ed is the editor chosen for it; nil is the favourite.
func (a *App) runTaskOpening(title string, what session.Record, ed *editors.Editor, fn func(log func(string)) (string, error)) {
	a.runTaskEnding(title, what, ed, nil, fn)
}

// runTaskNoting is runTask for a task whose outcome is a sentence rather than a
// directory: on success the log closes and the status line says it.
func (a *App) runTaskNoting(title string, fn func(log func(string)) (string, error)) {
	a.runTaskEnding(title, session.Record{}, nil, a.note, fn)
}

// runTaskThen is runTask for a task whose outcome something on screen should
// follow: on success the log closes and then runs, on the event loop, with
// what fn returned.
func (a *App) runTaskThen(title string, fn func(log func(string)) (string, error), then func(string)) {
	a.runTaskEnding(title, session.Record{}, nil, then, fn)
}

// runTaskEnding runs fn under a log. What it returns is a directory to open
// in the editor, unless then is given; then it is handed to then instead.
func (a *App) runTaskEnding(title string, what session.Record, ed *editors.Editor, then func(string), fn func(log func(string)) (string, error)) {
	view := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	view.SetTextColor(colText)
	box(view.Box, title).SetBorderPadding(0, 0, 1, 1)

	done := false
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		if done && (ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q') {
			a.closeModal(pageTask)
			a.clearSaid()
			return nil
		}
		return ev
	})

	a.pages.AddPage(pageTask, modalPct(view, 80, 70), true, true)
	a.tv.SetFocus(view)

	// The line logged last is the step under way: it turns a spinner until
	// the next line comes or the task ends, when it is marked done or
	// failed, so a wait shows what it waits for and that it is alive.
	// A task that logs nothing still has its title as the step under way.
	lines := []string{title}
	frame := 0
	render := func(last string) {
		var b strings.Builder
		for i, line := range lines {
			mark := "  "
			if i == len(lines)-1 {
				mark = last
			}
			b.WriteString(mark + tag(colMuted) + tview.Escape(line) + tagEnd + "\n")
		}
		view.SetText(b.String())
		// Scrolled here, on the event loop, rather than from a changed
		// handler: tview calls that from a goroutine of its own.
		view.ScrollToEnd()
	}
	spinning := func() string {
		frames := []rune(theme.Glyphs.Spinner)
		if len(frames) == 0 {
			frames = []rune(glyphDot)
		}
		return tag(colAccent) + string(frames[frame%len(frames)]) + tagEnd + " "
	}
	log := func(line string) {
		a.tv.QueueUpdateDraw(func() {
			lines = append(lines, line)
			render(spinning())
		})
	}
	render(spinning())
	go func() {
		ticks, stopTicker := a.animationTicker(spinInterval)
		defer stopTicker()
		for range ticks {
			stop := make(chan bool, 1)
			a.tv.QueueUpdateDraw(func() {
				if !done && len(lines) > 0 {
					frame++
					render(spinning())
				}
				stop <- done
			})
			select {
			case finished := <-stop:
				if finished {
					return
				}
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	go func() {
		dir, err := fn(log)
		a.tv.QueueUpdateDraw(func() {
			done = true
			if len(lines) > 0 {
				if err != nil {
					render(tag(colBad) + glyphCross + tagEnd + " ")
				} else {
					render(tag(colOn) + glyphCheck + tagEnd + " ")
				}
			}
			if err != nil {
				fmt.Fprintf(view, "\n%s%s%s\n\n%sPress Esc to close.%s\n",
					tag(colBad), tview.Escape(err.Error()), tagEnd, tag(colWarn), tagEnd)
				view.ScrollToEnd()
				return
			}
			if dir == "" || then != nil {
				a.closeModal(pageTask)
				a.refreshDisk()
				a.projectsPane.reload()
				a.mrsPane.reload()
				a.clearSaid()
				if then != nil {
					then(dir)
				}
			}
		})
		if err == nil && dir != "" && then == nil {
			a.openEditor(dir, what, ed)
		}
	}()
}

// saveConfig writes the configuration and refreshes everything that depends
// on it.
func (a *App) saveConfig() {
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the config: %v", err)
		return
	}
	a.rebuildClients()
	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
}

// saveVault writes the encrypted tokens.
func (a *App) saveVault() {
	if a.vault == nil {
		return
	}
	if err := a.vault.Save(a.cfg.VaultPath()); err != nil {
		a.errorf("cannot save the tokens: %v", err)
		return
	}
	a.rebuildClients()
}
