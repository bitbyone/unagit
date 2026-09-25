package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/incomm"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/session"
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
		scope := tag(colMuted) + "all repositories" + tagEnd
		if a.mrProjectScope.Path != "" {
			scope = tag(colWarn) + a.mrProjectScope.Path + tagEnd
		}
		return fmt.Sprintf("%s%d/%d merge requests · %s%s · scope %s",
			tag(colMuted), len(filtered), len(a.mrs), age, a.filterSummary(), tagEnd+scope)
	}

	render := func(query string) {
		filtered = a.filterMRs(query)
		a.drawMRs(p, filtered)
		p.updateHeader()
	}
	p.onQuery = render

	selected := func() (forge.MergeRequest, bool) {
		i := p.selectedIndex()
		if i < 0 || i >= len(a.mrs) {
			return forge.MergeRequest{}, false
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

	shared := a.filterKeysFor(p)
	p.onKey = func(ev *tcell.EventKey) *tcell.EventKey {
		if shared(ev) {
			return nil
		}
		// Ctrl-G gathers the list under the projects; g is taken by "go to
		// the first row".
		if ev.Key() == tcell.KeyCtrlG {
			a.toggleGrouping()
			return nil
		}
		// Ctrl-R opens the review worktree, next to Ctrl-O for the branch one.
		if ev.Key() == tcell.KeyCtrlR {
			if mr, ok := selected(); ok {
				a.openMRReview(mr)
			}
			return nil
		}
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
			a.note("repository filter cleared")
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
		case 'A':
			if mr, ok := selected(); ok {
				a.approveMR(mr, nil)
			}
			return nil
		case 'P':
			if mr, ok := selected(); ok {
				a.publishMR(mr)
			}
			return nil
		case 'c':
			if mr, ok := selected(); ok {
				a.showComments(mr)
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
		if !a.passesFilters(mr.Instance, path) {
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
	// A query ranks by how well it matched; without one the shared order wins.
	if strings.TrimSpace(query) != "" {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	} else if a.cfg.Filters.Order() == config.SortName {
		sort.SliceStable(hits, func(i, j int) bool {
			left, right := a.mrs[hits[i].idx], a.mrs[hits[j].idx]
			if lp, rp := a.projectPathOfMR(left), a.projectPathOfMR(right); lp != rp {
				return lp < rp
			}
			return left.IID < right.IID
		})
	} else {
		sort.SliceStable(hits, func(i, j int) bool {
			return a.mrSortTime(a.mrs[hits[i].idx]).After(a.mrSortTime(a.mrs[hits[j].idx]))
		})
	}
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

// mrColumns works out how wide each column may be for the current table
// width. The title takes whatever is left, and every cell is truncated to
// fit, so the branch column never falls off the right edge.
type mrColumns struct{ proj, iid, title, author, branch, com, pub, updated int }

func (a *App) mrColumns(width int, rows []int) mrColumns {
	c := mrColumns{iid: 3, updated: 8, com: 3}
	if a.cfg.Integrations.Incomm {
		c.pub = 3 // PUB: what waits to be published from Incomm
	}
	for _, idx := range rows {
		mr := a.mrs[idx]
		c.proj = max(c.proj, len(a.projectPathOfMR(mr)))
		c.iid = max(c.iid, len(fmt.Sprintf("!%d", mr.IID)))
		c.author = max(c.author, len(mr.Author.Username))
		c.branch = max(c.branch, len([]rune(mr.SourceBranch)))
		c.updated = max(c.updated, len(humanAge(mr.UpdatedAt)))
	}
	c.proj = atLeast(min(c.proj, 34), "REPO")
	c.author = atLeast(min(c.author, 14), "AUTHOR")
	c.branch = atLeast(min(c.branch, 26), "BRANCH")
	c.updated = atLeast(c.updated, "UPDATED")

	const (
		markW    = 2
		minTitle = 24
	)
	gaps := 7
	if c.pub > 0 {
		gaps++ // its own gap
	}
	fixed := func() int {
		return markW + c.proj + c.iid + c.author + c.branch + c.com + c.pub + c.updated + gaps
	}
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

// atLeast keeps a column wide enough for its own heading.
func atLeast(width int, heading string) int { return max(width, len(heading)) }

// field is one column of a row: text of a fixed width, in a colour.
type field struct {
	text   string
	width  int
	colour tcell.Color
	right  bool
	// raw is already marked up and its width already right.
	raw string
}

// rowText lays the fields out at their widths. The merge request table draws
// each row as a single cell, because tview cannot make a heading span the
// columns and the widths are worked out here anyway.
func rowText(fields []field) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(' ')
		}
		if f.raw != "" {
			b.WriteString(f.raw)
			continue
		}
		text := trunc(f.text, f.width)
		pad := strings.Repeat(" ", max(0, f.width-len([]rune(text))))
		if f.right {
			b.WriteString(pad + tag(f.colour) + tview.Escape(text) + tagEnd)
			continue
		}
		b.WriteString(tag(f.colour) + tview.Escape(text) + tagEnd + pad)
	}
	return b.String()
}

// mrGroup is the merge requests of one project, in the order the filter put
// them.
type mrGroup struct {
	key  projectKey
	rows []int
}

// groupByProject gathers the rows under their project, keeping the order the
// filter produced: the first merge request of a project decides where the
// project sits, and the rest follow inside it.
func (a *App) groupByProject(filtered []int) []mrGroup {
	var groups []mrGroup
	at := map[projectKey]int{}
	for _, idx := range filtered {
		mr := a.mrs[idx]
		key := projectKey{Instance: mr.Instance, Path: a.projectPathOfMR(mr)}
		if i, ok := at[key]; ok {
			groups[i].rows = append(groups[i].rows, idx)
			continue
		}
		at[key] = len(groups)
		groups = append(groups, mrGroup{key: key, rows: []int{idx}})
	}
	return groups
}

func (a *App) drawMRs(p *pane, filtered []int) {
	previous := p.selectedIndex()
	p.table.Clear()
	grouped := a.cfg.Filters.GroupByProject
	withServer := a.multiInstance() && !grouped

	serverW := 0
	if withServer {
		for _, idx := range filtered {
			serverW = max(serverW, len([]rune(a.instanceLabel(a.mrs[idx].Instance))))
		}
		serverW = atLeast(min(serverW, 16), "SERVER")
	}
	c := a.mrColumns(p.contentWidth()-serverW, filtered)
	if grouped {
		// The project moves into the heading, so its width goes to the title.
		// The gap it leaves behind pays for the indent on every row.
		c.title += c.proj
		c.proj = 0
	}

	// The header is laid out the same way the rows are.
	header := []field{{text: "", width: 2, colour: colDim}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: colDim})
	}
	if !grouped {
		header = append(header, field{text: "REPO", width: c.proj, colour: colDim})
	}
	header = append(header,
		field{text: "MR", width: c.iid, colour: colDim},
		field{text: "TITLE", width: c.title, colour: colDim},
		field{text: "AUTHOR", width: c.author, colour: colDim},
		field{text: "BRANCH", width: c.branch, colour: colDim},
		field{text: "COM", width: c.com, colour: colDim, right: true})
	if c.pub > 0 {
		header = append(header, field{text: "PUB", width: c.pub, colour: colDim, right: true})
	}
	header = append(header, field{text: "UPDATED", width: c.updated, colour: colDim})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).
		SetSelectable(false).SetExpansion(1))

	row := 0
	first := -1
	drawRow := func(idx int) {
		row++
		mr := a.mrs[idx]
		path := a.projectPathOfMR(mr)
		disk := a.diskOf(mr.Instance, path).MRs[mr.IID]

		mark := " " + mrMark(disk)
		if grouped {
			mark = "  " + mrMark(disk)
		}
		title := trunc(mr.Title, c.title)
		titleField := field{text: title, width: c.title, colour: colText}
		if mr.Draft {
			short := trunc(mr.Title, c.title-6)
			pad := strings.Repeat(" ", max(0, c.title-len([]rune(short))-6))
			titleField = field{raw: "[::d]draft[::-] " + tag(colText) + tview.Escape(short) + tagEnd + pad}
		}
		comments := ""
		if mr.Comments > 0 {
			comments = fmt.Sprintf("%d", mr.Comments)
		}
		pending := ""
		if disk.Pending > 0 {
			pending = fmt.Sprintf("%d", disk.Pending)
		}

		fields := []field{{raw: tag(mrMarkColor(disk)) + mark + tagEnd}}
		if withServer {
			fields = append(fields, field{text: a.instanceLabel(mr.Instance), width: serverW, colour: colAccent})
		}
		if !grouped {
			fields = append(fields, field{text: path, width: c.proj, colour: colAccent})
		}
		fields = append(fields,
			field{text: fmt.Sprintf("!%d", mr.IID), width: c.iid, colour: colWarn},
			titleField,
			field{text: mr.Author.Username, width: c.author, colour: colMuted},
			field{text: mr.SourceBranch, width: c.branch, colour: colBranch},
			field{text: comments, width: c.com, colour: colWarn, right: true})
		if c.pub > 0 {
			fields = append(fields, field{text: pending, width: c.pub, colour: colWarn, right: true})
		}
		fields = append(fields, field{text: humanAge(mr.UpdatedAt), width: c.updated, colour: colMuted})

		p.table.SetCell(row, 0, tview.NewTableCell(rowText(fields)).
			SetReference(idx).SetExpansion(1))
		if first < 0 {
			first = row
		}
	}

	if !grouped {
		for _, idx := range filtered {
			drawRow(idx)
		}
	} else {
		for _, group := range a.groupByProject(filtered) {
			row++
			heading := group.key.Path
			if a.multiInstance() {
				heading = a.instanceLabel(group.key.Instance) + " · " + heading
			}
			p.table.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("%s[::b]%s[::-]%s  %s(%d)%s",
				tag(colAccent), tview.Escape(heading), tagEnd, tag(colDim), len(group.rows), tagEnd)).
				SetSelectable(false).SetExpansion(1))
			for _, idx := range group.rows {
				drawRow(idx)
			}
		}
	}

	p.selectRow(previous, first)
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
func (a *App) openMR(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	client := a.client(mr.Instance)
	integrate := a.cfg.Integrations.Incomm
	a.runTaskOpening(fmt.Sprintf("Opening %s !%d", project.PathWithNamespace, mr.IID),
		a.sessionOf(mr, project.PathWithNamespace, session.ModeBranch),
		func(log func(string)) (string, error) {
			mr := a.refreshMR(client, mr, log)
			dir, err := a.newManager(mr.Instance, project.PathWithNamespace, log).EnsureMR(mr, project)
			if err == nil && integrate {
				reanchorAfterUpdate(dir, log)
			}
			return dir, err
		})
}

