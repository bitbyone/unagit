package ui

import (
	"fmt"
	"math"
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

	// The tags have a column of their own, right after the names. Hiding the
	// tags keeps the chezmoi badge, which is not one.
	showTags := !a.cfg.Filters.HideTags
	tagsOf := func(pr forge.Project) []string {
		if !showTags {
			return nil
		}
		return a.cfg.TagsOf(pr.Instance, pr.PathWithNamespace)
	}
	managed := func(pr forge.Project) bool { return a.managedDir(pr.Instance, pr.PathWithNamespace) != "" }
	// tagsWidth is what a row's tags take with every pill whole: the badge
	// at its longest when badge has room for it, a space, the pills. With
	// a badge as narrow as its glyph, it is what the tags need to say all
	// they say; the badge's words are worth room only after that.
	tagsWidth := func(pr forge.Project, badge int) int {
		_, w := a.pills(tagsOf(pr), math.MaxInt, behindList)
		bw := 0
		switch {
		case managed(pr):
			_, bw = chezmoiBadge(badge, a.cfg.Ends(), behindList)
		case pr.Starred:
			_, bw = a.starredBadge(pr, badge, a.cfg.Ends(), behindList)
		}
		if bw > 0 && w > 0 {
			w++
		}
		return w + bw
	}

	actW, syncW, editsW := len("ACTIVITY"), len("RMT"), cells(glyphEdits)
	// MR is how many merge requests have a worktree on disk, after a mark
	// when the repository's merge requests are hidden (H here, x there).
	mrW, hiddenW := 2, 0
	sizeW := len("SIZE")
	// CI is the newest pipeline of the clone's branch, and takes room only
	// when some row has one.
	ciW := 0
	var names, branches, servers, tagged, tagsShort []int
	var paths []string
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
		editsW = max(editsW, len(a.projectEdits(projectKey{pr.Instance, pr.PathWithNamespace})))
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)
		branch := info.Branch
		if branch == "" {
			branch = pr.DefaultBranch
		}
		branches = append(branches, len([]rune(branch)))
		actW = max(actW, len(humanAge(pr.LastActivityAt)))
		if withServer {
			servers = append(servers, len([]rune(a.instanceLabel(pr.Instance))))
		}
		paths = append(paths, tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))
		names = append(names, iconWidth(a.forgeIcon(pr.Instance))+len([]rune(name(pr))))
		if w := tagsWidth(pr, math.MaxInt); w > 0 {
			tagged = append(tagged, w)
			tagsShort = append(tagsShort, tagsWidth(pr, 3))
		}
	}

	// Grouped, every row is indented one step under its heading.
	markW := 2 + star
	if grouped {
		markW++
	}
	// The name is what the row is, so it minds a cut the most, and the
	// tags the user chose come next; the directory is said elsewhere too
	// (the detail), so it gives way first and is the first left out when
	// the row is tight, the server after it, then the tags.
	nameCol := flexColumn("REPOSITORY", names, 20, 2)
	branchCol := flexColumn("BRANCH", branches, 10, 1)
	pathCol := gistColumn("PATH", paths, minPath, 0.8)
	pathCol.drop = 1
	cols := []*listColumn{fixedColumn(markW), nameCol, branchCol, pathCol,
		fixedColumn(syncW), fixedColumn(editsW), fixedColumn(mrW), fixedColumn(2), fixedColumn(sizeW), fixedColumn(actW)}
	serverCol, tagsCol := &listColumn{}, &listColumn{}
	if withServer {
		serverCol = flexColumn("SERVER", servers, 6, 0.5)
		serverCol.drop = 2
		cols = append(cols, serverCol)
	}
	if len(tagged) > 0 {
		// A row without tags takes none of the column.
		for range len(filtered) - len(tagged) {
			tagged, tagsShort = append(tagged, 0), append(tagsShort, 0)
		}
		tagsCol = flexColumn("TAGS", tagged, 4, 1.5)
		_, short := spread(tagsShort)
		tagsCol.ideal = max(tagsCol.floor, short)
		tagsCol.drop = 3
		cols = append(cols, tagsCol)
	}
	if hiddenW > 0 {
		cols = append(cols, fixedColumn(hiddenW))
	}
	if ciW > 0 {
		cols = append(cols, fixedColumn(ciW))
	}
	// One cell stays free, so the last column does not touch the frame.
	spare := layoutColumns(p.contentWidth()-1, cols...)
	// What is left over keeps the columns after the names at the right
	// edge: it widens the tags when there are any, the names when not.
	if tagsCol.shown() {
		tagsCol.width += spare
	} else {
		nameCol.width += spare
	}
	nameW, branchW, pathW, serverW, tagsW := nameCol.width, branchCol.width, pathCol.width, serverCol.width, tagsCol.width
	withServer = withServer && serverCol.shown()
	const wtW = 2

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
		field{text: "RMT", width: syncW, colour: role("repositories.header")},
		field{text: glyphEdits, width: editsW, colour: role("repositories.header"), right: true})
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
		nameColour := role("repositories.name")
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
		fields = append(fields, field{icon: a.forgeIcon(pr.Instance), text: name(pr), width: nameW, colour: nameColour, shorten: shortenRepo})
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
		fields = append(fields, field{text: branch, width: branchW, colour: branchColour, shorten: shortenBranch})
		if ciW > 0 {
			ci, ciColour := ciMark(a.repositoryCI(pr))
			fields = append(fields, field{text: ci, width: ciW, colour: ciColour})
		}
		fields = append(fields,
			field{text: words, width: syncW, colour: wordsColour},
			field{text: a.projectEdits(projectKey{pr.Instance, pr.PathWithNamespace}), width: editsW, colour: role("repositories.edits"), right: true})
		if pathW > 0 {
			fields = append(fields, field{text: path, width: pathW, colour: pathColour, shorten: shortenPath})
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
