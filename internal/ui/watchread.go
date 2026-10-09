package ui

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/watch"
)

// Reading the watches. Each turn the poller asks every server once, in a
// single GraphQL query, for a fingerprint of what each watch due follows
// (forge.Fingerprints); only a watch whose fingerprint moved is read in
// full, over REST, and every one is read in full at least every
// watchFullEvery all the same, in case the fingerprint misses something.
// A server whose GraphQL fails is read in full for a while, as before
// there were fingerprints. A server short of its rate limit is not asked
// at all until it resets. A watched branch's distance behind its base is
// read from the clone on disk, which costs no request.

const (
	// watchFullEvery is the longest a watch goes without a full read.
	watchFullEvery = 5 * time.Minute
	// printsOffFor is how long a server whose fingerprints failed is read
	// in full only.
	printsOffFor = 10 * time.Minute
)

// watchReading is what one watch's read came to.
type watchReading struct {
	state watch.State
	// same is set when the fingerprint had not moved: nothing was read,
	// and the state is what it was.
	same bool
	// paused is when the server may be asked again, for one short of its
	// rate limit; nothing was read.
	paused time.Time
	// ended is why the watch ends: a merge request merged or closed, a
	// branch deleted on origin.
	ended string
	err   error
}

// read asks about the watches, a server at a time: the fingerprints of all
// its watches at once, then the ones that moved in full, several at a time
// as a refresh does. due are the watches asked for by hand, read in full
// whatever their fingerprint.
func (f *watchFollower) read(ws []watch.Watch, due map[string]bool) []watchReading {
	out := make([]watchReading, len(ws))
	if len(ws) == 0 {
		return out
	}
	a := f.app
	ctx, cancel := context.WithTimeout(f.ctx, time.Minute)
	defer cancel()
	byServer := map[string][]int{}
	for i, w := range ws {
		byServer[w.Instance] = append(byServer[w.Instance], i)
	}
	sem := make(chan struct{}, extrasFanOut)
	var wg sync.WaitGroup
	for instance, idx := range byServer {
		client := a.watchClient(ctx, instance)
		if client == nil {
			for _, i := range idx {
				out[i].err = fmt.Errorf("%s has no token - set one in %s", instance, settingsTab)
			}
			continue
		}
		if until := client.PausedUntil(); !until.IsZero() {
			for _, i := range idx {
				out[i].paused = until
			}
			continue
		}
		prints := f.fingerprints(ctx, client, instance, ws, idx)
		for n, i := range idx {
			w := ws[i]
			before := f.snap.States[w.Key()]
			print := ""
			if prints != nil {
				print = prints[n]
			}
			if print != "" && print == before.Print && before.Error == "" && !due[w.Key()] &&
				time.Since(before.Full) < watchFullEvery {
				out[i] = watchReading{state: before, same: true}
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				r := readWatch(ctx, client, w, before)
				r.state.Print, r.state.Full = print, time.Now()
				out[i] = r
			}()
		}
	}
	wg.Wait()
	return out
}

// fingerprints asks a server for the fingerprints of its watches, nil when
// it cannot say: its GraphQL failed, now or a short while ago.
func (f *watchFollower) fingerprints(ctx context.Context, client forge.Provider, instance string, ws []watch.Watch, idx []int) []string {
	if time.Now().Before(f.printsOff[instance]) {
		return nil
	}
	refs := make([]forge.WatchRef, len(idx))
	for n, i := range idx {
		w := ws[i]
		refs[n] = forge.WatchRef{Project: forge.Project{ID: w.ProjectID, PathWithNamespace: w.Project, Instance: w.Instance},
			IID: w.IID, Branch: w.Branch}
	}
	prints, err := client.Fingerprints(ctx, refs)
	if err != nil || len(prints) != len(refs) {
		f.printsOff[instance] = time.Now().Add(printsOffFor)
		return nil
	}
	return prints
}

