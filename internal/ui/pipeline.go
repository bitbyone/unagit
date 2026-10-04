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

// A merge request's pipeline up close: its jobs - GitHub's check runs - with
// the first that failed under the cursor. Enter reads a job's log, R runs it
// again, w opens it in the browser, so a red CI column can be dealt with
// without leaving the list.

// showPipeline loads the pipeline of a merge request's head and lists its
// jobs, the cursor on focus or else on the first that failed.
func (a *App) showPipeline(mr forge.MergeRequest, focus int64) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	var pipe *forge.Pipeline
	var jobs []forge.Job
	a.runTaskThen(fmt.Sprintf("Reading the pipeline of %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		pipe, jobs, err = client.PipelineJobs(ctx, mr)
		return "", err
	}, func(string) {
		if pipe == nil {
			a.note(fmt.Sprintf("!%d has no pipeline", mr.IID))
			return
		}
		a.listJobs(mr, pipe, jobs, focus)
	})
}

// listJobs shows the jobs of a pipeline.
func (a *App) listJobs(mr forge.MergeRequest, pipe *forge.Pipeline, jobs []forge.Job, focus int64) {
	if len(jobs) == 0 {
		mark, _ := ciMark(pipe.Status)
		a.note(fmt.Sprintf("!%d: %s %s, no jobs to show", mr.IID, mark, pipe.Status))
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
			mark = "○"
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
		return func() { a.listJobs(mr, pipe, jobs, at(it).ID) }
	}
	mark, _ := ciMark(pipe.Status)
	title := fmt.Sprintf("Pipeline · %s !%d · %s %s", a.projectPathOfMR(mr), mr.IID, mark, pipe.Status)
	opts := pickerOptions{start: max(start, 0), wide: true, explain: true, enterHint: "log", keys: []pickKey{
		{keys: "R", hint: "retry", run: func(it pickItem) { a.retryJob(mr, at(it)) }},
		{keys: "w", hint: "browser", run: func(it pickItem) { a.openWeb(at(it).WebURL) }},
		{keys: "W", hint: "pipeline", run: func(it pickItem) { a.openWeb(pipe.WebURL) }},
	}}
	a.showPickerWith(title, items, opts, func(it pickItem) { a.showJobLog(mr, at(it), back(it)) })
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
func (a *App) retryJob(mr forge.MergeRequest, job forge.Job) {
	client := a.client(mr.Instance)
	a.runTaskThen("Retrying "+job.Name, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return "", client.RetryJob(ctx, mr, job)
	}, func(string) {
		a.done(job.Name + " runs again")
		a.showPipeline(mr, job.ID)
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
func (a *App) showJobLog(mr forge.MergeRequest, job forge.Job, back func()) {
	client := a.client(mr.Instance)
	var text string
	a.runTaskThen("Reading the log of "+job.Name, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		raw, err := client.JobLog(ctx, mr, job)
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
