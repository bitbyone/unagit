// Package gitx wraps the git command line.
//
// HTTPS authentication is done with a one-shot credential helper that reads the
// token from the environment of the git child process. The token therefore
// never ends up in .git/config, in the remote URL, or in the process arguments.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	tokenEnv = "UNAGIT_GIT_TOKEN"
	userEnv  = "UNAGIT_GIT_USER"
)

// credentialHelper answers git's credential query from the environment. Both
// halves come from there, because the user name differs per forge: GitLab
// wants oauth2, GitHub x-access-token.
const credentialHelper = `!f() { if [ "$1" = get ]; then echo "username=$` + userEnv + `"; echo "password=$` + tokenEnv + `"; fi; }; f`

// Git runs git commands with the in-memory token attached.
type Git struct {
	token string
	user  string
	Log   func(string)
}

// New returns a Git runner. log may be nil.
func New(token string, log func(string)) *Git {
	if log == nil {
		log = func(string) {}
	}
	return &Git{token: token, user: "oauth2", Log: log}
}

// WithUser sets the user name the credential helper hands to git.
func (g *Git) WithUser(user string) *Git {
	if user != "" {
		g.user = user
	}
	return g
}

// Run executes git in dir and returns its combined output.
func (g *Git) Run(dir string, args ...string) (string, error) {
	full := append([]string{
		"-c", "credential.helper=",
		"-c", "credential.helper=" + credentialHelper,
	}, args...)

	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		tokenEnv+"="+g.token,
		userEnv+"="+g.user,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_LFS_SKIP_SMUDGE=0",
		// Over SSH, a locked key would otherwise leave git waiting for a
		// passphrase nobody can type into a captured pipe. Batch mode turns
		// that into an immediate, legible refusal; the agent still works.
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	g.Log("$ git " + strings.Join(args, " "))
	err := cmd.Run()
	out := buf.String()
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			g.Log("  " + line)
		}
	}
	if err != nil {
		return out, fmt.Errorf("git %s: %w\n%s%s",
			strings.Join(args, " "), err, strings.TrimSpace(out), hint(out))
	}
	return out, nil
}

// hint turns git's own words into something to do about them. Only the
// failures a person can act on are worth a line.
func hint(out string) string {
	switch {
	case strings.Contains(out, "Permission denied (publickey"):
		return "\n\nThe ssh key is not available. unagit runs git in batch mode, so a key " +
			"that needs a passphrase fails here rather than waiting for one nobody can type. " +
			"Load it once with:\n    ssh-add ~/.ssh/id_rsa\n" +
			"and check the key is on the server with: ssh -T git@<host>"
	case strings.Contains(out, "Host key verification failed"):
		return "\n\nThe host is not in your known_hosts yet. Connect once by hand to accept it:" +
			"\n    ssh -T git@<host>"
	case strings.Contains(out, "could not read Username"), strings.Contains(out, "Authentication failed"):
		return "\n\nThe token was refused. Check it in Settings [S], or clone over ssh instead."
	}
	return ""
}

func (g *Git) out(dir string, args ...string) (string, error) {
	s, err := g.Run(dir, args...)
	return strings.TrimSpace(s), err
}

// Clone clones url into dir.
func (g *Git) Clone(url, dir string) error {
	_, err := g.Run("", "clone", "--progress", url, dir)
	return err
}

// Fetch updates all remote refs and prunes deleted ones.
func (g *Git) Fetch(dir string) error {
	_, err := g.Run(dir, "fetch", "--all", "--prune", "--tags", "--force")
	return err
}

// FetchRefspec fetches a single refspec from origin.
func (g *Git) FetchRefspec(dir, refspec string) error {
	_, err := g.Run(dir, "fetch", "origin", refspec)
	return err
}

