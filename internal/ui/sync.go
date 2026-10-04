package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/incomm"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/workspace"
)

// syncFanOut is how many clones are fetched or updated at once: enough to
// hide the round trips, few enough not to look like an attack on the server.
const syncFanOut = 6

// loadRepoSync finds out where the branch of every main clone stands against
// origin, in the background. Without fetch it reads the refs already on disk,
// which costs a for-each-ref per clone; with fetch, as after r, each clone
// asks origin first. Rows fill in as their answers arrive.
func (a *App) loadRepoSync(fetch bool) {
	type job struct {
		key    projectKey
		dir    string
		branch string
		git    *gitx.Git
	}
	var jobs []job
	for key, info := range a.disk {
		if !info.Cloned {
			continue
		}
		git := a.pathManager(key.Instance, key.Path).Git()
		if fetch {
			git = a.newManager(key.Instance, key.Path, nil).Git()
		}
		jobs = append(jobs, job{key, a.projectDir(key.Instance, key.Path), info.Branch, git})
	}
	if len(jobs) == 0 {
		return
	}
	if fetch {
		a.fetching += len(jobs)
		a.reloadProjectsHeader()
	}
	go func() {
		sem := make(chan struct{}, syncFanOut)
		var wg sync.WaitGroup
		for _, j := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				failed := ""
				if fetch {
					if err := j.git.Fetch(j.dir); err != nil {
						failed = firstLine(err.Error())
					}
				}
				st := remoteState{Edits: max(j.git.Edits(j.dir), 0), Busy: j.git.OperationInProgress(j.dir)}
				upstreams := j.git.BranchUpstreams(j.dir)
				if u, ok := upstreams[j.branch]; ok {
					st.Upstream = u
				} else if upstreams == nil {
					st.Unreadable = true
				} else {
					st = detachedState(j.git, j.dir)
					st.Edits, st.Busy = max(j.git.Edits(j.dir), 0), j.git.OperationInProgress(j.dir)
				}
				a.tv.QueueUpdateDraw(func() {
					if a.repoSync == nil {
						a.repoSync = map[projectKey]remoteState{}
					}
					a.repoSync[j.key] = st
					if fetch {
						a.fetching--
						if a.fetchFailed == nil {
							a.fetchFailed = map[projectKey]string{}
						}
						if failed != "" {
							a.fetchFailed[j.key] = failed
						} else {
							delete(a.fetchFailed, j.key)
						}
					}
					if a.projectsPane != nil && a.projectsPane.reload != nil {
						a.projectsPane.reload()
					}
				})
			}()
		}
		wg.Wait()
	}()
}

