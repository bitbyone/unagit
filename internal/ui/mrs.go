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

	// Enter loads fresh detail from the API, Ctrl-O creates the worktree.
	p.onEnter = func() {
		if mr, ok := selected(); ok {
			a.showMRDetail(mr)
		}
	}
	p.onOpen = func() {
		if mr, ok := selected(); ok {
			a.openMR(mr)
		}
	}

	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() != tcell.KeyRune {
			return ev
		}
		switch ev.Rune() {
		case 'f':
			a.showProjectScopePicker()
			return nil
		case 'F':
			a.mrProjectScope = ""
			p.reload()
			a.note("project filter cleared")
			return nil
		case 'd':
			if mr, ok := selected(); ok {
				a.confirmDeleteMR(mr)
			}
			return nil
		case 'w':
			if mr, ok := selected(); ok && mr.WebURL != "" {
				_ = openBrowser(mr.WebURL)
				a.note("opened " + mr.WebURL)
			}
			return nil
		case 'r':
			a.refreshMRs()
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

// mrColumns works out how wide each column may be for the current table width.
// The title takes whatever is left, and every cell is truncated to fit, so the
// branch column never falls off the right edge.
type mrColumns struct{ proj, iid, title, author, branch, updated int }

func (a *App) mrColumns(width int, rows []int) mrColumns {
	c := mrColumns{iid: 3, updated: 8}
	for _, idx := range rows {
		mr := a.mrs[idx]
		c.proj = max(c.proj, len(a.projectPathOfMR(mr)))
		c.iid = max(c.iid, len(fmt.Sprintf("!%d", mr.IID)))
		c.author = max(c.author, len(mr.Author.Username))
		c.branch = max(c.branch, len([]rune(mr.SourceBranch)))
		c.updated = max(c.updated, len(humanAge(mr.UpdatedAt)))
	}
	c.proj = min(c.proj, 34)
	c.author = min(c.author, 14)
	c.branch = min(c.branch, 26)

	const (
		markW    = 2
		gaps     = 6
		minTitle = 24
	)
	fixed := func() int { return markW + c.proj + c.iid + c.author + c.branch + c.updated + gaps }
	c.title = width - fixed()
	// Give the title room by shrinking the least important columns first.
	for _, shrink := range []struct {
		col *int
		min int
	}{{&c.branch, 10}, {&c.proj, 16}, {&c.author, 8}} {
		if c.title >= minTitle {
			break
		}
		give := min(*shrink.col-shrink.min, minTitle-c.title)
		if give > 0 {
			*shrink.col -= give
			c.title += give
		}
	}
	c.title = max(c.title, 10)
	return c
}

func (a *App) drawMRs(p *pane, filtered []int) {
	p.table.Clear()
	p.setHeaders("", "PROJECT", "MR", "TITLE", "AUTHOR", "BRANCH", "UPDATED")
	c := a.mrColumns(p.contentWidth(), filtered)

	for row, idx := range filtered {
		mr := a.mrs[idx]
		path := a.projectPathOfMR(mr)

		mark := tview.NewTableCell(" ○").SetTextColor(colDim)
		if _, ok := a.disk[path].MRs[mr.IID]; ok {
			mark = tview.NewTableCell(" ●").SetTextColor(colOn)
		}
		mark.SetReference(idx)

		title := trunc(mr.Title, c.title)
		if mr.Draft {
			title = trunc(mr.Title, c.title-6)
			title = "[::d]draft[::-] " + tview.Escape(title)
		} else {
			title = tview.Escape(title)
		}

		p.table.SetCell(row+1, 0, mark)
		p.table.SetCell(row+1, 1, tview.NewTableCell(trunc(path, c.proj)).SetTextColor(colAccent))
		p.table.SetCell(row+1, 2, tview.NewTableCell(fmt.Sprintf("!%d", mr.IID)).SetTextColor(colWarn))
		p.table.SetCell(row+1, 3, tview.NewTableCell(title).SetTextColor(colText))
		p.table.SetCell(row+1, 4, tview.NewTableCell(trunc(mr.Author.Username, c.author)).SetTextColor(colMuted))
		p.table.SetCell(row+1, 5, tview.NewTableCell(trunc(mr.SourceBranch, c.branch)).SetTextColor(colBranch))
		p.table.SetCell(row+1, 6, tview.NewTableCell(humanAge(mr.UpdatedAt)).SetTextColor(colMuted))
		p.fill(row+1, 7)
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
