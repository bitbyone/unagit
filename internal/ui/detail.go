package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// detailBuf builds the text of the right hand column.
type detailBuf struct{ b strings.Builder }

func (d *detailBuf) raw(s string)   { d.b.WriteString(s) }
func (d *detailBuf) blank()         { d.b.WriteString("\n") }
func (d *detailBuf) String() string { return d.b.String() }

func (d *detailBuf) title(s string) {
	d.raw(tag(colAccent) + "[::b]" + tview.Escape(s) + "[::-]" + tagEnd + "\n")
}

func (d *detailBuf) sub(s string) {
	if s == "" {
		return
	}
	d.raw(tag(colMuted) + tview.Escape(s) + tagEnd + "\n")
}

func (d *detailBuf) section(s string) {
	d.blank()
	d.raw(tag(colWarn) + "[::b]" + strings.ToUpper(s) + "[::-]" + tagEnd + "\n")
}

// kv prints an aligned label/value pair.
func (d *detailBuf) kv(k, v string) {
	if strings.TrimSpace(v) == "" {
		return
	}
	d.raw(fmt.Sprintf("%s%-13s%s %s\n", tag(colDim), k, tagEnd, v))
}

func (d *detailBuf) text(s string) {
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		d.raw("  " + tag(colText) + tview.Escape(line) + tagEnd + "\n")
	}
}

// markdown renders a comment or a description with its formatting intact.
func (d *detailBuf) markdown(s string) {
	if out := renderMarkdown(s, "  "); out != "" {
		d.raw(out + "\n")
	}
}

func esc(s string) string { return tview.Escape(s) }

// ---------------------------------------------------------------- helpers

func absTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

