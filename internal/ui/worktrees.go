package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/incomm"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/session"
)

// worktreeRow is one worktree made from Repositories for a branch of its own,
// as opposed to the ones a merge request gets.
type worktreeRow struct {
	Instance string
	Path     string // the repository, as the forge names it
	Branch   string // what the worktree has checked out now
	Dir      string
	Moved    time.Time // when its HEAD last moved
	// Base is the branch a group member was made from, as the group noted it;
	// the repository's own note wins when there is one.
	Base string
	// Group is the folder of the grouped worktree a member belongs to, which
	// holds its Incomm comments; "" for a worktree of its own.
	Group string
	// Members is set for a grouped worktree: a row for each repository in it.
	// Path is then the group's name, Branch the branch they all share or "",
	// and Dir the directory holding them.
	Members []worktreeRow
}

// grouped tells a grouped worktree from the worktree of one repository.
func (r worktreeRow) grouped() bool { return r.Members != nil }

// remoteState is where a worktree's branch stands against origin. A worktree with
// no entry yet is still being looked at.
type remoteState struct {
	Unreadable bool // git could not be asked: the clone is gone or broken
	Detached   bool
	// From is the branch a detached HEAD came from and FromBehind how many
	// of its commits - origin's copy's, when there is one - HEAD lacks.
	From       string
	FromBehind int
	Upstream   gitx.Upstream
	// Base is the branch this one was made from, when unagit made it; Onto is
	// what it is compared with and rebased onto (origin's copy when there is
	// one), and BaseBehind how many commits that has which this branch lacks.
	// Only a branch not pushed yet is measured against its base.
	Base       string
	Onto       string
	BaseBehind int
	// ForceFrom is where origin's copy stood when unagit rebased the branch
	// away from it, while origin still has exactly that: a force push may
	// replace it. Anything pushed since leaves it empty, and the branch reads
	// as diverged.
	ForceFrom string
	// Edits counts the files with uncommitted changes, untracked ones too.
	Edits int
	// Busy is a rebase, merge, cherry-pick or revert git is in the middle of.
	Busy string
	// Comments counts what Incomm holds on this worktree's files, comments
	// and replies, and Pending what of it waits to be published.
	Comments, Pending int
	// Own counts, for a branch origin does not have, the commits that are on
	// no branch of origin: none means pushing would add an empty branch.
	Own int
}

// busyWords is an operation in progress as a column says it.
func busyWords(op string) string {
	switch op {
	case "rebase":
		return "rebasing"
	case "merge":
		return "merging"
	case "cherry-pick":
		return "cherry-picking"
	case "revert":
		return "reverting"
	}
	return op
}

// project is the repository the worktree hangs off. The index has its clone
// addresses; without it, only the path is known.
func (a *App) worktreeProject(r worktreeRow) forge.Project {
	if pr, ok := a.projByKey[projectKey{r.Instance, r.Path}]; ok {
		return pr
	}
	return forge.Project{PathWithNamespace: r.Path, Instance: r.Instance}
}

// openMRFor is the open merge request that has this worktree's branch as its
// source, if the index knows one.
func (a *App) openMRFor(r worktreeRow) (forge.MergeRequest, bool) {
	for _, mr := range a.mrs {
		if mr.Instance != r.Instance || mr.SourceBranch != r.Branch || a.projectPathOfMR(mr) != r.Path {
			continue
		}
		if mr.State == "closed" || mr.State == "merged" {
			continue
		}
		return mr, true
	}
	return forge.MergeRequest{}, false
}

