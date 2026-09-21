// Package workspace maps GitLab projects and merge requests onto directories
// under the configured root and keeps those directories up to date.
//
// Layout:
//
//	<root>/<group>/<project>            main clone, branch switching happens here
//	<root>/<group>/<project>.mrs/<iid>-<branch>   git worktree per merge request
//
// Worktrees share the main clone's object store, so a merge request checkout is
// cheap, yet each one has its own independent working tree - uncommitted
// changes survive switching between them.
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/gitx"
)

// Suffixes appended to a project directory to hold its merge request worktrees.
const (
	// MRSuffix holds branch worktrees: a real branch you can commit and push.
	MRSuffix = ".mrs"
	// ReviewSuffix holds review worktrees: HEAD sits on the merge base while
	// the index and the working tree hold the merge request, so the whole
	// change shows up as pending changes in an editor.
	ReviewSuffix = ".reviews"
)

// Manager performs all disk side effects.
type Manager struct {
	cfg *config.Config
	git *gitx.Git
}

// New returns a Manager. log receives progress lines and may be nil.
func New(cfg *config.Config, token string, log func(string)) *Manager {
	return &Manager{cfg: cfg, git: gitx.New(token, log)}
}

// Git exposes the underlying runner for status queries.
func (m *Manager) Git() *gitx.Git { return m.git }

func (m *Manager) log(format string, a ...any) { m.git.Log(fmt.Sprintf(format, a...)) }

// ProjectDir is the main clone directory of a project.
func (m *Manager) ProjectDir(projectPath string) string {
	return filepath.Join(m.cfg.Root(), filepath.FromSlash(projectPath))
}

// MRRoot is the directory holding every merge request worktree of a project.
func (m *Manager) MRRoot(projectPath string) string {
	return m.ProjectDir(projectPath) + MRSuffix
}

// MRDir is the branch worktree directory of a single merge request.
func (m *Manager) MRDir(projectPath string, iid int, sourceBranch string) string {
	return filepath.Join(m.MRRoot(projectPath), mrDirName(iid, sourceBranch))
}

// ReviewRoot is the directory holding every review worktree of a project.
func (m *Manager) ReviewRoot(projectPath string) string {
	return m.ProjectDir(projectPath) + ReviewSuffix
}

// ReviewDir is the review worktree directory of a single merge request.
func (m *Manager) ReviewDir(projectPath string, iid int, sourceBranch string) string {
	return filepath.Join(m.ReviewRoot(projectPath), mrDirName(iid, sourceBranch))
}

func mrDirName(iid int, sourceBranch string) string {
	return fmt.Sprintf("%d-%s", iid, Sanitize(sourceBranch))
}

// Sanitize turns a branch name into a single safe path segment.
func Sanitize(s string) string {
	repl := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-")
	s = repl.Replace(s)
	if len(s) > 60 {
		s = s[:60]
	}
	return strings.Trim(s, "-.")
}

// Exists reports whether a git working tree is present at dir.
func Exists(dir string) bool { return gitx.IsRepo(dir) }

// cloneURL builds the HTTPS clone URL for a project path.
func (m *Manager) cloneURL(projectPath string) string {
	return strings.TrimRight(m.cfg.GitLabURL, "/") + "/" + projectPath + ".git"
}

