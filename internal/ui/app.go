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

// App is the running TUI.
type App struct {
	tv     *tview.Application
	pages  *tview.Pages
	tabs   *tview.TextView
	status *tview.TextView
	tab    string

	cfg    *config.Config
	client *gitlab.Client
	ws     *workspace.Manager
	token  string
	blob   *secret.Blob // set while the token is still locked

	projects    []gitlab.Project
	mrs         []gitlab.MergeRequest
	groups      []gitlab.Group
	projByID    map[int]gitlab.Project
	projByPath  map[string]gitlab.Project
	projUpdated time.Time
	mrsUpdated  time.Time

	disk map[string]diskInfo

	projectsPane *pane
	mrsPane      *pane
	settings     *settingsView

	mrProjectScope string // project path the merge request list is limited to
}

// New builds the application with an already decrypted token.
func New(cfg *config.Config, token string) *App {
	a := &App{
		tv:    tview.NewApplication(),
		pages: tview.NewPages(),
		cfg:   cfg,
		disk:  map[string]diskInfo{},
	}
	a.setToken(token)
	return a
}

// NewLocked builds the application with the token still encrypted; the
// passphrase is asked for in a modal once the interface is up.
func NewLocked(cfg *config.Config, blob *secret.Blob) *App {
	return &App{
		tv:    tview.NewApplication(),
		pages: tview.NewPages(),
		cfg:   cfg,
		disk:  map[string]diskInfo{},
		blob:  blob,
	}
}

// setToken wires up everything that needs the decrypted token.
func (a *App) setToken(token string) {
	a.token = token
	a.client = gitlab.New(a.cfg.GitLabURL, token)
	a.ws = workspace.New(a.cfg, token, nil)
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

	if a.blob != nil {
		a.showUnlock()
	} else {
		a.start()
	}
	return a.tv.SetRoot(layout, true).EnableMouse(false).Run()
}

// start loads the cached indexes and shows the first tab. It runs once the
// token is available.
func (a *App) start() {
	a.loadIndexes()
	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
	a.settings.build()
	a.switchTab(pageProjects)

	if len(a.cfg.Groups) == 0 {
		a.switchTab(pageSettings)
		a.flash("No groups selected yet - pick the groups you work with, then refresh the indexes.")
	}
}

// globalKeys handles the keys that work on every page.
func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	if ev.Key() == tcell.KeyCtrlC {
		a.tv.Stop()
		return nil
	}
	return ev
}