// loadWorktreeRemotes finds out where every worktree's branch stands against
// origin: one for-each-ref per repository, in the background, because git has no
// business on the event loop. The answer replaces the cache when it arrives, and
// a load that a newer one has overtaken is dropped. Until then the rows keep what
// they showed, or "…".
func (a *App) loadWorktreeRemotes() {
	type job struct {
		dir  string
		git  *gitx.Git
		rows []worktreeRow
	}
	jobs := map[projectKey]*job{}
	var order []projectKey
	var rows []worktreeRow
	for _, r := range a.worktrees {
		if r.grouped() {
			rows = append(rows, r.Members...)
		} else {
			rows = append(rows, r)
		}
	}
	integrate := a.cfg.Integrations.Incomm
	for _, r := range rows {
		k := projectKey{r.Instance, r.Path}
		j := jobs[k]
		if j == nil {
			// Branches are shared by every worktree of a repository, so any one
			// directory answers for all of them.
			j = &job{dir: r.Dir, git: a.newManager(r.Instance, r.Path, nil).Git()}
			jobs[k] = j
			order = append(order, k)
		}
		j.rows = append(j.rows, r)
	}
	a.wtGen++
	gen := a.wtGen
	if len(order) == 0 {
		a.wtRemote = nil
		return
	}
	go func() {
		result := map[string]remoteState{}
		for _, k := range order {
			j := jobs[k]
			upstreams := j.git.BranchUpstreams(j.dir)
			bases := j.git.BranchBases(j.dir)
			rebased := j.git.RebasedFrom(j.dir)
			for _, r := range j.rows {
				result[r.Dir] = remoteStateOf(j.git, r, upstreams, bases, rebased, integrate)
			}
		}
		a.tv.QueueUpdateDraw(func() {
			if gen != a.wtGen {
				return
			}
			a.wtRemote = result
			if a.worktreesPane != nil {
				a.worktreesPane.reload()
			}
			// What git says of a repository depends on its base, known only now.
			if a.wtView != nil {
				a.readWorktreeFacts()
			}
		})
	}()
}

// remoteStateOf is where one worktree stands, out of what git said of its
// repository's branches. It runs off the event loop.
func remoteStateOf(git *gitx.Git, r worktreeRow, upstreams map[string]gitx.Upstream,
	bases, rebased map[string]string, integrate bool) remoteState {
	switch {
	case upstreams == nil:
		return remoteState{Unreadable: true}
	case r.Branch == "(detached)":
		return detachedState(git, r.Dir)
	}
	st := remoteState{Upstream: upstreams[r.Branch], Base: bases[r.Branch]}
	if st.Base == "" {
		st.Base = r.Base
	}
	if st.Upstream.Name == "" {
		st.Own = git.OwnCommits(r.Dir)
	}
	if st.Base != "" && st.Upstream.Name == "" {
		st.Onto = git.BaseRef(r.Dir, st.Base)
		if st.Onto != "" {
			st.BaseBehind = git.Count(r.Dir, "HEAD.."+st.Onto)
		}
	}
	if mark := rebased[r.Branch]; mark != "" && st.Upstream.Name != "" && !st.Upstream.Gone {
		at, _ := git.Run(r.Dir, "rev-parse", "refs/remotes/"+st.Upstream.Name)
		if strings.TrimSpace(at) == mark && !git.IsAncestor(r.Dir, mark, "HEAD") {
			st.ForceFrom = mark
		}
	}
	st.Edits = max(git.Edits(r.Dir), 0)
	st.Busy = git.OperationInProgress(r.Dir)
	if integrate {
		place := incomm.Place{Dir: r.Dir}
		if r.Group != "" {
			place = incomm.Place{Dir: r.Group, Prefix: filepath.Base(r.Dir) + "/"}
		}
		for _, t := range incomm.ThreadsAt(place) {
			st.Comments += 1 + len(t.Replies)
			st.Pending += t.PendingCount()
		}
	}
	return st
}

// remoteWords says what a state means, plainly, and how to colour it. plain is
// what the row shows; name is the upstream, drawn dimmer after "in sync".
func remoteWords(st remoteState, known bool) (plain, name string, colour tcell.Color) {
	u := st.Upstream
	switch {
	case !known:
		return "…", "", colDim
	case st.Unreadable:
		return "?", "", colDim
	case st.Busy != "":
		return busyWords(st.Busy), "", colBad
	case st.Detached:
		words, colour := detachedWords(st)
		return words, "", colour
	case u.Gone:
		return "upstream gone", "", colBad
	case st.ForceFrom != "":
		return "force push required", "", colForce
	case u.Name == "" && st.BaseBehind > 0:
		return fmt.Sprintf("↓%d behind %s", st.BaseBehind, st.Base), "", colWarn
	case u.Name == "":
		return "no upstream", "", colWarn
	case u.Ahead > 0 && u.Behind > 0:
		return fmt.Sprintf("↑%d ↓%d diverged", u.Ahead, u.Behind), "", colWarn
	case u.Behind > 0:
		return fmt.Sprintf("↓%d behind", u.Behind), "", colWarn
	case u.Ahead > 0:
		return fmt.Sprintf("↑%d unpushed", u.Ahead), "", colWarn
	}
	return "in sync", u.Name, colOn
}

