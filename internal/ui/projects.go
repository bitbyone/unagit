package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// projFiltered holds the indexes into App.projects currently shown.
type scored struct {
	idx   int
	score int
}

func (a *App) newProjectsPane() *pane {
	p := a.newPane("Repositories")
	var filtered []int

	p.headline = func() string {
		age := "never refreshed"
		if !a.projUpdated.IsZero() {
			age = "indexed " + humanAge(a.projUpdated)
		}
		return fmt.Sprintf("%s%d/%d repositories · %s%s%s",
			tag(colMuted), len(filtered), len(a.projects), age, a.filterSummary(a.cfg.Filters.GroupRepositories), tagEnd)
	}

	render := func(query string) {
		filtered = a.filterProjects(a.projects, query)
		a.drawProjects(p, filtered)
		p.updateHeader()
	}
	p.onQuery = render

	selected := func() (forge.Project, bool) {
		i := p.selectedIndex()
		if i < 0 || i >= len(a.projects) {
			return forge.Project{}, false
		}
		return a.projects[i], true
	}

	// Enter loads the detail column, Ctrl-O does the actual checkout. Once the
	// column is open it follows the cursor.
	p.onDetail = func(idx int, focus bool) {
		if idx >= 0 && idx < len(a.projects) {
			a.showProjectDetail(a.projects[idx], focus)
		}
	}
	p.onOpen = func(ask bool) {
		if pr, ok := selected(); ok {
			a.withEditor(ask, func(ed *editors.Editor) { a.openProject(pr, ed) })
		}
	}

	p.onAlt = func(r rune) bool {
		pr, ok := selected()
		if !ok {
			return false
		}
		switch r {
		case 'w':
			a.withEditor(true, func(ed *editors.Editor) { a.showWorktreePicker(pr, ed) })
		case 'b':
			a.withEditor(true, func(ed *editors.Editor) { a.showBranchPicker(pr, ed) })
		default:
			return false
		}
		return true
	}

	shared := a.filterKeysFor(p)
	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
		if shared(ev) {
			return nil
		}
		// Ctrl-G gathers the list under the groups, as it gathers merge
		// requests under their repositories.
		if ev.Key() == tcell.KeyCtrlG {
			a.toggleRepositoryGrouping()
			return nil
		}
		// Ctrl-W opens a worktree for a branch of its own; w (below) is
		// already taken by "open in the browser".
		if ev.Key() == tcell.KeyCtrlW {
			if pr, ok := selected(); ok {
				a.showWorktreePicker(pr, nil)
			}
			return nil
		}
		if ev.Key() != tcell.KeyRune {
			return ev
		}
		switch ev.Rune() {
		case 'e':
			if pr, ok := selected(); ok {
				a.showProjectDirectory(pr)
			}
			return nil
		case 'y':
			if pr, ok := selected(); ok {
				a.yankProject(pr)
			}
			return nil
		case 'b':
			if pr, ok := selected(); ok {
				a.showBranchPicker(pr, nil)
			}
			return nil
		case 'm':
			if pr, ok := selected(); ok {
				a.mrProjectScope = projectKey{pr.Instance, pr.PathWithNamespace}
				a.mrsPane.reload()
				a.switchTab(pageMRs)
			}
			return nil
		case 'd':
			if pr, ok := selected(); ok {
				a.manageWorktrees(pr)
			}
			return nil
		case 'w':
			if pr, ok := selected(); ok && pr.WebURL != "" {
				_ = openBrowser(pr.WebURL)
				a.note("opened " + pr.WebURL)
			}
			return nil
		case 'r':
			a.refreshProjects()
			return nil
		}
		return ev
	}

	p.reload = func() { render(p.query) }
	return p
}

