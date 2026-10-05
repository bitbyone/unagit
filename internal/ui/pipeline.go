package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// A pipeline up close: its jobs - GitHub's check runs - with the first that
// failed under the cursor, those still to run listed too. Enter reads a
// job's log, or a trigger job's pipeline; R runs it again, or starts a
// manual one; w opens it in the browser, so a red CI column can be dealt with without leaving
// unagit. It is a merge request's pipeline, or a branch's - a clone's, a
// worktree's, one repository's of a group.

// ciTarget is whose pipeline is shown: what it is called, the repository its
// jobs belong to, and how its newest pipeline is read.
type ciTarget struct {
	instance string
	project  forge.Project
	label    string
	load     func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error)
	// back, when set, is where Esc on the jobs goes: a downstream pipeline
	// goes back to the one that started it.
	back func()
	// pipelines, when set, lists the other pipelines of the same merge
	// request, branch or commit, newest first, for P.
	pipelines func(ctx context.Context, client forge.Provider) ([]forge.Pipeline, error)
}

// mrCI is a merge request's pipeline: the one its head ran.
func (a *App) mrCI(mr forge.MergeRequest) ciTarget {
	path := a.projectPathOfMR(mr)
	return ciTarget{
		instance: mr.Instance,
		project:  forge.Project{ID: mr.ProjectID, PathWithNamespace: path, Instance: mr.Instance},
		label:    fmt.Sprintf("%s !%d", path, mr.IID),
		load: func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error) {
			return client.PipelineJobs(ctx, mr)
		},
		pipelines: func(ctx context.Context, client forge.Provider) ([]forge.Pipeline, error) {
			return client.Pipelines(ctx, forge.Project{ID: mr.ProjectID, PathWithNamespace: path, Instance: mr.Instance}, forge.PipelineQuery{MR: &mr})
		},
	}
}

// branchCI is a branch's newest pipeline in a repository of the lists.
func (a *App) branchCI(instance, projectPath, branch string) (ciTarget, error) {
	pr, ok := a.projByKey[projectKey{instance, projectPath}]
	if !ok {
		return ciTarget{}, fmt.Errorf("%s is not in the list of repositories - R refreshes it", projectPath)
	}
	if branch == "" || strings.HasPrefix(branch, "(") || strings.HasPrefix(branch, "@") {
		return ciTarget{}, fmt.Errorf("%s has no branch checked out - a pipeline belongs to a branch", projectPath)
	}
	return ciTarget{
		instance: instance,
		project:  pr,
		label:    projectPath + " · " + branch,
		load: func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error) {
			return client.BranchPipelineJobs(ctx, pr, branch)
		},
		pipelines: func(ctx context.Context, client forge.Provider) ([]forge.Pipeline, error) {
			return client.Pipelines(ctx, pr, forge.PipelineQuery{Ref: branch})
		},
	}, nil
}

// commitCI is the pipelines that ran for one commit, the newest first.
func commitCI(instance string, project forge.Project, label, sha string) ciTarget {
	list := func(ctx context.Context, client forge.Provider) ([]forge.Pipeline, error) {
		return client.Pipelines(ctx, project, forge.PipelineQuery{SHA: sha})
	}
	return ciTarget{
		instance: instance,
		project:  project,
		label:    label + " · " + shortSHA(sha),
		load: func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error) {
			pipes, err := list(ctx, client)
			if err != nil || len(pipes) == 0 {
				return nil, nil, err
			}
			jobs, err := client.Jobs(ctx, project, pipes[0])
			return &pipes[0], jobs, err
		},
		pipelines: list,
	}
}

// ofPipeline is a target narrowed to one of its pipelines: the jobs are
// that pipeline's, and P still lists the others.
func (t ciTarget) ofPipeline(pipe forge.Pipeline, back func()) ciTarget {
	t.back = back
	t.load = func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error) {
		fresh := pipe
		jobs, err := client.Jobs(ctx, t.project, pipe)
		return &fresh, jobs, err
	}
	return t
}

// showMRPipeline shows a merge request's pipeline.
func (a *App) showMRPipeline(mr forge.MergeRequest, focus int64) { a.showPipeline(a.mrCI(mr), focus) }

// showBranchPipeline shows a branch's pipeline, or says why there is none to
// show.
func (a *App) showBranchPipeline(instance, projectPath, branch string) {
	target, err := a.branchCI(instance, projectPath, branch)
	if err != nil {
		a.flash(err.Error())
		return
	}
	a.showPipeline(target, 0)
}

