package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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
		scope := tag(colMuted) + "all projects" + tagEnd
		if a.mrProjectScope.Path != "" {
			scope = tag(colWarn) + a.mrProjectScope.Path + tagEnd
		}
		return fmt.Sprintf("%s%d/%d merge requests · %s · scope %s",
			tag(colMuted), len(filtered), len(a.mrs), age, tagEnd+scope)
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

	// Enter loads fresh detail from the API, Ctrl-O creates the worktree. Once
	// the column is open it follows the cursor.
	p.onDetail = func(idx int, focus bool) {
		if idx >= 0 && idx < len(a.mrs) {
			a.showMRDetail(a.mrs[idx], focus)
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
			a.mrProjectScope = projectKey{}
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
		case 'v':
			if mr, ok := selected(); ok {
				a.openMRReview(mr)
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
		key := projectKey{Instance: mr.Instance, Path: path}
		if a.mrProjectScope.Path != "" && key != a.mrProjectScope {
			continue
		}
		hay := fmt.Sprintf("%s %s !%d %s %s %s %s", a.instanceLabel(mr.Instance), path, mr.IID,
			mr.Title, mr.Author.Username, mr.SourceBranch, mr.TargetBranch)
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
	withServer := a.multiInstance()
	headers := []string{"", "PROJECT", "MR", "TITLE", "AUTHOR", "BRANCH", "UPDATED"}
	if withServer {
		headers = []string{"", "SERVER", "PROJECT", "MR", "TITLE", "AUTHOR", "BRANCH", "UPDATED"}
	}
	p.setHeaders(headers...)

	serverW := 0
	if withServer {
		for _, idx := range filtered {
			serverW = max(serverW, len([]rune(a.instanceLabel(a.mrs[idx].Instance))))
		}
		serverW = min(serverW, 16)
	}
	c := a.mrColumns(p.contentWidth()-serverW, filtered)

	for row, idx := range filtered {
		mr := a.mrs[idx]
		path := a.projectPathOfMR(mr)
		disk := a.diskOf(mr.Instance, path).MRs[mr.IID]

		mark := tview.NewTableCell(" " + mrMark(disk)).SetTextColor(mrMarkColor(disk))
		mark.SetReference(idx)

		title := trunc(mr.Title, c.title)
		if mr.Draft {
			title = "[::d]draft[::-] " + tview.Escape(trunc(mr.Title, c.title-6))
		} else {
			title = tview.Escape(title)
		}

		col := 0
		set := func(cell *tview.TableCell) {
			p.table.SetCell(row+1, col, cell)
			col++
		}
		set(mark)
		if withServer {
			set(tview.NewTableCell(trunc(a.instanceLabel(mr.Instance), serverW)).SetTextColor(colAccent))
		}
		set(tview.NewTableCell(trunc(path, c.proj)).SetTextColor(colAccent))
		set(tview.NewTableCell(fmt.Sprintf("!%d", mr.IID)).SetTextColor(colWarn))
		set(tview.NewTableCell(title).SetTextColor(colText))
		set(tview.NewTableCell(trunc(mr.Author.Username, c.author)).SetTextColor(colMuted))
		set(tview.NewTableCell(trunc(mr.SourceBranch, c.branch)).SetTextColor(colBranch))
		set(tview.NewTableCell(humanAge(mr.UpdatedAt)).SetTextColor(colMuted))
		p.fill(row+1, col)
	}
	if len(filtered) > 0 {
		p.table.Select(1, 0)
	}
	p.table.ScrollToBeginning()
}

// mrMark shows at a glance which worktrees a merge request has on disk.
func mrMark(d mrDisk) string {
	switch {
	case d.Branch && d.Review:
		return "◉"
	case d.Review:
		return "◐"
	case d.Branch:
		return "●"
	}
	return "○"
}

func mrMarkColor(d mrDisk) tcell.Color {
	if d.Branch || d.Review {
		return colOn
	}
	return colDim
}

// openMR materialises the merge request worktree and opens the editor there.
func (a *App) openMR(mr gitlab.MergeRequest) {
	path, httpURL := a.mrOrigin(mr)
	a.runTask(fmt.Sprintf("Opening %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		return a.newManager(mr.Instance, path, log).EnsureMR(mr, path, httpURL)
	})
}

// openMRReview prepares the review worktree, where the merge request shows up
// as pending changes rather than as a stack of commits. The diff base comes
// from the API, so it is the very commit GitLab renders its own diff against.
func (a *App) openMRReview(mr gitlab.MergeRequest) {
	path, httpURL := a.mrOrigin(mr)
	client := a.client(mr.Instance)
	a.runTask(fmt.Sprintf("Opening %s !%d for review", path, mr.IID), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var rev workspace.Review
		if client == nil {
			log("! no token for this server, falling back to the local merge base")
		} else {
			log("Asking GitLab what this merge request is diffed against ...")
			if det, err := client.MergeRequest(ctx, mr.ProjectID, mr.IID); err != nil {
				log("! " + err.Error())
				log("  falling back to the local merge base")
			} else {
				rev = workspace.Review{BaseSHA: det.DiffRefs.BaseSHA, HeadSHA: det.DiffRefs.HeadSHA}
			}
		}
		return a.newManager(mr.Instance, path, log).EnsureMRReview(mr, path, httpURL, rev)
	})
}

// mrOrigin resolves where a merge request's project lives.
func (a *App) mrOrigin(mr gitlab.MergeRequest) (path, httpURL string) {
	path = a.projectPathOfMR(mr)
	if pr, ok := a.projByKey[projectKey{mr.Instance, path}]; ok {
		httpURL = pr.HTTPURLToRepo
	}
	return path, httpURL
}

func openBrowser(url string) error { return workspace.OpenBrowser(url) }