// watchClient is the server's client, read on the loop: the clients change
// when Settings does.
func (a *App) watchClient(ctx context.Context, instance string) forge.Provider {
	ch := make(chan forge.Provider, 1)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !a.onLoopWait(ctx, func() { ch <- a.client(instance) }) {
		return nil
	}
	return <-ch
}

// readWatch reads a watch in full: a merge request's state, activity and
// newest pipeline - whatever its head ran, so a push moves the watch on -
// or a branch's newest pipeline and whether it is still there. The jobs are
// read only when a pipeline has newly failed, to name the job; the commits
// counted only when the head moved.
func readWatch(ctx context.Context, client forge.Provider, w watch.Watch, before watch.State) watchReading {
	st := watch.State{Read: time.Now(), Base: before.Base, Behind: before.Behind}
	var pipe *forge.Pipeline
	var err error
	var mr forge.MergeRequest
	pr := forge.Project{ID: w.ProjectID, PathWithNamespace: w.Project, Instance: w.Instance}
	if w.IID > 0 {
		mr = forge.MergeRequest{IID: w.IID, ProjectID: w.ProjectID, TargetProjectID: w.ProjectID, ProjectPath: w.Project, Instance: w.Instance}
		det, err := client.MergeRequestDetail(ctx, mr)
		if err != nil {
			return watchReading{err: err}
		}
		mr = withDetail(mr, det)
		st.Title, st.URL = det.Title, det.WebURL
		switch state := strings.ToLower(det.State); state {
		case "merged", "closed":
			st.Status, st.Pipeline, st.SHA = before.Status, before.Pipeline, before.SHA
			return watchReading{state: st, ended: "merge request " + state}
		}
		readActivity(ctx, client, mr, det, before, &st)
		pipe, err = client.MergeRequestPipeline(ctx, mr)
	} else {
		if exists, err := client.BranchExists(ctx, pr, w.Branch); err == nil && !exists {
			st.Status, st.Pipeline, st.SHA = before.Status, before.Pipeline, before.SHA
			return watchReading{state: st, ended: "branch deleted on origin"}
		}
		pipe, err = client.LatestPipeline(ctx, pr, w.Branch)
	}
	if err != nil {
		return watchReading{err: err}
	}
	if pipe == nil {
		return watchReading{state: st}
	}
	st.Pipeline, st.Status, st.SHA, st.WebURL = pipe.ID, pipe.Status, pipe.SHA, pipe.WebURL
	st.Started = pipe.StartedAt
	if st.Started.IsZero() {
		st.Started = pipe.CreatedAt
	}
	if pipe.User != nil {
		st.User = pipe.User.Username
	}
	// The jobs are read when the pipeline moved, so its news can say which
	// job it was - one run by hand in a pipeline that had passed, say.
	st.Failed, st.Jobs = before.Failed, before.Jobs
	if before.Pipeline != st.Pipeline || before.Status != st.Status || before.SHA != st.SHA || before.Jobs == nil {
		var jobs []forge.Job
		var err error
		if w.IID > 0 {
			_, jobs, err = client.PipelineJobs(ctx, mr)
		} else {
			_, jobs, err = client.BranchPipelineJobs(ctx, pr, w.Branch)
		}
		if err == nil {
			st.Jobs = jobStates(jobs)
			st.Failed = firstFailed(jobs)
		}
	}
	if ciStateOf(st.Status) != ciFailed {
		st.Failed = ""
	}
	if before.Pipeline == st.Pipeline && before.SHA == st.SHA {
		finished := ciStateOf(before.Status) != ciRunning && before.Status != ""
		st.Again = before.Again || finished && ciStateOf(st.Status) == ciRunning
	}
	return watchReading{state: st}
}

// jobStates are the jobs as a watch keeps them: the latest attempt of
// each, never nil once read.
func jobStates(jobs []forge.Job) []watch.JobState {
	out := []watch.JobState{}
	for _, j := range jobs {
		if j.Retried {
			continue
		}
		js := watch.JobState{Name: j.Name, Status: j.Status}
		if j.User != nil {
			js.User = cmp.Or(j.User.Name, j.User.Username)
		}
		out = append(out, js)
	}
	return out
}