// worktreePipeline shows the pipeline of a worktree's branch: its open merge
// request's when there is one, as the merge request list shows it, else the
// branch's own. A group asks which of its repositories first.
func (a *App) worktreePipeline(r worktreeRow) {
	if !r.grouped() {
		if mr, open := a.openMRFor(r); open {
			a.showMRPipeline(mr, 0)
			return
		}
		a.showBranchPipeline(r.Instance, r.Path, r.Branch)
		return
	}
	if len(r.Members) == 0 {
		a.flash(r.Path + " holds no repository on disk")
		return
	}
	items := make([]pickItem, len(r.Members))
	for i, m := range r.Members {
		mark, _ := ciMark("")
		if mr, open := a.openMRFor(m); open {
			mark, _ = ciMark(mr.Pipeline)
		}
		items[i] = pickItem{Label: esc(strings.TrimSpace(mark + " " + m.Path)), Sub: esc(m.Branch), Data: i}
	}
	a.showPicker("Pipeline of which repository · "+r.Path, items, func(it pickItem) {
		a.worktreePipeline(r.Members[it.Data.(int)])
	})
}

// showPipeline loads a pipeline and lists its jobs, the cursor on focus or
// else on the first that failed.
func (a *App) showPipeline(target ciTarget, focus int64) { a.showPipelineThen(target, focus, nil) }

// showPipelineThen is showPipeline with something said once the jobs are on
// screen, so the list does not cover it.
func (a *App) showPipelineThen(target ciTarget, focus int64, then func()) {
	client := a.client(target.instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(target.instance))
		return
	}
	var pipe *forge.Pipeline
	var jobs []forge.Job
	a.runTaskThen("Reading the pipeline of "+target.label, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		pipe, jobs, err = target.load(ctx, client)
		return "", err
	}, func(string) {
		if pipe == nil {
			a.note(target.label + " has no pipeline")
			return
		}
		a.listJobs(target, pipe, jobs, focus)
		if then != nil {
			then()
		}
	})
}

// listJobs shows the jobs of a pipeline, and while it runs follows it: the
// jobs are read again every so often and put in place, the cursor staying
// on the job it was on.
func (a *App) listJobs(target ciTarget, pipe *forge.Pipeline, jobs []forge.Job, focus int64) {
	if len(jobs) == 0 {
		mark, _ := ciMark(pipe.Status)
		a.note(fmt.Sprintf("%s: %s %s, no jobs to show", target.label, mark, pipe.Status))
		return
	}
	// state is the pipeline as last read; the follower puts it again.
	state := &pipelineState{pipe: pipe, jobs: jobs}
	start := -1
	for i, j := range jobs {
		switch {
		case j.ID == focus && focus != 0:
			start = i
		case start < 0 && focus == 0 && j.Status == "failed" && !j.Retried:
			start = i
		}
	}
	at := func(it pickItem) forge.Job { return it.Data.(forge.Job) }
	back := func(it pickItem) func() {
		return func() { a.listJobs(target, state.pipe, state.jobs, at(it).ID) }
	}
	opts := pickerOptions{start: max(start, 0), wide: true, explain: true, enterHint: "log", back: target.back,
		enterName: "Show Log", enterAbout: "Read the job's log, or a trigger job's own pipeline.",
		same: func(x, y pickItem) bool { return at(x).ID == at(y).ID },
		keys: []pickKey{
			{keys: "R", hint: "run", name: "Run Job", about: "Start a manual or delayed job, or run a finished one again.", run: func(it pickItem) { a.runJob(target, at(it)) }},
			a.browserKey("w", "browser", "Open Job in Browser", "The job's page on the forge; the jobs stay open.",
				func(it pickItem) string { return at(it).WebURL }),
			a.browserKey("W", "pipeline", "Open Pipeline in Browser", "The whole pipeline's page on the forge; the jobs stay open.",
				func(pickItem) string { return state.pipe.WebURL }),
		}}
	if target.pipelines != nil {
		opts.keys = append(opts.keys, pickKey{keys: "P", hint: "pipelines", name: "Earlier Pipelines…",
			about: "Every pipeline of the same merge request, branch or commit, the newest first.",
			run:   func(it pickItem) { a.showPipelineList(target, state.pipe.ID, back(it)) }})
	}
	picker := a.showPickerWith(state.title(target), jobItems(jobs), opts, func(it pickItem) {
		job := at(it)
		if job.Trigger {
			a.showDownstream(target, job, back(it))
			return
		}
		a.showJobLog(target, job, back(it))
	})
	if state.moving() {
		go a.followPipeline(target, state, picker)
	}
}