// reanchorAfterUpdate puts the Incomm comments of a worktree back on their code.
// It runs right after the worktree was brought up to date, because that is when
// the code moves under them; it is idempotent, so running it when nothing moved
// costs a moment and changes nothing. A failure is only logged: the worktree is
// fine, and Incomm re-anchors again whenever it lists.
func reanchorAfterUpdate(dir string, log func(string)) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := incomm.Reanchor(ctx, dir); err != nil {
		log("! Incomm could not re-anchor the comments: " + err.Error())
		return
	}
	log("Incomm: comments are on their code")
}

// refreshMR asks the forge for this one merge request and puts what it says into
// its row, so opening it leaves the list as current as the worktree. The rest of
// the list waits for r. The fresh values are also what the checkout should use:
// a merge request retargeted since the index was made would otherwise be fetched
// and diffed against its old target. When the forge cannot be asked, the request
// is opened as the index knew it. The client comes from the caller because this
// runs off the event loop.
func (a *App) refreshMR(client forge.Provider, mr forge.MergeRequest, log func(string)) forge.MergeRequest {
	if client == nil {
		return mr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	det, err := client.MergeRequestDetail(ctx, mr)
	if err != nil {
		log("! could not refresh the merge request: " + err.Error())
		return mr
	}
	fresh := withDetail(mr, det)
	a.tv.QueueUpdateDraw(func() { a.applyMRUpdate(fresh, true, false) })
	return fresh
}

// withDetail is mr with what the forge just said about it. Instance and
// ProjectPath are unagit's own and stay.
func withDetail(mr forge.MergeRequest, det *forge.MergeRequestDetail) forge.MergeRequest {
	mr.Title, mr.Draft, mr.State = det.Title, det.Draft, det.State
	mr.SourceBranch, mr.TargetBranch = det.SourceBranch, det.TargetBranch
	mr.UpdatedAt, mr.Comments = det.UpdatedAt, det.UserNotesCount
	if det.WebURL != "" {
		mr.WebURL = det.WebURL
	}
	if det.Author.Username != "" {
		mr.Author = det.Author
	}
	return mr
}

// sameRow reports whether two versions of a merge request agree on everything
// the list shows, so a refresh that found nothing new redraws nothing.
func sameRow(a, b forge.MergeRequest) bool {
	return a.Title == b.Title && a.Draft == b.Draft && a.State == b.State &&
		a.SourceBranch == b.SourceBranch && a.TargetBranch == b.TargetBranch &&
		a.WebURL == b.WebURL && a.Comments == b.Comments && a.UpdatedAt.Equal(b.UpdatedAt) &&
		a.Author == b.Author
}

// applyMRUpdate replaces one row of the index and redraws it, without touching
// the rest of the list or the time the list was indexed. It does nothing when the
// row already says the same, which is what keeps a detail that follows the cursor
// from redrawing the list on every step. redetail also reloads the detail column
// when it shows this request. keepOrder shows the fresh time without moving the
// row (see sortHold); opening a request lets it move. It runs on the event loop.
func (a *App) applyMRUpdate(fresh forge.MergeRequest, redetail, keepOrder bool) {
	for i := range a.mrs {
		if a.mrs[i].Instance != fresh.Instance || a.mrs[i].IID != fresh.IID || a.mrs[i].ID != fresh.ID {
			continue
		}
		key := keyOfMR(fresh)
		if sameRow(a.mrs[i], fresh) {
			if !keepOrder {
				delete(a.sortHold, key)
			}
			return
		}
		if keepOrder {
			if _, held := a.sortHold[key]; !held && !a.mrs[i].UpdatedAt.Equal(fresh.UpdatedAt) {
				if a.sortHold == nil {
					a.sortHold = map[mrKey]time.Time{}
				}
				a.sortHold[key] = a.mrs[i].UpdatedAt
			}
		} else {
			delete(a.sortHold, key)
		}
		a.mrs[i] = fresh
		_ = index.Save(config.IndexPath("mrs"), index.MergeRequests{
			Version: index.Version, UpdatedAt: a.mrsUpdated, Items: a.mrs})
		a.mrsPane.reload()
		if redetail {
			a.reloadMRDetail(fresh)
		}
		return
	}
}

// openMRReview prepares the review worktree, where the merge request shows up
// as pending changes rather than as a stack of commits. The diff base comes
// from the API, so it is the very commit GitLab renders its own diff against.
func (a *App) openMRReview(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	path := project.PathWithNamespace
	client := a.client(mr.Instance)
	integrate := a.cfg.Integrations.Incomm
	a.runTaskOpening(fmt.Sprintf("Opening %s !%d for review", path, mr.IID),
		a.sessionOf(mr, path, session.ModeReview),
		func(log func(string)) (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			mr := a.refreshMR(client, mr, log)
			var rev workspace.Review
			if client == nil {
				log("! no token for this server, falling back to the local merge base")
			} else {
				log("Asking GitLab what this merge request is diffed against ...")
				if det, err := client.MergeRequestDetail(ctx, mr); err != nil {
					log("! " + err.Error())
					log("  falling back to the local merge base")
				} else {
					rev = workspace.Review{BaseSHA: det.DiffRefs.BaseSHA, HeadSHA: det.DiffRefs.HeadSHA}
				}
			}

			dir, err := a.newManager(mr.Instance, path, log).EnsureMRReview(mr, project, rev)
			if err != nil || !integrate {
				return dir, err
			}
			reanchorAfterUpdate(dir, log)
			if client == nil {
				return "", fmt.Errorf("incomm needs comments from the server; configure its token in Settings")
			}
			log("Importing merge request comments into Incomm ...")
			notes, err := client.MergeRequestNotes(ctx, mr, 0)
			if err != nil {
				return "", fmt.Errorf("load comments for incomm: %w", err)
			}
			if err := incomm.Import(ctx, dir, mr, notes, log); err != nil {
				return "", err
			}
			return dir, nil
		})
}

// sessionOf describes what an editor is about to be handed, for the record
// another terminal reads.
func (a *App) sessionOf(mr forge.MergeRequest, path, mode string) session.Record {
	return session.Record{
		Instance: mr.Instance,
		Server:   a.instanceLabel(mr.Instance),
		Project:  path,
		IID:      mr.IID,
		Title:    mr.Title,
		Mode:     mode,
	}
}

// mrProject is the repository a merge request belongs to. The project index
// has the clone addresses; without it, only the path is known and the manager
// builds the address from the server's own.
func (a *App) mrProject(mr forge.MergeRequest) forge.Project {
	path := a.projectPathOfMR(mr)
	if pr, ok := a.projByKey[projectKey{mr.Instance, path}]; ok {
		return pr
	}
	return forge.Project{PathWithNamespace: path, Instance: mr.Instance}
}

func openBrowser(url string) error { return workspace.OpenBrowser(url) }