// jobMoves are the jobs whose status moved between two readings of one
// pipeline, as they are now, each with what it was.
func jobMoves(before, after []watch.JobState) (moved []watch.JobState, was []string) {
	old := map[string]string{}
	for _, j := range before {
		old[j.Name] = j.Status
	}
	for _, j := range after {
		if o, ok := old[j.Name]; ok && o != j.Status {
			moved = append(moved, j)
			was = append(was, o)
		}
	}
	return moved, was
}

// jobNames lists jobs by name for a sentence: two in full, then how many
// more.
func jobNames(jobs []watch.JobState) string {
	var names []string
	for _, j := range jobs {
		names = append(names, j.Name)
	}
	if len(names) > 2 {
		return fmt.Sprintf("%s, %s and %d more", names[0], names[1], len(names)-2)
	}
	return strings.Join(names, " and ")
}

// jobChanges is the news of jobs moving in a pipeline that had finished
// and runs again: a job started by hand, run again, passed or failed.
// Nothing for a pipeline on its first run, when the jobs were not read or
// none moved: the pipeline's own words stand.
func jobChanges(w watch.Watch, before, after watch.State, news func(watch.Level, string, string) watch.Event) []watch.Event {
	if !after.Again || before.Jobs == nil || after.Jobs == nil {
		return nil
	}
	moved, was := jobMoves(before.Jobs, after.Jobs)
	if len(moved) == 0 {
		return nil
	}
	where := fmt.Sprintf("in pipeline #%d of %s", after.Pipeline, w.Subject())
	var started, startedByHand, passed, failed []watch.JobState
	by := ""
	for i, j := range moved {
		switch ciStateOf(j.Status) {
		case ciRunning:
			if j.Status == "manual" {
				continue
			}
			if was[i] == "manual" {
				startedByHand = append(startedByHand, j)
			} else {
				started = append(started, j)
			}
			by = cmp.Or(by, j.User)
		case ciPassed:
			passed = append(passed, j)
		case ciFailed:
			failed = append(failed, j)
		}
	}
	byWhom := ""
	if by != "" {
		byWhom = " by " + by
	}
	var out []watch.Event
	switch {
	case len(startedByHand) > 0:
		out = append(out, news(watch.Info, "Manual job started", fmt.Sprintf("%s %s was started%s, by hand", jobNames(startedByHand), where, byWhom)))
	case len(started) > 0:
		out = append(out, news(watch.Info, "Job started", fmt.Sprintf("%s %s was started%s", jobNames(started), where, byWhom)))
	}
	if len(failed) > 0 {
		out = append(out, news(watch.Danger, "Job failed", fmt.Sprintf("%s %s failed", jobNames(failed), where)))
	}
	if len(passed) > 0 && len(failed) == 0 && ciStateOf(after.Status) != ciRunning {
		line := fmt.Sprintf("%s %s passed", jobNames(passed), where)
		if ciStateOf(after.Status) == ciPassed {
			line += " · the pipeline has passed"
		}
		out = append(out, news(watch.Success, "Job passed", line))
	}
	return out
}

