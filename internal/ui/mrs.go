package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/workspace"
)

func (a *App) newMRsPane() *pane {
	p := a.newPane("Merge requests")
	var filtered []int

	p.headline = func() string {
		age := "never refreshed"
		if !a.mrsUpdated.IsZero() {
			age = "indexed " + humanAge(a.mrsUpdated)
		}
		scope := "[darkgray]all projects[-]"
		if a.mrProjectScope != "" {
			scope = "[orange]" + a.mrProjectScope + "[-]"
		}
		return fmt.Sprintf("[darkgray]%d/%d merge requests  %s  scope:[-] %s", len(filtered), len(a.mrs), age, scope)
	}

	render := func(query string) {
		filtered = a.filterMRs(query)
		a.drawMRs(p, filtered)
		p.updateHeader()
	}
	p.onQuery = render

	selected := func() (gitlab.MergeRequest, bool) {
		i := p.selectedIndex()
		if i < 0 || i >= len(a.mrs) {
			return gitlab.MergeRequest{}, false
		}
		return a.mrs[i], true
	}

	p.onEnter = func() {
		if mr, ok := selected(); ok {
			a.openMR(mr)
		}
	}

	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyTab {
			a.show(pageProjects)
			a.tv.SetFocus(a.projectsPane.table)
			return nil
		}
		if ev.Key() != tcell.KeyRune {
			return ev
		}
		switch ev.Rune() {
		case 'p':
			a.showProjectScopePicker()
			return nil
		case 'P':
			a.mrProjectScope = ""
			p.reload()
			a.setStatus("project scope cleared")
			return nil
		case 'd':
			if mr, ok := selected(); ok {
				a.confirmDeleteMR(mr)
			}
			return nil
		case 'w':
			if mr, ok := selected(); ok && mr.WebURL != "" {
				_ = openBrowser(mr.WebURL)
				a.flash("opened " + mr.WebURL)
			}
			return nil
		case 'r':
			a.refreshMRs()
			return nil
		case 's':
			a.show(pageSettings)
			a.tv.SetFocus(a.settings.tree)
			return nil
		}
		return ev
	}

	p.reload = func() { render(p.query) }
	return p
}

func (a *App) filterMRs(query string) []int {
	var hits []scored
	for i, mr := range a.mrs {
		path := a.projectPathOfMR(mr)
		if a.mrProjectScope != "" && path != a.mrProjectScope {
			continue
		}
		hay := fmt.Sprintf("%s !%d %s %s %s %s", path, mr.IID, mr.Title, mr.Author.Username, mr.SourceBranch, mr.TargetBranch)
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

func (a *App) drawMRs(p *pane, filtered []int) {
	p.table.Clear()
	p.setHeaders("", "PROJECT", "MR", "TITLE", "AUTHOR", "BRANCH", "UPDATED")
	for row, idx := range filtered {
		mr := a.mrs[idx]
		path := a.projectPathOfMR(mr)

		mark := tview.NewTableCell(" ○").SetTextColor(tcell.ColorDimGray)
		if _, ok := a.disk[path].MRs[mr.IID]; ok {
			mark = tview.NewTableCell(" ●").SetTextColor(tcell.ColorGreen)
		}
		mark.SetReference(idx)

		title := mr.Title
		if mr.Draft {
			title = "[gray]draft[-] " + title
		}

		p.table.SetCell(row+1, 0, mark)
		p.table.SetCell(row+1, 1, tview.NewTableCell(path).SetTextColor(tcell.ColorDarkCyan).SetMaxWidth(34))
		p.table.SetCell(row+1, 2, tview.NewTableCell(fmt.Sprintf("!%d", mr.IID)).SetTextColor(tcell.ColorOrange))
		p.table.SetCell(row+1, 3, tview.NewTableCell(tview.Escape(title)).SetExpansion(1))
		p.table.SetCell(row+1, 4, tview.NewTableCell(mr.Author.Username).SetTextColor(tcell.ColorGray).SetMaxWidth(16))
		p.table.SetCell(row+1, 5, tview.NewTableCell(mr.SourceBranch).SetTextColor(tcell.ColorSteelBlue).SetMaxWidth(28))
		p.table.SetCell(row+1, 6, tview.NewTableCell(humanAge(mr.UpdatedAt)).SetTextColor(tcell.ColorGray))
	}
	if len(filtered) > 0 {
		p.table.Select(1, 0)
	}
	p.table.ScrollToBeginning()
}

// openMR materialises the merge request worktree and opens the editor there.
func (a *App) openMR(mr gitlab.MergeRequest) {
	path := a.projectPathOfMR(mr)
	httpURL := ""
	if pr, ok := a.projByPath[path]; ok {
		httpURL = pr.HTTPURLToRepo
	}
	a.runTask(fmt.Sprintf("Opening %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		return a.newManager(log).EnsureMR(mr, path, httpURL)
	})
}

func openBrowser(url string) error { return workspace.OpenBrowser(url) }
