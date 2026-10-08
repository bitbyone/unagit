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
// The scores are read only when something shows them, and not as a job: it
// is a local file, read in milliseconds, and a spinner on every tab switch
// says nothing but that one was pressed.
func (a *App) refreshZoxide() {
	on := a.zoxideOn()
	a.zoxideEnabled.Store(on)
	a.zoxideGen++
	gen := a.zoxideGen
	if !on {
		if a.zoxideScores != nil {
			a.zoxideScores, a.zoxideParents = nil, nil
			a.afterZoxide()
		}
		return
	}
	if !a.zoxideShown() {
		return
	}
	go func() {
		scores, err := a.zoxideTool().Scores()
		a.tv.QueueUpdateDraw(func() {
			if gen != a.zoxideGen {
				return
			}
			if err == nil {
				a.zoxideScores, a.zoxideParents = scores, nil
			}
			a.afterZoxide()
		})
	}()
}

// zoxideShown says whether anything on screen uses the scores: a list in
// the frecency order, or Settings, whose card counts them.
func (a *App) zoxideShown() bool {
	if a.currentTab() == pageSettings {
		return true
	}
	return a.order(config.ListRepositories) == config.SortFrecency ||
		a.order(config.ListWorktrees) == config.SortFrecency
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
	parents := a.zoxideByParent()
	for _, root := range m.WorktreeRoots(key.Path) {
		if score, ok := parents[zoxide.Path(root)]; ok {
			best, known = max(best, score), true
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

// zoxideByParent is the best score among the directories of each parent: a
// repository's worktrees are the directories of its worktree roots, and the
// sort looks each root up instead of walking every score for every row.
func (a *App) zoxideByParent() map[string]float64 {
	if a.zoxideParents == nil && a.zoxideScores != nil {
		a.zoxideParents = make(map[string]float64, len(a.zoxideScores))
		for path, score := range a.zoxideScores {
			parent := filepath.Dir(path)
			if best, ok := a.zoxideParents[parent]; !ok || score > best {
				a.zoxideParents[parent] = score
			}
		}
	}
	return a.zoxideParents
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