// remoteCell is the REMOTE column of one row, padded to width.
func remoteCell(st remoteState, known bool, width int) string {
	plain, name, colour := remoteWords(st, known)
	text := tag(colour) + tview.Escape(trunc(plain, width)) + tagEnd
	used := len([]rune(trunc(plain, width)))
	if name != "" && width-used > 3 {
		name = trunc(name, width-used-1)
		text += tag(colDim) + " " + tview.Escape(name) + tagEnd
		used += 1 + len([]rune(name))
	}
	return text + strings.Repeat(" ", max(0, width-used))
}

// newWorktreesPane is the list of every branch worktree on disk, across all
// repositories.
func (a *App) newWorktreesPane() *pane {
	p := a.newPane("Worktrees")
	var filtered []int

	p.headline = func() string {
		fetching := ""
		if a.fetching > 0 {
			fetching = fmt.Sprintf(" · fetching %d", a.fetching)
		}
		if a.syncingComments {
			fetching += " · syncing comments"
		}
		return fmt.Sprintf("%s%d/%d worktrees · %s%s%s", tag(colMuted), len(filtered), len(a.worktrees),
			sortLabel(a.cfg.Filters.Order()), fetching, tagEnd)
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
		if idx < 0 || idx >= len(a.worktrees) {
			return
		}
		// A worktree is a view of its own, not a column beside the list.
		a.showWorktreeView(a.worktrees[idx])
	}
	p.onOpen = func(ask bool) {
		r, ok := selected()
		if !ok {
			return
		}
		a.withEditor(ask, func(ed *editors.Editor) {
			if r.grouped() {
				a.openGroup(r, ed)
			} else {
				a.openWorktree(r, ed)
			}
		})
	}
	p.selection = func() (string, []uiAction) {
		r, ok := selected()
		if !ok {
			return "", nil
		}
		return "Actions · " + r.Path, a.worktreeListActions(p, r)
	}
	p.screen = func() (string, []uiAction) { return "Worktrees", a.worktreesActions(p) }
	return p
}

