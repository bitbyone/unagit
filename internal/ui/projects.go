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
		return fmt.Sprintf("[darkgray]%d/%d projects  %s  root: %s[-]", len(filtered), len(a.projects), age, a.cfg.RootDir)
	}

	render := func(query string) {
		filtered = filterProjects(a.projects, query)
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

	// Enter loads the detail column, Ctrl-O does the actual checkout.
	p.onEnter = func() {
		if pr, ok := selected(); ok {
			a.showProjectDetail(pr)
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
				a.mrProjectScope = pr.PathWithNamespace
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

func filterProjects(projects []gitlab.Project, query string) []int {
	var hits []scored
	for i, p := range projects {
		hay := p.PathWithNamespace + " " + p.Name + " " + p.Description
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
	p.setHeaders("", "PROJECT", "BRANCH", "MR", "ACTIVITY")
	for row, idx := range filtered {
		pr := a.projects[idx]
		info := a.disk[pr.PathWithNamespace]

		mark := tview.NewTableCell(" ○").SetTextColor(tcell.ColorDimGray)
		if info.Cloned {
			mark = tview.NewTableCell(" ●").SetTextColor(tcell.ColorGreen)
		}
		mark.SetReference(idx)

		branch := info.Branch
		if !info.Cloned {
			branch = pr.DefaultBranch
		}
		branchCell := tview.NewTableCell(branch).SetTextColor(tcell.ColorSteelBlue)
		if !info.Cloned {
			branchCell.SetTextColor(tcell.ColorDimGray)
		}

		mrCount := ""
		if n := len(info.MRs); n > 0 {
			mrCount = fmt.Sprintf("%d", n)
		}

		p.table.SetCell(row+1, 0, mark)
		p.table.SetCell(row+1, 1, tview.NewTableCell(pr.PathWithNamespace).SetExpansion(1))
		p.table.SetCell(row+1, 2, branchCell)
		p.table.SetCell(row+1, 3, tview.NewTableCell(mrCount).SetTextColor(tcell.ColorOrange))
		p.table.SetCell(row+1, 4, tview.NewTableCell(humanAge(pr.LastActivityAt)).SetTextColor(tcell.ColorGray))
	}
	if len(filtered) > 0 {
		p.table.Select(1, 0)
	}
	p.table.ScrollToBeginning()
}

// openProject clones or updates the main checkout and opens the editor.
func (a *App) openProject(pr gitlab.Project) {
	a.runTask("Opening "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		return a.newManager(log).EnsureProject(pr)
	})
}
