package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

// worktreeRow is one worktree made from Repositories for a branch of its own,
// as opposed to the ones a merge request gets.
type worktreeRow struct {
	Instance string
	Path     string // the repository, as the forge names it
	Branch   string // what the worktree has checked out now
	Dir      string
	Moved    time.Time // when its HEAD last moved
}

// project is the repository the worktree hangs off. The index has its clone
// addresses; without it, only the path is known.
func (a *App) worktreeProject(r worktreeRow) forge.Project {
	if pr, ok := a.projByKey[projectKey{r.Instance, r.Path}]; ok {
		return pr
	}
	return forge.Project{PathWithNamespace: r.Path, Instance: r.Instance}
}

// newWorktreesPane is the list of every branch worktree on disk, across all
// repositories.
func (a *App) newWorktreesPane() *pane {
	p := a.newPane("Worktrees")
	var filtered []int

	p.headline = func() string {
		return fmt.Sprintf("%s%d/%d worktrees · %s%s", tag(colMuted), len(filtered), len(a.worktrees),
			sortLabel(a.cfg.Filters.Order()), tagEnd)
	}

	render := func(query string) {
		filtered = a.filterWorktrees(query)
		a.drawWorktrees(p, filtered)
		p.updateHeader()
	}
	p.onQuery = render
	p.reload = func() { render(p.query) }

	selected := func() (worktreeRow, bool) {
		i := p.selectedIndex()
		if i < 0 || i >= len(a.worktrees) {
			return worktreeRow{}, false
		}
		return a.worktrees[i], true
	}

	p.onDetail = func(idx int, focus bool) {
		if idx >= 0 && idx < len(a.worktrees) {
			a.showWorktreeDetail(a.worktrees[idx], focus)
		}
	}
	p.onOpen = func() {
		if r, ok := selected(); ok {
			a.openWorktree(r)
		}
	}
	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() != tcell.KeyRune {
			return ev
		}
		switch ev.Rune() {
		case 'd':
			if r, ok := selected(); ok {
				a.confirmDeleteWorktreeEntry(a.worktreeProject(r), workspace.WorktreeEntry{
					Label: r.Branch, Kind: "branch", Dirs: []string{r.Dir}})
			}
			return nil
		case 'r':
			a.refreshDisk()
			a.note("looked at the disk again")
			return nil
		}
		return ev
	}
	return p
}

func (a *App) filterWorktrees(query string) []int {
	var hits []scored
	for i, r := range a.worktrees {
		if a.cfg.Filters.IsHidden(r.Instance, r.Path) {
			continue
		}
		hay := r.Path + " " + r.Branch + " " + a.instanceLabel(r.Instance)
		score, ok := fuzzy.Match(query, hay)
		if !ok {
			continue
		}
		hits = append(hits, scored{idx: i, score: score})
	}
	switch {
	case strings.TrimSpace(query) != "":
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	case a.cfg.Filters.Order() == config.SortName:
		sort.SliceStable(hits, func(i, j int) bool {
			l, r := a.worktrees[hits[i].idx], a.worktrees[hits[j].idx]
			if l.Path != r.Path {
				return l.Path < r.Path
			}
			return l.Branch < r.Branch
		})
	default:
		sort.SliceStable(hits, func(i, j int) bool {
			return a.worktrees[hits[i].idx].Moved.After(a.worktrees[hits[j].idx].Moved)
		})
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

func (a *App) drawWorktrees(p *pane, filtered []int) {
	previous := p.selectedIndex()
	p.table.Clear()
	withServer := a.multiInstance()

	repoW, branchW, serverW, pathW, actW := 10, 6, 0, 0, 8
	for _, idx := range filtered {
		r := a.worktrees[idx]
		repoW = max(repoW, len([]rune(r.Path)))
		branchW = max(branchW, len([]rune(r.Branch)))
		actW = max(actW, len(humanAge(r.Moved)))
		if withServer {
			serverW = max(serverW, len([]rune(a.instanceLabel(r.Instance))))
		}
		pathW = max(pathW, len([]rune(tildePath(r.Dir))))
	}
	repoW = atLeast(min(repoW, 48), "REPOSITORY")
	branchW = atLeast(min(branchW, 32), "BRANCH")
	actW = atLeast(actW, "ACTIVITY")
	if withServer {
		serverW = atLeast(min(serverW, 16), "SERVER")
	}
	if pathW > 0 {
		pathW = atLeast(min(pathW, 44), "PATH")
	}

	const (
		markW   = 2
		gaps    = 4
		minRepo = 20
	)
	room := p.contentWidth()
	fixed := markW + branchW + actW + gaps
	if withServer {
		fixed += serverW + 1
	}
	// The directory is what goes first when the row is tight, whole: half a
	// path says nothing, and the repository and branch already say which row it is.
	if pathW > 0 && room-fixed-pathW-1 >= minRepo {
		fixed += pathW + 1
	} else {
		pathW = 0
	}
	repoW = atLeast(max(min(repoW, room-fixed), 10), "REPOSITORY")

	header := []field{{text: "", width: markW, colour: colDim}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: colDim})
	}
	header = append(header,
		field{text: "REPOSITORY", width: repoW, colour: colDim},
		field{text: "BRANCH", width: branchW, colour: colDim})
	if pathW > 0 {
		header = append(header, field{text: "PATH", width: pathW, colour: colDim})
	}
	header = append(header, field{text: "ACTIVITY", width: actW, colour: colDim})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).SetSelectable(false).SetExpansion(1))

	for row, idx := range filtered {
		r := a.worktrees[idx]
		fields := []field{{raw: tag(colOn) + " ●" + tagEnd}}
		if withServer {
			fields = append(fields, field{text: a.instanceLabel(r.Instance), width: serverW, colour: colAccent})
		}
		fields = append(fields,
			field{text: r.Path, width: repoW, colour: colText},
			field{text: r.Branch, width: branchW, colour: colBranch})
		if pathW > 0 {
			fields = append(fields, field{text: tildePath(r.Dir), width: pathW, colour: colMuted})
		}
		fields = append(fields, field{text: humanAge(r.Moved), width: actW, colour: colMuted})
		p.table.SetCell(row+1, 0, tview.NewTableCell(rowText(fields)).SetReference(idx).SetExpansion(1))
	}

	first := 0
	if len(filtered) > 0 {
		first = 1
	}
	p.selectRow(previous, first)
}

