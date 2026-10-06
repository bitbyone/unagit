package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
	// Its rows carry tags and a path; beside a detail column they would be
	// cut to nothing well before the default width.
	p.stackBelow = 180
	// Space picks several repositories for one grouped worktree.
	p.markable = true
	var filtered []int

	p.headline = func() string {
		age := "never refreshed"
		if !a.projUpdated.IsZero() {
			age = "indexed " + humanAge(a.projUpdated)
		}
		return fmt.Sprintf("%s%d/%d repositories · %s%s%s",
			tag(colMuted), len(filtered), len(a.projects), age, a.filterSummary(a.cfg.Filters.GroupRepositories)+a.tagSummary(), tagEnd)
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

	p.selection = func() (string, []uiAction) {
		if picked := a.markedProjects(); len(picked) > 0 {
			return fmt.Sprintf("Actions · %d marked repositories", len(picked)), a.markedRepositoryActions(p, picked)
		}
		pr, ok := selected()
		if !ok {
			return "", nil
		}
		return "Actions · " + pr.PathWithNamespace, a.repositoryActions(p, pr)
	}
	p.screen = func() (string, []uiAction) { return "Repositories", a.repositoriesActions(p) }

	p.reload = func() { render(p.query) }
	return p
}

func (a *App) filterProjects(projects []forge.Project, query string) []int {
	var hits []scored
	for i, p := range projects {
		if !a.passesFilters(p.Instance, p.PathWithNamespace) {
			continue
		}
		tags := a.cfg.TagsOf(p.Instance, p.PathWithNamespace)
		if !a.cfg.Filters.PassesTags(tags) {
			continue
		}
		hay := strings.Join(tags, " ") + " " + p.PathWithNamespace + " " + p.Name + " " + p.Description + " " + a.instanceLabel(p.Instance)
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
	p.kept.reset()
	grouped := a.cfg.Filters.GroupRepositories
	// Grouped, the server and the group move into the headings and each row
	// names only the repository.
	withServer := a.multiInstance() && !grouped
	favourite := func(idx int) bool {
		pr := a.projects[idx]
		return a.cfg.Filters.IsFavourite(pr.Instance, pr.PathWithNamespace, 0)
	}
	star := starColumn(filtered, favourite)
	name := func(pr forge.Project) string {
		if grouped {
			return repositoryName(pr.PathWithNamespace)
		}
		return pr.PathWithNamespace
	}

	branchW, actW, serverW, pathW, syncW := 6, 8, 0, 0, len("REMOTE")
	// MR is how many merge requests have a worktree on disk, after a mark
	// when the repository's merge requests are hidden (H here, x there).
	mrW, hiddenW := 2, 0
	sizeW := len("SIZE")
	// CI is the newest pipeline of the clone's branch, and takes room only
	// when some row has one.
	ciW := 0
	for _, idx := range filtered {
		pr := a.projects[idx]
		if a.repositoryCI(pr) != "" {
			ciW = 2
		}
		sizeW = max(sizeW, len([]rune(a.repoSizeWords(projectKey{pr.Instance, pr.PathWithNamespace}))))
		if a.cfg.Filters.HidesMRsOf(pr.Instance, pr.PathWithNamespace) {
			hiddenW = 1
		}
		words, _ := a.syncWords(projectKey{pr.Instance, pr.PathWithNamespace})
		syncW = max(syncW, len([]rune(words)))
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
	markW := 2 + star
	if grouped {
		markW++
	}
	const (
		editsW  = len("EDITS")
		wtW     = 2
		gaps    = 6
		minName = 20
	)
	fixed := markW + branchW + syncW + editsW + pathW + mrW + wtW + sizeW + 1 + actW + gaps + 2
	if hiddenW > 0 {
		fixed += hiddenW + 1
	}
	if ciW > 0 {
		fixed += ciW + 1
	}
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

	// The tags have a column of their own, right after the longest name; the
	// other columns keep their places at the end. Hiding the tags keeps the
	// chezmoi badge, which is not one.
	showTags := !a.cfg.Filters.HideTags
	tagsOf := func(pr forge.Project) []string {
		if !showTags {
			return nil
		}
		return a.cfg.TagsOf(pr.Instance, pr.PathWithNamespace)
	}
	managed := func(pr forge.Project) bool { return a.managedDir(pr.Instance, pr.PathWithNamespace) != "" }
	tagsW := 0
	longest, tagged := 0, false
	for _, idx := range filtered {
		pr := a.projects[idx]
		longest = max(longest, iconWidth(a.forgeIcon(pr.Instance))+len([]rune(name(pr))))
		tagged = tagged || len(tagsOf(pr)) > 0 || managed(pr) || pr.Starred
	}
	longest = atLeast(longest, "REPOSITORY")
	if tagged {
		// When it is tight the names keep two thirds of the room.
		names := min(longest, max(nameW*2/3, 10))
		if nameW-names-1 >= 4 {
			tagsW = nameW - names - 1
			nameW = names
		}
	}

	// The heat of a size is where it stands between the least and the most
	// a repository takes.
	least, most := a.repoSizeRange()
	header := []field{{text: "", width: markW, colour: role("repositories.header")}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: role("repositories.header")})
	}
	header = append(header,
		field{text: "REPOSITORY", width: nameW, colour: role("repositories.header")})
	if tagsW > 0 {
		header = append(header, field{text: "TAGS", width: tagsW, colour: role("repositories.header")})
	}
	header = append(header, field{text: "BRANCH", width: branchW, colour: role("repositories.header")})
	if ciW > 0 {
		header = append(header, field{text: "CI", width: ciW, colour: role("repositories.header")})
	}
	header = append(header,
		field{text: "REMOTE", width: syncW, colour: role("repositories.header")},
		field{text: "EDITS", width: editsW, colour: role("repositories.header"), right: true})
	if pathW > 0 {
		header = append(header, field{text: "PATH", width: pathW, colour: role("repositories.header")})
	}
	// The mark of hidden merge requests is a column of its own, without a
	// heading, so it stands in one column and the counts in another.
	if hiddenW > 0 {
		header = append(header, field{text: "", width: hiddenW, colour: role("repositories.header")})
	}
	header = append(header,
		field{text: "MR", width: mrW, colour: role("repositories.header"), right: true},
		field{text: "WT", width: wtW, colour: role("repositories.header"), right: true},
		field{text: "SIZE", width: sizeW, colour: role("repositories.header"), right: true},
		field{text: "ACTIVITY", width: actW, colour: role("repositories.header")})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).
		SetSelectable(false).SetExpansion(1))

	drawRow := func(row, idx int) {
		pr := a.projects[idx]
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)

		mark, markColour := " "+glyphRing, colDim
		if info.Cloned {
			mark, markColour = " "+glyphDot, colOn
		}
		// A marked row is told by its band alone; its mark still says what is
		// on disk.
		nameColour := colText
		if grouped {
			mark = " " + mark
		}
		branch, branchColour := info.Branch, role("repositories.branch")
		if branch == pr.DefaultBranch {
			branchColour = role("repositories.default_branch")
		}
		if !info.Cloned {
			branch, branchColour = pr.DefaultBranch, colDim
		}
		path := tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace))
		pathColour := colDim
		if info.Cloned {
			pathColour = role("repositories.path")
		}
		mrCount := ""
		if n := len(info.MRs); n > 0 {
			mrCount = fmt.Sprintf("%d", n)
		}
		mrField := field{text: mrCount, width: mrW, colour: role("repositories.mr"), right: true}
		hiddenMark := ""
		if a.cfg.Filters.HidesMRsOf(pr.Instance, pr.PathWithNamespace) {
			hiddenMark = glyphHidden
		}
		wtCount := ""
		if info.Worktrees > 0 {
			wtCount = fmt.Sprintf("%d", info.Worktrees)
		}

		fields := []field{{raw: starred(star, favourite(idx), tag(markColour)+mark+tagEnd)}}
		nameX := markW + 1
		if withServer {
			fields = append(fields, field{text: a.instanceLabel(pr.Instance), width: serverW, colour: role("repositories.server")})
			nameX += serverW + 1
		}
		fields = append(fields, field{icon: a.forgeIcon(pr.Instance), text: name(pr), width: nameW, colour: nameColour})
		if tagsW > 0 {
			var starred *forge.Project
			if pr.Starred {
				starred = &pr
			}
			tags, pills := a.tagsField(tagsOf(pr), tagsW, p.marks[idx], managed(pr), starred)
			pills.x += nameX + nameW + 1
			p.kept.keep(row, pills)
			fields = append(fields, field{raw: tags})
		}
		words, wordsColour := a.syncWords(projectKey{pr.Instance, pr.PathWithNamespace})
		fields = append(fields, field{text: branch, width: branchW, colour: branchColour})
		if ciW > 0 {
			ci, ciColour := ciMark(a.repositoryCI(pr))
			fields = append(fields, field{text: ci, width: ciW, colour: ciColour})
		}
		fields = append(fields,
			field{text: words, width: syncW, colour: wordsColour},
			field{text: a.projectEdits(projectKey{pr.Instance, pr.PathWithNamespace}), width: editsW, colour: role("repositories.edits"), right: true})
		if pathW > 0 {
			fields = append(fields, field{text: path, width: pathW, colour: pathColour})
		}
		if hiddenW > 0 {
			fields = append(fields, field{text: hiddenMark, width: hiddenW, colour: role("repositories.hidden")})
		}
		fields = append(fields,
			mrField,
			field{text: wtCount, width: wtW, colour: role("repositories.wt"), right: true},
			field{text: a.repoSizeWords(projectKey{pr.Instance, pr.PathWithNamespace}), width: sizeW,
				colour: heatColour(a.repoSize[projectKey{pr.Instance, pr.PathWithNamespace}], least, most, role("repositories.size")), right: true},
			field{text: humanAge(pr.LastActivityAt), width: actW, colour: role("repositories.activity")})

		cell := tview.NewTableCell(rowText(fields)).SetReference(idx).SetExpansion(1)
		if p.marks[idx] {
			cell.SetBackgroundColor(colMarked).SetSelectedStyle(styleMarkedSelected)
		}
		p.table.SetCell(row, 0, cell)
	}

	layout := listLayout{favourite: favourite, draw: drawRow, width: p.contentWidth()}
	if grouped {
		layout.group = func(idx int) (string, string) {
			pr := a.projects[idx]
			return headingKey(a, pr.Instance, namespaceOf(pr.PathWithNamespace))
		}
	}
	p.selectRow(previous, a.layRows(p.table, filtered, layout))
}