// pipelineState is a pipeline and its jobs as last read.
type pipelineState struct {
	pipe *forge.Pipeline
	jobs []forge.Job
}

// moving reports whether the pipeline will change by itself: it, or a job
// of it, is running or waiting its turn or its time. One that waits only
// for a hand does not.
func (s *pipelineState) moving() bool {
	if ciStateOf(s.pipe.Status) == ciRunning {
		return true
	}
	for _, j := range s.jobs {
		if jobMoving(j) {
			return true
		}
	}
	return false
}

// jobMoving reports whether a job will change by itself. An earlier
// attempt never will, whatever it was left saying.
func jobMoving(j forge.Job) bool {
	return !j.Retried && (ciStateOf(j.Status) == ciRunning || j.Status == "scheduled")
}

// title is the jobs' title: whose pipeline, how it stands, and whether it
// is being followed.
func (s *pipelineState) title(target ciTarget) string {
	mark, _ := ciMark(s.pipe.Status)
	mark, status := painted(mark, s.pipe.Status)
	title := fmt.Sprintf("Pipeline · %s · %s %s", esc(target.label), mark, status)
	if s.moving() {
		title += " · following"
	}
	return title
}

// jobItems are the rows of the jobs, each carrying its job.
func jobItems(jobs []forge.Job) []pickItem {
	nameW, stageW := 0, 0
	for _, j := range jobs {
		nameW = max(nameW, len([]rune(jobName(j))))
		stageW = max(stageW, len([]rune(j.Stage)))
	}
	nameW = min(nameW, 48)
	items := make([]pickItem, len(jobs))
	for i, j := range jobs {
		mark, status := painted(jobMark(j), j.Status)
		items[i] = pickItem{
			Label: mark + esc(fmt.Sprintf("  %-*s  %-*s", stageW, j.Stage, nameW, trim(jobName(j), nameW))),
			Sub:   strings.TrimSpace(status + "  " + esc(duration(j.Duration))),
			About: esc(jobAbout(j)),
			Data:  j,
		}
	}
	return items
}

// followEvery is how often a running pipeline is read again; a running
// job's log is read a little more often.
func (a *App) followEvery() time.Duration {
	if a.ciEvery > 0 {
		return a.ciEvery
	}
	return 5 * time.Second
}

// followPipeline reads a running pipeline again every so often and puts
// its jobs in the picker, until the picker is closed or nothing in the
// pipeline moves any more. A failed reading is tried again next time.
func (a *App) followPipeline(target ciTarget, state *pipelineState, picker *livePicker) {
	client := a.client(target.instance)
	if client == nil {
		return
	}
	for {
		time.Sleep(a.followEvery())
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		var still bool
		if !a.onLoopWait(ctx, func() { still = picker.open() && state.moving() }) || !still {
			cancel()
			return
		}
		pipe, jobs, err := target.load(ctx, client)
		cancel()
		if err != nil || pipe == nil {
			continue
		}
		a.tv.QueueUpdateDraw(func() {
			if !picker.open() {
				return
			}
			state.pipe, state.jobs = pipe, jobs
			picker.set(state.title(target), jobItems(jobs))
		})
	}
}

// waitsForAHand reports whether a job runs only when started: a manual
// one, or a delayed one before its time.
func waitsForAHand(j forge.Job) bool { return j.Status == "manual" || j.Status == "scheduled" }

// jobMark is a job's state as one glyph: a manual job's and a delayed one's
// are told apart from those that merely wait their turn.
// stateColour is the colour of a job's or a pipeline's state, as the CI
// column has it.
func stateColour(status string) tcell.Color {
	_, c := ciMark(status)
	return c
}

// painted is a state's mark and its word in the state's colour.
func painted(mark, status string) (string, string) {
	c := stateColour(status)
	return tag(c) + esc(mark) + tagEnd, tag(c) + esc(status) + tagEnd
}