// EnsureProject clones the project if needed, then fetches and fast-forwards
// the current branch. It returns the directory to open.
func (m *Manager) EnsureProject(p gitlab.Project) (string, error) {
	dir := m.ProjectDir(p.PathWithNamespace)
	if !Exists(dir) {
		url := p.HTTPURLToRepo
		if url == "" {
			url = m.cloneURL(p.PathWithNamespace)
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		m.log("Cloning %s", p.PathWithNamespace)
		if err := m.git.Clone(url, dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	m.log("Updating %s", p.PathWithNamespace)
	if err := m.git.Fetch(dir); err != nil {
		return dir, err
	}
	return dir, m.pullIfClean(dir)
}

// ensureMain makes sure the main clone exists; it is the object store every
// merge request worktree hangs off.
func (m *Manager) ensureMain(projectPath, httpURL string) (string, error) {
	dir := m.ProjectDir(projectPath)
	if Exists(dir) {
		return dir, nil
	}
	if httpURL == "" {
		httpURL = m.cloneURL(projectPath)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	m.log("Cloning %s (base clone for merge request worktrees)", projectPath)
	return dir, m.git.Clone(httpURL, dir)
}

func (m *Manager) pullIfClean(dir string) error {
	st := m.git.Status(dir)
	if st.Dirty {
		m.log("! working tree has local changes, skipping pull")
		return nil
	}
	if err := m.git.PullFF(dir); err != nil {
		m.log("! fast-forward failed, opening the tree as it is")
	}
	return nil
}

// EnsureMR prepares an isolated worktree for a merge request and returns its
// directory. The merge request head is fetched from refs/merge-requests/<iid>/head,
// which also works for merge requests opened from a fork.
func (m *Manager) EnsureMR(mr gitlab.MergeRequest, projectPath, httpURL string) (string, error) {
	if projectPath == "" {
		return "", fmt.Errorf("unknown project path for merge request !%d - refresh the project index", mr.IID)
	}
	mainDir, err := m.ensureMain(projectPath, httpURL)
	if err != nil {
		return "", err
	}
	wtDir := m.MRDir(projectPath, mr.IID, mr.SourceBranch)
	headRef := fmt.Sprintf("refs/merge-requests/%d/head", mr.IID)

	if Exists(wtDir) {
		m.log("Updating merge request worktree !%d", mr.IID)
		// The target branch first: fetching it last would leave FETCH_HEAD
		// pointing at the wrong commit for the fast-forward below.
		if mr.TargetBranch != "" {
			_ = m.git.FetchRefspec(wtDir, mr.TargetBranch)
		}
		if err := m.git.FetchRefspec(wtDir, headRef); err != nil {
			m.log("! fetch failed, opening the worktree as it is")
			return wtDir, nil
		}
		st := m.git.Status(wtDir)
		if st.Dirty {
			m.log("! worktree has local changes, skipping update")
			m.recordBranchMeta(wtDir, projectPath, mr)
			return wtDir, nil
		}
		if _, err := m.git.Run(wtDir, "merge", "--ff-only", "FETCH_HEAD"); err != nil {
			m.log("! cannot fast-forward (local commits or a force push), opening as it is")
		}
		m.recordBranchMeta(wtDir, projectPath, mr)
		return wtDir, nil
	}

	m.log("Creating worktree for !%d (%s)", mr.IID, mr.SourceBranch)
	m.git.WorktreePrune(mainDir)
	if mr.TargetBranch != "" {
		_ = m.git.FetchRefspec(mainDir, mr.TargetBranch)
	}
	if err := m.git.FetchRefspec(mainDir, headRef); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(wtDir), 0o755); err != nil {
		return "", err
	}

	sameProject := mr.SourceProjectID == 0 || mr.SourceProjectID == mr.TargetProjectID
	branch := mr.SourceBranch
	if !sameProject {
		branch = fmt.Sprintf("mr-%d-%s", mr.IID, Sanitize(mr.SourceBranch))
	}
	if err := m.addWorktree(mainDir, wtDir, branch); err != nil {
		// The branch may already be checked out in the main clone or in another
		// worktree; fall back to a name reserved for this merge request.
		fallback := fmt.Sprintf("unagit-mr-%d", mr.IID)
		m.log("! %v", err)
		m.log("  retrying on branch %s", fallback)
		if err := m.addWorktree(mainDir, wtDir, fallback); err != nil {
			return "", err
		}
		branch = fallback
	}
	if sameProject && m.git.RemoteBranchExists(mainDir, mr.SourceBranch) {
		_ = m.git.SetUpstream(wtDir, branch, mr.SourceBranch)
	}
	m.recordBranchMeta(wtDir, projectPath, mr)
	return wtDir, nil
}

// recordBranchMeta stores the diff base of a branch worktree, so an editor can
// show the merge request as one change even though it is a stack of commits.
func (m *Manager) recordBranchMeta(dir, projectPath string, mr gitlab.MergeRequest) {
	head, err := m.git.RevParse(dir, "HEAD")
	if err != nil {
		return
	}
	base := ""
	if mr.TargetBranch != "" {
		base, _ = m.git.MergeBase(dir, "origin/"+mr.TargetBranch, "HEAD")
	}
	m.writeMeta(dir, Meta{
		IID: mr.IID, Project: projectPath, Source: mr.SourceBranch, Target: mr.TargetBranch,
		Base: base, Head: head, URL: mr.WebURL, Mode: ModeBranch,
	})
}

func (m *Manager) addWorktree(mainDir, wtDir, branch string) error {
	if !m.git.LocalBranchExists(mainDir, branch) {
		if err := m.git.CreateBranch(mainDir, branch, "FETCH_HEAD"); err != nil {
			return err
		}
	}
	return m.git.WorktreeAdd(mainDir, wtDir, branch)
}

// SwitchBranch checks a branch out in the main clone of a project, cloning it
// first when needed.
func (m *Manager) SwitchBranch(p gitlab.Project, branch string) (string, error) {
	dir, err := m.ensureMain(p.PathWithNamespace, p.HTTPURLToRepo)
	if err != nil {
		return "", err
	}
	if st := m.git.Status(dir); st.Dirty {
		return dir, fmt.Errorf("the working tree has uncommitted changes - commit or stash them before switching branch")
	}
	if err := m.git.Fetch(dir); err != nil {
		m.log("! fetch failed, continuing with the refs already on disk")
	}
	m.log("Switching to %s", branch)
	if m.git.LocalBranchExists(dir, branch) {
		if err := m.git.Checkout(dir, branch); err != nil {
			return dir, err
		}
	} else if m.git.RemoteBranchExists(dir, branch) {
		if err := m.git.CheckoutTracking(dir, branch); err != nil {
			return dir, err
		}
	} else {
		return dir, fmt.Errorf("branch %q not found locally or on origin", branch)
	}
	return dir, m.pullIfClean(dir)
}

// Removal describes what deleting a directory would throw away.
type Removal struct {
	Dir      string
	Warnings []string
	MRDirs   []string
}

// InspectProject reports the state of a project clone and of every merge
// request worktree underneath it.
func (m *Manager) InspectProject(projectPath string) Removal {
	r := Removal{Dir: m.ProjectDir(projectPath)}
	if Exists(r.Dir) {
		if s := m.git.Status(r.Dir).Describe(); s != "" {
			r.Warnings = append(r.Warnings, "main clone: "+s)
		}
	}
	for _, root := range []string{m.MRRoot(projectPath), m.ReviewRoot(projectPath)} {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			if !Exists(dir) {
				continue
			}
			r.MRDirs = append(r.MRDirs, dir)
			if s := m.describeWorktree(dir); s != "" {
				r.Warnings = append(r.Warnings, filepath.Base(root)+"/"+e.Name()+": "+s)
			}
		}
	}
	return r
}

// InspectDir reports the state of a single working tree.
func (m *Manager) InspectDir(dir string) Removal {
	r := Removal{Dir: dir}
	if Exists(dir) {
		if s := m.describeWorktree(dir); s != "" {
			r.Warnings = append(r.Warnings, s)
		}
	}
	return r
}

// describeWorktree summarises local work. A review worktree always has a
// staged difference by design, so only the reviewer's own unstaged edits count
// as work that would be lost.
func (m *Manager) describeWorktree(dir string) string {
	if m.ReadMeta(dir).Mode == ModeReview {
		if edits := m.git.UnstagedFiles(dir); len(edits) > 0 {
			return fmt.Sprintf("%d file(s) edited in the review", len(edits))
		}
		return ""
	}
	return m.git.Status(dir).Describe()
}

// RemoveProject deletes the main clone together with all of its merge request
// worktrees.
func (m *Manager) RemoveProject(projectPath string) error {
	dir := m.ProjectDir(projectPath)
	for _, root := range []string{m.MRRoot(projectPath), m.ReviewRoot(projectPath)} {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		m.log("Removing %s", root)
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	m.log("Removing %s", dir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	m.pruneEmptyParents(filepath.Dir(dir))
	return nil
}

// RemoveMR deletes the worktrees of a merge request - both the branch one and
// the review one, whichever exist.
func (m *Manager) RemoveMR(projectPath string, iid int, sourceBranch string) error {
	mainDir := m.ProjectDir(projectPath)
	for _, wtDir := range []string{
		m.MRDir(projectPath, iid, sourceBranch),
		m.ReviewDir(projectPath, iid, sourceBranch),
	} {
		if _, err := os.Stat(wtDir); err != nil {
			continue
		}
		m.log("Removing %s", wtDir)
		if Exists(mainDir) {
			if err := m.git.WorktreeRemove(mainDir, wtDir, true); err != nil {
				m.log("! worktree remove failed, deleting the directory directly")
			}
		}
		if err := os.RemoveAll(wtDir); err != nil {
			return err
		}
		m.pruneEmptyParents(filepath.Dir(wtDir))
	}
	if Exists(mainDir) {
		m.git.WorktreePrune(mainDir)
	}
	return nil
}

// pruneEmptyParents removes empty directories up to (but not including) the root.
func (m *Manager) pruneEmptyParents(dir string) {
	root := filepath.Clean(m.cfg.Root())
	for {
		dir = filepath.Clean(dir)
		if dir == root || !strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// OpenEditor runs the configured editor in dir and blocks until it exits.
// The caller must have suspended the TUI first.
func (m *Manager) OpenEditor(dir string) error {
	args := m.cfg.EditorArgs
	if len(args) == 0 {
		args = []string{"."}
	}
	cmd := exec.Command(m.cfg.Editor, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// OpenBrowser opens a URL with the platform's default handler.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