func (a *App) filterWorktrees(query string) []int {
	var hits []scored
	for i, r := range a.worktrees {
		hay := r.Path + " " + r.Branch + " " + a.instanceLabel(r.Instance)
		if r.grouped() {
			for _, m := range r.Members {
				hay += " " + m.Path + " " + m.Branch
			}
		} else if a.cfg.Filters.IsHidden(r.Instance, r.Path) {
			continue
		}
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

	repoW, branchW, serverW, pathW, actW, remoteW, mrW := 10, 6, 0, 0, 8, len("REMOTE"), len("MR")
	reposW, editsW, comW := len("REPOS"), len("EDITS"), len("COM")
	mrs := map[int]string{}
	for _, idx := range filtered {
		r := a.worktrees[idx]
		repoW = max(repoW, len([]rune(r.Path)))
		branchW = max(branchW, len([]rune(a.worktreeBranch(r))))
		actW = max(actW, len(humanAge(r.Moved)))
		if withServer {
			serverW = max(serverW, len([]rune(a.worktreeServer(r))))
		}
		pathW = max(pathW, len([]rune(tildePath(r.Dir))))
		plain, _ := a.worktreeRemoteWords(r)
		remoteW = max(remoteW, len([]rune(plain)))
		if mr := a.worktreeMR(r); mr != "" {
			mrs[idx] = mr
			mrW = max(mrW, len([]rune(mr)))
		}
	}
	repoW = atLeast(min(repoW, 48), "REPOSITORY")
	branchW = atLeast(min(branchW, 32), "BRANCH")
	actW = atLeast(actW, "ACTIVITY")
	remoteW = min(remoteW, 34)
	if withServer {
		serverW = atLeast(min(serverW, 16), "SERVER")
	}
	pathW = atLeast(min(pathW, 44), "PATH")

	const (
		markW   = 2
		minRepo = 20
	)
	room := p.contentWidth()
	// Everything but the repository and the optional columns; each column after
	// the first costs a space in front of it.
	fields := 5 // mark, repository, repos, branch, remote, activity: the count of gaps
	fixed := markW + reposW + branchW + remoteW + actW
	if withServer {
		fixed += serverW
		fields++
	}
	// What gives way when the row is tight, in this order: the directory, whole
	// (half a path says nothing), the merge request, the comments, then the
	// edits. REMOTE stays. The comments are counted only with Incomm on.
	showPath, showMR, showEdits := true, true, true
	showComments := a.cfg.Integrations.Incomm
	cost := func() int {
		total, gaps := fixed, fields
		for _, c := range []struct {
			on bool
			w  int
		}{{showPath, pathW}, {showMR, mrW}, {showComments, comW}, {showEdits, editsW}} {
			if c.on {
				total += c.w
				gaps++
			}
		}
		return total + gaps
	}
	for room-cost() < minRepo && (showPath || showMR || showComments || showEdits) {
		switch {
		case showPath:
			showPath = false
		case showMR:
			showMR = false
		case showComments:
			showComments = false
		default:
			showEdits = false
		}
	}
	repoW = atLeast(max(min(repoW, room-cost()), 10), "REPOSITORY")

	header := []field{{text: "", width: markW, colour: colDim}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: colDim})
	}
	header = append(header,
		field{text: "REPOSITORY", width: repoW, colour: colDim},
		field{text: "REPOS", width: reposW, colour: colDim, right: true},
		field{text: "BRANCH", width: branchW, colour: colDim},
		field{text: "REMOTE", width: remoteW, colour: colDim})
	if showEdits {
		header = append(header, field{text: "EDITS", width: editsW, colour: colDim, right: true})
	}
	if showMR {
		header = append(header, field{text: "MR", width: mrW, colour: colDim})
	}
	if showComments {
		header = append(header, field{text: "COM", width: comW, colour: colDim, right: true})
	}
	if showPath {
		header = append(header, field{text: "PATH", width: pathW, colour: colDim})
	}
	header = append(header, field{text: "ACTIVITY", width: actW, colour: colDim})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).SetSelectable(false).SetExpansion(1))

	for row, idx := range filtered {
		r := a.worktrees[idx]
		mark, count, countColour, branchColour := tag(colOn)+" ●"+tagEnd, "1", colDim, colBranch
		if r.grouped() {
			mark, count, countColour = tag(colAccent)+" ◆"+tagEnd, fmt.Sprintf("%d", len(r.Members)), colWarn
			if r.Branch == "" {
				branchColour = colMuted
			}
		}
		cells := []field{{raw: mark}}
		if withServer {
			cells = append(cells, field{text: a.worktreeServer(r), width: serverW, colour: colAccent})
		}
		plain, colour := a.worktreeRemoteWords(r)
		remote := field{text: plain, width: remoteW, colour: colour}
		if !r.grouped() {
			st, known := a.wtRemote[r.Dir]
			remote = field{raw: remoteCell(st, known, remoteW)}
		}
		cells = append(cells,
			field{text: r.Path, width: repoW, colour: colText},
			field{text: count, width: reposW, colour: countColour, right: true},
			field{text: a.worktreeBranch(r), width: branchW, colour: branchColour},
			remote)
		if showEdits {
			cells = append(cells, field{text: a.worktreeEdits(r), width: editsW, colour: colWarn, right: true})
		}
		if showMR {
			cells = append(cells, field{text: mrs[idx], width: mrW, colour: colAccent})
		}
		if showComments {
			com, colour := a.worktreeComments(r)
			cells = append(cells, field{text: com, width: comW, colour: colour, right: true})
		}
		if showPath {
			cells = append(cells, field{text: tildePath(r.Dir), width: pathW, colour: colMuted})
		}
		cells = append(cells, field{text: humanAge(r.Moved), width: actW, colour: colMuted})
		p.table.SetCell(row+1, 0, tview.NewTableCell(rowText(cells)).SetReference(idx).SetExpansion(1))
	}

	first := 0
	if len(filtered) > 0 {
		first = 1
	}
	p.selectRow(previous, first)
}

// worktreeComments is the COM column: how many comments Incomm holds on the
// worktree, across every repository of a grouped one - drawn as a warning
// while some of them wait to be published. Nothing when there are none.
func (a *App) worktreeComments(r worktreeRow) (string, tcell.Color) {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	total, pending := 0, 0
	for _, m := range members {
		total += a.wtRemote[m.Dir].Comments
		pending += a.wtRemote[m.Dir].Pending
	}
	if total == 0 {
		return "", colDim
	}
	if pending > 0 {
		return fmt.Sprintf("%d", total), colWarn
	}
	return fmt.Sprintf("%d", total), colMuted
}

