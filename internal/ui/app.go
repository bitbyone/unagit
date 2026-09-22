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
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/secret"
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
)

// mrDisk records which worktrees a merge request has on disk.
type mrDisk struct {
	Branch bool // .mrs: a real branch, can be committed and pushed
	Review bool // .reviews: the whole change staged on the merge base
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

	cfg     *config.Config
	vault   *secret.Vault
	clients map[string]*gitlab.Client

	projects    []gitlab.Project
	mrs         []gitlab.MergeRequest
	groups      []gitlab.Group
	projByKey   map[projectKey]gitlab.Project
	projUpdated time.Time
	mrsUpdated  time.Time

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
		tv:    tview.NewApplication(),
		pages: tview.NewPages(),
		cfg:   cfg,
		disk:  map[projectKey]diskInfo{},
	}
	a.setVault(vault)
	return a
}

// NewLocked builds the application with the tokens still encrypted; the
// passphrase is asked for in a modal once the interface is up.
func NewLocked(cfg *config.Config) *App {
	return &App{
		tv:    tview.NewApplication(),
		pages: tview.NewPages(),
		cfg:   cfg,
		disk:  map[projectKey]diskInfo{},
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
	a.clients = make(map[string]*gitlab.Client, len(a.cfg.Instances))
	if a.vault == nil {
		return
	}
	for _, inst := range a.cfg.Instances {
		if token := a.vault.Token(inst.ID); token != "" {
			a.clients[inst.ID] = gitlab.New(inst.URL, token)
		}
	}
}

// client returns the API client of an instance, or nil when it has no token.
func (a *App) client(instanceID string) *gitlab.Client { return a.clients[instanceID] }

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
	case len(a.cfg.Instances) == 0:
		a.switchTab(pageSettings)
		a.settings.selectSection(sectionServers)
		a.flash("Add your first GitLab server: press a")
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
		a.tv.Stop()
		return nil
	}
	return ev
}

