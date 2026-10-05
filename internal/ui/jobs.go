package ui

import (
	"fmt"
	"strings"
	"time"
)

// A long job - refreshing a whole list from the servers - runs behind the
// interface rather than in a dialog over it: the lists stay there to move
// in and read, and every list's header says what is under way, with a
// spinner so it is plain that something is.

// bgJob is one job under way: what it is, and how far it has got.
type bgJob struct {
	title    string
	progress string
}

// spinInterval is how often the spinner turns.
const spinInterval = 120 * time.Millisecond

// startJob notes a job as under way and starts the spinner. It runs on the
// event loop; the job's own goroutine reports through jobProgress.
func (a *App) startJob(title string) *bgJob {
	j := &bgJob{title: title}
	a.jobs = append(a.jobs, j)
	if len(a.jobs) == 1 {
		go a.spin()
	}
	a.showJobs()
	return j
}

// jobProgress says how far a job has got. It may be called from any
// goroutine.
func (a *App) jobProgress(j *bgJob, text string) {
	a.tv.QueueUpdateDraw(func() {
		j.progress = text
		a.showJobs()
	})
}

// endJob takes a job off the headers. It runs on the event loop.
func (a *App) endJob(j *bgJob) {
	for i, other := range a.jobs {
		if other == j {
			a.jobs = append(a.jobs[:i], a.jobs[i+1:]...)
			break
		}
	}
	a.showJobs()
}

// spin turns the spinner while any job is under way.
func (a *App) spin() {
	ticker := time.NewTicker(spinInterval)
	defer ticker.Stop()
	for range ticker.C {
		stop := make(chan bool, 1)
		a.tv.QueueUpdateDraw(func() {
			a.spinFrame++
			a.showJobs()
			stop <- len(a.jobs) == 0
		})
		select {
		case done := <-stop:
			if done {
				return
			}
		case <-time.After(5 * time.Second):
			// The event loop is gone: unagit is closing.
			return
		}
	}
}

// jobLine is what the headers say of the jobs under way: the spinner, and
// each job with its progress; "" when there are none.
func (a *App) jobLine() string {
	if len(a.jobs) == 0 {
		return ""
	}
	frames := []rune(theme.Glyphs.Spinner)
	if len(frames) == 0 {
		frames = []rune(glyphDot)
	}
	var parts []string
	for _, j := range a.jobs {
		part := j.title
		if j.progress != "" {
			part += " · " + j.progress
		}
		parts = append(parts, part)
	}
	return tag(colAccent) + string(frames[a.spinFrame%len(frames)]) + " " + esc(strings.Join(parts, "  ")) + tagEnd
}

// showJobs draws the right-hand end of every status line again: the word
// last said and the jobs under way.
func (a *App) showJobs() {
	for _, p := range []*pane{a.projectsPane, a.mrsPane, a.worktreesPane} {
		if p != nil {
			p.updateHeader()
		}
	}
	if a.settingsLine != nil {
		a.settingsLine.setRight(a.rightLine())
	}
}

// rightLine is what the right-hand end of a status line says: the note or
// success last said, then the jobs under way. Both come and go; what stays
// - counts, filters, the order - is the left's.
func (a *App) rightLine() string {
	parts := make([]string, 0, 2)
	if a.transient != "" {
		parts = append(parts, a.transient)
	}
	if jobs := a.jobLine(); jobs != "" {
		parts = append(parts, jobs)
	}
	return strings.Join(parts, "   ")
}

// addFetching counts fetches that start (n > 0) or end (n < 0). While any
// runs they are one job on the status line, with how many there are.
func (a *App) addFetching(n int) {
	a.fetching = max(0, a.fetching+n)
	switch {
	case a.fetching > 0 && a.fetchJob == nil:
		a.fetchJob = a.startJob("fetching")
	case a.fetching == 0 && a.fetchJob != nil:
		a.endJob(a.fetchJob)
		a.fetchJob = nil
	}
	if a.fetchJob != nil {
		a.fetchJob.progress = fmt.Sprintf("%d repositories", a.fetching)
		if a.fetching == 1 {
			a.fetchJob.progress = "1 repository"
		}
		a.showJobs()
	}
}

// runInBackground runs a job behind the interface. busy guards against the
// same job twice; fn reports its progress, and then runs on the event loop
// when it succeeds. A failure is said as an error.
func (a *App) runInBackground(title string, busy *bool, fn func(progress func(string)) error, then func()) {
	if *busy {
		a.note("already " + title)
		return
	}
	*busy = true
	j := a.startJob(title)
	go func() {
		err := fn(func(text string) { a.jobProgress(j, text) })
		a.tv.QueueUpdateDraw(func() {
			*busy = false
			a.endJob(j)
			if err != nil {
				a.errorf("%s: %v", title, err)
				return
			}
			if then != nil {
				then()
			}
		})
	}()
}