// modalOpen reports whether a modal page covers the current tab.
func (a *App) modalOpen() bool {
	switch name, _ := a.pages.GetFrontPage(); name {
	case pageTask, pageConfirm, pageHelp, pagePicker, pageUnlock:
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
	a.reindexProjects()
}

func (a *App) reindexProjects() {
	a.projByID = make(map[int]gitlab.Project, len(a.projects))
	a.projByPath = make(map[string]gitlab.Project, len(a.projects))
	for _, p := range a.projects {
		a.projByID[p.ID] = p
		a.projByPath[p.PathWithNamespace] = p
	}
}

// projectPathOfMR resolves the target project path of a merge request, falling
// back to the "group/project!iid" reference GitLab returns.
func (a *App) projectPathOfMR(mr gitlab.MergeRequest) string {
	if p, ok := a.projByID[mr.ProjectID]; ok {
		return p.PathWithNamespace
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

// snapshotGroups copies the selected groups so a background refresh cannot
// race with the settings view.
func (a *App) snapshotGroups() []config.Group {
	return append([]config.Group(nil), a.cfg.Groups...)
}

// snapshotProjectPaths copies the project id to path mapping for background use.
func (a *App) snapshotProjectPaths() map[int]string {
	m := make(map[int]string, len(a.projects))
	for _, p := range a.projects {
		m[p.ID] = p.PathWithNamespace
	}
	return m
}

// refreshProjects re-reads every selected group's project list from the API.
func (a *App) refreshProjects() {
	if len(a.cfg.Groups) == 0 {
		a.errorf("no groups selected - open Settings [S] first")
		return
	}
	groups := a.snapshotGroups()
	a.runTask("Refreshing projects", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var all []gitlab.Project
		for _, g := range groups {
			scope := "including subgroups"
			if !g.IncludesSubgroups() {
				scope = "this group only"
			}
			log(fmt.Sprintf("Fetching projects of %s (%s) ...", g.FullPath, scope))
			ps, err := a.client.GroupProjects(ctx, g.ID, g.IncludesSubgroups())
			if err != nil {
				return "", err
			}
			log(fmt.Sprintf("  %d project(s)", len(ps)))
			all = append(all, ps...)
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
			a.settings.buildInfo()
		})
		log(fmt.Sprintf("Done: %d project(s) indexed.", len(all)))
		return "", nil
	})
}

// refreshMRs re-reads every selected group's open merge requests from the API.
func (a *App) refreshMRs() {
	if len(a.cfg.Groups) == 0 {
		a.errorf("no groups selected - open Settings [S] first")
		return
	}
	groups := a.snapshotGroups()
	paths := a.snapshotProjectPaths()
	a.runTask("Refreshing merge requests", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var all []gitlab.MergeRequest
		for _, g := range groups {
			log(fmt.Sprintf("Fetching merge requests of %s ...", g.FullPath))
			ms, err := a.client.GroupMergeRequests(ctx, g.ID)
			if err != nil {
				return "", err
			}
			// GitLab's group endpoint always descends into subgroups, so a
			// group selected on its own is narrowed down here.
			kept := ms[:0]
			for _, mr := range ms {
				if g.Owns(resolveMRPath(mr, paths)) {
					kept = append(kept, mr)
				}
			}
			log(fmt.Sprintf("  %d open merge request(s)", len(kept)))
			all = append(all, kept...)
		}
		all = index.DedupeMergeRequests(all)
		for i := range all {
			all[i].ProjectPath = resolveMRPath(all[i], paths)
		}
		idx := index.MergeRequests{UpdatedAt: time.Now(), Items: all}
		if err := index.Save(config.IndexPath("mrs"), idx); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.mrs, a.mrsUpdated = all, idx.UpdatedAt
			a.refreshDisk()
			a.mrsPane.reload()
			a.projectsPane.reload()
			a.settings.buildInfo()
		})
		log(fmt.Sprintf("Done: %d merge request(s) indexed.", len(all)))
		return "", nil
	})
}

// refreshGroups re-reads the group tree from the API.
func (a *App) refreshGroups() {
	a.runTask("Refreshing groups", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		log("Fetching every group you are a member of ...")
		gs, err := a.client.Groups(ctx)
		if err != nil {
			return "", err
		}
		sort.Slice(gs, func(i, j int) bool { return gs[i].FullPath < gs[j].FullPath })
		if err := index.Save(config.IndexPath("groups"), index.Groups{UpdatedAt: time.Now(), Items: gs}); err != nil {
			return "", err
		}
		a.tv.QueueUpdateDraw(func() {
			a.groups = gs
			a.settings.build()
		})
		log(fmt.Sprintf("Done: %d group(s).", len(gs)))
		return "", nil
	})
}

// ---------------------------------------------------------------- disk state

// refreshDisk rebuilds the cached on-disk state by looking at the filesystem.
// It reads .git/HEAD directly instead of shelling out to git, so it stays fast
// even with hundreds of projects.
func (a *App) refreshDisk() {
	disk := make(map[string]diskInfo, len(a.projects))
	seen := map[string]bool{}

	inspect := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		dir := a.ws.ProjectDir(path)
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
		}{{a.ws.MRRoot(path), false}, {a.ws.ReviewRoot(path), true}} {
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
			disk[path] = info
		}
	}

	for _, p := range a.projects {
		inspect(p.PathWithNamespace)
	}
	for _, m := range a.mrs {
		inspect(a.projectPathOfMR(m))
	}
	a.disk = disk
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
			a.pages.RemovePage(pageTask)
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
				a.pages.RemovePage(pageTask)
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
	a.tv.QueueUpdateDraw(func() { a.pages.RemovePage(pageTask) })
	a.tv.Suspend(func() {
		fmt.Printf("\n→ %s\n", dir)
		if err := a.ws.OpenEditor(dir); err != nil {
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

// newManager returns a workspace manager whose log lines go to fn.
func (a *App) newManager(log func(string)) *workspace.Manager {
	return workspace.New(a.cfg, a.token, log)
}