// CurrentBranch returns the checked out branch, or an empty string when HEAD
// is detached.
func (g *Git) CurrentBranch(dir string) string {
	s, err := g.out(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || s == "HEAD" {
		return ""
	}
	return s
}

// LocalBranchExists reports whether refs/heads/<branch> exists.
func (g *Git) LocalBranchExists(dir, branch string) bool {
	_, err := g.out(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// RemoteBranchExists reports whether refs/remotes/origin/<branch> exists.
func (g *Git) RemoteBranchExists(dir, branch string) bool {
	_, err := g.out(dir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// Status describes the local state of a working tree.
type Status struct {
	Dirty      bool
	DirtyFiles int
	Unpushed   int
	NoUpstream bool
	Branch     string
}

// Describe returns a human readable summary, or an empty string when the tree
// is clean and fully pushed.
func (s Status) Describe() string {
	var parts []string
	if s.DirtyFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d uncommitted change(s)", s.DirtyFiles))
	}
	if s.Unpushed > 0 {
		parts = append(parts, fmt.Sprintf("%d unpushed commit(s)", s.Unpushed))
	}
	if s.NoUpstream {
		parts = append(parts, "branch has no upstream")
	}
	return strings.Join(parts, ", ")
}

// Status inspects a working tree.
func (g *Git) Status(dir string) Status {
	st := Status{Branch: g.CurrentBranch(dir)}
	if out, err := g.out(dir, "status", "--porcelain"); err == nil && out != "" {
		st.DirtyFiles = len(strings.Split(out, "\n"))
		st.Dirty = true
	}
	if out, err := g.out(dir, "rev-list", "--count", "@{upstream}..HEAD"); err == nil {
		st.Unpushed, _ = strconv.Atoi(out)
	} else {
		st.NoUpstream = true
	}
	return st
}

// PullFF fast-forwards the current branch. It is a no-op without an upstream.
func (g *Git) PullFF(dir string) error {
	if _, err := g.out(dir, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		g.Log("  (no upstream for the current branch, skipping pull)")
		return nil
	}
	_, err := g.Run(dir, "merge", "--ff-only", "@{upstream}")
	return err
}

// Checkout switches to an existing local branch.
func (g *Git) Checkout(dir, branch string) error {
	_, err := g.Run(dir, "checkout", branch)
	return err
}

// CheckoutTracking creates a local branch tracking origin/<branch>.
func (g *Git) CheckoutTracking(dir, branch string) error {
	_, err := g.Run(dir, "checkout", "-b", branch, "--track", "origin/"+branch)
	return err
}

// CreateBranch creates a local branch at startPoint without checking it out.
func (g *Git) CreateBranch(dir, branch, startPoint string) error {
	_, err := g.Run(dir, "branch", branch, startPoint)
	return err
}

// SetUpstream points a local branch at origin/<remoteBranch>.
func (g *Git) SetUpstream(dir, branch, remoteBranch string) error {
	_, err := g.Run(dir, "branch", "--set-upstream-to=origin/"+remoteBranch, branch)
	return err
}

// WorktreeAdd checks out an existing local branch into its own directory.
func (g *Git) WorktreeAdd(mainDir, path, branch string) error {
	_, err := g.Run(mainDir, "worktree", "add", path, branch)
	return err
}

// CheckedOut maps every branch checked out in a working tree of the repository
// to that tree's directory. Git refuses a second checkout of the same branch,
// and saying where the first one is lets the user do something about it.
func (g *Git) CheckedOut(mainDir string) map[string]string {
	out, err := g.out(mainDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	found := map[string]string{}
	dir := ""
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			dir = rest
		}
		if branch, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			found[branch] = dir
		}
	}
	return found
}

// CheckedOutIn is the working tree that has branch checked out, or "".
func (g *Git) CheckedOutIn(mainDir, branch string) string {
	return g.CheckedOut(mainDir)[branch]
}

// BaseRef is what a branch made from base is compared with and rebased onto:
// origin's copy when there is one, since the local branch may lag behind it,
// or "" when base is nowhere to be found.
func (g *Git) BaseRef(dir, base string) string {
	switch {
	case g.RemoteBranchExists(dir, base):
		return "origin/" + base
	case g.LocalBranchExists(dir, base):
		return base
	}
	return ""
}

// Count is how many commits a range holds, 0 when git cannot say.
func (g *Git) Count(dir, revRange string) int {
	out, err := g.out(dir, "rev-list", "--count", revRange)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(out)
	return n
}

// baseKey is where unagit notes the branch a new branch was made from, so it
// can later say how far that base has moved on and rebase onto it.
const baseKey = "unagitbase"

// SetBranchBase records the branch that branch was made from.
func (g *Git) SetBranchBase(dir, branch, base string) error {
	_, err := g.Run(dir, "config", "branch."+branch+"."+baseKey, base)
	return err
}

// BranchBases maps every branch with a recorded base to that base, from one
// git config for the whole repository.
func (g *Git) BranchBases(dir string) map[string]string {
	out, err := g.out(dir, "config", "--get-regexp", `^branch\..*\.`+baseKey+`$`)
	if err != nil || out == "" {
		return nil
	}
	bases := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		branch := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), "."+baseKey)
		bases[branch] = value
	}
	return bases
}