// readActivity reads what has happened to a merge request besides its
// pipeline: its head and how many commits it has, how many comments, who
// approved it. What cannot be read keeps what was read before.
func readActivity(ctx context.Context, client forge.Provider, mr forge.MergeRequest, det *forge.MergeRequestDetail, before watch.State, st *watch.State) {
	st.Head, st.Comments, st.Known = det.SHA, det.UserNotesCount, true
	people := func(us []forge.User) []string {
		out := []string{}
		for _, u := range us {
			out = append(out, cmp.Or(u.Name, u.Username))
		}
		return out
	}
	st.Assignees, st.Reviewers, st.Draft, st.Meta = people(det.Assignees), people(det.Reviewers), det.Draft, true
	st.Labels = []string{}
	for _, l := range det.Labels {
		st.Labels = append(st.Labels, l.Name)
	}
	st.Commits, st.Approvers = before.Commits, before.Approvers
	st.HeadBy, st.HeadTitle = before.HeadBy, before.HeadTitle
	if st.Head != before.Head || !before.Known {
		if newest, n, err := client.MergeRequestCommits(ctx, mr, 1); err == nil {
			st.Commits = n
			if len(newest) > 0 {
				st.HeadBy, st.HeadTitle = newest[0].AuthorName, newest[0].Title
			}
		}
	}
	if ap, err := client.MergeRequestApprovals(ctx, mr); err == nil && ap != nil {
		st.Approvers = slices.Sorted(slices.Values(ap.ApprovedBy))
	}
}

// firstFailed is the first job that failed and was not run again.
func firstFailed(jobs []forge.Job) string {
	for _, j := range jobs {
		if !j.Retried && ciStateOf(j.Status) == ciFailed {
			return j.Name
		}
	}
	return ""
}

// watchChanges is what changed between two readings, as events: an end, a
// merge request's activity, a branch falling behind its base, the
// pipeline. The first reading is never news - only a change from what was
// last held.
//
// Each says what happened in a heading of a few words and a sentence of
// where and how, which names the watch by its number or branch alone: the
// repository and the merge request's title go beside it wherever it is
// shown.
func watchChanges(w watch.Watch, before, after watch.State, known bool, ended string) []watch.Event {
	ev := newsOf(w, before, after)
	who := w.Subject()
	switch ended {
	case "":
	case "merge request merged":
		return []watch.Event{ev(watch.Success, "MR merged", who+" was merged · no longer watched")}
	case "merge request closed":
		return []watch.Event{ev(watch.Warning, "MR closed", who+" was closed without merging · no longer watched")}
	case "branch deleted on origin":
		return []watch.Event{ev(watch.Warning, "Branch deleted", who+" was deleted on origin · no longer watched")}
	default:
		return []watch.Event{ev(watch.Warning, "No longer watched", who+": "+ended)}
	}
	if !known {
		return nil
	}
	var out []watch.Event
	if before.Known && after.Known {
		if after.Head != before.Head && after.Head != "" {
			commit := headCommit(after)
			switch n := after.Commits - before.Commits; {
			case n == 1:
				out = append(out, ev(watch.Info, "New commit in MR", who+" has a new commit"+commit))
			case n > 1:
				line := fmt.Sprintf("%s has %d new commits", who, n)
				if commit != "" {
					line += ", the last" + commit
				}
				out = append(out, ev(watch.Info, "New commits in MR", line))
			default:
				out = append(out, ev(watch.Warning, "MR force-pushed", fmt.Sprintf("%s was rewritten, its head now %s%s", who, shortSHA(after.Head), commit)))
			}
		}
		switch n := after.Comments - before.Comments; {
		case n == 1:
			out = append(out, ev(watch.Info, "New comment in MR", who+" has a new comment"))
		case n > 1:
			out = append(out, ev(watch.Info, "New comments in MR", fmt.Sprintf("%s has %d new comments", who, n)))
		}
		if before.Meta && after.Meta {
			metaChanges(who, before, after, func(level watch.Level, heading, line string) {
				out = append(out, ev(level, heading, line))
			})
		}
		if added := missing(after.Approvers, before.Approvers); len(added) > 0 {
			out = append(out, ev(watch.Success, "MR approved", who+" was approved by "+strings.Join(added, ", ")))
		}
		if gone := missing(before.Approvers, after.Approvers); len(gone) > 0 {
			out = append(out, ev(watch.Warning, "Approval withdrawn", strings.Join(gone, ", ")+" withdrew the approval of "+who))
		}
	}
	if before.Base != "" && after.Base == before.Base && after.Behind > before.Behind {
		out = append(out, ev(watch.Info, "Base moved on", fmt.Sprintf("%s is %d %s behind %s",
			who, after.Behind, plural(after.Behind, "commit", "commits"), after.Base)))
	}
	return append(out, pipelineChanges(w, before, after, known, "")...)
}

