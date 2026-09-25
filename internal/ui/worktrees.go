package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/index"
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

// remoteState is where a worktree's branch stands against origin. A worktree with
// no entry yet is still being looked at.
type remoteState struct {
	Unreadable bool // git could not be asked: the clone is gone or broken
	Detached   bool
	Upstream   gitx.Upstream
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
	for _, r := range a.worktrees {
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
			for _, r := range j.rows {
				switch {
				case upstreams == nil:
					result[r.Dir] = remoteState{Unreadable: true}
				case r.Branch == "(detached)":
					result[r.Dir] = remoteState{Detached: true}
				default:
					result[r.Dir] = remoteState{Upstream: upstreams[r.Branch]}
				}
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
		})
	}()
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
	case st.Detached:
		return "detached", "", colDim
	case u.Gone:
		return "upstream gone", "", colBad
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
			a.note("looked at the disk and at origin again")
			return nil
		case 'P':
			if r, ok := selected(); ok {
				a.pushWorktree(r)
			}
			return nil
		case 'n':
			if r, ok := selected(); ok {
				a.newMergeRequest(r)
			}
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

	repoW, branchW, serverW, pathW, actW, remoteW, mrW := 10, 6, 0, 0, 8, len("REMOTE"), len("MR")
	mrs := map[int]forge.MergeRequest{}
	for _, idx := range filtered {
		r := a.worktrees[idx]
		repoW = max(repoW, len([]rune(r.Path)))
		branchW = max(branchW, len([]rune(r.Branch)))
		actW = max(actW, len(humanAge(r.Moved)))
		if withServer {
			serverW = max(serverW, len([]rune(a.instanceLabel(r.Instance))))
		}
		pathW = max(pathW, len([]rune(tildePath(r.Dir))))
		st, known := a.wtRemote[r.Dir]
		plain, name, _ := remoteWords(st, known)
		if name != "" {
			plain += " " + name
		}
		remoteW = max(remoteW, len([]rune(plain)))
		if mr, ok := a.openMRFor(r); ok {
			mrs[idx] = mr
			mrW = max(mrW, len(fmt.Sprintf("!%d", mr.IID)))
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
	fields := 4 // mark, repository, branch, remote, activity: the count of gaps
	fixed := markW + branchW + remoteW + actW
	if withServer {
		fixed += serverW
		fields++
	}
	// What gives way when the row is tight, in this order: the directory, whole
	// (half a path says nothing), then the merge request. REMOTE stays.
	showPath, showMR := true, true
	cost := func() int {
		total, gaps := fixed, fields
		if showPath {
			total += pathW
			gaps++
		}
		if showMR {
			total += mrW
			gaps++
		}
		return total + gaps
	}
	for room-cost() < minRepo && (showPath || showMR) {
		if showPath {
			showPath = false
		} else {
			showMR = false
		}
	}
	repoW = atLeast(max(min(repoW, room-cost()), 10), "REPOSITORY")

	header := []field{{text: "", width: markW, colour: colDim}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: colDim})
	}
	header = append(header,
		field{text: "REPOSITORY", width: repoW, colour: colDim},
		field{text: "BRANCH", width: branchW, colour: colDim},
		field{text: "REMOTE", width: remoteW, colour: colDim})
	if showMR {
		header = append(header, field{text: "MR", width: mrW, colour: colDim})
	}
	if showPath {
		header = append(header, field{text: "PATH", width: pathW, colour: colDim})
	}
	header = append(header, field{text: "ACTIVITY", width: actW, colour: colDim})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).SetSelectable(false).SetExpansion(1))

	for row, idx := range filtered {
		r := a.worktrees[idx]
		st, known := a.wtRemote[r.Dir]
		cells := []field{{raw: tag(colOn) + " ●" + tagEnd}}
		if withServer {
			cells = append(cells, field{text: a.instanceLabel(r.Instance), width: serverW, colour: colAccent})
		}
		cells = append(cells,
			field{text: r.Path, width: repoW, colour: colText},
			field{text: r.Branch, width: branchW, colour: colBranch},
			field{raw: remoteCell(st, known, remoteW)})
		if showMR {
			mr := ""
			if m, ok := mrs[idx]; ok {
				mr = fmt.Sprintf("!%d", m.IID)
			}
			cells = append(cells, field{text: mr, width: mrW, colour: colAccent})
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

	st, known := a.wtRemote[r.Dir]
	plain, name, colour := remoteWords(st, known)
	remote := tag(colour) + esc(a.remoteSentence(st, known, plain)) + tagEnd
	if name != "" {
		remote += tag(colDim) + " with " + esc(name) + tagEnd
	}
	mrLine := tag(colDim) + "no open merge request · n creates one" + tagEnd
	if mr, ok := a.openMRFor(r); ok {
		mrLine = tag(colAccent) + fmt.Sprintf("!%d", mr.IID) + tagEnd + " " + esc(mr.Title)
	}

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
		d.kv("Remote", remote)
		d.kv("Merge request", mrLine)
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
		state := tag(colOn) + "clean" + tagEnd
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

// remoteSentence is remoteWords for the detail column, where there is room to
// say it in full.
func (a *App) remoteSentence(st remoteState, known bool, plain string) string {
	u := st.Upstream
	switch {
	case !known:
		return "still looking"
	case st.Unreadable:
		return "cannot be read: the clone is missing or broken"
	case st.Detached:
		return "detached HEAD, no branch to push"
	case u.Gone:
		return "upstream gone: the branch was deleted on origin"
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
// when it can. Nothing here ever forces: a branch origin has moved past has to be
// brought up to date first.
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
	case u.Behind > 0:
		return fmt.Sprintf("origin has %d commit(s) this branch lacks: pull or rebase first, unagit never forces", u.Behind)
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
	if st.Upstream.Name != "" && st.Upstream.Ahead == 0 {
		a.flash(r.Branch + " is already on origin")
		return
	}
	setUpstream := st.Upstream.Name == ""
	a.runTask(fmt.Sprintf("Pushing %s (%s)", r.Path, r.Branch), func(log func(string)) (string, error) {
		return "", a.newManager(r.Instance, r.Path, log).Git().Push(r.Dir, r.Branch, setUpstream)
	})
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
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
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
		if commits, err := mgr.Git().CommitsAhead(r.Dir, "origin/"+defaultBranch); err == nil {
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
	form.SetItemPadding(0)
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
	form.AddTextArea("Description", description, 0, 5, 0, nil)
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
	a.showFormModalSized(fmt.Sprintf("New merge request · %s · %s", pr.PathWithNamespace, r.Branch), form, 92, 17)
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