// worktreeEdits is the EDITS column: how many files have uncommitted changes,
// across every member of a grouped worktree; nothing when there are none.
func (a *App) worktreeEdits(r worktreeRow) string {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	total := 0
	for _, m := range members {
		total += a.wtRemote[m.Dir].Edits
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("%d", total)
}

// worktreeBranch is what the BRANCH column says: the branch, or for a grouped
// worktree whose members are on different ones, how many.
func (a *App) worktreeBranch(r worktreeRow) string {
	if !r.grouped() || r.Branch != "" {
		return r.Branch
	}
	seen := map[string]bool{}
	for _, m := range r.Members {
		seen[m.Branch] = true
	}
	return fmt.Sprintf("%d branches", len(seen))
}

// worktreeServer is the SERVER column: a grouped worktree names one only when
// every member lives on it.
func (a *App) worktreeServer(r worktreeRow) string {
	if !r.grouped() {
		return a.instanceLabel(r.Instance)
	}
	server := ""
	for i, m := range r.Members {
		if i > 0 && m.Instance != r.Members[0].Instance {
			return "several"
		}
		server = a.instanceLabel(m.Instance)
	}
	return server
}

// worktreeMR is the MR column: the open merge request of the branch, or for a
// grouped worktree, the first of its members' and how many more there are.
func (a *App) worktreeMR(r worktreeRow) string {
	if !r.grouped() {
		if mr, ok := a.openMRFor(r); ok {
			return fmt.Sprintf("!%d", mr.IID)
		}
		return ""
	}
	var found []int
	for _, m := range r.Members {
		if mr, ok := a.openMRFor(m); ok {
			found = append(found, mr.IID)
		}
	}
	switch len(found) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("!%d", found[0])
	}
	return fmt.Sprintf("!%d +%d", found[0], len(found)-1)
}

// worktreeRemoteWords is the REMOTE column as plain text and a colour. A
// grouped worktree says what its members share, or the worst of them and how
// many are not in sync.
func (a *App) worktreeRemoteWords(r worktreeRow) (string, tcell.Color) {
	if !r.grouped() {
		st, known := a.wtRemote[r.Dir]
		plain, name, colour := remoteWords(st, known)
		if name != "" {
			plain += " " + name
		}
		return plain, colour
	}
	if len(r.Members) == 0 {
		return "?", colDim
	}
	worst, worstRank, behind, same := "", -1, 0, true
	var worstColour tcell.Color
	first := ""
	for i, m := range r.Members {
		st, known := a.wtRemote[m.Dir]
		plain, _, colour := remoteWords(st, known)
		if !known {
			return "…", colDim
		}
		if i == 0 {
			first = plain
		} else if plain != first {
			same = false
		}
		rank := remoteRank(st)
		if rank > 0 {
			behind++
		}
		if rank > worstRank {
			worst, worstRank, worstColour = plain, rank, colour
		}
	}
	if same {
		return worst, worstColour
	}
	return fmt.Sprintf("%s · %d/%d", worst, behind, len(r.Members)), worstColour
}

// remoteRank orders the states by how much they want attention; in sync is 0.
func remoteRank(st remoteState) int {
	u := st.Upstream
	switch {
	case st.Busy != "":
		return 7
	case st.Unreadable, st.Detached:
		return 1
	case u.Gone:
		return 6
	case st.ForceFrom != "":
		return 5
	case u.Name == "" && st.BaseBehind > 0:
		return 4
	case u.Name == "":
		return 2
	case u.Ahead > 0 && u.Behind > 0:
		return 5
	case u.Behind > 0:
		return 4
	case u.Ahead > 0:
		return 3
	}
	return 0
}

// showWorktreeAt switches to Worktrees with the cursor on the worktree in dir,
// one just made: opening it is the user's next step, not an automatic one.
func (a *App) showWorktreeAt(dir string) {
	p := a.worktreesPane
	if p.query != "" {
		p.clearFilter()
	}
	a.switchTab(pageWorktrees)
	p.reload()
	p.selectWhere(func(i int) bool { return i < len(a.worktrees) && sameDir(a.worktrees[i].Dir, dir) })
	a.done("created " + tildePath(dir) + " · Ctrl-O opens it")
}