// metaChanges says what changed of who a merge request is for and what it
// is called: its assignees and reviewers, its labels, its title, whether
// it is a draft, handing each to say.
func metaChanges(who string, before, after watch.State, say func(watch.Level, string, string)) {
	names := func(xs []string) string {
		if len(xs) > 2 {
			return fmt.Sprintf("%s, %s and %d more", xs[0], xs[1], len(xs)-2)
		}
		return strings.Join(xs, " and ")
	}
	if added, gone := missing(after.Assignees, before.Assignees), missing(before.Assignees, after.Assignees); len(added)+len(gone) > 0 {
		switch {
		case len(added) > 0 && len(gone) > 0:
			say(watch.Info, "Assignee changed", fmt.Sprintf("%s was reassigned from %s to %s", who, names(gone), names(added)))
		case len(added) > 0:
			say(watch.Info, "Assignee changed", fmt.Sprintf("%s is now assigned to %s", who, names(added)))
		default:
			say(watch.Info, "Assignee changed", fmt.Sprintf("%s is no longer assigned to %s", who, names(gone)))
		}
	}
	if added := missing(after.Reviewers, before.Reviewers); len(added) > 0 {
		say(watch.Info, "Reviewer added", fmt.Sprintf("%s asks %s for a review", who, names(added)))
	}
	if gone := missing(before.Reviewers, after.Reviewers); len(gone) > 0 {
		say(watch.Info, "Reviewer removed", fmt.Sprintf("%s no longer reviews %s", names(gone), who))
	}
	if added, gone := missing(after.Labels, before.Labels), missing(before.Labels, after.Labels); len(added)+len(gone) > 0 {
		var parts []string
		if len(added) > 0 {
			parts = append(parts, "labelled "+names(added))
		}
		if len(gone) > 0 {
			parts = append(parts, "no longer "+names(gone))
		}
		say(watch.Info, "Labels changed", who+" is "+strings.Join(parts, ", "))
	}
	if after.Title != before.Title && before.Title != "" && after.Title != "" {
		say(watch.Info, "MR retitled", fmt.Sprintf("%s is now %q", who, trunc(after.Title, headCommitMax)))
	}
	if after.Draft != before.Draft {
		if after.Draft {
			say(watch.Warning, "MR marked draft", who+" was marked a draft")
		} else {
			say(watch.Success, "MR marked ready", who+" was marked ready")
		}
	}
}

// newsOf makes a watch's events.
func newsOf(w watch.Watch, before, after watch.State) func(watch.Level, string, string) watch.Event {
	title := cmp.Or(after.Title, before.Title, w.Title)
	return func(level watch.Level, heading, line string) watch.Event {
		return watch.Event{Key: w.Key(), What: w.Label(), Heading: heading, Line: line, Project: w.Project, Level: level, Title: title}
	}
}

// headCommitMax is how much of a commit's subject a sentence quotes.
const headCommitMax = 40

// headCommit is the head commit's author and subject, as the end of a
// sentence about it: ` by Jane Doe: "Fix the…"`.
func headCommit(st watch.State) string {
	out := ""
	if st.HeadBy != "" {
		out = " by " + st.HeadBy
	}
	if st.HeadTitle != "" {
		out += `: "` + trunc(st.HeadTitle, headCommitMax) + `"`
	}
	return out
}