// WorktreeRemove detaches a worktree directory from the repository.
func (g *Git) WorktreeRemove(mainDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := g.Run(mainDir, append(args, path)...)
	return err
}

// WorktreePrune drops stale worktree administrative entries.
func (g *Git) WorktreePrune(mainDir string) {
	_, _ = g.Run(mainDir, "worktree", "prune")
}

// LocalBranches lists local branch names.
func (g *Git) LocalBranches(dir string) []string {
	out, err := g.out(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// RemoteURL returns the address a remote points at.
func (g *Git) RemoteURL(dir, remote string) (string, error) {
	return g.out(dir, "remote", "get-url", remote)
}

// SetRemoteURL points a remote at another address.
func (g *Git) SetRemoteURL(dir, remote, url string) error {
	_, err := g.Run(dir, "remote", "set-url", remote, url)
	return err
}

// IsRepo reports whether dir is the top level of a git working tree.
func IsRepo(dir string) bool {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	_, err := os.Stat(dir + "/.git")
	return err == nil
}

// CommitExists reports whether an object is present locally and is a commit.
func (g *Git) CommitExists(dir, sha string) bool {
	if sha == "" {
		return false
	}
	_, err := g.out(dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// MergeBase returns the common ancestor of two commits.
func (g *Git) MergeBase(dir, a, b string) (string, error) {
	return g.out(dir, "merge-base", a, b)
}

// RevParse resolves a revision to a commit id.
func (g *Git) RevParse(dir, rev string) (string, error) {
	return g.out(dir, "rev-parse", rev+"^{commit}")
}

// WorktreeAddDetached checks a commit out into its own directory with a
// detached HEAD.
func (g *Git) WorktreeAddDetached(mainDir, path, commit string) error {
	_, err := g.Run(mainDir, "worktree", "add", "--detach", path, commit)
	return err
}

// ReadTree points the index and the working tree at a commit while leaving
// HEAD where it is. Local edits that do not conflict are kept.
func (g *Git) ReadTree(dir, commit string) error {
	_, err := g.Run(dir, "read-tree", "-u", "-m", commit)
	return err
}

// ResetHard moves HEAD, the index and the working tree to a commit.
func (g *Git) ResetHard(dir, commit string) error {
	_, err := g.Run(dir, "reset", "--hard", commit)
	return err
}

// UnstagedFiles lists the files the working tree changes on top of the index.
func (g *Git) UnstagedFiles(dir string) []string {
	return g.names(dir, "diff", "--name-only")
}

// ChangedSince lists the working tree files that differ from a commit. In a
// review worktree, where the merge request itself is pending by design, this
// is what tells the reviewer's own edits apart from it.
func (g *Git) ChangedSince(dir, commit string) []string {
	return g.names(dir, "diff", "--name-only", commit)
}

// AddedPaths lists the files head adds on top of base.
func (g *Git) AddedPaths(dir, base, head string) []string {
	return g.names(dir, "diff", "--name-only", "--diff-filter=A", base, head)
}

func (g *Git) names(dir string, args ...string) []string {
	out, err := g.out(dir, args...)
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Exclude makes the repository ignore a pattern, through info/exclude: a
// local ignore file that is never committed and holds for every worktree of
// the repository. A pattern already there is not added again.
func (g *Git) Exclude(dir, pattern string) error {
	common, err := g.out(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	path := filepath.Join(common, "info", "exclude")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	data = append(data, []byte(pattern+"\n")...)
	return os.WriteFile(path, data, 0o644)
}

// TrackedUnder lists the files of the index below sub, a directory of the
// worktree.
func (g *Git) TrackedUnder(dir, sub string) []string {
	return g.names(dir, "ls-files", "--", sub)
}

// SkipWorktree sets or clears the skip-worktree bit of paths: while it is
// set, git takes the file in the working tree to be the one in the index, so
// what is done to it shows in no diff and no status.
func (g *Git) SkipWorktree(dir string, paths []string, skip bool) error {
	flag := "--no-skip-worktree"
	if skip {
		flag = "--skip-worktree"
	}
	const perCall = 200
	for len(paths) > 0 {
		n := min(perCall, len(paths))
		if _, err := g.Run(dir, append([]string{"update-index", flag, "--"}, paths[:n]...)...); err != nil {
			return err
		}
		paths = paths[n:]
	}
	return nil
}

// ResetIndex points the index back at HEAD and leaves the working tree as it
// is, so what was staged reads as unstaged work instead.
func (g *Git) ResetIndex(dir string) error {
	_, err := g.Run(dir, "reset", "-q")
	return err
}

// IntentToAdd records paths in the index without their content. A file that
// is only in the working tree is untracked, and git diff - along with every
// editor that draws its gutter from it - passes untracked files by; an
// intent-to-add entry makes one read as a whole new file instead.
func (g *Git) IntentToAdd(dir string, paths []string) error {
	// Long enough to keep it to one call for any realistic merge request,
	// short enough to stay well inside the argument limit.
	const perCall = 200
	for len(paths) > 0 {
		n := min(perCall, len(paths))
		if _, err := g.Run(dir, append([]string{"add", "-N", "--"}, paths[:n]...)...); err != nil {
			return err
		}
		paths = paths[n:]
	}
	return nil
}

// EnableWorktreeConfig turns on per-worktree configuration, so that each
// merge request directory can carry its own metadata.
func (g *Git) EnableWorktreeConfig(mainDir string) error {
	_, err := g.Run(mainDir, "config", "extensions.worktreeConfig", "true")
	return err
}

// SetWorktreeConfig writes a key into this worktree's own configuration.
func (g *Git) SetWorktreeConfig(dir, key, value string) error {
	_, err := g.Run(dir, "config", "--worktree", key, value)
	return err
}

// UnsetWorktreeConfig removes a key from this worktree's own configuration. A
// key that is not there is not an error.
func (g *Git) UnsetWorktreeConfig(dir, key string) {
	if g.WorktreeConfig(dir, key) != "" {
		_, _ = g.Run(dir, "config", "--worktree", "--unset", key)
	}
}

// WorktreeConfig reads a configuration key as seen from a worktree.
func (g *Git) WorktreeConfig(dir, key string) string {
	v, err := g.out(dir, "config", "--get", key)
	if err != nil {
		return ""
	}
	return v
}

// Upstream is where a local branch stands against the branch it tracks.
type Upstream struct {
	Name   string // the upstream's short name, "origin/feat/x"; empty when there is none
	Ahead  int    // commits here the upstream lacks
	Behind int    // commits there this one lacks
	Gone   bool   // the upstream is configured but no longer exists
}

// BranchUpstreams says, for every local branch, what it tracks and how far it has
// drifted, from one for-each-ref rather than a command per branch.
func (g *Git) BranchUpstreams(dir string) map[string]Upstream {
	out, err := g.out(dir, "for-each-ref",
		"--format=%(refname:short)%09%(upstream:short)%09%(upstream:track)", "refs/heads")
	if err != nil {
		return nil
	}
	branches := map[string]Upstream{}
	for _, line := range strings.Split(out, "\n") {
		// The last line has lost its trailing tab, and with it the empty track field.
		parts := append(strings.SplitN(line, "\t", 3), "", "")
		if parts[0] == "" {
			continue
		}
		u := Upstream{Name: parts[1]}
		for _, field := range strings.Split(strings.Trim(parts[2], "[]"), ",") {
			field = strings.TrimSpace(field)
			switch {
			case field == "gone":
				u.Gone = true
			case strings.HasPrefix(field, "ahead "):
				u.Ahead, _ = strconv.Atoi(strings.TrimPrefix(field, "ahead "))
			case strings.HasPrefix(field, "behind "):
				u.Behind, _ = strconv.Atoi(strings.TrimPrefix(field, "behind "))
			}
		}
		branches[parts[0]] = u
	}
	return branches
}

// Push sends a branch to origin, and with setUpstream makes origin's copy the one
// it tracks. It never forces: a branch that origin has moved past is refused.
func (g *Git) Push(dir, branch string, setUpstream bool) error {
	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	_, err := g.Run(dir, append(args, "origin", branch)...)
	return err
}

// CommitMsg is what a commit says.
type CommitMsg struct {
	Subject string
	Body    string
}

// CommitsAhead lists the commits of HEAD that base lacks, oldest first.
func (g *Git) CommitsAhead(dir, base string) ([]CommitMsg, error) {
	out, err := g.Run(dir, "log", "--reverse", "--format=%s%x1f%b%x1e", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []CommitMsg
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.Trim(record, "\n")
		if record == "" {
			continue
		}
		subject, body, _ := strings.Cut(record, "\x1f")
		commits = append(commits, CommitMsg{Subject: strings.TrimSpace(subject), Body: strings.TrimSpace(body)})
	}
	return commits, nil
}

// LogEntry is one commit as a list of them shows it.
type LogEntry struct {
	SHA     string
	Subject string
	Author  string
	When    time.Time
	Merge   bool // it has more than one parent
}

// Commits lists the commits of to that from lacks, oldest first.
func (g *Git) Commits(dir, from, to string) ([]LogEntry, error) {
	out, err := g.out(dir, "log", "--reverse", "--format=%H%x1f%P%x1f%an%x1f%ct%x1f%s", from+".."+to)
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x1f", 5)
		if len(f) < 5 {
			continue
		}
		unix, _ := strconv.ParseInt(f[3], 10, 64)
		entries = append(entries, LogEntry{
			SHA:     f[0],
			Merge:   len(strings.Fields(f[1])) > 1,
			Author:  f[2],
			When:    time.Unix(unix, 0),
			Subject: f[4],
		})
	}
	return entries, nil
}

// IsAncestor reports whether a is reachable from b.
func (g *Git) IsAncestor(dir, a, b string) bool {
	_, err := g.out(dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// Unseen lists the commits of base..head that seen neither contains nor has an
// equivalent of. git cherry compares patches rather than commit ids, so a
// commit that a rebase rewrote still counts as seen.
func (g *Git) Unseen(dir, seen, head, base string) (map[string]bool, error) {
	out, err := g.out(dir, "cherry", seen, head, base)
	if err != nil {
		return nil, err
	}
	unseen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if sha, ok := strings.CutPrefix(line, "+ "); ok {
			unseen[strings.TrimSpace(sha)] = true
		}
	}
	return unseen, nil
}