// openWorktree opens the editor in a worktree as it is on disk.
func (a *App) openWorktree(r worktreeRow, ed *editors.Editor) {
	a.openNow(r.Dir, session.Record{
		Instance: r.Instance,
		Server:   a.instanceLabel(r.Instance),
		Project:  r.Path,
		Title:    r.Branch,
		Mode:     session.ModeBranch,
	}, ed)
}

// remoteSentence is remoteWords for the detail column, where there is room to
// say it in full.
func (a *App) remoteSentence(st remoteState, known bool, plain string) string {
	u := st.Upstream
	switch {
	case !known:
		return "still looking"
	case st.Unreadable:
		return "cannot be read: the clone is missing or broken"
	case st.Busy != "":
		return "a " + st.Busy + " is in progress: finish it, or abort it, in the directory"
	case st.Detached:
		return detachedSentence(st)
	case u.Gone:
		return "upstream gone: the branch was deleted on origin"
	case st.ForceFrom != "":
		return fmt.Sprintf("rebased onto %s; origin still has the old commits · P force-pushes, asking first", st.Base)
	case u.Name == "" && st.BaseBehind > 0:
		return fmt.Sprintf("not on origin yet, %d behind %s · p rebases onto it · P pushes it", st.BaseBehind, st.Onto)
	case u.Name == "":
		return "no upstream: not on origin yet · P pushes it"
	case u.Ahead > 0 && u.Behind > 0:
		return fmt.Sprintf("diverged: %d unpushed, %d behind origin", u.Ahead, u.Behind)
	case u.Behind > 0:
		return fmt.Sprintf("%d commit(s) behind origin", u.Behind)
	case u.Ahead > 0:
		return fmt.Sprintf("%d unpushed commit(s) · P pushes them", u.Ahead)
	}
	return plain
}

// pushBlocked says why a worktree's branch cannot be pushed as it stands, or ""
// when it can. The one push that forces is of a branch Ctrl-R rebased, over
// exactly what origin had then; any other branch origin has moved past has to
// be brought up to date first.
func pushBlocked(st remoteState, known bool) string {
	u := st.Upstream
	switch {
	case !known:
		return "still checking where this branch stands - try again in a moment"
	case st.Unreadable:
		return "the clone of this repository cannot be read"
	case st.Detached:
		return "this worktree has a detached HEAD, there is no branch to push"
	case u.Gone:
		return "the upstream is gone: the branch was deleted on origin. Push it again by hand if you want it back"
	case st.ForceFrom != "":
		return ""
	case u.Behind > 0:
		return fmt.Sprintf("origin has %d commit(s) this branch lacks: pull or rebase first, unagit forces only what Ctrl-R rebased", u.Behind)
	}
	return ""
}

// pushWorktree sends a worktree's branch to origin: with -u when it has no
// upstream yet, plainly when it is ahead of one.
func (a *App) pushWorktree(r worktreeRow) {
	st, known := a.wtRemote[r.Dir]
	if why := pushBlocked(st, known); why != "" {
		a.flash(why)
		return
	}
	if st.ForceFrom != "" {
		body := fmt.Sprintf("[::b]%s[::-] was rebased onto %s, so origin's copy has to be replaced.\n\n"+
			"Force-push it? Only origin's copy as it was before the rebase is replaced: "+
			"if anyone pushed since, git refuses.", esc(r.Branch), esc(st.Base))
		a.confirmWith("Force push", body, "Force push", nil, func() {
			a.runTask(fmt.Sprintf("Force-pushing %s (%s)", r.Path, r.Branch), func(log func(string)) (string, error) {
				return "", forcePush(a.newManager(r.Instance, r.Path, log).Git(), r.Dir, r.Branch, st.ForceFrom)
			})
		})
		return
	}
	if st.Upstream.Name != "" && st.Upstream.Ahead == 0 {
		a.flash(r.Branch + " is already on origin")
		return
	}
	if st.Upstream.Name == "" && st.Own == 0 {
		a.flash(r.Branch + " has nothing of its own to push yet - commit first")
		return
	}
	setUpstream := st.Upstream.Name == ""
	a.runTask(fmt.Sprintf("Pushing %s (%s)", r.Path, r.Branch), func(log func(string)) (string, error) {
		return "", a.newManager(r.Instance, r.Path, log).Git().Push(r.Dir, r.Branch, setUpstream)
	})
}

