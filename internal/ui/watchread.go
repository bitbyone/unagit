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
	if ciStateOf(st.Status) == ciFailed {
		st.Failed = before.Failed
		if before.Pipeline != st.Pipeline || before.Status != st.Status || before.SHA != st.SHA {
			var jobs []forge.Job
			if w.IID > 0 {
				_, jobs, _ = client.PipelineJobs(ctx, mr)
			} else {
				_, jobs, _ = client.BranchPipelineJobs(ctx, pr, w.Branch)
			}
			st.Failed = firstFailed(jobs)
		}
	}
	return watchReading{state: st}
}

// readActivity reads what has happened to a merge request besides its
// pipeline: its head and how many commits it has, how many comments, who
// approved it. What cannot be read keeps what was read before.
func readActivity(ctx context.Context, client forge.Provider, mr forge.MergeRequest, det *forge.MergeRequestDetail, before watch.State, st *watch.State) {
	st.Head, st.Comments, st.Known = det.SHA, det.UserNotesCount, true
	st.Commits, st.Approvers = before.Commits, before.Approvers
	if st.Head != before.Head || !before.Known {
		if _, n, err := client.MergeRequestCommits(ctx, mr, 1); err == nil {
			st.Commits = n
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
func watchChanges(w watch.Watch, before, after watch.State, known bool, ended string) []watch.Event {
	title := cmp.Or(after.Title, before.Title, w.Title)
	ev := func(line string, level watch.Level) watch.Event {
		return watch.Event{Key: w.Key(), What: w.Label(), Line: line, Level: level, Title: title}
	}
	if ended != "" {
		level := watch.Warning
		if ended == "merge request merged" {
			level = watch.Success
		}
		return []watch.Event{ev(ended+" · no longer watched", level)}
	}
	if !known {
		return nil
	}
	var out []watch.Event
	if before.Known && after.Known {
		if after.Head != before.Head && after.Head != "" {
			n := after.Commits - before.Commits
			switch {
			case n > 0:
				out = append(out, ev(fmt.Sprintf("%d new %s · head %s", n, plural(n, "commit", "commits"), shortSHA(after.Head)), watch.Info))
			default:
				out = append(out, ev("head rewritten · "+shortSHA(after.Head), watch.Warning))
			}
		}
		if n := after.Comments - before.Comments; n > 0 {
			out = append(out, ev(fmt.Sprintf("%d new %s", n, plural(n, "comment", "comments")), watch.Info))
		}
		if added := missing(after.Approvers, before.Approvers); len(added) > 0 {
			out = append(out, ev("approved by "+strings.Join(added, ", "), watch.Success))
		}
		if gone := missing(before.Approvers, after.Approvers); len(gone) > 0 {
			out = append(out, ev("approval withdrawn by "+strings.Join(gone, ", "), watch.Warning))
		}
	}
	if before.Base != "" && after.Base == before.Base && after.Behind > before.Behind {
		out = append(out, ev(fmt.Sprintf("%s moved on · %d behind it", after.Base, after.Behind), watch.Info))
	}
	return append(out, pipelineChanges(w, before, after, known, "")...)
}

// pipelineChanges is what changed in a watch's pipeline, as events: one
// for each change, a pipeline that began as much as one that ended.
func pipelineChanges(w watch.Watch, before, after watch.State, known bool, ended string) []watch.Event {
	title := cmp.Or(after.Title, before.Title, w.Title)
	ev := func(line string, level watch.Level) []watch.Event {
		return []watch.Event{{Key: w.Key(), What: w.Label(), Line: line, Level: level, Title: title}}
	}
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
	switch after.Status {
	case "manual":
		return ev("pipeline waits for a manual job", watch.Warning)
	case "canceled", "cancelled":
		return ev("pipeline cancelled", watch.Warning)
	}
	switch ciStateOf(after.Status) {
	case ciPassed:
		return ev("pipeline passed", watch.Success)
	case ciFailed:
		line := "pipeline failed"
		if after.Failed != "" {
			line += " · " + after.Failed
		}
		return ev(line, watch.Danger)
	case ciRunning:
		if fresh || ciStateOf(before.Status) != ciRunning {
			return ev("pipeline started", watch.Info)
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