func (a *App) filterProjects(projects []forge.Project, query string) []int {
	var hits []scored
	for i, p := range projects {
		if !a.passesFilters(p.Instance, p.PathWithNamespace) {
			continue
		}
		hay := p.PathWithNamespace + " " + p.Name + " " + p.Description + " " + a.instanceLabel(p.Instance)
		score, ok := fuzzy.Match(query, hay)
		if !ok {
			continue
		}
		hits = append(hits, scored{idx: i, score: score})
	}
	// A query ranks by how well it matched; without one the shared order wins.
	if strings.TrimSpace(query) != "" {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	} else if a.cfg.Filters.Order() == config.SortName {
		sort.SliceStable(hits, func(i, j int) bool {
			return projects[hits[i].idx].PathWithNamespace < projects[hits[j].idx].PathWithNamespace
		})
	} else {
		sort.SliceStable(hits, func(i, j int) bool {
			return projects[hits[i].idx].LastActivityAt.After(projects[hits[j].idx].LastActivityAt)
		})
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

func (a *App) drawProjects(p *pane, filtered []int) {
	previous := p.selectedIndex()
	p.table.Clear()
	grouped := a.cfg.Filters.GroupRepositories
	// Grouped, the server and the group move into the headings and each row
	// names only the repository.
	withServer := a.multiInstance() && !grouped
	name := func(pr forge.Project) string {
		if grouped {
			return repositoryName(pr.PathWithNamespace)
		}
		return pr.PathWithNamespace
	}

	branchW, actW, serverW, pathW := 6, 8, 0, 0
	for _, idx := range filtered {
		pr := a.projects[idx]
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)
		branch := info.Branch
		if branch == "" {
			branch = pr.DefaultBranch
		}
		branchW = max(branchW, len([]rune(branch)))
		actW = max(actW, len(humanAge(pr.LastActivityAt)))
		if withServer {
			serverW = max(serverW, len([]rune(a.instanceLabel(pr.Instance))))
		}
		pathW = max(pathW, len([]rune(tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))))
	}
	branchW = atLeast(min(branchW, 24), "BRANCH")
	actW = atLeast(actW, "ACTIVITY")
	if withServer {
		serverW = atLeast(min(serverW, 16), "SERVER")
	}
	if pathW > 0 {
		pathW = atLeast(min(pathW, 44), "PATH")
	}

	// Grouped, every row is indented one step under its heading.
	markW := 2
	if grouped {
		markW = 3
	}
	const (
		mrW     = 2
		wtW     = 2
		gaps    = 6
		minName = 20
	)
	fixed := markW + branchW + pathW + mrW + wtW + actW + gaps
	if withServer {
		fixed += serverW + 1
	}
	if pathW > 0 {
		fixed++ // its own gap
	}
	nameW := p.contentWidth() - fixed
	// When it is tight the path goes first, whole: half a directory is worth
	// nothing, and the repository column already says which row this is.
	if nameW < minName && pathW > 0 {
		nameW += pathW + 1
		pathW = 0
	}
	if nameW < minName {
		give := min(branchW-10, minName-nameW)
		if give > 0 {
			branchW -= give
			nameW += give
		}
	}
	nameW = atLeast(max(nameW, 10), "REPOSITORY")

	header := []field{{text: "", width: markW, colour: colDim}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: colDim})
	}
	header = append(header,
		field{text: "REPOSITORY", width: nameW, colour: colDim},
		field{text: "BRANCH", width: branchW, colour: colDim})
	if pathW > 0 {
		header = append(header, field{text: "PATH", width: pathW, colour: colDim})
	}
	header = append(header,
		field{text: "MR", width: mrW, colour: colDim, right: true},
		field{text: "WT", width: wtW, colour: colDim, right: true},
		field{text: "ACTIVITY", width: actW, colour: colDim})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).
		SetSelectable(false).SetExpansion(1))

	row, first := 0, 0
	drawRow := func(idx int) {
		row++
		if first == 0 {
			first = row
		}
		pr := a.projects[idx]
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)

		mark, markColour := " ○", colDim
		if info.Cloned {
			mark, markColour = " ●", colOn
		}
		if grouped {
			mark = " " + mark
		}
		branch, branchColour := info.Branch, colBranch
		if !info.Cloned {
			branch, branchColour = pr.DefaultBranch, colDim
		}
		path := tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace))
		pathColour := colDim
		if info.Cloned {
			pathColour = colMuted
		}
		mrCount := ""
		if n := len(info.MRs); n > 0 {
			mrCount = fmt.Sprintf("%d", n)
		}
		wtCount := ""
		if info.Worktrees > 0 {
			wtCount = fmt.Sprintf("%d", info.Worktrees)
		}

		fields := []field{{raw: tag(markColour) + mark + tagEnd}}
		if withServer {
			fields = append(fields, field{text: a.instanceLabel(pr.Instance), width: serverW, colour: colAccent})
		}
		fields = append(fields,
			field{text: name(pr), width: nameW, colour: colText},
			field{text: branch, width: branchW, colour: branchColour})
		if pathW > 0 {
			fields = append(fields, field{text: path, width: pathW, colour: pathColour})
		}
		fields = append(fields,
			field{text: mrCount, width: mrW, colour: colWarn, right: true},
			field{text: wtCount, width: wtW, colour: colWarn, right: true},
			field{text: humanAge(pr.LastActivityAt), width: actW, colour: colMuted})

		p.table.SetCell(row, 0, tview.NewTableCell(rowText(fields)).
			SetReference(idx).SetExpansion(1))
	}

	if !grouped {
		for _, idx := range filtered {
			drawRow(idx)
		}
	} else {
		byGroup := func(idx int) (string, string) {
			pr := a.projects[idx]
			return headingKey(a, pr.Instance, namespaceOf(pr.PathWithNamespace))
		}
		for _, group := range gather(filtered, byGroup) {
			row++
			setGroupHeading(p.table, row, group)
			for _, idx := range group.rows {
				drawRow(idx)
			}
		}
	}
	p.selectRow(previous, first)
}

