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
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/github"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/secret"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// page names
const (
	pageProjects = "projects"
	pageMRs      = "mrs"
	pageSettings = "settings"
	pageHelp     = "help"
	pageTask     = "task"
	pageConfirm  = "confirm"
	pagePicker   = "picker"
	pageUnlock   = "unlock"
	pageForm     = "form"
	pageComments = "comments"
	pageHidden   = "hidden"
)

// mrDisk records which worktrees a merge request has on disk.
type mrDisk struct {
	Branch bool // .mrs: a real branch, can be committed and pushed
	Review bool // .reviews: the whole change pending on the merge base
}

// diskInfo is the cached on-disk state of one project.
type diskInfo struct {
	Cloned bool
	Branch string
	MRs    map[int]mrDisk
}

// projectKey identifies a project across instances: two servers can host the
// same path.
type projectKey struct {
	Instance string
	Path     string
}

// App is the running TUI.
type App struct {
	tv     *tview.Application
	pages  *tview.Pages
	tabs   *tview.TextView
	status *tview.TextView
	tab    string

	cfg      *config.Config
	sessions *session.Store
	vault    *secret.Vault
	clients  map[string]forge.Provider
	// logins maps an instance to the account its token belongs to, filled in
	// when a token is verified.
	logins map[string]string

	projects    []forge.Project
	mrs         []forge.MergeRequest
	groups      []forge.Group
	projByKey   map[projectKey]forge.Project
	projUpdated time.Time
	mrsUpdated  time.Time
	// stale marks an index written before unagit knew about a field it shows
	// now, so a column would be empty until it is refreshed.
	staleProjects bool
	staleMRs      bool

	disk map[projectKey]diskInfo

	projectsPane *pane
	mrsPane      *pane
	settings     *settingsView

	mrProjectScope projectKey // the project the merge request list is limited to
}

// New builds the application with an already open vault, for tests and for
// callers that unlocked it themselves.
func New(cfg *config.Config, vault *secret.Vault) *App {
	a := &App{
		tv:       tview.NewApplication(),
		pages:    tview.NewPages(),
		cfg:      cfg,
		sessions: session.New(config.Dir()),
		disk:     map[projectKey]diskInfo{},
	}
	a.setVault(vault)
	return a
}