func jobMark(j forge.Job) string {
	switch j.Status {
	case "manual":
		return glyphManual
	case "scheduled":
		return glyphScheduled
	}
	if mark, _ := ciMark(j.Status); mark != "" {
		return mark
	}
	return glyphRing
}

// jobName is a job's name, a trigger job's marked as one and an earlier
// attempt's set in under the attempt that followed.
func jobName(j forge.Job) string {
	name := j.Name
	if j.Trigger {
		name = glyphTrigger + " " + name
	}
	if j.Retried {
		name = glyphRetried + " " + name
	}
	return name
}

// jobAbout is the sentence under the jobs about the one under the cursor.
func jobAbout(j forge.Job) string {
	parts := []string{j.Name, j.Status}
	switch {
	case j.Retried:
		parts = append(parts, "an earlier attempt: the job was run again - Enter reads what this one said")
	case j.Status == "manual":
		parts = append(parts, "waits to be started - R starts it")
	case j.Status == "scheduled":
		parts = append(parts, "waits for its time - R starts it now")
	case j.Trigger && j.Downstream != nil:
		parts = append(parts, "starts a pipeline of its own - Enter lists its jobs")
	case j.Trigger:
		parts = append(parts, "starts a pipeline of its own, not yet started")
	}
	if d := duration(j.Duration); d != "" {
		parts = append(parts, d)
	}
	return strings.Join(parts, " · ") + " " + j.WebURL
}

// showDownstream lists the jobs of the pipeline a trigger job started; Esc
// comes back to the pipeline it was started from.
func (a *App) showDownstream(parent ciTarget, job forge.Job, back func()) {
	if job.Downstream == nil {
		a.note(job.Name + " has not started its pipeline yet")
		back()
		return
	}
	project := forge.Project{ID: job.Downstream.ProjectID, Instance: parent.instance}
	if project.ID == parent.project.ID {
		project = parent.project
	}
	a.showPipeline(ciTarget{
		instance: parent.instance,
		project:  project,
		label:    parent.label + " " + glyphTrigger + " " + job.Name,
		back:     back,
		load: func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error) {
			return client.DownstreamJobs(ctx, job)
		},
	}, 0)
}

// duration says how long a job ran, coarsely.
func duration(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	d := time.Duration(seconds) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// runJob starts a job: one that waits for a hand is played, one that ran
// is run again; then the pipeline comes back, the cursor on it.
func (a *App) runJob(target ciTarget, job forge.Job) {
	client := a.client(target.instance)
	title, said := "Retrying "+job.Name, job.Name+" runs again"
	if waitsForAHand(job) {
		title, said = "Starting "+job.Name, job.Name+" started"
	}
	a.runTaskThen(title, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if waitsForAHand(job) {
			return "", client.PlayJob(ctx, target.project, job)
		}
		return "", client.RetryJob(ctx, target.project, job)
	}, func(string) {
		a.showPipelineThen(target, job.ID, func() { a.done(said) })
	})
}

// logTail is as much of a log as is shown: the end, where a job fails.
const logTail = 400