// forcePush replaces origin's copy of a rebased branch, and once origin has
// it, forgets where the copy stood before.
func forcePush(git *gitx.Git, dir, branch, lease string) error {
	if err := git.ForcePush(dir, branch, lease); err != nil {
		return err
	}
	git.ClearRebasedFrom(dir, branch)
	return nil
}

// humanBranch turns a branch name into something that reads as a title: its last
// segment, with words instead of dashes and underscores.
func humanBranch(branch string) string {
	if i := strings.LastIndex(branch, "/"); i >= 0 && i < len(branch)-1 {
		branch = branch[i+1:]
	}
	words := strings.Join(strings.FieldsFunc(branch, func(c rune) bool { return c == '-' || c == '_' }), " ")
	r := []rune(words)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// mergeRequestDefaults proposes a title and a description from the commits the
// branch adds: a single commit says it all, several are listed under the branch
// name. Without commits to read, nothing is proposed.
func mergeRequestDefaults(branch string, commits []gitx.CommitMsg) (title, description string) {
	switch len(commits) {
	case 0:
		return "", ""
	case 1:
		return commits[0].Subject, commits[0].Body
	}
	lines := make([]string, len(commits))
	for i, c := range commits {
		lines[i] = "- " + c.Subject
	}
	return humanBranch(branch), strings.Join(lines, "\n")
}

// newMergeRequest starts creating a merge request for a worktree's branch.
func (a *App) newMergeRequest(r worktreeRow) {
	pr := a.worktreeProject(r)
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(pr.Instance))
		return
	}
	if mr, ok := a.openMRFor(r); ok {
		a.flash(fmt.Sprintf("!%d is already open for this branch", mr.IID))
		return
	}
	st, known := a.wtRemote[r.Dir]
	if why := pushBlocked(st, known); why != "" {
		a.flash(why)
		return
	}
	u := st.Upstream
	if u.Name == "" || u.Ahead > 0 {
		what := "not on origin yet"
		if u.Name != "" {
			what = fmt.Sprintf("not on origin yet (%d unpushed)", u.Ahead)
		}
		body := fmt.Sprintf("[::b]%s[::-] is %s.\n\nA merge request needs it there: push it and continue?", esc(r.Branch), what)
		a.confirmWith("Create merge request", body, "Push and continue", nil, func() {
			a.prepareMergeRequest(r, pr, client, true, u.Name == "")
		})
		return
	}
	a.prepareMergeRequest(r, pr, client, false, false)
}

// prepareMergeRequest pushes if asked, then collects what the form needs: the
// branches to target and a title and description drawn from the commits.
func (a *App) prepareMergeRequest(r worktreeRow, pr forge.Project, client forge.Provider, push, setUpstream bool) {
	a.runTask(fmt.Sprintf("Preparing a merge request for %s", r.Branch), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		mgr := a.newManager(pr.Instance, pr.PathWithNamespace, log)
		if push {
			if err := mgr.Git().Push(r.Dir, r.Branch, setUpstream); err != nil {
				return "", err
			}
		}
		branches, err := client.ProjectBranches(ctx, pr)
		if err != nil {
			return "", err
		}
		defaultBranch := pr.DefaultBranch
		var targets []string
		for _, b := range branches {
			if b.Default && defaultBranch == "" {
				defaultBranch = b.Name
			}
			if b.Name != r.Branch {
				targets = append(targets, b.Name)
			}
		}
		if len(targets) == 0 {
			return "", fmt.Errorf("%s has no other branch to merge %s into", pr.PathWithNamespace, r.Branch)
		}
		var title, description string
		if commits, err := mgr.Git().CommitsAheadOf(r.Dir, "origin/"+defaultBranch, r.Branch); err == nil {
			title, description = mergeRequestDefaults(r.Branch, commits)
		} else {
			log("! could not read the commits, so nothing is proposed: " + err.Error())
		}
		a.tv.QueueUpdateDraw(func() {
			a.closeModal(pageTask)
			a.showMergeRequestForm(r, pr, client, targets, defaultBranch, title, description)
		})
		return "", nil
	})
}

