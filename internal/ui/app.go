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
	"github.com/tobola/unagit/internal/secret"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// page names
const (
	pageProjects  = "projects"
	pageMRs       = "mrs"
	pageWorktrees = "worktrees"
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
	tab      string

	cfg      *config.Config
	sessions *session.Store
	vault    *secret.Vault
	clients  map[string]forge.Provider
	// logins maps an instance to the account its token belongs to, filled in
	// when a token is verified.
	logins map[string]string

	projects []forge.Project
	mrs      []forge.MergeRequest
	// me is who the token of each server belongs to, as the last refresh
	// of the merge requests found out.
	me map[string]string
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
	// themeProblem says why the chosen one is not on, until it is said.
	themes       themeSet
	themeProblem string
	// repoSync is where each main clone's branch stands against origin, read
	// from the refs on disk; r fetches first. fetchFailed says why a fetch did
	// not get through, and fetching counts the fetches still running.
	repoSync    map[projectKey]remoteState
	fetchFailed map[projectKey]string
	fetching    int
	// syncingComments is set while r brings merge request comments into the
	// worktrees' Incomm stores.
	syncingComments bool

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
	// tview's widgets copy its styles when they are made, so the theme comes
	// first - and once per process, which also orders it before the widgets
	// of every other application made in parallel.
	applyTheme()
	a := &App{
		tv:       tview.NewApplication(),
		pages:    tview.NewPages(),
		cfg:      cfg,
		sessions: session.New(cfg.Dir()),
		disk:     map[projectKey]diskInfo{},
	}
	a.setVault(vault)
	return a
}

// NewLocked builds the application with the tokens still encrypted; the
// passphrase is asked for in a modal once the interface is up.
func NewLocked(cfg *config.Config) *App {
	applyTheme()
	return &App{
		tv:       tview.NewApplication(),
		pages:    tview.NewPages(),
		cfg:      cfg,
		sessions: session.New(cfg.Dir()),
		disk:     map[projectKey]diskInfo{},
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
	applyTheme()
	a.chooseTheme()
	layout := a.buildInterface()

	a.tv.SetInputCapture(a.globalKeys)
	a.tv.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		a.screen = screen
		return false
	})
	// A form in NORMAL types nothing, so no cursor blinks in its field.
	a.tv.SetAfterDrawFunc(a.markFocusedField)

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
	err := a.tv.SetRoot(layout, true).EnableMouse(false).Run()
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
	a.settings = a.newSettingsView()

	a.pages.AddPage(pageProjects, a.projectsPane.root, true, true)
	a.pages.AddPage(pageMRs, a.mrsPane.root, true, false)
	a.pages.AddPage(pageWorktrees, a.worktreesPane.root, true, false)
	a.tab = pageProjects
	a.drawTabs()

	settingsLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.settings.root, 0, 1, true).
		AddItem(tview.NewFlex().AddItem(a.status, 0, 1, false).AddItem(a.helpHint, 8, 0, false), 1, 0, false)
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
	return ev
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
	case pageTask, pageConfirm, pageHelp, pagePicker, pageUnlock, pageForm, pageComments, pageToggles, pageWorktree,
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

func (a *App) setStatus(msg string) {
	if a.status == nil {
		return
	}
	if a.currentTab() == pageSettings {
		a.status.SetText(" " + msg)
		return
	}
	a.status.SetText("")
	var p *pane
	switch a.currentTab() {
	case pageMRs:
		p = a.mrsPane
	case pageWorktrees:
		p = a.worktreesPane
	default:
		p = a.projectsPane
	}
	if p != nil {
		p.statusMessage = msg
		p.updateHeader()
	}
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
// what is under way or how the view changed, what was done, a failure. The status line is the main screens' own; while a
// dialog or the worktree view is in front it is out of the eye's way, under
// the dimmed screen, so there the message comes up over the dialog instead
// (showMessage).
func (a *App) flash(msg string) { a.say(msg, sevWarning) }
func (a *App) note(msg string)  { a.say(msg, sevInfo) }
func (a *App) done(msg string)  { a.say(msg, sevSuccess) }
func (a *App) errorf(f string, v ...any) {
	a.say(fmt.Sprintf(f, v...), sevError)
}

func (a *App) say(msg string, sev severity) {
	if a.modalOpen() {
		a.showMessage(msg, sev)
		return
	}
	colour := sev.colour()
	if sev == sevInfo {
		colour = colMuted
	}
	a.setStatus(tag(colour) + tview.Escape(msg) + tagEnd)
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
		ProjectDirectory: a.cloneDir(instanceID, projectPath), ManagedDirectory: a.managedDir(instanceID, projectPath)}, nil)
}