// reloadProjectsHeader redraws the line under Repositories, which counts the
// fetches still running.
func (a *App) reloadProjectsHeader() {
	if a.projectsPane != nil && a.projectsPane.headline != nil {
		a.projectsPane.updateHeader()
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// syncWords is the REMOTE column of a repository: short, since it sits in a
// row with much else. A repository not cloned has nothing to say.
func (a *App) syncWords(key projectKey) (string, tcell.Color) {
	if !a.disk[key].Cloned {
		return "", colDim
	}
	if _, failed := a.fetchFailed[key]; failed {
		return "✗ fetch", colBad
	}
	st, known := a.repoSync[key]
	u := st.Upstream
	switch {
	case !known:
		return "…", colDim
	case st.Unreadable:
		return "?", colDim
	case st.Busy != "":
		return busyWords(st.Busy), colBad
	case st.Detached:
		return detachedWords(st)
	case u.Gone:
		return "gone", colBad
	case u.Name == "":
		return "local", colDim
	case u.Ahead > 0 && u.Behind > 0:
		return fmt.Sprintf("↑%d↓%d", u.Ahead, u.Behind), colWarn
	case u.Behind > 0:
		return fmt.Sprintf("↓%d", u.Behind), colWarn
	case u.Ahead > 0:
		return fmt.Sprintf("↑%d", u.Ahead), colMuted
	}
	return "✓", colOn
}

// projectEdits is the EDITS column of a repository: its clone's files with
// uncommitted changes, nothing when there are none or it is not cloned.
func (a *App) projectEdits(key projectKey) string {
	if n := a.repoSync[key].Edits; n > 0 && a.disk[key].Cloned {
		return fmt.Sprintf("%d", n)
	}
	return ""
}

// syncSentence says the same in full, for the detail column.
func (a *App) syncSentence(key projectKey) string {
	if why, failed := a.fetchFailed[key]; failed {
		return tag(colBad) + "the last fetch failed: " + esc(why) + tagEnd
	}
	st, known := a.repoSync[key]
	u := st.Upstream
	text, colour := "", colOn
	switch {
	case !known:
		text, colour = "still looking", colDim
	case st.Unreadable:
		text, colour = "cannot be read: the clone is broken", colDim
	case st.Busy != "":
		text, colour = "a "+st.Busy+" is in progress: finish it, or abort it, in the clone", colBad
	case st.Detached:
		text, colour = detachedSentence(st), colDim
	case u.Gone:
		text, colour = "upstream gone: the branch was deleted on origin", colBad
	case u.Name == "":
		text, colour = "no upstream: the branch is not on origin", colDim
	case u.Ahead > 0 && u.Behind > 0:
		text, colour = fmt.Sprintf("%d behind %s, %d of your own · p rebases them", u.Behind, u.Name, u.Ahead), colWarn
	case u.Behind > 0:
		text, colour = fmt.Sprintf("%d behind %s · p pulls", u.Behind, u.Name), colWarn
	case u.Ahead > 0:
		text, colour = fmt.Sprintf("%d unpushed commit(s) on %s", u.Ahead, u.Name), colMuted
	default:
		text = "up to date with " + u.Name
	}
	return tag(colour) + esc(text) + tagEnd
}

// updateItem is one working tree to bring up to origin: a main clone, a
// worktree, or a member of a grouped one. base is the branch a branch not yet
// pushed is rebased onto; empty for a clone.
type updateItem struct {
	label    string
	instance string
	path     string
	dir      string
	base     string
	// notes is where its Incomm comments live, re-anchored once it moved: the
	// worktree itself, or the folder of the group it belongs to.
	notes string
}

// updateProject brings the main clone's branch up to origin: a fast-forward
// when there is nothing of yours in the way, a rebase of your commits and
// edits when there is, and nothing at all when that would conflict.
func (a *App) updateProject(pr forge.Project) {
	key := projectKey{pr.Instance, pr.PathWithNamespace}
	if !a.disk[key].Cloned {
		a.flash(pr.PathWithNamespace + " is not cloned - C clones it")
		return
	}
	a.updateMany("Updating "+pr.PathWithNamespace, []updateItem{{label: pr.PathWithNamespace,
		instance: pr.Instance, path: pr.PathWithNamespace, dir: a.projectDir(pr.Instance, pr.PathWithNamespace)}})
}

// updateAllClones updates every cloned repository whose branch origin has
// moved past. Each one asks origin first, so what counts as behind is what
// origin says now, not what the column said.
func (a *App) updateAllClones() {
	var items []updateItem
	for _, pr := range a.projects {
		if a.disk[projectKey{pr.Instance, pr.PathWithNamespace}].Cloned {
			items = append(items, updateItem{label: pr.PathWithNamespace, instance: pr.Instance,
				path: pr.PathWithNamespace, dir: a.projectDir(pr.Instance, pr.PathWithNamespace)})
		}
	}
	if len(items) == 0 {
		a.flash("no repository is cloned yet")
		return
	}
	a.updateMany(fmt.Sprintf("Updating %d cloned repositories", len(items)), items)
}

// worktreeItems turns rows of the Worktrees list into what updateMany takes: a
// grouped worktree is all of its members.
func (a *App) worktreeItems(rows []worktreeRow) []updateItem {
	var items []updateItem
	for _, r := range rows {
		members := []worktreeRow{r}
		if r.grouped() {
			members = r.Members
		}
		for _, m := range members {
			base := a.wtRemote[m.Dir].Base
			if base == "" {
				base = m.Base
			}
			label := m.Path + " (" + m.Branch + ")"
			items = append(items, updateItem{label: label, instance: m.Instance, path: m.Path, dir: m.Dir, base: base, notes: r.Dir})
		}
	}
	return items
}

// updateWorktree updates one row of Worktrees: a worktree, or every member of
// a grouped one.
func (a *App) updateWorktree(r worktreeRow) {
	items := a.worktreeItems([]worktreeRow{r})
	if len(items) == 0 {
		a.flash(r.Path + " holds no repository")
		return
	}
	a.updateMany("Updating "+r.Path, items)
}

// updateAllWorktrees updates every worktree on disk, grouped ones included.
func (a *App) updateAllWorktrees() {
	items := a.worktreeItems(a.worktrees)
	if len(items) == 0 {
		a.flash("there is no worktree yet")
		return
	}
	a.updateMany(fmt.Sprintf("Updating %d worktrees", len(items)), items)
}

// updateMany updates working trees, several repositories at once but the
// worktrees of one repository one after another, since they share its refs.
// One alone gets the whole git log; several only say how each ended, as their
// lines would interleave into nonsense. One that cannot be updated without a
// conflict is left as it was and named at the end.
func (a *App) updateMany(title string, items []updateItem) {
	a.moveMany(title, items, (*workspace.Manager).UpdateBranch)
}

// rebaseWorktree puts a worktree's branch, or every member's of a grouped one,
// on top of what its base is now, pushed or not: Ctrl-R.
func (a *App) rebaseWorktree(r worktreeRow) {
	items := a.worktreeItems([]worktreeRow{r})
	if len(items) == 0 {
		a.flash(r.Path + " holds no repository")
		return
	}
	a.moveMany("Rebasing "+r.Path+" onto its base", items, (*workspace.Manager).RebaseOntoBase)
}

// moveMany runs move - an update or a rebase - over working trees.
func (a *App) moveMany(title string, items []updateItem, move func(*workspace.Manager, string, string) (string, error)) {
	integrate := a.cfg.Integrations.Incomm
	a.runTaskNoting(title, func(log func(string)) (string, error) {
		// The code moved under the comments: put them back on it, whatever
		// came of the moves.
		if integrate {
			defer func() {
				var dirs []string
				for _, it := range items {
					dirs = append(dirs, it.notes)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if err := incomm.Reanchor(ctx, dirs...); err != nil {
					log("! Incomm could not re-anchor the comments: " + err.Error())
				}
			}()
		}
		if len(items) == 1 {
			it := items[0]
			outcome, err := move(a.newManager(it.instance, it.path, log), it.dir, it.base)
			if err != nil {
				return "", err
			}
			return it.label + ": " + outcome, nil
		}
		byRepo := map[projectKey][]updateItem{}
		var order []projectKey
		for _, it := range items {
			k := projectKey{it.instance, it.path}
			if byRepo[k] == nil {
				order = append(order, k)
			}
			byRepo[k] = append(byRepo[k], it)
		}
		var (
			mu                        sync.Mutex
			updated, current, skipped int
			refused                   []string
		)
		sem := make(chan struct{}, syncFanOut)
		var wg sync.WaitGroup
		for _, k := range order {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				mgr := a.newManager(k.Instance, k.Path, nil)
				for _, it := range byRepo[k] {
					outcome, err := move(mgr, it.dir, it.base)
					mu.Lock()
					switch {
					case errors.Is(err, workspace.ErrNotTracking):
						skipped++
					case err != nil:
						refused = append(refused, it.label+": "+firstLine(err.Error()))
						log("! " + it.label + ": left as it was")
					case outcome == workspace.UpdateCurrent:
						current++
					default:
						updated++
						log(it.label + ": " + outcome)
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		summary := fmt.Sprintf("%d updated · %d up to date", updated, current)
		if skipped > 0 {
			summary += fmt.Sprintf(" · %d with nothing to update from", skipped)
		}
		log(summary)
		if len(refused) > 0 {
			return "", fmt.Errorf("%d %s left as %s - update by hand:\n%s",
				len(refused), plural(len(refused), "was", "were"), plural(len(refused), "it was", "they were"),
				strings.Join(refused, "\n"))
		}
		return summary, nil
	})
}

// fetchWorktrees asks origin about every repository a worktree hangs off,
// once each and in the background, then reads where the branches stand.
func (a *App) fetchWorktrees() {
	dirs := map[projectKey]string{}
	for _, it := range a.worktreeItems(a.worktrees) {
		k := projectKey{it.instance, it.path}
		if _, ok := dirs[k]; !ok {
			dirs[k] = it.dir
		}
	}
	if len(dirs) == 0 {
		return
	}
	a.fetching += len(dirs)
	a.reloadWorktreesHeader()
	go func() {
		sem := make(chan struct{}, syncFanOut)
		var wg sync.WaitGroup
		for k, dir := range dirs {
			wg.Add(1)
			sem <- struct{}{}
			git := a.newManager(k.Instance, k.Path, nil).Git()
			go func() {
				defer func() { <-sem; wg.Done() }()
				_ = git.Fetch(dir)
				a.tv.QueueUpdateDraw(func() {
					a.fetching--
					a.reloadWorktreesHeader()
				})
			}()
		}
		wg.Wait()
		a.tv.QueueUpdateDraw(func() {
			// Merge requests closed since are let go first, so that neither
			// their comments are synced nor a new one is kept from opening.
			a.forgetClosedWorktreeMRs(func() {
				if !a.cfg.Integrations.Incomm {
					a.loadWorktreeRemotes()
					return
				}
				// r is also when the merge requests' comments come in: by
				// hand, never on opening.
				a.syncingComments = true
				a.reloadWorktreesHeader()
				a.syncWorktreeComments(a.worktrees, func() {
					a.syncingComments = false
					a.loadWorktreeRemotes()
					a.reloadWorktreesHeader()
				})
			})
		})
	}()
}

// forgetClosedWorktreeMRs asks the forge about every merge request open, as far
// as the index knows, from a worktree, and lets go of the ones closed or merged
// since: the worktree stays as it is, it just has no merge request any more,
// and n can open a new one. The rest of the list waits for r in Merge
// requests. then runs on the event loop once the answers are in.
func (a *App) forgetClosedWorktreeMRs(then func()) {
	type ask struct {
		mr     forge.MergeRequest
		client forge.Provider
	}
	var asks []ask
	seen := map[mrKey]bool{}
	for _, r := range a.worktrees {
		members := []worktreeRow{r}
		if r.grouped() {
			members = r.Members
		}
		for _, m := range members {
			mr, ok := a.openMRFor(m)
			client := a.client(m.Instance)
			if !ok || client == nil || seen[keyOfMR(mr)] {
				continue
			}
			seen[keyOfMR(mr)] = true
			asks = append(asks, ask{mr, client})
		}
	}
	if len(asks) == 0 {
		then()
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		closed := map[mrKey]string{}
		for _, q := range asks {
			// An answer that does not come leaves the merge request as it was.
			if det, err := q.client.MergeRequestDetail(ctx, q.mr); err == nil {
				if det.State == "closed" || det.State == "merged" {
					closed[keyOfMR(q.mr)] = det.State
				}
			}
		}
		a.tv.QueueUpdateDraw(func() {
			if len(closed) > 0 {
				kept := a.mrs[:0]
				var gone []string
				for _, mr := range a.mrs {
					if state, ok := closed[keyOfMR(mr)]; ok {
						gone = append(gone, fmt.Sprintf("!%d %s", mr.IID, state))
						continue
					}
					kept = append(kept, mr)
				}
				a.mrs = kept
				_ = index.Save(config.IndexPath("mrs"), index.MergeRequests{
					Version: index.Version, UpdatedAt: a.mrsUpdated, Items: a.mrs})
				a.mrsPane.reload()
				a.worktreesPane.reload()
				a.note("no longer open: " + strings.Join(gone, ", "))
			}
			then()
		})
	}()
}

func (a *App) reloadWorktreesHeader() {
	if a.worktreesPane != nil && a.worktreesPane.headline != nil {
		a.worktreesPane.updateHeader()
	}
}

// updateMR brings the merge request's branch worktree up to its head on the
// forge, by the same rules as every other update. Ctrl-O no longer does it.
func (a *App) updateMR(mr forge.MergeRequest) {
	project := a.mrProject(mr)
	if !workspace.Exists(a.mrDir(mr.Instance, project.PathWithNamespace, mr.IID, mr.SourceBranch)) {
		a.flash(fmt.Sprintf("!%d has no branch worktree yet - Ctrl-O makes it", mr.IID))
		return
	}
	integrate := a.cfg.Integrations.Incomm
	a.runTaskNoting(fmt.Sprintf("Updating !%d", mr.IID), func(log func(string)) (string, error) {
		mgr := a.newManager(mr.Instance, project.PathWithNamespace, log)
		outcome, err := mgr.UpdateMR(mr, project)
		if err != nil {
			return "", err
		}
		// The code moved under the comments.
		if integrate && outcome != workspace.UpdateCurrent {
			reanchorAfterUpdate(mgr.MRDir(project.PathWithNamespace, mr.IID, mr.SourceBranch), log)
		}
		return fmt.Sprintf("!%d: %s", mr.IID, outcome), nil
	})
}

// detachedLabel is what the branch column says of a detached HEAD: the commit
// it is at, marked so it is not taken for a branch.
func detachedLabel(sha string) string { return "@" + shortSHA(sha) }

// detachedState is where a detached HEAD stands: which branch it left and
// how many of that branch's commits it is behind. It runs off the event
// loop.
func detachedState(git *gitx.Git, dir string) remoteState {
	st := remoteState{Detached: true, From: git.LastBranch(dir)}
	if st.From != "" {
		ref := "origin/" + st.From
		if !git.RemoteBranchExists(dir, st.From) {
			ref = st.From
		}
		st.FromBehind = max(git.Count(dir, "HEAD.."+ref), 0)
	}
	return st
}

// detachedWords is the REMOTE column of a detached HEAD: how far behind the
// branch it came from, which says it is an old state.
func detachedWords(st remoteState) (string, tcell.Color) {
	switch {
	case st.From == "":
		return "detached", colDim
	case st.FromBehind > 0:
		return fmt.Sprintf("↓%d %s", st.FromBehind, st.From), colWarn
	}
	return "at " + st.From, colDim
}

// detachedSentence says the same in a sentence, and the way back.
func detachedSentence(st remoteState) string {
	switch {
	case st.From == "":
		return "detached HEAD: no branch is out · b picks one"
	case st.FromBehind > 0:
		return fmt.Sprintf("detached HEAD, %d commit(s) behind %s · Back to Branch returns to it", st.FromBehind, st.From)
	}
	return fmt.Sprintf("detached HEAD at the tip of %s · Back to Branch returns to it", st.From)
}
