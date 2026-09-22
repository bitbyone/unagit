package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/gitlab"
)

// projFiltered holds the indexes into App.projects currently shown.
type scored struct {
	idx   int
	score int
}

func (a *App) newProjectsPane() *pane {
	p := a.newPane("Projects")
	var filtered []int

	p.headline = func() string {
		age := "never refreshed"
		if !a.projUpdated.IsZero() {
			age = "indexed " + humanAge(a.projUpdated)
		}
		return fmt.Sprintf("%s%d/%d projects · %s · root %s%s",
			tag(colMuted), len(filtered), len(a.projects), age, tildePath(a.cfg.Root()), tagEnd)
	}

	render := func(query string) {
		filtered = a.filterProjects(a.projects, query)
		a.drawProjects(p, filtered)
		p.updateHeader()
	}
	p.onQuery = render

	selected := func() (gitlab.Project, bool) {
		i := p.selectedIndex()
		if i < 0 || i >= len(a.projects) {
			return gitlab.Project{}, false
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

	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
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

func (a *App) filterProjects(projects []gitlab.Project, query string) []int {
	var hits []scored
	for i, p := range projects {
		hay := p.PathWithNamespace + " " + p.Name + " " + p.Description + " " + a.instanceLabel(p.Instance)
		score, ok := fuzzy.Match(query, hay)
		if !ok {
			continue
		}
		hits = append(hits, scored{idx: i, score: score})
	}
	if strings.TrimSpace(query) != "" {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

func (a *App) drawProjects(p *pane, filtered []int) {
	p.table.Clear()
	withServer := a.multiInstance()
	headers := []string{"", "PROJECT", "BRANCH", "MR", "ACTIVITY"}
	if withServer {
		headers = []string{"", "SERVER", "PROJECT", "BRANCH", "MR", "ACTIVITY"}
	}
	p.setHeaders(headers...)

	branchW, actW, serverW := 6, 8, 0
	for _, idx := range filtered {
		pr := a.projects[idx]
		branch := a.diskOf(pr.Instance, pr.PathWithNamespace).Branch
		if branch == "" {
			branch = pr.DefaultBranch
		}
		branchW = max(branchW, len([]rune(branch)))
		actW = max(actW, len(humanAge(pr.LastActivityAt)))
		if withServer {
			serverW = max(serverW, len([]rune(a.instanceLabel(pr.Instance))))
		}
	}
	branchW = min(branchW, 28)
	serverW = min(serverW, 16)
	// mark + gaps + the MR count column
	fixed := 2 + branchW + 3 + actW + len(headers)
	if withServer {
		fixed += serverW
	}
	projW := max(p.contentWidth()-fixed, 20)

	for row, idx := range filtered {
		pr := a.projects[idx]
		info := a.diskOf(pr.Instance, pr.PathWithNamespace)

		mark := tview.NewTableCell(" ○").SetTextColor(colDim)
		if info.Cloned {
			mark = tview.NewTableCell(" ●").SetTextColor(colOn)
		}
		mark.SetReference(idx)

		branch, branchColor := info.Branch, colBranch
		if !info.Cloned {
			branch, branchColor = pr.DefaultBranch, colDim
		}
		mrCount := ""
		if n := len(info.MRs); n > 0 {
			mrCount = fmt.Sprintf("%d", n)
		}

		col := 0
		set := func(cell *tview.TableCell) {
			p.table.SetCell(row+1, col, cell)
			col++
		}
		set(mark)
		if withServer {
			set(tview.NewTableCell(trunc(a.instanceLabel(pr.Instance), serverW)).SetTextColor(colAccent))
		}
		set(tview.NewTableCell(trunc(pr.PathWithNamespace, projW)).SetTextColor(colText))
		set(tview.NewTableCell(trunc(branch, branchW)).SetTextColor(branchColor))
		set(tview.NewTableCell(mrCount).SetTextColor(colWarn))
		set(tview.NewTableCell(humanAge(pr.LastActivityAt)).SetTextColor(colMuted))
		p.fill(row+1, col)
	}
	if len(filtered) > 0 {
		p.table.Select(1, 0)
	}
	p.table.ScrollToBeginning()
}

// openProject clones or updates the main checkout and opens the editor.
func (a *App) openProject(pr gitlab.Project) {
	a.runTask("Opening "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		return a.newManager(pr.Instance, pr.PathWithNamespace, log).EnsureProject(pr)
	})
}