// closeModal removes a modal page and gives the keyboard back to whatever was
// underneath it. Without this, closing a dialog would leave nothing focused.
func (a *App) closeModal(page string) {
	a.pages.RemovePage(page)
	a.restoreFocus()
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
	switch name, _ := a.pages.GetFrontPage(); name {
	case pageTask, pageConfirm, pageHelp, pagePicker, pageUnlock, pageForm:
		return true
	}
	return false
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
	}
	if m, err := index.Load[index.MergeRequests](config.IndexPath("mrs")); err == nil {
		a.mrs, a.mrsUpdated = m.Items, m.UpdatedAt
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
	a.projByKey = make(map[projectKey]gitlab.Project, len(a.projects))
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
func (a *App) projectPathOfMR(mr gitlab.MergeRequest) string {
	if mr.ProjectPath != "" {
		return mr.ProjectPath
	}
	return resolveMRPath(mr, nil)
}

// resolveMRPath is the goroutine-safe variant: byID is a snapshot taken on the
// event loop before the refresh starts.
func resolveMRPath(mr gitlab.MergeRequest, byID map[int]string) string {
	if path, ok := byID[mr.ProjectID]; ok && path != "" {
		return path
	}
	if mr.ProjectPath != "" {
		return mr.ProjectPath
	}
	if ref := mr.References.Full; ref != "" {
		if i := strings.Index(ref, "!"); i > 0 {
			return ref[:i]
		}
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
// root, the instance's URL and its token.
func (a *App) newManager(instanceID, projectPath string, log func(string)) *workspace.Manager {
	opts := workspace.Options{
		Root:       a.rootFor(instanceID, projectPath),
		Editor:     a.cfg.Editor,
		EditorArgs: a.cfg.EditorArgs,
	}
	if inst := a.cfg.Instance(instanceID); inst != nil {
		opts.GitLabURL = inst.URL
	}
	if a.vault != nil {
		opts.Token = a.vault.Token(instanceID)
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

// refreshProjects re-reads every selected group's project list from the API.
func (a *App) refreshProjects() {
	instances, err := a.instancesWithTokens()
	if err != nil {
		a.errorf("%v", err)
		return
	}
	a.runTask("Refreshing projects", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var all []gitlab.Project
		for _, inst := range instances {
			client := a.client(inst.ID)
			for _, g := range inst.Groups {
				scope := "including subgroups"
				if !g.IncludesSubgroups() {
					scope = "this group only"
				}
				log(fmt.Sprintf("%s: fetching projects of %s (%s) ...", inst.Label(), g.FullPath, scope))
				ps, err := client.GroupProjects(ctx, g.ID, g.IncludesSubgroups())
				if err != nil {
					return "", fmt.Errorf("%s: %w", inst.Label(), err)
				}
				for i := range ps {
					ps[i].Instance = inst.ID
				}
				log(fmt.Sprintf("  %d project(s)", len(ps)))
				all = append(all, ps...)
			}
		}
		all = index.DedupeProjects(all)
		idx := index.Projects{UpdatedAt: time.Now(), Items: all}
		if err := index.Save(config.IndexPath("projects"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.projects, a.projUpdated = all, idx.UpdatedAt
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
	paths := a.snapshotProjectPaths()
	a.runTask("Refreshing merge requests", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var all []gitlab.MergeRequest
		for _, inst := range instances {
			client := a.client(inst.ID)
			for _, g := range inst.Groups {
				log(fmt.Sprintf("%s: fetching merge requests of %s ...", inst.Label(), g.FullPath))
				ms, err := client.GroupMergeRequests(ctx, g.ID)
				if err != nil {
					return "", fmt.Errorf("%s: %w", inst.Label(), err)
				}
				// GitLab's group endpoint always descends into subgroups, so a
				// group selected on its own is narrowed down here.
				kept := ms[:0]
				for _, mr := range ms {
					mr.Instance = inst.ID
					mr.ProjectPath = resolveMRPath(mr, paths[inst.ID])
					if g.Owns(mr.ProjectPath) {
						kept = append(kept, mr)
					}
				}
				log(fmt.Sprintf("  %d open merge request(s)", len(kept)))
				all = append(all, kept...)
			}
		}
		all = index.DedupeMergeRequests(all)
		idx := index.MergeRequests{UpdatedAt: time.Now(), Items: all}
		if err := index.Save(config.IndexPath("mrs"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.mrs, a.mrsUpdated = all, idx.UpdatedAt
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

// refreshGroups re-reads the group trees from every instance that has a token.
func (a *App) refreshGroups() {
	var instances []config.Instance
	for _, inst := range a.cfg.Instances {
		if a.client(inst.ID) != nil {
			instances = append(instances, inst)
		}
	}
	if len(instances) == 0 {
		a.errorf("no server with a token yet - add one in Settings")
		return
	}
	a.runTask("Refreshing groups", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var all []gitlab.Group
		for _, inst := range instances {
			log(fmt.Sprintf("%s: fetching every group you are a member of ...", inst.Label()))
			gs, err := a.client(inst.ID).Groups(ctx)
			if err != nil {
				return "", fmt.Errorf("%s: %w", inst.Label(), err)
			}
			for i := range gs {
				gs[i].Instance = inst.ID
			}
			log(fmt.Sprintf("  %d group(s)", len(gs)))
			all = append(all, gs...)
		}
		sort.Slice(all, func(i, j int) bool {
			if all[i].Instance != all[j].Instance {
				return all[i].Instance < all[j].Instance
			}
			return all[i].FullPath < all[j].FullPath
		})
		if err := index.Save(config.IndexPath("groups"), index.Groups{UpdatedAt: time.Now(), Items: all}); err != nil {
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
			a.openEditor(dir)
		}
	}()
}

// openEditor suspends the TUI, runs the editor and restores the interface.
func (a *App) openEditor(dir string) {
	a.tv.QueueUpdateDraw(func() { a.closeModal(pageTask) })
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