// pipelineChanges is what changed in a watch's pipeline, as events: one
// for each change, a pipeline that began as much as one that ended.
func pipelineChanges(w watch.Watch, before, after watch.State, known bool, ended string) []watch.Event {
	if ended != "" {
		return watchChanges(w, before, after, known, ended)
	}
	if !known || after.Status == "" {
		return nil
	}
	fresh := after.Pipeline != before.Pipeline || after.SHA != before.SHA
	if !fresh && after.Status == before.Status {
		return nil
	}
	news := newsOf(w, before, after)
	// The same pipeline moving again is a job in it: name the job.
	if !fresh {
		if evs := jobChanges(w, before, after, news); len(evs) > 0 {
			return evs
		}
	}
	ev := func(level watch.Level, heading, how string) []watch.Event {
		pipeline := "Pipeline"
		if after.Pipeline > 0 {
			pipeline += fmt.Sprintf(" #%d", after.Pipeline)
		}
		return []watch.Event{news(level, heading, pipeline+" of "+w.Subject()+" "+how)}
	}
	switch after.Status {
	case "manual":
		return ev(watch.Warning, "Manual job waiting", "waits for a manual job to be started")
	case "canceled", "cancelled":
		return ev(watch.Warning, "Pipeline cancelled", "was cancelled")
	}
	switch ciStateOf(after.Status) {
	case ciPassed:
		return ev(watch.Success, "Pipeline passed", "passed")
	case ciFailed:
		how := "failed"
		if after.Failed != "" {
			how += " in " + after.Failed
		}
		return ev(watch.Danger, "Pipeline failed", how)
	case ciRunning:
		if fresh || ciStateOf(before.Status) != ciRunning {
			how := "started"
			if after.User != "" {
				how += " by " + after.User
			}
			if after.SHA != "" {
				how += " for " + shortSHA(after.SHA)
			}
			return ev(watch.Info, "Pipeline running", how)
		}
	}
	return nil
}

// missing is what of xs ys does not have.
func missing(xs, ys []string) []string {
	var out []string
	for _, x := range xs {
		if !slices.Contains(ys, x) {
			out = append(out, x)
		}
	}
	return out
}

// watchDisk is where a watched branch is on disk and what it is measured
// against: the branch unagit made it from, or the repository's default
// branch. Nothing for a merge request, for a branch not on disk, or for the
// default branch itself. It runs on the loop.
type watchDisk struct{ dir, base string }

func (a *App) watchDiskOf(w watch.Watch) (watchDisk, bool) {
	if w.IID > 0 || w.Branch == "" {
		return watchDisk{}, false
	}
	pr := a.worktreeProject(worktreeRow{Instance: w.Instance, Path: w.Project})
	pick := func(dir, base string) (watchDisk, bool) {
		base = cmp.Or(base, pr.DefaultBranch)
		if dir == "" || base == "" || base == w.Branch {
			return watchDisk{}, false
		}
		return watchDisk{dir, base}, true
	}
	var rows []worktreeRow
	for _, r := range a.worktrees {
		rows = append(rows, r)
		rows = append(rows, r.Members...)
	}
	for _, r := range rows {
		if !r.grouped() && r.Instance == w.Instance && r.Path == w.Project && r.Branch == w.Branch {
			return pick(r.Dir, cmp.Or(a.wtRemote[r.Dir].Base, r.Base))
		}
	}
	if info := a.diskOf(w.Instance, w.Project); info.Cloned && info.Branch == w.Branch {
		return pick(a.projectDir(w.Instance, w.Project), "")
	}
	return watchDisk{}, false
}

// readBehind measures how far each watched branch on disk is behind its
// base, by the refs the clone has - no request. It runs off the loop.
func (f *watchFollower) readBehind(ws []watch.Watch) map[string]watch.State {
	a := f.app
	disks := map[string]watchDisk{}
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	if !a.onLoopWait(ctx, func() {
		for _, w := range ws {
			if d, ok := a.watchDiskOf(w); ok {
				disks[w.Key()] = d
			}
		}
	}) {
		return nil
	}
	git := gitx.New("", nil)
	out := map[string]watch.State{}
	for key, d := range disks {
		onto := git.BaseRef(d.dir, d.base)
		if onto == "" {
			continue
		}
		out[key] = watch.State{Base: d.base, Behind: git.Count(d.dir, "HEAD.."+onto)}
	}
	return out
}
