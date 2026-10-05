package ui

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/fuzzy"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/incomm"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func (a *App) newMRsPane() *pane {
	p := a.newPane("Merge requests")
	p.stackBelow = 130
	// Space marks merge requests for what can be done to several at once.
	p.markable = true
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
			tag(colMuted), len(filtered), len(a.mrs), age, a.filterSummary(a.cfg.Filters.GroupByProject)+a.whoseSummary()+a.authorSummary(), tagEnd+scope)
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
	p.onOpen = func(ask bool) {
		if mr, ok := selected(); ok {
			a.withEditor(ask, func(ed *editors.Editor) { a.openMR(mr, ed) })
		}
	}

	p.selection = func() (string, []uiAction) {
		if picked := a.markedMRs(); len(picked) > 0 {
			return fmt.Sprintf("Actions · %d marked merge requests", len(picked)), a.markedMRActions(p, picked)
		}
		mr, ok := selected()
		if !ok {
			return "", nil
		}
		return fmt.Sprintf("Actions · %s !%d", a.projectPathOfMR(mr), mr.IID), a.mergeRequestActions(p, mr)
	}
	p.screen = func() (string, []uiAction) { return "Merge requests", a.mergeRequestsActions(p) }

	p.reload = func() {
		render(p.query)
		// A pipeline seen running is followed until it ends.
		a.watchCI()
	}
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
		if !a.passesFilters(mr.Instance, path) || !a.passesMRFilters(mr) {
			continue
		}
		hay := fmt.Sprintf("%s %s !%d %s %s %s %s", a.instanceLabel(mr.Instance), path, mr.IID,
			mr.Title, mr.Author.Username+" "+a.named(mr.Instance, mr.Author).Name, mr.SourceBranch, mr.TargetBranch)
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
type mrColumns struct{ proj, iid, title, author, branch, com, pub, fresh, appr, ci, updated int }

