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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
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

// Options is everything a Manager needs to know. The root is resolved by the
// caller, because it depends on the instance and the group a project sits in.
// Clone protocols.
const (
	ProtocolHTTPS = "https"
	ProtocolSSH   = "ssh"
)

type Options struct {
	Root       string
	GitLabURL  string
	Editor     string
	EditorArgs []string
	Token      string
	// CloneProtocol is ProtocolHTTPS or ProtocolSSH; empty means HTTPS, which
	// is what unagit did before it could do anything else.
	CloneProtocol string
	// GitUser is the user name the HTTPS credential helper hands to git.
	GitUser string
	// HeadRefFormat is where the forge publishes a merge request head, with a
	// single %d for the number. Empty means GitLab's layout.
	HeadRefFormat string
}

// Manager performs all disk side effects.
type Manager struct {
	opts Options
	git  *gitx.Git
}

// New returns a Manager. log receives progress lines and may be nil.
func New(opts Options, log func(string)) *Manager {
	return &Manager{opts: opts, git: gitx.New(opts.Token, log).WithUser(opts.GitUser)}
}

// headRef is where the merge request head can be fetched from.
func (m *Manager) headRef(iid int) string {
	format := m.opts.HeadRefFormat
	if format == "" {
		format = "refs/merge-requests/%d/head"
	}
	return fmt.Sprintf(format, iid)
}

// Root is the directory this manager clones into.
func (m *Manager) Root() string { return config.Expand(m.opts.Root) }

// Git exposes the underlying runner for status queries.
func (m *Manager) Git() *gitx.Git { return m.git }

func (m *Manager) log(format string, a ...any) { m.git.Log(fmt.Sprintf(format, a...)) }

// The layout functions take the root explicitly, because which root a project
// belongs to depends on its instance and on the group it sits in.

// ProjectDirIn is the main clone directory of a project under root.
func ProjectDirIn(root, projectPath string) string {
	return filepath.Join(config.Expand(root), filepath.FromSlash(projectPath))
}

// MRRootIn holds every branch worktree of a project under root.
func MRRootIn(root, projectPath string) string { return ProjectDirIn(root, projectPath) + MRSuffix }

// ReviewRootIn holds every review worktree of a project under root.
func ReviewRootIn(root, projectPath string) string {
	return ProjectDirIn(root, projectPath) + ReviewSuffix
}

// MRDirIn is the branch worktree of one merge request under root.
func MRDirIn(root, projectPath string, iid int, sourceBranch string) string {
	return filepath.Join(MRRootIn(root, projectPath), mrDirName(iid, sourceBranch))
}

// ReviewDirIn is the review worktree of one merge request under root.
func ReviewDirIn(root, projectPath string, iid int, sourceBranch string) string {
	return filepath.Join(ReviewRootIn(root, projectPath), mrDirName(iid, sourceBranch))
}

// ProjectDir is the main clone directory of a project.
func (m *Manager) ProjectDir(projectPath string) string {
	return ProjectDirIn(m.Root(), projectPath)
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

// RemoteURL is where a project is cloned from, under the configured protocol.
// The address the forge itself reported is preferred - it knows about custom
// SSH ports and hosts - and only built by hand when it is missing.
func (m *Manager) RemoteURL(p forge.Project) string {
	if m.opts.CloneProtocol == ProtocolSSH {
		if p.SSHURLToRepo != "" {
			return p.SSHURLToRepo
		}
		return "git@" + hostOf(m.opts.GitLabURL) + ":" + p.PathWithNamespace + ".git"
	}
	if p.HTTPURLToRepo != "" {
		return p.HTTPURLToRepo
	}
	return strings.TrimRight(m.opts.GitLabURL, "/") + "/" + p.PathWithNamespace + ".git"
}

// hostOf is the host part of a server URL.
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Hostname()
	}
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://"), "/")
}

// EnsureProject clones the project if needed, then fetches and fast-forwards
// the current branch. It returns the directory to open.
func (m *Manager) EnsureProject(p forge.Project) (string, error) {
	dir := m.ProjectDir(p.PathWithNamespace)
	if !Exists(dir) {
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		m.log("Cloning %s", p.PathWithNamespace)
		if err := m.git.Clone(m.RemoteURL(p), dir); err != nil {
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
func (m *Manager) ensureMain(p forge.Project) (string, error) {
	dir := m.ProjectDir(p.PathWithNamespace)
	if Exists(dir) {
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	m.log("Cloning %s (base clone for merge request worktrees)", p.PathWithNamespace)
	return dir, m.git.Clone(m.RemoteURL(p), dir)
}

// SetRemote points an existing clone at another address, for switching a
// project between HTTPS and SSH.
func (m *Manager) SetRemote(p forge.Project) (string, error) {
	dir := m.ProjectDir(p.PathWithNamespace)
	if !Exists(dir) {
		return "", nil
	}
	want := m.RemoteURL(p)
	if current, err := m.git.RemoteURL(dir, "origin"); err == nil && current == want {
		return "", nil
	}
	return want, m.git.SetRemoteURL(dir, "origin", want)
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
func (m *Manager) EnsureMR(mr forge.MergeRequest, project forge.Project) (string, error) {
	projectPath := project.PathWithNamespace
	if projectPath == "" {
		return "", fmt.Errorf("unknown project path for merge request !%d - refresh the project index", mr.IID)
	}
	mainDir, err := m.ensureMain(project)
	if err != nil {
		return "", err
	}
	wtDir := m.MRDir(projectPath, mr.IID, mr.SourceBranch)
	headRef := m.headRef(mr.IID)

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
func (m *Manager) recordBranchMeta(dir, projectPath string, mr forge.MergeRequest) {
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
func (m *Manager) SwitchBranch(p forge.Project, branch string) (string, error) {
	dir, err := m.ensureMain(p)
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
	root := filepath.Clean(m.Root())
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
	args := m.opts.EditorArgs
	if len(args) == 0 {
		args = []string{"."}
	}
	editor := m.opts.Editor
	if editor == "" {
		editor = "nvim"
	}
	cmd := exec.Command(editor, args...)
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