// sgr matches the escape sequences a CI log is coloured with.
var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// noise matches what a log carries that is not text and not colour: the
// other escape sequences - clearing a line, hiding the cursor - and
// GitLab's section markers.
var noise = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-ln-z]|section_(start|end):[0-9]+:[A-Za-z0-9_.-]+(\[[^\]]*\])?`)

// logMarkup turns a CI log into the markup a text view draws, in its
// colours as the forge's own page shows them; of a line rewritten in place -
// a progress bar - only what it ended as.
func logMarkup(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if at := strings.LastIndex(strings.TrimRight(line, "\r"), "\r"); at >= 0 {
			line = line[at+1:]
		}
		lines[i] = colourLine(noise.ReplaceAllString(line, ""))
	}
	if len(lines) > logTail {
		lines = append([]string{fmt.Sprintf("… %d earlier lines; w opens the whole log", len(lines)-logTail)}, lines[len(lines)-logTail:]...)
	}
	// Each line ends where it began, so a colour left on cannot run into the
	// next, nor into the line about the earlier ones.
	return strings.Join(lines, "[-:-:-]\n") + "[-:-:-]"
}

// colourLine turns one line's colour sequences into tview's tags. The text
// between them is escaped first: a log is full of brackets, and "[red]" in
// it is something a test printed, not a colour.
func colourLine(line string) string {
	var b strings.Builder
	last := 0
	for _, at := range sgr.FindAllStringIndex(line, -1) {
		b.WriteString(tview.Escape(line[last:at[0]]))
		b.WriteString(keptSGR(line[at[0]:at[1]]))
		last = at[1]
	}
	b.WriteString(tview.Escape(line[last:]))
	return tview.TranslateANSI(b.String())
}

// keptSGR is a colour sequence without what cannot be drawn well here:
// underline and blink (an underline once set stays on in this tview), and
// black ink, which is the background of most themes - it becomes grey, as
// on GitLab's own page.
func keptSGR(seq string) string {
	params := strings.Split(strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m"), ";")
	kept := params[:0]
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "4", "04", "5", "05", "24", "25":
			continue
		case "30":
			kept = append(kept, "90")
			continue
		case "38", "48":
			// An extended colour takes its arguments with it, untouched.
			kept = append(kept, params[i:]...)
			i = len(params)
			continue
		default:
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 && len(params) > 0 && params[0] != "" && params[0] != "0" {
		return ""
	}
	return "\x1b[" + strings.Join(kept, ";") + "m"
}

// showJobLog reads a job's log and shows its end; Esc goes back. While the
// job runs the log is read again every so often, and when the end is in
// view it stays in view, as tail -f does.
func (a *App) showJobLog(target ciTarget, job forge.Job, back func()) {
	client := a.client(target.instance)
	var text string
	a.runTaskThen("Reading the log of "+job.Name, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		text, err = readJobLog(ctx, client, target, job)
		return "", err
	}, func(string) {
		view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(true).SetTextColor(colText)
		view.SetText(text)
		view.ScrollToEnd()
		title := func(j forge.Job) string {
			mark, status := painted(jobMark(j), j.Status)
			t := fmt.Sprintf("%s %s · %s", mark, esc(j.Name), status)
			if jobMoving(j) {
				t += " · following"
			}
			return t
		}
		box(view.Box, title(job))
		hintPanel(view.Box, func() string {
			return "j/k scroll · Ctrl-D/U half a page · Ctrl-F/B page · g/G top/end · w browser · Esc back to the jobs"
		}, 0, 0, 1, 1)
		// atEnd is whether the reader is at the end, where new lines are
		// followed; scrolling up leaves it, G comes back to it.
		atEnd := true
		view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			switch ev.Key() {
			case tcell.KeyUp, tcell.KeyPgUp, tcell.KeyHome, tcell.KeyCtrlU, tcell.KeyCtrlB:
				atEnd = false
			case tcell.KeyEnd:
				atEnd = true
			case tcell.KeyRune:
				switch ev.Rune() {
				case 'k', 'g':
					atEnd = false
				case 'G':
					atEnd = true
				}
			}
			if halfPage(view, ev) {
				return nil
			}
			switch {
			case ev.Key() == tcell.KeyEsc, ev.Rune() == 'q', ev.Rune() == 'h':
				a.closeModal(pageCommit)
				back()
				return nil
			case ev.Rune() == 'w':
				a.openWeb(job.WebURL)
				return nil
			}
			return ev
		})
		page := modalPct(view, 90, 85)
		a.pages.AddPage(pageCommit, page, true, true)
		a.tv.SetFocus(view)
		if jobMoving(job) {
			open := func() bool { return a.pages.GetPage(pageCommit) == page }
			go a.followLog(target, job, open, func(j forge.Job, text string) {
				row, col := view.GetScrollOffset()
				view.SetText(text)
				if atEnd {
					view.ScrollToEnd()
				} else {
					view.ScrollTo(row, col)
				}
				box(view.Box, title(j))
			})
		}
	})
}

// readJobLog is a job's log as markup. GitHub gives a log only once its
// job has finished, and that is said in its place rather than as an error.
func readJobLog(ctx context.Context, client forge.Provider, target ciTarget, job forge.Job) (string, error) {
	raw, err := client.JobLog(ctx, target.project, job)
	if err != nil && jobMoving(job) && client.Kind() == forge.KindGitHub {
		return tag(colMuted) + "GitHub gives a job's log once the job has finished; it appears here then." + tagEnd, nil
	}
	return logMarkup(raw), err
}

// followLog reads a running job and its log again every so often and hands
// both to show, until the log is closed or the job is done - read once
// more then, so its last lines are there.
func (a *App) followLog(target ciTarget, job forge.Job, open func() bool, show func(forge.Job, string)) {
	client := a.client(target.instance)
	if client == nil {
		return
	}
	for jobMoving(job) {
		time.Sleep(a.followEvery() * 3 / 5)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		var still bool
		if !a.onLoopWait(ctx, func() { still = open() }) || !still {
			cancel()
			return
		}
		if _, jobs, err := target.load(ctx, client); err == nil {
			for _, j := range jobs {
				if j.ID == job.ID {
					job = j
				}
			}
		}
		text, err := readJobLog(ctx, client, target, job)
		cancel()
		if err != nil {
			continue
		}
		current := job
		a.tv.QueueUpdateDraw(func() {
			if open() {
				show(current, text)
			}
		})
	}
}

// showPipelineList lists the pipelines of a target's merge request, branch
// or commit, the one shown marked; Enter lists a pipeline's jobs, and Esc
// comes back to back.
func (a *App) showPipelineList(target ciTarget, current int, back func()) {
	client := a.client(target.instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(target.instance))
		return
	}
	var pipes []forge.Pipeline
	a.runTaskThen("Reading the pipelines of "+target.label, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		pipes, err = target.pipelines(ctx, client)
		return "", err
	}, func(string) {
		a.listPipelines(target, pipes, current, back)
	})
}

// showCommitPipelines reads the pipelines of a commit: one goes straight to
// its jobs, several are listed; back is where Esc comes to from either.
func (a *App) showCommitPipelines(target ciTarget, back func()) {
	client := a.client(target.instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(target.instance))
		return
	}
	var pipes []forge.Pipeline
	a.runTaskThen("Reading the pipelines of "+target.label, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		pipes, err = target.pipelines(ctx, client)
		return "", err
	}, func(string) {
		if len(pipes) == 1 {
			a.showPipeline(target.ofPipeline(pipes[0], back), 0)
			return
		}
		a.listPipelines(target, pipes, 0, back)
	})
}

// listPipelines shows pipelines read for a target.
func (a *App) listPipelines(target ciTarget, pipes []forge.Pipeline, current int, back func()) {
	if len(pipes) == 0 {
		a.note(target.label + " has no pipeline")
		if back != nil {
			back()
		}
		return
	}
	refW := 0
	for _, p := range pipes {
		refW = max(refW, len([]rune(p.Ref)))
	}
	refW = min(refW, 32)
	items := make([]pickItem, len(pipes))
	start := 0
	for i, p := range pipes {
		mark, _ := ciMark(p.Status)
		if mark == "" {
			mark = glyphRing
		}
		on := " "
		if p.ID == current && current != 0 {
			on, start = glyphDot, i
		}
		when := p.CreatedAt
		if when.IsZero() {
			when = p.UpdatedAt
		}
		age := ""
		if !when.IsZero() {
			age = humanAge(when)
		}
		id := "#" + fmt.Sprint(p.ID)
		if p.ID == 0 {
			id = shortSHA(p.SHA)
		}
		paintedMark, status := painted(mark, p.Status)
		items[i] = pickItem{
			Label: esc(on+" ") + paintedMark + esc(fmt.Sprintf("  %-8s  %-*s", id, refW, trim(p.Ref, refW))),
			Sub:   strings.TrimSpace(status + "  " + esc(strings.TrimSpace(p.Source+"  "+age))),
			About: esc(strings.TrimSpace(fmt.Sprintf("%s · %s · %s %s", id, p.Status, shortSHA(p.SHA), p.WebURL))),
			Data:  p,
		}
	}
	at := func(it pickItem) forge.Pipeline { return it.Data.(forge.Pipeline) }
	again := func(it pickItem) func() {
		return func() { a.listPipelines(target, pipes, at(it).ID, back) }
	}
	opts := pickerOptions{start: start, wide: true, explain: true, enterHint: "jobs", back: back,
		enterName: "Show Jobs", enterAbout: "List the pipeline's jobs, earlier attempts with them.",
		keys: []pickKey{
			a.browserKey("w", "browser", "Open Pipeline in Browser", "The pipeline's page on the forge; the list stays open.",
				func(it pickItem) string { return at(it).WebURL }),
		}}
	a.showPickerWith("Pipelines · "+target.label, items, opts, func(it pickItem) {
		a.showPipeline(target.ofPipeline(at(it), again(it)), 0)
	})
}
