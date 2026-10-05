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
// failed under the cursor. Enter reads a job's log, R runs it again, w opens
// it in the browser, so a red CI column can be dealt with without leaving
// unagit. It is a merge request's pipeline, or a branch's - a clone's, a
// worktree's, one repository's of a group.

// ciTarget is whose pipeline is shown: what it is called, the repository its
// jobs belong to, and how its newest pipeline is read.
type ciTarget struct {
	instance string
	project  forge.Project
	label    string
	load     func(ctx context.Context, client forge.Provider) (*forge.Pipeline, []forge.Job, error)
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
	}, nil
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

// listJobs shows the jobs of a pipeline.
func (a *App) listJobs(target ciTarget, pipe *forge.Pipeline, jobs []forge.Job, focus int64) {
	if len(jobs) == 0 {
		mark, _ := ciMark(pipe.Status)
		a.note(fmt.Sprintf("%s: %s %s, no jobs to show", target.label, mark, pipe.Status))
		return
	}
	nameW, stageW := 0, 0
	for _, j := range jobs {
		nameW = max(nameW, len([]rune(j.Name)))
		stageW = max(stageW, len([]rune(j.Stage)))
	}
	nameW = min(nameW, 48)
	items := make([]pickItem, len(jobs))
	start := -1
	for i, j := range jobs {
		mark, _ := ciMark(j.Status)
		if mark == "" {
			mark = glyphRing
		}
		items[i] = pickItem{
			Label: esc(fmt.Sprintf("%s  %-*s  %-*s", mark, stageW, j.Stage, nameW, trim(j.Name, nameW))),
			Sub:   esc(strings.TrimSpace(j.Status + "  " + duration(j.Duration))),
			About: esc(strings.TrimSpace(fmt.Sprintf("%s · %s · %s %s", j.Name, j.Status, duration(j.Duration), j.WebURL))),
			Data:  i,
		}
		switch {
		case j.ID == focus && focus != 0:
			start = i
		case start < 0 && focus == 0 && j.Status == "failed":
			start = i
		}
	}
	at := func(it pickItem) forge.Job { return jobs[it.Data.(int)] }
	back := func(it pickItem) func() {
		return func() { a.listJobs(target, pipe, jobs, at(it).ID) }
	}
	mark, _ := ciMark(pipe.Status)
	title := fmt.Sprintf("Pipeline · %s · %s %s", target.label, mark, pipe.Status)
	opts := pickerOptions{start: max(start, 0), wide: true, explain: true, enterHint: "log", keys: []pickKey{
		{keys: "R", hint: "retry", run: func(it pickItem) { a.retryJob(target, at(it)) }},
		{keys: "w", hint: "browser", run: func(it pickItem) { a.openWeb(at(it).WebURL) }},
		{keys: "W", hint: "pipeline", run: func(it pickItem) { a.openWeb(pipe.WebURL) }},
	}}
	a.showPickerWith(title, items, opts, func(it pickItem) { a.showJobLog(target, at(it), back(it)) })
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

// retryJob runs a job again and comes back to the pipeline, the cursor on it.
func (a *App) retryJob(target ciTarget, job forge.Job) {
	client := a.client(target.instance)
	a.runTaskThen("Retrying "+job.Name, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return "", client.RetryJob(ctx, target.project, job)
	}, func(string) {
		a.showPipelineThen(target, job.ID, func() { a.done(job.Name + " runs again") })
	})
}

// logTail is as much of a log as is shown: the end, where a job fails.
const logTail = 400

// ansi matches the escape sequences a CI log is coloured with, and GitLab's
// section markers.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|section_(start|end):[0-9]+:[A-Za-z0-9_.-]+(\[[^\]]*\])?`)

// cleanLog makes a CI log plain text: no colours, and of a line rewritten in
// place - a progress bar - only what it ended as.
func cleanLog(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if at := strings.LastIndex(strings.TrimRight(line, "\r"), "\r"); at >= 0 {
			line = line[at+1:]
		}
		lines[i] = ansi.ReplaceAllString(line, "")
	}
	if len(lines) > logTail {
		lines = append([]string{fmt.Sprintf("… %d earlier lines; w opens the whole log", len(lines)-logTail)}, lines[len(lines)-logTail:]...)
	}
	return strings.Join(lines, "\n")
}

// showJobLog reads a job's log and shows its end; Esc goes back.
func (a *App) showJobLog(target ciTarget, job forge.Job, back func()) {
	client := a.client(target.instance)
	var text string
	a.runTaskThen("Reading the log of "+job.Name, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		raw, err := client.JobLog(ctx, target.project, job)
		text = cleanLog(raw)
		return "", err
	}, func(string) {
		view := tview.NewTextView().SetWrap(true).SetScrollable(true).SetTextColor(colText)
		view.SetText(text)
		view.ScrollToEnd()
		mark, _ := ciMark(job.Status)
		box(view.Box, fmt.Sprintf("%s %s · %s", mark, job.Name, job.Status))
		hintPanel(view.Box, func() string { return "j/k scroll · g/G top/end · w browser · Esc back to the jobs" }, 0, 0, 1, 1)
		view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
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
		a.pages.AddPage(pageCommit, modalPct(view, 90, 85), true, true)
		a.tv.SetFocus(view)
	})
}