// markedProjects is the repositories picked with space, in the list's data
// order.
func (a *App) markedProjects() []forge.Project {
	var out []forge.Project
	for _, i := range a.projectsPane.marked() {
		if i < len(a.projects) {
			out = append(out, a.projects[i])
		}
	}
	return out
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

// openProject opens the editor in the main clone as it is on disk, cloning it
// first only when there is nothing to open yet.
func (a *App) openProject(pr forge.Project, ed *editors.Editor) {
	what := session.Record{
		Instance: pr.Instance,
		Server:   a.instanceLabel(pr.Instance),
		Project:  pr.PathWithNamespace,
		Mode:     session.ModeRepository,
	}
	if dir := a.projectDir(pr.Instance, pr.PathWithNamespace); workspace.Exists(dir) {
		a.openNow(dir, what, ed)
		return
	}
	a.runTaskOpening("Cloning "+pr.PathWithNamespace, what, ed, func(log func(string)) (string, error) {
		return a.newManager(pr.Instance, pr.PathWithNamespace, log).CloneProject(pr)
	})
}

// cloneProject keeps the task's directory empty so completion returns to the list.
func (a *App) cloneProject(pr forge.Project) {
	// Nothing to do is said in passing, not with a dialog that comes and
	// goes before it can be read.
	if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.note(pr.PathWithNamespace + " is already cloned, at " + tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))
		return
	}
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
	if dir := a.managedDir(pr.Instance, pr.PathWithNamespace); dir != "" {
		a.flash("chezmoi keeps this repository at " + tildePath(dir) + "; turn the integration off in Settings › Integrations to clone it elsewhere")
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
		a.done("Clone directory: " + tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))
	}
	form.AddButton("Save", func() { apply(form.GetFormItem(0).(*tview.InputField).GetText()) })
	form.AddButton("Inherit", func() { apply("") })
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("Clone directory · "+pr.PathWithNamespace, form, 12)
}

// forgeIcon is the icon of a repository's server, to go before its name in
// a list - "" where the terminal draws no Nerd Font icons. It is only drawn:
// what is copied, searched or sorted is the name alone.
func (a *App) forgeIcon(instance string) string {
	if inst := a.cfg.Instance(instance); inst != nil && inst.IsGitHub() {
		return glyphForgeGitHub
	}
	return glyphForgeGitLab
}

// iconWidth is what an icon and its space take in a column.
func iconWidth(icon string) int {
	if icon == "" {
		return 0
	}
	return len([]rune(icon)) + 1
}
