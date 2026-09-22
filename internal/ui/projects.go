package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
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
			tag(colMuted), len(filtered), len(a.projects), age, a.filterSummary(), tagEnd)
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
	p.onOpen = func() {
		if pr, ok := selected(); ok {
			a.openProject(pr)
		}
	}

	shared := a.filterKeysFor(p)
	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
		if shared(ev) {
			return nil
		}
		if ev.Key() != tcell.KeyRune {
			return ev
		}
		switch ev.Rune() {
		case 'b':
			if pr, ok := selected(); ok {
				a.showBranchPicker(pr)
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
				a.confirmDeleteProject(pr)
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
	withServer := a.multiInstance()

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
		if info.Cloned {
			pathW = max(pathW, len([]rune(tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace)))))
		}
	}
	branchW = atLeast(min(branchW, 24), "BRANCH")
	actW = atLeast(actW, "ACTIVITY")
	if withServer {
		serverW = atLeast(min(serverW, 16), "SERVER")
	}
	if pathW > 0 {
		pathW = atLeast(min(pathW, 44), "PATH")
	}

	const (
		markW   = 2
		mrW     = 2
		gaps    = 5
		minName = 20
	)
	fixed := markW + branchW + pathW + mrW + actW + gaps
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
		field{text: "ACTIVITY", width: actW, colour: colDim})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).
		SetSelectable(false).SetExpansion(1))

	row := 0
	for _, idx := range filtered {
		row++
		pr := a.projects[idx]
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)

		mark, markColour := " ○", colDim
		if info.Cloned {
			mark, markColour = " ●", colOn
		}
		branch, branchColour := info.Branch, colBranch
		if !info.Cloned {
			branch, branchColour = pr.DefaultBranch, colDim
		}
		path := ""
		if info.Cloned {
			path = tildePath(a.projectDir(pr.Instance, pr.PathWithNamespace))
		}
		mrCount := ""
		if n := len(info.MRs); n > 0 {
			mrCount = fmt.Sprintf("%d", n)
		}

		fields := []field{{raw: tag(markColour) + mark + tagEnd}}
		if withServer {
			fields = append(fields, field{text: a.instanceLabel(pr.Instance), width: serverW, colour: colAccent})
		}
		fields = append(fields,
			field{text: pr.PathWithNamespace, width: nameW, colour: colText},
			field{text: branch, width: branchW, colour: branchColour})
		if pathW > 0 {
			fields = append(fields, field{text: path, width: pathW, colour: colMuted})
		}
		fields = append(fields,
			field{text: mrCount, width: mrW, colour: colWarn, right: true},
			field{text: humanAge(pr.LastActivityAt), width: actW, colour: colMuted})

		p.table.SetCell(row, 0, tview.NewTableCell(rowText(fields)).
			SetReference(idx).SetExpansion(1))
	}

	first := 0
	if len(filtered) > 0 {
		first = 1
	}
	p.selectRow(previous, first)
}

// openProject clones or updates the main checkout and opens the editor.
func (a *App) openProject(pr forge.Project) {
	a.runTask("Opening "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		return a.newManager(pr.Instance, pr.PathWithNamespace, log).EnsureProject(pr)
	})
}