// openWorktree brings a worktree up to date and opens the editor in it.
func (a *App) openWorktree(r worktreeRow) {
	pr := a.worktreeProject(r)
	a.runTaskOpening(fmt.Sprintf("Opening %s (%s)", r.Path, r.Branch),
		session.Record{
			Instance: r.Instance,
			Server:   a.instanceLabel(r.Instance),
			Project:  r.Path,
			Title:    r.Branch,
			Mode:     session.ModeBranch,
		}, func(log func(string)) (string, error) {
			log("Updating " + r.Branch)
			return a.newManager(pr.Instance, pr.PathWithNamespace, log).UpdateWorktree(r.Dir)
		})
}

// showWorktreeDetail fills the detail column with the state of one worktree.
// What git says takes a moment, so it comes in after the outline.
func (a *App) showWorktreeDetail(r worktreeRow, focus bool) {
	p := a.worktreesPane
	p.detailSeq++
	seq := p.detailSeq
	title := r.Path + " · " + r.Branch

	outline := func(state, commits string) string {
		d := &detailBuf{}
		d.title(r.Branch)
		d.sub(r.Path)
		d.section("Worktree")
		if a.multiInstance() {
			d.kv("Server", esc(a.instanceLabel(r.Instance)))
		}
		d.kv("Repository", esc(r.Path))
		d.kv("Branch", tag(colBranch)+esc(r.Branch)+tagEnd)
		d.kv("Directory", esc(tildePath(r.Dir)))
		if !r.Moved.IsZero() {
			d.kv("Last moved", humanAge(r.Moved))
		}
		d.kv("State", state)
		if commits != "" {
			d.section("Latest commits")
			for _, line := range strings.Split(strings.TrimRight(commits, "\n"), "\n") {
				d.raw("  " + esc(line) + "\n")
			}
		}
		return d.String()
	}
	p.openDetail(title, outline(tag(colMuted)+"looking …"+tagEnd, ""), focus)

	mgr := a.newManager(r.Instance, r.Path, nil)
	go func() {
		git := mgr.Git()
		state := tag(colOn) + "clean, nothing to push" + tagEnd
		if s := git.Status(r.Dir).Describe(); s != "" {
			state = tag(colWarn) + esc(s) + tagEnd
		}
		// A directory git cannot read has nothing to list, and its error is not a commit.
		commits, err := git.Run(r.Dir, "log", "-8", "--format=%h  %s  (%cr)")
		if err != nil {
			commits = ""
		}
		a.tv.QueueUpdateDraw(func() {
			if p.detailSeq != seq {
				return
			}
			p.setDetail(title, outline(state, commits))
		})
	}()
}