// NewLocked builds the application with the tokens still encrypted; the
// passphrase is asked for in a modal once the interface is up.
func NewLocked(cfg *config.Config) *App {
	return &App{
		tv:       tview.NewApplication(),
		pages:    tview.NewPages(),
		cfg:      cfg,
		sessions: session.New(config.Dir()),
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

	a.tabs = tview.NewTextView().SetDynamicColors(true)
	a.status = tview.NewTextView().SetDynamicColors(true)

	a.projectsPane = a.newProjectsPane()
	a.mrsPane = a.newMRsPane()
	a.settings = a.newSettingsView()

	a.pages.AddPage(pageProjects, a.projectsPane.root, true, true)
	a.pages.AddPage(pageMRs, a.mrsPane.root, true, false)
	a.pages.AddPage(pageSettings, a.settings.root, true, false)
	a.tab = pageProjects
	a.drawTabs()

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.tabs, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.tv.SetInputCapture(a.globalKeys)

	if a.vault == nil {
		a.showUnlock()
	} else {
		a.start()
	}
	return a.tv.SetRoot(layout, true).EnableMouse(false).Run()
}

// start loads the cached indexes and shows the first tab. It runs once the
// vault is open.
func (a *App) start() {
	a.loadIndexes()
	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
	a.settings.reload()
	a.switchTab(pageProjects)

	switch {
	case a.staleProjects || a.staleMRs:
		a.flash("The cached index is from an older unagit - press r on each tab to fill in what it did not know")
	case len(a.cfg.Instances) == 0:
		a.switchTab(pageSettings)
		a.settings.selectSection(sectionGitLab)
		a.flash("Add your first GitLab server: press a - GitHub is the section below")
	case len(a.selectedGroups()) == 0:
		a.switchTab(pageSettings)
		a.settings.selectSection(sectionGroups)
		a.flash("Pick the groups you work with, then refresh the indexes with p and m")
	}
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
	if ev.Key() == tcell.KeyCtrlC {
		if a.currentTab() == pageProjects && !a.modalOpen() {
			idx := a.projectsPane.selectedIndex()
			if idx >= 0 && idx < len(a.projects) {
				a.cloneProject(a.projects[idx])
			}
			return nil
		}
		a.tv.Stop()
		return nil
	}
	return ev
}

// closeModal removes a modal page and gives the keyboard back to whatever was
// underneath it. Without this, closing a dialog would leave nothing focused.
func (a *App) closeModal(page string) {
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
	case pageTask, pageConfirm, pageHelp, pagePicker, pageUnlock, pageForm, pageComments, pageHidden:
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
	// The per-tab line below the table carries the counts; this line is for
	// messages and the two keys worth repeating.
	text := " "
	if msg != "" {
		text += msg + "  "
	}
	a.status.SetText(text + tag(colDim) + "? help · q quit" + tagEnd)
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

func (a *App) flash(msg string) { a.setStatus(tag(colWarn) + tview.Escape(msg) + tagEnd) }
func (a *App) note(msg string)  { a.setStatus(tag(colMuted) + tview.Escape(msg) + tagEnd) }
func (a *App) errorf(f string, v ...any) {
	a.setStatus(tag(colBad) + tview.Escape(fmt.Sprintf(f, v...)) + tagEnd)
}

// ------------------------------------------------------------------- indexes

func (a *App) loadIndexes() {
	if p, err := index.Load[index.Projects](config.IndexPath("projects")); err == nil {
		a.projects, a.projUpdated = p.Items, p.UpdatedAt
		a.staleProjects = index.Stale(p.Version, len(p.Items))
	}
	if m, err := index.Load[index.MergeRequests](config.IndexPath("mrs")); err == nil {
		a.mrs, a.mrsUpdated = m.Items, m.UpdatedAt
		a.staleMRs = index.Stale(m.Version, len(m.Items))
	}
	if g, err := index.Load[index.Groups](config.IndexPath("groups")); err == nil {
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

func (a *App) projectDir(instanceID, projectPath string) string {
	return workspace.ProjectDirIn(a.rootFor(instanceID, projectPath), projectPath)
}

func (a *App) mrRoot(instanceID, projectPath string) string {
	return workspace.MRRootIn(a.rootFor(instanceID, projectPath), projectPath)
}

func (a *App) reviewRoot(instanceID, projectPath string) string {
	return workspace.ReviewRootIn(a.rootFor(instanceID, projectPath), projectPath)
}

func (a *App) mrDir(instanceID, projectPath string, iid int, branch string) string {
	return workspace.MRDirIn(a.rootFor(instanceID, projectPath), projectPath, iid, branch)
}

func (a *App) reviewDir(instanceID, projectPath string, iid int, branch string) string {
	return workspace.ReviewDirIn(a.rootFor(instanceID, projectPath), projectPath, iid, branch)
}

// newManager builds a workspace manager for one project, with that project's
// root, its server's URL and token, and the way that forge publishes merge
// request heads.
func (a *App) newManager(instanceID, projectPath string, log func(string)) *workspace.Manager {
	opts := workspace.Options{
		Root:       a.rootFor(instanceID, projectPath),
		Editor:     a.cfg.Editor,
		EditorArgs: a.cfg.EditorArgs,
	}
	if inst := a.cfg.Instance(instanceID); inst != nil {
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
			return nil, fmt.Errorf("no token for %s - set one in Settings [S]", strings.Join(missing, ", "))
		}
		return nil, fmt.Errorf("no groups selected - open Settings [S] first")
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
		if err := index.Save(config.IndexPath("projects"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.projects, a.projUpdated, a.staleProjects = all, idx.UpdatedAt, false
			a.reindexProjects()
			a.refreshDisk()
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
		idx := index.MergeRequests{Version: index.Version, UpdatedAt: time.Now(), Items: all}
		if err := index.Save(config.IndexPath("mrs"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.mrs, a.mrsUpdated, a.staleMRs = all, idx.UpdatedAt, false
			a.refreshDisk()
			a.mrsPane.reload()
			a.projectsPane.reload()
			a.settings.reload()
		})
		log(fmt.Sprintf("Done: %d merge request(s) indexed.", len(all)))
		return "", nil
	})
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
		if err := index.Save(config.IndexPath("groups"), index.Groups{Version: index.Version, UpdatedAt: time.Now(), Items: all}); err != nil {
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
	disk := make(map[projectKey]diskInfo, len(a.projects))
	seen := map[projectKey]bool{}

	inspect := func(key projectKey) {
		if key.Path == "" || seen[key] {
			return
		}
		seen[key] = true
		dir := a.projectDir(key.Instance, key.Path)
		info := diskInfo{MRs: map[int]mrDisk{}}
		if head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD")); err == nil {
			info.Cloned = true
			info.Branch = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/"))
		} else if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && !fi.IsDir() {
			info.Cloned = true
		}
		for _, mode := range []struct {
			root   string
			review bool
		}{
			{a.mrRoot(key.Instance, key.Path), false},
			{a.reviewRoot(key.Instance, key.Path), true},
		} {
			entries, _ := os.ReadDir(mode.root)
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				num := name
				if i := strings.Index(name, "-"); i > 0 {
					num = name[:i]
				}
				iid, err := strconv.Atoi(num)
				if err != nil {
					continue
				}
				d := info.MRs[iid]
				if mode.review {
					d.Review = true
				} else {
					d.Branch = true
				}
				info.MRs[iid] = d
			}
		}
		if info.Cloned || len(info.MRs) > 0 {
			disk[key] = info
		}
	}

	for _, p := range a.projects {
		inspect(projectKey{p.Instance, p.PathWithNamespace})
	}
	for _, m := range a.mrs {
		inspect(projectKey{m.Instance, a.projectPathOfMR(m)})
	}
	a.disk = disk
}

// diskOf returns the cached state of one project.
func (a *App) diskOf(instanceID, projectPath string) diskInfo {
	return a.disk[projectKey{instanceID, projectPath}]
}

// --------------------------------------------------------------- long tasks

// runTask shows a log modal and runs fn on a background goroutine. When fn
// returns a directory, the TUI is suspended and the editor is opened there.
func (a *App) runTask(title string, fn func(log func(string)) (string, error)) {
	a.runTaskOpening(title, session.Record{}, fn)
}

// runTaskOpening is runTask for the tasks that end in an editor: what they
// are opening is written down while it is open, so another terminal can find
// the directory.
func (a *App) runTaskOpening(title string, what session.Record, fn func(log func(string)) (string, error)) {
	view := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	view.SetChangedFunc(func() { view.ScrollToEnd() })
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

	a.pages.AddPage(pageTask, modalPct(view, 80, 70), true, true)
	a.tv.SetFocus(view)

	log := func(line string) {
		a.tv.QueueUpdateDraw(func() {
			fmt.Fprintln(view, tag(colMuted)+tview.Escape(line)+tagEnd)
		})
	}

	go func() {
		dir, err := fn(log)
		a.tv.QueueUpdateDraw(func() {
			done = true
			if err != nil {
				fmt.Fprintf(view, "\n%s%s%s\n\n%sPress Esc to close.%s\n",
					tag(colBad), tview.Escape(err.Error()), tagEnd, tag(colWarn), tagEnd)
				return
			}
			if dir == "" {
				a.closeModal(pageTask)
				a.refreshDisk()
				a.projectsPane.reload()
				a.mrsPane.reload()
				a.setStatus("")
			}
		})
		if err == nil && dir != "" {
			a.openEditor(dir, what)
		}
	}()
}

// openEditor suspends the TUI, runs the editor and restores the interface.
// unagit stays alive throughout - the editor is its child - so the directory
// is on record for exactly as long as it is open, and another terminal can
// find its way there.
func (a *App) openEditor(dir string, what session.Record) {
	a.tv.QueueUpdateDraw(func() { a.closeModal(pageTask) })
	what.Dir = dir
	defer a.sessions.Open(what)()
	a.tv.Suspend(func() {
		fmt.Printf("\n→ %s\n", dir)
		opts := workspace.Options{Editor: a.cfg.Editor, EditorArgs: a.cfg.EditorArgs}
		if err := workspace.New(opts, nil).OpenEditor(dir); err != nil {
			fmt.Fprintf(os.Stderr, "editor failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "press enter to return to unagit")
			var s string
			fmt.Scanln(&s)
		}
	})
	a.tv.QueueUpdateDraw(func() {
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		a.note("opened " + dir)
	})
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
	if err := a.vault.Save(config.VaultPath()); err != nil {
		a.errorf("cannot save the tokens: %v", err)
		return
	}
	a.rebuildClients()
}