func (a *App) mrColumns(width int, rows []int) mrColumns {
	// NEW: commits pushed since your last review; CI: the head's pipeline.
	// NEW, APPR and CI take room only when a row has something in them:
	// a list nobody has reviewed, approved or built keeps its titles whole.
	c := mrColumns{iid: 3, updated: 8, com: len("COM")}
	if a.cfg.Integrations.Incomm {
		c.pub = 3 // PUB: what waits to be published from Incomm
	}
	for _, idx := range rows {
		mr := a.mrs[idx]
		c.proj = max(c.proj, len(a.projectPathOfMR(mr)))
		c.iid = max(c.iid, len(fmt.Sprintf("!%d", mr.IID)))
		c.author = max(c.author, len([]rune(personName(a.named(mr.Instance, mr.Author)))))
		c.branch = max(c.branch, len([]rune(mr.SourceBranch)))
		c.updated = max(c.updated, len(humanAge(mr.UpdatedAt)))
		if a.mrFresh[keyOfMR(mr)] != 0 {
			c.fresh = 3
		}
		_, comW := commentWords(mr)
		c.com = max(c.com, comW)
		if _, w := approvalWords(mr, a.me[mr.Instance]); w > 0 {
			c.appr = max(c.appr, w, len("APPR"))
		}
		if mr.Pipeline != "" {
			c.ci = 2
		}
	}
	c.proj = atLeast(min(c.proj, 34), "REPO")
	c.author = atLeast(min(c.author, 18), "AUTHOR")
	c.branch = atLeast(min(c.branch, 26), "BRANCH")
	c.updated = atLeast(c.updated, "UPDATED")

	const (
		markW    = 2
		minTitle = 24
	)
	gaps := 7
	for _, w := range []int{c.fresh, c.appr, c.ci} {
		if w > 0 {
			gaps++
		}
	}
	if c.pub > 0 {
		gaps++ // its own gap
	}
	fixed := func() int {
		return markW + c.proj + c.iid + c.author + c.branch + c.com + c.pub + c.fresh + c.appr + c.ci + c.updated + gaps
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

func (a *App) drawMRs(p *pane, filtered []int) {
	previous := p.selectedIndex()
	p.table.Clear()
	grouped := a.cfg.Filters.GroupByProject
	withServer := a.multiInstance() && !grouped
	favourite := func(idx int) bool {
		mr := a.mrs[idx]
		return a.cfg.Filters.IsFavourite(mr.Instance, a.projectPathOfMR(mr), mr.IID)
	}
	star := starColumn(filtered, favourite)

	serverW := 0
	if withServer {
		for _, idx := range filtered {
			serverW = max(serverW, len([]rune(a.instanceLabel(a.mrs[idx].Instance))))
		}
		serverW = atLeast(min(serverW, 16), "SERVER")
	}
	c := a.mrColumns(p.contentWidth()-serverW-star, filtered)
	if grouped {
		// The project moves into the heading, so its width goes to the title.
		// The gap it leaves behind pays for the indent on every row.
		c.title += c.proj
		c.proj = 0
	}

	// The header is laid out the same way the rows are.
	header := []field{{text: "", width: 2 + star, colour: role("merge_requests.header")}}
	if withServer {
		header = append(header, field{text: "SERVER", width: serverW, colour: role("merge_requests.header")})
	}
	if !grouped {
		header = append(header, field{text: "REPO", width: c.proj, colour: role("merge_requests.header")})
	}
	header = append(header,
		field{text: "MR", width: c.iid, colour: role("merge_requests.header")},
		field{text: "TITLE", width: c.title, colour: role("merge_requests.header")},
		field{text: "AUTHOR", width: c.author, colour: role("merge_requests.header")},
		field{text: "BRANCH", width: c.branch, colour: role("merge_requests.header")})
	// NEW first: commits to look at come before what was said about them.
	if c.fresh > 0 {
		header = append(header, field{text: "NEW", width: c.fresh, colour: role("merge_requests.header"), right: true})
	}
	header = append(header, field{text: "COM", width: c.com, colour: role("merge_requests.header"), right: true})
	if c.pub > 0 {
		header = append(header, field{text: "PUB", width: c.pub, colour: role("merge_requests.header"), right: true})
	}
	if c.appr > 0 {
		header = append(header, field{text: "APPR", width: c.appr, colour: role("merge_requests.header")})
	}
	if c.ci > 0 {
		header = append(header, field{text: "CI", width: c.ci, colour: role("merge_requests.header")})
	}
	header = append(header, field{text: "UPDATED", width: c.updated, colour: role("merge_requests.header")})
	p.table.SetCell(0, 0, tview.NewTableCell(rowText(header)).
		SetSelectable(false).SetExpansion(1))

	drawRow := func(row, idx int) {
		mr := a.mrs[idx]
		path := a.projectPathOfMR(mr)
		disk := a.diskOf(mr.Instance, path).MRs[mr.IID]

		mark := " " + mrMark(disk)
		if grouped {
			mark = "  " + mrMark(disk)
		}
		mark = tag(mrMarkColor(disk)) + mark + tagEnd
		mark = starred(star, favourite(idx), mark)
		title := trunc(mr.Title, c.title)
		titleField := field{text: title, width: c.title, colour: role("merge_requests.title")}
		if mr.Draft {
			short := trunc(mr.Title, c.title-6)
			pad := strings.Repeat(" ", max(0, c.title-len([]rune(short))-6))
			titleField = field{raw: "[::d]draft[::-] " + tag(colText) + tview.Escape(short) + tagEnd + pad}
		}
		comments, commentsW := commentWords(mr)
		pending := ""
		if disk.Pending > 0 {
			pending = fmt.Sprintf("%d", disk.Pending)
		}

		fields := []field{{raw: mark}}
		if withServer {
			fields = append(fields, field{text: a.instanceLabel(mr.Instance), width: serverW, colour: role("merge_requests.server")})
		}
		if !grouped {
			fields = append(fields, field{text: path, width: c.proj, colour: role("merge_requests.repository")})
		}
		fields = append(fields,
			field{text: fmt.Sprintf("!%d", mr.IID), width: c.iid, colour: role("merge_requests.iid")},
			titleField,
			field{text: personName(a.named(mr.Instance, mr.Author)), width: c.author, colour: role("merge_requests.author")},
			field{text: mr.SourceBranch, width: c.branch, colour: role("merge_requests.branch")})
		if c.fresh > 0 {
			fields = append(fields, field{text: freshWords(a.mrFresh[keyOfMR(mr)]), width: c.fresh, colour: role("merge_requests.new"), right: true})
		}
		fields = append(fields, field{raw: rightAligned(comments, commentsW, c.com)})
		if c.pub > 0 {
			fields = append(fields, field{text: pending, width: c.pub, colour: role("merge_requests.pending"), right: true})
		}
		ci, ciColour := ciMark(mr.Pipeline)
		appr, apprW := approvalWords(mr, a.me[mr.Instance])
		if c.appr > 0 {
			fields = append(fields, field{raw: rightAligned(appr, apprW, c.appr)})
		}
		if c.ci > 0 {
			fields = append(fields, field{text: ci, width: c.ci, colour: ciColour})
		}
		fields = append(fields, field{text: humanAge(mr.UpdatedAt), width: c.updated, colour: role("merge_requests.updated")})

		cell := tview.NewTableCell(rowText(fields)).SetReference(idx).SetExpansion(1)
		if p.marks[idx] {
			cell.SetBackgroundColor(colMarked).SetSelectedStyle(styleMarkedSelected)
		}
		p.table.SetCell(row, 0, cell)
	}

	layout := listLayout{favourite: favourite, draw: drawRow, width: p.contentWidth()}
	if grouped {
		layout.group = func(idx int) (string, string) {
			mr := a.mrs[idx]
			return headingKey(a, mr.Instance, a.projectPathOfMR(mr))
		}
	}
	p.selectRow(previous, a.layRows(p.table, filtered, layout))
}

// mrMark shows at a glance which worktrees a merge request has on disk.
func mrMark(d mrDisk) string {
	switch {
	case d.Branch && d.Review:
		return glyphDiskBoth
	case d.Review:
		return glyphDiskReview
	case d.Branch:
		return glyphDiskBranch
	}
	return glyphDiskNone
}

func mrMarkColor(d mrDisk) tcell.Color {
	if d.Branch || d.Review {
		return colOn
	}
	return colDim
}

// openMR opens the editor in the merge request's branch worktree as it is on
// disk, and makes the worktree first only when there is none yet.
func (a *App) openMR(mr forge.MergeRequest, ed *editors.Editor) {
	project := a.mrProject(mr)
	if dir := a.mrDir(mr.Instance, project.PathWithNamespace, mr.IID, mr.SourceBranch); workspace.Exists(dir) {
		a.openNow(dir, a.sessionOf(mr, project.PathWithNamespace, session.ModeBranch), ed)
		return
	}
	client := a.client(mr.Instance)
	integrate := a.cfg.Integrations.Incomm
	a.runTaskOpening(fmt.Sprintf("Opening %s !%d", project.PathWithNamespace, mr.IID),
		a.sessionOf(mr, project.PathWithNamespace, session.ModeBranch), ed,
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
	if det.SHA != "" {
		mr.SHA = det.SHA
	}
	mr.Reviewers, mr.Assignees = det.Reviewers, det.Assignees
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
		a.saveMRIndex()
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
func (a *App) openMRReview(mr forge.MergeRequest, ed *editors.Editor) {
	a.openMRReviewFrom(mr, "", ed)
}

// reviewRefs asks the forge which two commits it diffs the merge request
// between. Without an answer the review falls back to the local merge base.
func reviewRefs(ctx context.Context, client forge.Provider, mr forge.MergeRequest, log func(string)) workspace.Review {
	if client == nil {
		log("! no token for this server, falling back to the local merge base")
		return workspace.Review{}
	}
	log("Asking the forge what this merge request is diffed against ...")
	det, err := client.MergeRequestDetail(ctx, mr)
	if err != nil {
		log("! " + err.Error())
		log("  falling back to the local merge base")
		return workspace.Review{}
	}
	return workspace.Review{BaseSHA: det.DiffRefs.BaseSHA, HeadSHA: det.DiffRefs.HeadSHA}
}

// forgeCommits asks the forge for all the commits of a merge request, oldest
// first.
func forgeCommits(ctx context.Context, client forge.Provider, mr forge.MergeRequest) ([]workspace.MRCommit, error) {
	if client == nil {
		return nil, fmt.Errorf("no token for this server")
	}
	listed, _, err := client.MergeRequestCommits(ctx, mr, 0)
	if err != nil {
		return nil, err
	}
	// The forge answers newest first.
	commits := make([]workspace.MRCommit, len(listed))
	for i, c := range listed {
		commits[len(listed)-1-i] = workspace.MRCommit{LogEntry: gitx.LogEntry{
			SHA:     c.ID,
			Subject: c.Title,
			Author:  c.AuthorName,
			When:    c.CommittedDate,
			Merge:   len(c.ParentIDs) > 1,
		}}
	}
	return commits, nil
}

// openMRReviewFrom is openMRReview narrowed to the commits from one onwards;
// an empty from is the whole merge request.
func (a *App) openMRReviewFrom(mr forge.MergeRequest, from string, ed *editors.Editor) {
	project := a.mrProject(mr)
	path := project.PathWithNamespace
	client := a.client(mr.Instance)
	integrate := a.cfg.Integrations.Incomm
	title := fmt.Sprintf("Opening %s !%d for review", path, mr.IID)
	if from != "" {
		title = fmt.Sprintf("Opening %s !%d for review from %.8s", path, mr.IID, from)
	}
	a.runTaskOpening(title,
		a.sessionOf(mr, path, session.ModeReview), ed,
		func(log func(string)) (string, error) {
			return a.prepareReview(mr, project, client, from, integrate, log)
		})
}

// cloneMRReview makes the review worktree the way Ctrl-R does - cloning the
// repository first when it has to - and stops there: C, for a review to read
// later or in another tool.
func (a *App) cloneMRReview(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	client := a.client(mr.Instance)
	integrate := a.cfg.Integrations.Incomm
	a.runTaskNoting(fmt.Sprintf("Preparing %s !%d for review", project.PathWithNamespace, mr.IID),
		func(log func(string)) (string, error) {
			dir, err := a.prepareReview(mr, project, client, "", integrate, log)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("!%d is ready for review in %s · Ctrl-R opens it", mr.IID, tildePath(dir)), nil
		})
}

// prepareReview builds or refreshes the review worktree of a merge request,
// from a commit onwards when from is set, and brings Incomm's comments in when
// that integration is on. It runs off the event loop.
func (a *App) prepareReview(mr forge.MergeRequest, project forge.Project, client forge.Provider,
	from string, integrate bool, log func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mr = a.refreshMR(client, mr, log)
	rev := reviewRefs(ctx, client, mr, log)
	rev.From = from

	dir, err := a.newManager(mr.Instance, project.PathWithNamespace, log).EnsureMRReview(mr, project, rev)
	if err == nil {
		// The review has the head now; a mark made without it is past.
		a.tv.QueueUpdate(func() { a.forgetSeen(mr) })
	}
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

// openBrowser hands an address to the system browser. Tests replace it.
var openBrowser = workspace.OpenBrowser

// ciMark is a pipeline's status as one glyph and its colour; "" for none.
// GitLab's words and GitHub's are both here.
func ciMark(status string) (string, tcell.Color) {
	switch status {
	case "manual":
		return glyphManual, role("ci.manual")
	case "scheduled":
		return glyphScheduled, role("ci.manual")
	case "canceled", "cancelled", "skipped":
		return glyphCIIdle, role("ci.idle")
	}
	switch ciStateOf(status) {
	case ciNone:
		return "", colDim
	case ciPassed:
		return glyphCIDone, role("ci.success")
	case ciFailed:
		return glyphCIDone, role("ci.failed")
	case ciRunning:
		return ciFrame(), role("ci.running")
	}
	return glyphCIIdle, role("ci.idle")
}

// ciTurn is how far the mark of a pipeline under way has turned; the
// watcher of running pipelines turns it (cipoll.go).
var ciTurn int

// ciFrame is the mark of a pipeline under way, as far as it has turned.
func ciFrame() string {
	frames := []rune(theme.Glyphs.CIRunning)
	if len(frames) == 0 {
		return glyphDot
	}
	return string(frames[ciTurn%len(frames)])
}

// ciState is what a pipeline's status comes to, whichever forge's words it
// is in.
type ciState int

const (
	ciNone ciState = iota
	ciPassed
	ciFailed
	ciRunning
	ciOther
)

func ciStateOf(status string) ciState {
	switch status {
	case "":
		return ciNone
	case "success":
		return ciPassed
	case "failed", "failure", "error":
		return ciFailed
	case "running", "pending", "created", "waiting_for_resource", "preparing":
		return ciRunning
	}
	return ciOther
}

// freshWords is the NEW column: how many commits were pushed since the last
// review, "●" when some were but they are not on disk to count.
func freshWords(n int) string {
	switch {
	case n > 0:
		return fmt.Sprintf("%s%d", glyphDot, n)
	case n < 0:
		return glyphDot
	}
	return ""
}

// loadMRFresh finds, off the event loop, which merge requests have commits
// newer than the head their review last checked out. A review records that
// head; the index knows the head now. A load that a newer one has overtaken
// is dropped.
func (a *App) loadMRFresh() {
	type job struct {
		key  mrKey
		dir  string // where to count; "" when nothing is on disk
		head string
		seen string // a mark, standing for the review's head
		mgr  *workspace.Manager
	}
	var jobs []job
	for _, mr := range a.mrs {
		if mr.SHA == "" {
			continue
		}
		path := a.projectPathOfMR(mr)
		disk := a.diskOf(mr.Instance, path)
		j := job{key: keyOfMR(mr), head: mr.SHA, seen: a.seen[seenKey(mr)], mgr: a.pathManager(mr.Instance, path)}
		switch {
		case disk.MRs[mr.IID].Review:
			j.dir = a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)
		case j.seen == "":
			continue
		case disk.Cloned:
			j.dir = a.projectDir(mr.Instance, path)
		}
		jobs = append(jobs, j)
	}
	a.freshGen++
	gen := a.freshGen
	// Counting runs git in every review; it is one job on the status line
	// however often it is started again before it ends.
	if len(jobs) > 0 && a.freshJob == nil {
		a.freshJob = a.startJob("counting new commits")
	}
	go func() {
		fresh := map[mrKey]int{}
		for _, j := range jobs {
			seen := j.seen
			if seen == "" {
				seen = j.mgr.ReadMeta(j.dir).Head
			}
			if seen == "" || seen == j.head {
				continue
			}
			n := -1
			if j.dir != "" && j.mgr.Git().HasCommit(j.dir, j.head) && j.mgr.Git().HasCommit(j.dir, seen) {
				n = j.mgr.Git().Count(j.dir, seen+".."+j.head)
			}
			if n != 0 {
				fresh[j.key] = n
			}
		}
		a.tv.QueueUpdateDraw(func() {
			if gen != a.freshGen {
				return
			}
			if a.freshJob != nil {
				a.endJob(a.freshJob)
				a.freshJob = nil
			}
			if !maps.Equal(fresh, a.mrFresh) {
				a.mrFresh = fresh
				if a.mrsPane != nil {
					a.mrsPane.reload()
				}
			}
			if then := a.afterFresh; then != nil {
				a.afterFresh = nil
				then()
			}
		})
	}()
}

// commentWords is the COM column: where the forge can tell, the threads
// still to resolve, those resolved and the comments in all, as 2/4/9 in
// their own colours; where it cannot, the comments alone. It comes back as
// markup and the cells it takes.
func commentWords(mr forge.MergeRequest) (string, int) {
	all := fmt.Sprint(mr.Comments)
	switch {
	case mr.UnresolvedKnown && (mr.Unresolved > 0 || mr.Resolved > 0 || mr.Comments > 0):
		open, done := fmt.Sprint(mr.Unresolved), fmt.Sprint(mr.Resolved)
		return tag(role("comments.unresolved")) + open + tagEnd + tag(colDim) + "/" + tagEnd +
				tag(role("comments.resolved")) + done + tagEnd + tag(colDim) + "/" + tagEnd +
				tag(role("comments.all")) + all + tagEnd,
			len(open) + len(done) + len(all) + 2
	case mr.Comments > 0:
		return tag(role("comments.all")) + all + tagEnd, len(all)
	}
	return "", 0
}

// approvalWords is the APPR column: the approvals given of those asked
// for, as 1/2 - amber while some are missing, green once all are in - or,
// where none is asked for, the approvals alone; and a mark before it when
// one of them is yours. It comes back as markup and
// the cells it takes.
func approvalWords(mr forge.MergeRequest, me string) (string, int) {
	n := len(mr.ApprovedBy)
	if n == 0 && mr.ApprovalsRequired == 0 {
		return "", 0
	}
	colour := role("approvals.missing")
	if n >= mr.ApprovalsRequired {
		colour = role("approvals.done")
	}
	words := fmt.Sprintf("%d/%d", n, mr.ApprovalsRequired)
	if mr.ApprovalsRequired == 0 {
		// "1/0" read as an error, not as nothing asked for.
		words = fmt.Sprint(n)
	}
	markup, width := tag(colour)+words+tagEnd, len(words)
	for _, who := range mr.ApprovedBy {
		if who == me && me != "" {
			markup = tag(role("approvals.mine")) + glyphApproved + tagEnd + markup
			width += len([]rune(glyphApproved))
			break
		}
	}
	return markup, width
}

// rightAligned is markup of a width padded on the left to fill cells.
func rightAligned(markup string, width, cells int) string {
	return strings.Repeat(" ", max(0, cells-width)) + markup
}
