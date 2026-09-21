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
)

// diskInfo is the cached on-disk state of one project.
type diskInfo struct {
	Cloned bool
	Branch string
	MRs    map[int]string // merge request iid -> worktree directory name
}

// App is the running TUI.
type App struct {
	tv     *tview.Application
	pages  *tview.Pages
	cfg    *config.Config
	client *gitlab.Client
	ws     *workspace.Manager
	token  string

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

	status *tview.TextView
}

// New builds the application. The token is kept in memory only.
func New(cfg *config.Config, token string) *App {
	a := &App{
		tv:     tview.NewApplication(),
		pages:  tview.NewPages(),
		cfg:    cfg,
		client: gitlab.New(cfg.GitLabURL, token),
		token:  token,
		disk:   map[string]diskInfo{},
	}
	a.ws = workspace.New(cfg, token, nil)
	return a
}

// Run loads the cached indexes and starts the event loop.
func (a *App) Run() error {
	a.loadIndexes()

	a.status = tview.NewTextView().SetDynamicColors(true)
	a.status.SetBackgroundColor(tcell.ColorDarkSlateGray)

	a.projectsPane = a.newProjectsPane()
	a.mrsPane = a.newMRsPane()
	a.settings = a.newSettingsView()

	a.pages.AddPage(pageProjects, a.projectsPane.root, true, true)
	a.pages.AddPage(pageMRs, a.mrsPane.root, true, false)
	a.pages.AddPage(pageSettings, a.settings.root, true, false)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.tv.SetInputCapture(a.globalKeys)

	a.refreshDisk()
	a.projectsPane.reload()
	a.mrsPane.reload()
	a.setStatus("")

	if len(a.cfg.Groups) == 0 {
		a.show(pageSettings)
		a.flash("No groups selected yet - pick the groups you work with, then refresh the indexes.")
	}

	return a.tv.SetRoot(layout, true).EnableMouse(false).Run()
}

// globalKeys handles the keys that work on every page.
func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	// Modal pages own their keys entirely.
	if name, _ := a.pages.GetFrontPage(); name == pageTask || name == pageConfirm || name == pageHelp || name == pagePicker {
		return ev
	}
	if ev.Key() == tcell.KeyCtrlC {
		a.tv.Stop()
		return nil
	}
	return ev
}

func (a *App) show(page string) {
	a.pages.SwitchToPage(page)
	a.setStatus("")
}

// current returns the visible non-modal page name.
func (a *App) currentPage() string {
	name, _ := a.pages.GetFrontPage()
	return name
}

// ---------------------------------------------------------------- status bar

func (a *App) setStatus(msg string) {
	if a.status == nil {
		return
	}
	left := ""
	switch a.currentPage() {
	case pageProjects:
		left = fmt.Sprintf("[::b]PROJECTS[::-] %d  (mrs: %d)", len(a.projects), len(a.mrs))
	case pageMRs:
		scope := "all projects"
		if a.mrProjectScope != "" {
			scope = a.mrProjectScope
		}
		left = fmt.Sprintf("[::b]MERGE REQUESTS[::-] %d  scope: %s", len(a.mrs), scope)
	case pageSettings:
		left = "[::b]SETTINGS[::-]"
	}
	if msg != "" {
		left += "  [yellow]" + tview.Escape(msg) + "[-]"
	}
	a.status.SetText(" " + left + "  [darkgray]|[-] ? help  [darkgray]|[-] q quit")
}

func (a *App) flash(msg string) { a.setStatus(msg) }

func (a *App) errorf(format string, args ...any) {
	a.setStatus("[red]" + fmt.Sprintf(format, args...))
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

// RefreshProjects re-reads every selected group's project list from the API.
func (a *App) refreshProjects() {
	if len(a.cfg.Groups) == 0 {
		a.errorf("no groups selected - open settings (s) first")
		return
	}
	groups := a.snapshotGroups()
	a.runTask("Refreshing projects", func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var all []gitlab.Project
		for _, g := range groups {
			log(fmt.Sprintf("Fetching projects of %s ...", g.FullPath))
			ps, err := a.client.GroupProjects(ctx, g.ID)
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
			a.settings.reload()
		})
		log(fmt.Sprintf("Done: %d project(s) indexed.", len(all)))
		return "", nil
	})
}

// refreshMRs re-reads every selected group's open merge requests from the API.
func (a *App) refreshMRs() {
	if len(a.cfg.Groups) == 0 {
		a.errorf("no groups selected - open settings (s) first")
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
			log(fmt.Sprintf("  %d open merge request(s)", len(ms)))
			all = append(all, ms...)
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
			a.settings.reload()
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
		info := diskInfo{MRs: map[int]string{}}
		if head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD")); err == nil {
			info.Cloned = true
			info.Branch = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/"))
		} else if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && !fi.IsDir() {
			info.Cloned = true
		}
		entries, _ := os.ReadDir(a.ws.MRRoot(path))
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			num := name
			if i := strings.Index(name, "-"); i > 0 {
				num = name[:i]
			}
			if iid, err := strconv.Atoi(num); err == nil {
				info.MRs[iid] = name
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
	view.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)

	frame := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(view, 0, 6, true).
			AddItem(nil, 0, 1, false), 0, 6, true).
		AddItem(nil, 0, 1, false)

	done := false
	frame.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if done && (ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q') {
			a.pages.RemovePage(pageTask)
			a.setStatus("")
			return nil
		}
		return ev
	})

	a.pages.AddPage(pageTask, frame, true, true)
	a.tv.SetFocus(frame)

	log := func(line string) {
		a.tv.QueueUpdateDraw(func() {
			fmt.Fprintln(view, tview.Escape(line))
		})
	}

	go func() {
		dir, err := fn(log)
		a.tv.QueueUpdateDraw(func() {
			done = true
			if err != nil {
				fmt.Fprintf(view, "\n[red]%s[-]\n\n[yellow]Press Esc to close.[-]\n", tview.Escape(err.Error()))
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
		a.setStatus("opened " + dir)
	})
}

// newManager returns a workspace manager whose log lines go to fn.
func (a *App) newManager(log func(string)) *workspace.Manager {
	return workspace.New(a.cfg, a.token, log)
}
