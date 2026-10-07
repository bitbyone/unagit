package ui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/zoxide"
)

func (a *App) zoxideTool() *zoxide.Client {
	a.zoxideOnce.Do(func() { a.zoxideClient.Store(zoxide.Find(a.executable)) })
	return a.zoxideClient.Load()
}

func (a *App) zoxideOn() bool {
	return a.zoxideTool().Enabled(a.cfg.Integrations.Zoxide)
}

// Editor and removal tasks read the switch without reading the configuration
// while Settings can be writing it on the event loop.
func (a *App) zoxideAdd(dir string) {
	if a.zoxideEnabled.Load() {
		_ = a.zoxideTool().Add(dir)
	}
}

func (a *App) zoxideRemove(dir string) {
	if a.zoxideEnabled.Load() {
		_ = a.zoxideTool().Remove(dir)
	}
}

// A list holds one snapshot until the next tab switch, so recording a visit
// cannot move the row out from under the cursor. Slow tools stay off the loop.
func (a *App) refreshZoxide() {
	on := a.zoxideOn()
	a.zoxideEnabled.Store(on)
	a.zoxideGen++
	gen := a.zoxideGen
	if !on {
		if a.zoxideScores != nil {
			a.zoxideScores = nil
			a.afterZoxide()
		}
		return
	}
	job := a.startJob("Reading zoxide visits")
	go func() {
		scores, err := a.zoxideTool().Scores()
		a.tv.QueueUpdateDraw(func() {
			a.endJob(job)
			if gen != a.zoxideGen {
				return
			}
			if err == nil {
				a.zoxideScores = scores
			}
			a.afterZoxide()
		})
	}()
}

func (a *App) afterZoxide() {
	if a.projectsPane == nil {
		return
	}
	a.projectsPane.reload()
	a.worktreesPane.reload()
	if a.settings != nil {
		a.settings.integrations.paintFocus(a.settings.integrations.active)
	}
}

func underDirectory(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, strings.TrimRight(dir, string(filepath.Separator))+string(filepath.Separator))
}

// Worktrees carry visits back to their repository, including the older layout
// and members of groups, whose folder is what the editor opens.
func (a *App) repositoryScore(key projectKey) (float64, bool) {
	m := a.pathManager(key.Instance, key.Path)
	best, known := a.zoxideScores[zoxide.Path(m.ProjectDir(key.Path))]
	roots := m.WorktreeRoots(key.Path)
	for i := range roots {
		roots[i] = zoxide.Path(roots[i])
	}
	for path, score := range a.zoxideScores {
		for _, root := range roots {
			if filepath.Dir(path) == root {
				best, known = max(best, score), true
				break
			}
		}
	}
	for _, row := range a.worktrees {
		for _, member := range row.Members {
			if member.Instance == key.Instance && member.Path == key.Path {
				for _, dir := range []string{row.Dir, member.Dir} {
					if score, ok := a.zoxideScores[zoxide.Path(dir)]; ok {
						best, known = max(best, score), true
					}
				}
			}
		}
	}
	return best, known
}

type visitScore struct {
	value float64
	known bool
}

func byVisits(hits []scored, score func(int) (float64, bool), when func(int) time.Time) {
	// Resolving a repository's worktrees is done once per row, never once per
	// comparison of rows in the sort.
	visits := make(map[int]visitScore, len(hits))
	for _, hit := range hits {
		n, ok := score(hit.idx)
		visits[hit.idx] = visitScore{n, ok}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		l, r := visits[hits[i].idx], visits[hits[j].idx]
		if l.known != r.known {
			return l.known
		}
		if c := cmp.Compare(l.value, r.value); c != 0 {
			return c > 0
		}
		return when(hits[i].idx).After(when(hits[j].idx))
	})
}

func (a *App) zoxideFound() string {
	if !a.zoxideOn() {
		return ""
	}
	roots := []string{a.cfg.Root()}
	for _, inst := range a.cfg.Instances {
		roots = append(roots, a.cfg.RootFor(&inst, ""))
		for _, group := range inst.Groups {
			roots = append(roots, a.cfg.GroupRoot(&inst, group))
		}
		for _, dir := range inst.ProjectDirs {
			roots = append(roots, dir)
		}
	}
	count := 0
	for dir := range a.zoxideScores {
		for _, root := range roots {
			if underDirectory(dir, zoxide.Path(config.Expand(root))) {
				count++
				break
			}
		}
	}
	noun := "directories"
	if count == 1 {
		noun = "directory"
	}
	return fmt.Sprintf("%d %s known under your roots", count, noun)
}

// An explicit installation check refreshes the cached executable; ordinary
// visits use the one already found, including visits in worker goroutines.
func (a *App) checkZoxide() {
	a.zoxideTool()
	a.zoxideClient.Store(zoxide.Find(a.executable))
	a.refreshZoxide()
}