// Labels of the GitLab-only options; the form's labels are also how it finds them.
const (
	labelDeleteBranch = "Delete source branch"
	labelSquash       = "Squash commits"
)

// showMergeRequestForm asks for what the forge requires of a new merge request.
func (a *App) showMergeRequestForm(r worktreeRow, pr forge.Project, client forge.Provider,
	targets []string, defaultBranch, title, description string) {
	gitlab := client.Kind() == forge.KindGitLab
	form := tview.NewForm()
	styleForm(form)
	// A blank row between fields, or the input fields' bands run into one
	// another and read as a single block.
	form.SetItemPadding(1)
	selected := 0
	for i, t := range targets {
		if t == defaultBranch {
			selected = i
		}
	}
	// Title and description take whatever the modal has left after the labels,
	// so they follow the terminal instead of being drawn over the frame.
	form.AddInputField("Title", title, 0, nil, nil)
	addSelect(form, "Target branch", targets, selected)
	addTextArea(form, "Description", description, 5)
	addCheckbox(form, "Draft", false)
	if gitlab {
		addCheckbox(form, labelDeleteBranch, false)
		addCheckbox(form, labelSquash, false)
	}
	checked := func(label string) bool {
		item := form.GetFormItemByLabel(label)
		box, ok := item.(*tview.Checkbox)
		return ok && box.IsChecked()
	}
	create := func() {
		req := forge.NewMergeRequest{
			Title:        strings.TrimSpace(form.GetFormItemByLabel("Title").(*tview.InputField).GetText()),
			Description:  form.GetFormItemByLabel("Description").(*tview.TextArea).GetText(),
			SourceBranch: r.Branch,
			Draft:        checked("Draft"),
		}
		_, req.TargetBranch = form.GetFormItemByLabel("Target branch").(*tview.DropDown).GetCurrentOption()
		if gitlab {
			req.RemoveSourceBranch = checked(labelDeleteBranch)
			req.Squash = checked(labelSquash)
		}
		switch {
		case req.Title == "":
			a.flash("enter a title")
			return
		case req.TargetBranch == "" || req.TargetBranch == req.SourceBranch:
			a.flash("pick a target branch other than the source")
			return
		}
		a.closeModal(pageForm)
		a.createMergeRequest(r, pr, client, req)
	}
	form.AddButton("Create", create)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModalSized(fmt.Sprintf("New merge request · %s · %s", pr.PathWithNamespace, r.Branch), form, 92, 20)
}

// createMergeRequest sends the form to the forge, and on success puts the new
// request into the list, so the row shows it without waiting for a refresh.
func (a *App) createMergeRequest(r worktreeRow, pr forge.Project, client forge.Provider, req forge.NewMergeRequest) {
	a.runTask(fmt.Sprintf("Creating a merge request for %s", r.Branch), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		mr, err := client.CreateMergeRequest(ctx, pr, req)
		if err != nil {
			if errors.Is(err, forge.ErrMergeRequestExists) {
				log("! " + err.Error())
			}
			return "", err
		}
		mr.Instance = pr.Instance
		if mr.ProjectPath == "" {
			mr.ProjectPath = pr.PathWithNamespace
		}
		log(fmt.Sprintf("Created !%d", mr.IID))
		created := *mr
		a.tv.QueueUpdateDraw(func() {
			a.adoptMergeRequest(created)
			body := fmt.Sprintf("Merge request [::b]!%d[::-] created.\n\n%s", created.IID, esc(created.WebURL))
			a.confirmWith("Merge request created", body, "Open in browser", nil, func() {
				if created.WebURL != "" {
					_ = openBrowser(created.WebURL)
				}
			})
		})
		return "", nil
	})
}

// adoptMergeRequest adds a request the forge just created to the list and to the
// saved index, leaving the time the list was indexed as it was. It runs on the
// event loop.
func (a *App) adoptMergeRequest(mr forge.MergeRequest) {
	for _, have := range a.mrs {
		if have.Instance == mr.Instance && have.IID == mr.IID && have.ProjectID == mr.ProjectID {
			return
		}
	}
	a.mrs = append(a.mrs, mr)
	_ = index.Save(config.IndexPath("mrs"), index.MergeRequests{
		Version: index.Version, UpdatedAt: a.mrsUpdated, Items: a.mrs})
	a.mrsPane.reload()
	a.worktreesPane.reload()
}
