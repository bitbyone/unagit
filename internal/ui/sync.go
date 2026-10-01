package ui

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
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
				st := remoteState{}
				upstreams := j.git.BranchUpstreams(j.dir)
				if u, ok := upstreams[j.branch]; ok {
					st.Upstream = u
				} else if upstreams == nil {
					st.Unreadable = true
				} else {
					st.Detached = true
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
	case st.Detached:
		return "detached", colDim
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
	case st.Detached:
		text, colour = "detached HEAD, no branch to update", colDim
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

// updateProject brings the main clone's branch up to origin: a fast-forward
// when there is nothing of yours in the way, a rebase of your commits and
// edits when there is, and nothing at all when that would conflict.
func (a *App) updateProject(pr forge.Project) {
	key := projectKey{pr.Instance, pr.PathWithNamespace}
	if !a.disk[key].Cloned {
		a.flash(pr.PathWithNamespace + " is not cloned - Ctrl-C clones it")
		return
	}
	a.runTaskNoting("Updating "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		mgr := a.newManager(pr.Instance, pr.PathWithNamespace, log)
		outcome, err := mgr.UpdateClone(mgr.ProjectDir(pr.PathWithNamespace))
		if err != nil {
			return "", err
		}
		return pr.PathWithNamespace + ": " + outcome, nil
	})
}

// updateAllClones updates every cloned repository whose branch origin has
// moved past, several at once. Each one asks origin first, so what counts as
// behind is what origin says now, not what the column said. One that cannot
// be updated without a conflict is left as it was and named at the end.
func (a *App) updateAllClones() {
	var projects []forge.Project
	for _, pr := range a.projects {
		if a.disk[projectKey{pr.Instance, pr.PathWithNamespace}].Cloned {
			projects = append(projects, pr)
		}
	}
	if len(projects) == 0 {
		a.flash("no repository is cloned yet")
		return
	}
	a.runTaskNoting(fmt.Sprintf("Updating %d cloned repositories", len(projects)), func(log func(string)) (string, error) {
		var (
			mu                        sync.Mutex
			updated, current, skipped int
			refused                   []string
		)
		sem := make(chan struct{}, syncFanOut)
		var wg sync.WaitGroup
		for _, pr := range projects {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				// The git lines of several clones at once would interleave into
				// nonsense, so each says only how it ended.
				mgr := a.newManager(pr.Instance, pr.PathWithNamespace, nil)
				outcome, err := mgr.UpdateClone(mgr.ProjectDir(pr.PathWithNamespace))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case errors.Is(err, workspace.ErrNotTracking):
					skipped++
				case err != nil:
					refused = append(refused, pr.PathWithNamespace+": "+firstLine(err.Error()))
					log("! " + pr.PathWithNamespace + ": left as it was")
				case outcome == workspace.UpdateCurrent:
					current++
				default:
					updated++
					log(pr.PathWithNamespace + ": " + outcome)
				}
			}()
		}
		wg.Wait()
		summary := fmt.Sprintf("%d updated · %d up to date", updated, current)
		if skipped > 0 {
			summary += fmt.Sprintf(" · %d without an upstream", skipped)
		}
		log(summary)
		if len(refused) > 0 {
			return "", fmt.Errorf("%d repositor%s left as %s - update by hand:\n%s",
				len(refused), plural(len(refused), "y was", "ies were"), plural(len(refused), "it was", "they were"),
				strings.Join(refused, "\n"))
		}
		return summary, nil
	})
}