func when(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s %s(%s)%s", humanAge(t), tag(colDim), absTime(t), tagEnd)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// pipelineColor maps a GitLab pipeline status onto the palette.
func pipelineMark(status string) string {
	c := colMuted
	switch status {
	case "success":
		c = colOn
	case "failed":
		c = colBad
	case "running", "pending", "created", "waiting_for_resource", "preparing":
		c = colWarn
	case "canceled", "skipped", "manual", "scheduled":
		c = colDim
	}
	return tag(c) + "●" + tagEnd + " " + status
}

func users(list []forge.User) string {
	if len(list) == 0 {
		return ""
	}
	names := make([]string, 0, len(list))
	for _, u := range list {
		names = append(names, u.Username)
	}
	return esc(strings.Join(names, ", "))
}

func commitLines(d *detailBuf, commits []forge.Commit) {
	for _, c := range commits {
		d.raw(fmt.Sprintf("  %s%-8s%s %s%s%s\n",
			tag(colAccent), c.ShortID, tagEnd,
			tag(colText), esc(trim(c.Title, 60)), tagEnd))
		d.raw(fmt.Sprintf("           %s%s · %s%s\n",
			tag(colDim), esc(c.AuthorName), humanAge(c.CommittedDate), tagEnd))
	}
}

func trim(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ------------------------------------------------------- project detail

// showProjectDetail loads everything the API offers about a project and
// renders it in the right hand column.
func (a *App) showProjectDetail(pr forge.Project, focus bool) {
	p := a.projectsPane
	p.detailSeq++
	seq := p.detailSeq
	p.openDetail(pr.PathWithNamespace, a.projectSkeleton(pr), focus)

	client := a.client(pr.Instance)
	if client == nil {
		p.setDetail(pr.PathWithNamespace,
			a.renderProject(pr, nil, nil, nil, nil, []string{
				a.instanceLabel(pr.Instance) + " has no token yet - set one in Settings [S]"}))
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			detail   *forge.ProjectDetail
			commits  []forge.Commit
			langs    map[string]float64
			pipeline *forge.Pipeline
			problems []string
		)
		fail := func(what string, err error) {
			mu.Lock()
			problems = append(problems, what+": "+err.Error())
			mu.Unlock()
		}
		wg.Add(4)
		go func() {
			defer wg.Done()
			d, err := client.ProjectDetail(ctx, pr)
			if err != nil {
				fail("project", err)
				return
			}
			detail = d
		}()
		go func() {
			defer wg.Done()
			c, err := client.ProjectCommits(ctx, pr, pr.DefaultBranch, 5)
			if err != nil {
				fail("commits", err)
				return
			}
			commits = c
		}()
		go func() {
			defer wg.Done()
			l, err := client.ProjectLanguages(ctx, pr)
			if err != nil {
				fail("languages", err)
				return
			}
			langs = l
		}()
		go func() {
			defer wg.Done()
			pl, err := client.LatestPipeline(ctx, pr, pr.DefaultBranch)
			if err != nil {
				fail("pipelines", err)
				return
			}
			pipeline = pl
		}()
		wg.Wait()

		a.tv.QueueUpdateDraw(func() {
			if p.detailSeq != seq {
				return // the user moved on
			}
			p.setDetail(pr.PathWithNamespace, a.renderProject(pr, detail, commits, langs, pipeline, problems))
		})
	}()
}

func (a *App) projectSkeleton(pr forge.Project) string {
	d := &detailBuf{}
	d.title(pr.PathWithNamespace)
	d.sub(pr.Description)
	d.blank()
	d.raw(tag(colMuted) + "Loading from GitLab…" + tagEnd + "\n")
	return d.String()
}

func (a *App) renderProject(pr forge.Project, det *forge.ProjectDetail, commits []forge.Commit,
	langs map[string]float64, pipeline *forge.Pipeline, problems []string) string {

	d := &detailBuf{}
	d.title(pr.PathWithNamespace)
	if det != nil && det.Description != "" {
		d.sub(det.Description)
	} else {
		d.sub(pr.Description)
	}

	d.section("Project")
	if a.multiInstance() {
		d.kv("Server", esc(a.instanceLabel(pr.Instance)))
	}
	if det != nil {
		d.kv("Visibility", esc(det.Visibility))
		d.kv("Default", esc(det.DefaultBranch))
		d.kv("Created", when(det.CreatedAt))
		d.kv("Activity", when(det.LastActivityAt))
		d.kv("Stars", fmt.Sprintf("%d", det.StarCount))
		d.kv("Forks", fmt.Sprintf("%d", det.ForksCount))
		d.kv("Open issues", fmt.Sprintf("%d", det.OpenIssuesCount))
		d.kv("Merge method", esc(det.MergeMethod))
		d.kv("License", esc(det.License))
		if len(det.Topics) > 0 {
			d.kv("Topics", esc(strings.Join(det.Topics, ", ")))
		}
		if det.CommitCount > 0 {
			d.kv("Commits", fmt.Sprintf("%d", det.CommitCount))
		}
		if det.RepositorySize > 0 {
			d.kv("Repo size", humanBytes(det.RepositorySize))
		}
		d.kv("Forked from", esc(det.ForkedFrom))
		if det.Archived {
			d.kv("Archived", tag(colWarn)+"yes"+tagEnd)
		}
		if det.EmptyRepo {
			d.kv("Repository", tag(colWarn)+"empty"+tagEnd)
		}
		d.kv("URL", tag(colDim)+esc(det.WebURL)+tagEnd)
	} else {
		d.kv("Default", esc(pr.DefaultBranch))
		d.kv("Activity", when(pr.LastActivityAt))
		d.kv("URL", tag(colDim)+esc(pr.WebURL)+tagEnd)
	}

	if len(langs) > 0 {
		d.section("Languages")
		type lang struct {
			name string
			pct  float64
		}
		list := make([]lang, 0, len(langs))
		for n, v := range langs {
			list = append(list, lang{n, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].pct > list[j].pct })
		for _, l := range list {
			bars := int(l.pct/5 + 0.5)
			if bars == 0 && l.pct > 0 {
				bars = 1
			}
			d.raw(fmt.Sprintf("  %s%-14s%s %s%-20s%s %s%.1f%%%s\n",
				tag(colText), esc(trim(l.name, 14)), tagEnd,
				tag(colAccent), strings.Repeat("━", bars), tagEnd,
				tag(colDim), l.pct, tagEnd))
		}
	}

	if pipeline != nil {
		d.section("Latest pipeline")
		d.raw("  " + pipelineMark(pipeline.Status) + tag(colDim) + "  " + esc(pipeline.Ref) +
			" · " + humanAge(pipeline.UpdatedAt) + tagEnd + "\n")
	}

	if len(commits) > 0 {
		d.section(fmt.Sprintf("Recent commits (%d)", len(commits)))
		commitLines(d, commits)
	}

	// Open merge requests already known from the index.
	var open []forge.MergeRequest
	for _, mr := range a.mrs {
		if mr.Instance == pr.Instance && a.projectPathOfMR(mr) == pr.PathWithNamespace {
			open = append(open, mr)
		}
	}
	if len(open) > 0 {
		d.section(fmt.Sprintf("Open merge requests (%d)", len(open)))
		for i, mr := range open {
			if i == 8 {
				d.raw(fmt.Sprintf("  %s… %d more%s\n", tag(colDim), len(open)-i, tagEnd))
				break
			}
			d.raw(fmt.Sprintf("  %s!%-5d%s %s%s%s %s%s%s\n",
				tag(colWarn), mr.IID, tagEnd,
				tag(colText), esc(trim(mr.Title, 46)), tagEnd,
				tag(colDim), esc(mr.Author.Username), tagEnd))
		}
	}

	d.section("On disk")
	info := a.diskOf(pr.Instance, pr.PathWithNamespace)
	if info.Cloned {
		d.kv("Clone", tag(colOn)+"●"+tagEnd+" "+esc(a.projectDir(pr.Instance, pr.PathWithNamespace)))
		d.kv("Branch", esc(info.Branch))
	} else {
		d.kv("Planned path", tag(colDim)+esc(a.projectDir(pr.Instance, pr.PathWithNamespace))+tagEnd)
		d.kv("Clone", tag(colDim)+"○ not cloned (Ctrl-C clones, Ctrl-O clones and opens)"+tagEnd)
	}
	if n := len(info.MRs); n > 0 {
		d.kv("Worktrees", fmt.Sprintf("%d merge request worktree(s)", n))
	}

	if len(problems) > 0 {
		d.section("Could not load")
		for _, p := range problems {
			d.raw("  " + tag(colBad) + esc(p) + tagEnd + "\n")
		}
	}
	return d.String()
}

// ------------------------------------------------- merge request detail

// showMRDetail always fetches fresh data: a merge request under review changes
// while you look at it.
func (a *App) showMRDetail(mr forge.MergeRequest, focus bool) {
	p := a.mrsPane
	path := a.projectPathOfMR(mr)
	title := fmt.Sprintf("%s !%d", path, mr.IID)

	p.detailSeq++
	seq := p.detailSeq

	skeleton := &detailBuf{}
	skeleton.title(fmt.Sprintf("!%d  %s", mr.IID, mr.Title))
	skeleton.sub(path)
	skeleton.blank()
	skeleton.raw(tag(colMuted) + "Loading from GitLab…" + tagEnd + "\n")
	p.openDetail(title, skeleton.String(), focus)

	client := a.client(mr.Instance)
	if client == nil {
		p.setDetail(title, a.renderMR(mr, path, nil, nil, nil, 0, nil, []string{
			a.instanceLabel(mr.Instance) + " has no token yet - set one in Settings [S]"}))
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		var (
			wg          sync.WaitGroup
			mu          sync.Mutex
			detail      *forge.MergeRequestDetail
			notes       []forge.Note
			commits     []forge.Commit
			commitCount int
			approvals   *forge.Approvals
			problems    []string
		)
		fail := func(what string, err error) {
			mu.Lock()
			problems = append(problems, what+": "+err.Error())
			mu.Unlock()
		}
		wg.Add(3)
		go func() {
			defer wg.Done()
			d, err := client.MergeRequestDetail(ctx, mr)
			if err != nil {
				fail("merge request", err)
				return
			}
			detail = d
		}()
		go func() {
			defer wg.Done()
			n, err := client.MergeRequestNotes(ctx, mr, 50)
			if err != nil {
				fail("comments", err)
				return
			}
			notes = n
		}()
		go func() {
			defer wg.Done()
			c, n, err := client.MergeRequestCommits(ctx, mr, 10)
			if err != nil {
				fail("commits", err)
				return
			}
			commits, commitCount = c, n
		}()
		// Approvals are a paid feature on some tiers; a failure is not worth
		// reporting.
		approvals, _ = client.MergeRequestApprovals(ctx, mr)
		wg.Wait()

		a.tv.QueueUpdateDraw(func() {
			if p.detailSeq != seq {
				return
			}
			p.setDetail(title, a.renderMR(mr, path, detail, notes, commits, commitCount, approvals, problems))
		})
	}()
}

func (a *App) renderMR(mr forge.MergeRequest, path string, det *forge.MergeRequestDetail,
	notes []forge.Note, commits []forge.Commit, commitCount int, approvals *forge.Approvals,
	problems []string) string {

	d := &detailBuf{}
	title := mr.Title
	if det != nil {
		title = det.Title
	}
	head := fmt.Sprintf("!%d  %s", mr.IID, title)
	if (det != nil && det.Draft) || mr.Draft {
		head += "  [draft]"
	}
	d.title(head)
	d.sub(path)
	d.raw(fmt.Sprintf("%s%s → %s%s\n", tag(colAccent), esc(mr.SourceBranch), esc(mr.TargetBranch), tagEnd))

	d.section("Merge request")
	if a.multiInstance() {
		d.kv("Server", esc(a.instanceLabel(mr.Instance)))
	}
	author := esc(mr.Author.Username)
	if det != nil {
		author = esc(det.Author.Username)
		if det.Author.Name != "" {
			author += tag(colDim) + " (" + esc(det.Author.Name) + ")" + tagEnd
		}
	}
	d.kv("Author", author)
	if det != nil {
		d.kv("Created", when(det.CreatedAt))
		d.kv("Updated", when(det.UpdatedAt))
		d.kv("Reviewers", users(det.Reviewers))
		d.kv("Assignees", users(det.Assignees))
		if len(det.Labels) > 0 {
			d.kv("Labels", esc(strings.Join(det.Labels, ", ")))
		}
		d.kv("Milestone", esc(det.Milestone))
		status := det.MergeStatus
		if det.HasConflicts {
			status = tag(colBad) + "conflicts" + tagEnd + tag(colDim) + " (" + esc(status) + ")" + tagEnd
		} else {
			status = esc(status)
		}
		d.kv("Merge status", status)
		if !det.BlockingDiscussionsResolved {
			d.kv("Discussions", tag(colWarn)+"unresolved threads block the merge"+tagEnd)
		}
		if det.Pipeline != nil {
			d.kv("Pipeline", pipelineMark(det.Pipeline.Status))
		}
		size := ""
		if commitCount > 0 {
			size = fmt.Sprintf("%d commit(s)", commitCount)
		}
		if det.ChangesCount != "" {
			if size != "" {
				size += tag(colDim) + " · " + tagEnd
			}
			size += esc(det.ChangesCount) + " file(s)"
		}
		if det.DivergedCommitsCount > 0 {
			size += fmt.Sprintf("%s · %d behind %s%s",
				tag(colWarn), det.DivergedCommitsCount, esc(det.TargetBranch), tagEnd)
		}
		d.kv("Size", size)
		d.kv("Comments", fmt.Sprintf("%d", det.UserNotesCount))
		if det.Upvotes+det.Downvotes > 0 {
			d.kv("Votes", fmt.Sprintf("+%d / -%d", det.Upvotes, det.Downvotes))
		}
		if det.TasksTotal > 0 {
			d.kv("Tasks", fmt.Sprintf("%d/%d done", det.TasksDone, det.TasksTotal))
		}
	} else {
		d.kv("Updated", when(mr.UpdatedAt))
	}
	if approvals != nil {
		text := fmt.Sprintf("%d", len(approvals.ApprovedBy))
		if approvals.Required > 0 {
			text = fmt.Sprintf("%d of %d", len(approvals.ApprovedBy), approvals.Required)
		}
		if len(approvals.ApprovedBy) > 0 {
			text += tag(colDim) + " · " + esc(strings.Join(approvals.ApprovedBy, ", ")) + tagEnd
		}
		d.kv("Approvals", text)
	}
	if det != nil {
		d.kv("URL", tag(colDim)+esc(det.WebURL)+tagEnd)
	}

	if det != nil && strings.TrimSpace(det.Description) != "" {
		d.section("Description")
		d.markdown(det.Description)
	}

	if len(commits) > 0 {
		heading := fmt.Sprintf("Commits (%d)", len(commits))
		if commitCount > len(commits) {
			heading = fmt.Sprintf("Commits (%d of %d)", len(commits), commitCount)
		}
		d.section(heading)
		commitLines(d, commits)
	}

	// Only real comments; the system notes are bookkeeping noise. The detail
	// column shows the three newest, the whole conversation lives behind c.
	var human []forge.Note
	for _, n := range notes {
		if !n.System && strings.TrimSpace(n.Body) != "" {
			human = append(human, n)
		}
	}
	if len(human) > 0 {
		const shown = 3
		total := len(human)
		if det != nil && det.UserNotesCount > total {
			total = det.UserNotesCount
		}
		heading := fmt.Sprintf("Comments (%d)", total)
		if total > shown {
			heading = fmt.Sprintf("Comments (%d newest of %d)", shown, total)
		}
		d.section(heading)
		for i, n := range human {
			if i == shown {
				d.raw(fmt.Sprintf("  %s… %d more · press c to read them all%s\n",
					tag(colDim), total-shown, tagEnd))
				break
			}
			d.raw("  " + noteHeader(n, false) + "\n")
			d.markdown(trimBody(n.Body))
			d.blank()
		}
	}

	d.section("On disk")
	disk := a.diskOf(mr.Instance, path).MRs[mr.IID]
	if disk.Branch {
		d.kv("Branch", tag(colOn)+"●"+tagEnd+" "+esc(a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)))
	} else {
		d.kv("Branch", tag(colDim)+"○ Ctrl-O checks the branch out and opens the editor"+tagEnd)
	}
	if disk.Review {
		d.kv("Review", tag(colOn)+"◐"+tagEnd+" "+esc(a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)))
	} else {
		d.kv("Review", tag(colDim)+"○ Ctrl-R opens the change as pending edits to diff through"+tagEnd)
	}

	if len(problems) > 0 {
		d.section("Could not load")
		for _, p := range problems {
			d.raw("  " + tag(colBad) + esc(p) + tagEnd + "\n")
		}
	}
	return d.String()
}

// trimBody shortens a very long comment so one chatty thread cannot push
// everything else out of view.
func trimBody(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 12 {
		lines = append(lines[:12], "…")
	}
	return strings.Join(lines, "\n")
}