// namespaceOf is the group or subgroup a repository lives in - on GitHub its
// owner - and repositoryName what is left of its path.
func namespaceOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return path
}

func repositoryName(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

// openProject clones or updates the main checkout and opens the editor.
func (a *App) openProject(pr forge.Project, ed *editors.Editor) {
	a.runTaskOpening("Opening "+pr.PathWithNamespace, session.Record{
		Instance: pr.Instance,
		Server:   a.instanceLabel(pr.Instance),
		Project:  pr.PathWithNamespace,
		Mode:     session.ModeRepository,
	}, ed, func(log func(string)) (string, error) {
		return a.newManager(pr.Instance, pr.PathWithNamespace, log).EnsureProject(pr)
	})
}

// cloneProject keeps the task's directory empty so completion returns to the list.
func (a *App) cloneProject(pr forge.Project) {
	a.runTask("Cloning "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		_, err := a.newManager(pr.Instance, pr.PathWithNamespace, log).CloneProject(pr)
		return "", err
	})
}

// An override is a destination, so group and server roots must not be added to it.
func (a *App) showProjectDirectory(pr forge.Project) {
	inst := a.cfg.Instance(pr.Instance)
	if inst == nil {
		return
	}
	if workspace.Exists(a.projectDir(pr.Instance, pr.PathWithNamespace)) {
		a.flash("repository is already cloned; move or delete it before changing its directory")
		return
	}
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Directory", inst.ProjectDirs[pr.PathWithNamespace], 46, nil, nil)
	form.AddTextView("", "Enter the full repository directory (absolute or ~/…).\nBlank restores the group and server rules.", 46, 3, true, false)
	apply := func(value string) {
		if workspace.Exists(a.projectDir(pr.Instance, pr.PathWithNamespace)) {
			a.flash("repository is already cloned; delete it before changing its directory")
			return
		}
		value = config.Expand(strings.TrimSpace(value))
		if value != "" && (!filepath.IsAbs(value) || filepath.Clean(value) == string(filepath.Separator)) {
			a.flash("enter an absolute repository directory, or leave it blank to inherit")
			return
		}
		if value != "" {
			value = filepath.Clean(value)
		}
		old := inst.ProjectDirs[pr.PathWithNamespace]
		if inst.ProjectDirs == nil {
			inst.ProjectDirs = map[string]string{}
		}
		if value == "" {
			delete(inst.ProjectDirs, pr.PathWithNamespace)
		} else {
			inst.ProjectDirs[pr.PathWithNamespace] = value
		}
		if err := a.cfg.Save(); err != nil {
			if old == "" {
				delete(inst.ProjectDirs, pr.PathWithNamespace)
			} else {
				inst.ProjectDirs[pr.PathWithNamespace] = old
			}
			a.flash(err.Error())
			return
		}
		a.closeModal(pageForm)
		a.refreshDisk()
		a.projectsPane.reload()
		a.mrsPane.reload()
		a.note("Clone directory: " + tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))
	}
	form.AddButton("Save", func() { apply(form.GetFormItem(0).(*tview.InputField).GetText()) })
	form.AddButton("Inherit", func() { apply("") })
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("Clone directory · "+pr.PathWithNamespace, form, 12)
}