// newManager builds a workspace manager for one project, with that project's
// root, its server's URL and token, and the way that forge publishes merge
// request heads.
func (a *App) newManager(instanceID, projectPath string, log func(string)) *workspace.Manager {
	opts := workspace.Options{
		Root:             a.rootFor(instanceID, projectPath),
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
	a.runTask("Refreshing projects", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		log(fmt.Sprintf("Asking %d group(s) on %d server(s), %d at a time …",
			len(jobs), len(instances), refreshFanOut))
		all, err := fanOut(ctx, jobs, func(ctx context.Context, job groupJob) ([]forge.Project, error) {
			scope := "including subgroups"
			if !job.group.IncludesSubgroups() {
				scope = "this group only"
			}
			log(fmt.Sprintf("%s · %s (%s) …", job.inst.Label(), job.group.FullPath, scope))
			ps, err := job.client.GroupProjects(ctx, forgeGroup(job.group), job.group.IncludesSubgroups())
			if err != nil {
				return nil, err
			}
			for i := range ps {
				ps[i].Instance = job.inst.ID
			}
			log(fmt.Sprintf("%s · %s: %d project(s)", job.inst.Label(), job.group.FullPath, len(ps)))
			return ps, nil
		})
		if err != nil {
			return "", err
		}

		all = index.DedupeProjects(all)
		idx := index.Projects{Version: index.Version, UpdatedAt: time.Now(), Items: all}
		if err := index.Save(a.cfg.IndexPath("projects"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.projects, a.projUpdated, a.staleProjects = all, idx.UpdatedAt, false
			// The marks point into the old list.
			a.projectsPane.marks = nil
			a.reindexProjects()
			a.refreshDisk()
			a.loadRepoSync(true)
			a.projectsPane.reload()
			a.mrsPane.reload()
			a.settings.reload()
		})
		log(fmt.Sprintf("Done: %d project(s) indexed.", len(all)))
		return "", nil
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
	before := map[mrKey]bool{}
	for _, mr := range a.mrs {
		before[keyOfMR(mr)] = true
	}
	clients := map[string]forge.Provider{}
	for _, j := range jobs {
		clients[j.inst.ID] = j.client
	}
	paths := a.snapshotProjectPaths()
	a.runTask("Refreshing merge requests", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		log(fmt.Sprintf("Asking %d group(s) on %d server(s), %d at a time …",
			len(jobs), len(instances), refreshFanOut))
		all, err := fanOut(ctx, jobs, func(ctx context.Context, job groupJob) ([]forge.MergeRequest, error) {
			log(fmt.Sprintf("%s · %s …", job.inst.Label(), job.group.FullPath))
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
			log(fmt.Sprintf("%s · %s: %d open merge request(s)", job.inst.Label(), job.group.FullPath, len(kept)))
			return kept, nil
		})
		if err != nil {
			return "", err
		}

		all = index.DedupeMergeRequests(all)
		log("Reading their pipelines, approvals and threads …")
		mrExtras(ctx, all, clients)
		me := map[string]string{}
		for id, client := range clients {
			if u, err := client.CurrentUser(ctx); err == nil && u != nil {
				me[id] = u.Username
			}
		}
		idx := index.MergeRequests{Version: index.Version, UpdatedAt: time.Now(), Items: all, Me: me}
		if err := index.Save(a.cfg.IndexPath("mrs"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.mrs, a.mrsUpdated, a.staleMRs, a.me = all, idx.UpdatedAt, false, me
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
		log(fmt.Sprintf("Done: %d merge request(s) indexed.", len(all)))
		return "", nil
	})
}

// mrExtras fills in what the listings leave out - the pipeline, the
// approvals, the threads not resolved - for every merge request, several at
// a time. What cannot be read is left out: it is a mark in a column, not a
// reason to fail the refresh. It runs off the event loop.
func mrExtras(ctx context.Context, mrs []forge.MergeRequest, clients map[string]forge.Provider) {
	sem := make(chan struct{}, refreshFanOut)
	var wg sync.WaitGroup
	for i := range mrs {
		client := clients[mrs[i].Instance]
		if client == nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(mr *forge.MergeRequest) {
			defer func() { <-sem; wg.Done() }()
			if p, err := client.MergeRequestPipeline(ctx, *mr); err == nil && p != nil {
				mr.Pipeline = p.Status
			}
			if ap, err := client.MergeRequestApprovals(ctx, *mr); err == nil && ap != nil {
				mr.ApprovedBy, mr.ApprovalsRequired = ap.ApprovedBy, ap.Required
			}
			if n, known, err := client.UnresolvedThreads(ctx, *mr); err == nil {
				mr.Unresolved, mr.UnresolvedKnown = n, known
			}
		}(&mrs[i])
	}
	wg.Wait()
}

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
	a.runTask("Refreshing groups", func(log func(string)) (string, error) {
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
	// Counting what waits to be published reads a small file per worktree, and
	// only means something when Incomm is in use.
	a.disk, a.worktrees = a.scanDisk(a.cfg.Integrations.Incomm)
	a.localRefreshed = time.Now()
	a.loadWorktreeRemotes()
	a.loadWorktreeSizes(false)
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
		if done && (ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q') {
			a.closeModal(pageTask)
			a.setStatus("")
			return nil
		}
		return ev
	})

	footer := tview.NewTextView().SetTextColor(colDim).SetText("j/k scroll · g/G first/last · Ctrl-F/B page")
	block := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, true).AddItem(footer, 1, 0, false)
	fitFooter(block, footer, 0)
	a.pages.AddPage(pageTask, modalPct(block, 80, 70), true, true)
	a.tv.SetFocus(view)

	log := func(line string) {
		a.tv.QueueUpdateDraw(func() {
			fmt.Fprintln(view, tag(colMuted)+tview.Escape(line)+tagEnd)
			// Scrolled here, on the event loop, rather than from a changed
			// handler: tview calls that from a goroutine of its own.
			view.ScrollToEnd()
		})
	}

	go func() {
		dir, err := fn(log)
		a.tv.QueueUpdateDraw(func() {
			done = true
			footer.SetText("j/k scroll · g/G first/last · Ctrl-F/B page · Enter/Esc/q close")
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
				a.setStatus("")
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
