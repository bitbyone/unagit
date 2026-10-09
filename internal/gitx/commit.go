package gitx

import (
	"fmt"
	"strings"
)

// unagit treats a working tree as IntelliJ does: a file is versioned or it
// is not, and a change to a versioned file is committed as it is on disk.
// The index is git's detail, never something to stage into first.

// EditCounts counts a working tree's uncommitted files: the versioned ones -
// changed, added, deleted, renamed, staged or not - and the unversioned ones
// git does not track yet. Both are -1 when git cannot say.
func (g *Git) EditCounts(dir string) (versioned, unversioned int) {
	// Without the index lock: it is read in the background, and a lock taken
	// to refresh the index fails a commit or a checkout running beside it.
	out, err := g.Run(dir, "--no-optional-locks", "status", "--porcelain")
	if err != nil {
		return -1, -1
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "??"):
			unversioned++
		default:
			versioned++
		}
	}
	return versioned, unversioned
}

// CommitVersioned commits every change to a versioned file as it is on
// disk, and leaves the unversioned ones alone. It answers the new commit's
// short id, or "" when no versioned file has changed. A review worktree is
// refused: its HEAD is the merge base on purpose, and a commit there would
// write the whole merge request into its history.
func (g *Git) CommitVersioned(dir, message string) (string, error) {
	if mode, _ := g.out(dir, "config", "--get", "unagit.mr.mode"); mode == "review" {
		return "", fmt.Errorf("this is a review worktree: its changes are the merge request's, not yours to commit")
	}
	versioned, _ := g.EditCounts(dir)
	if versioned == 0 {
		return "", nil
	}
	if _, err := g.Run(dir, "commit", "--all", "--quiet", "--message", message); err != nil {
		return "", err
	}
	return g.out(dir, "rev-parse", "--short", "HEAD")
}

// LocalCommits are the commits of HEAD that no branch of any remote has:
// only those may be rewritten without a force push.
func (g *Git) LocalCommits(dir string) map[string]bool {
	out, err := g.out(dir, "rev-list", "HEAD", "--not", "--remotes")
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, sha := range strings.Fields(out) {
		set[sha] = true
	}
	return set
}

// UpstreamTip is the commit the branch's upstream was at when it was last
// fetched: the lease of a force push, which then replaces only that.
func (g *Git) UpstreamTip(dir string) (string, error) {
	return g.out(dir, "rev-parse", "--verify", "@{upstream}")
}

// RewordCommit gives a commit of the branch checked out a new message,
// leaving what it changed, its author and the commits after it as they
// were. The newest is amended; an older one is written again with the new
// message and the commits after it replayed onto it, uncommitted edits
// stashed around the replay. It answers the commit's new id.
func (g *Git) RewordCommit(dir, sha, message string) (string, error) {
	branch, err := g.out(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("HEAD is detached - check out the branch the commit is on first")
	}
	if op := g.OperationInProgress(dir); op != "" {
		return "", fmt.Errorf("a %s is in progress - finish or abort it first", op)
	}
	head, err := g.out(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	full, err := g.out(dir, "rev-parse", "--verify", sha+"^{commit}")
	if err != nil {
		return "", err
	}
	if full == head {
		// --only with nothing named amends the message alone, whatever the
		// index holds.
		if _, err := g.Run(dir, "commit", "--amend", "--only", "--allow-empty", "--quiet", "--message", message); err != nil {
			return "", err
		}
		return g.out(dir, "rev-parse", "HEAD")
	}
	if _, err := g.Run(dir, "merge-base", "--is-ancestor", full, head); err != nil {
		return "", fmt.Errorf("%s is not on %s", full[:min(8, len(full))], branch)
	}
	meta, err := g.out(dir, "show", "--no-patch", "--date=raw", "--format=%an%x00%ae%x00%ad%x00%T%x00%P", full)
	if err != nil {
		return "", err
	}
	parts := strings.Split(meta, "\x00")
	if len(parts) != 5 {
		return "", fmt.Errorf("cannot read %s: %q", full, meta)
	}
	args := []string{"commit-tree", parts[3]}
	for _, parent := range strings.Fields(parts[4]) {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)
	author := []string{"GIT_AUTHOR_NAME=" + parts[0], "GIT_AUTHOR_EMAIL=" + parts[1], "GIT_AUTHOR_DATE=" + parts[2]}
	rewritten, err := g.runEnv(dir, author, args...)
	if err != nil {
		return "", err
	}
	rewritten = strings.TrimSpace(rewritten)
	// No editor may open: there is no terminal to type into.
	if _, err := g.runEnv(dir, []string{"GIT_EDITOR=true"}, "rebase", "--quiet", "--rebase-merges", "--autostash",
		"--onto", rewritten, full, branch); err != nil {
		_, _ = g.Run(dir, "rebase", "--abort")
		return "", err
	}
	return rewritten, nil
}

// PushHead sends the branch checked out to origin, setting its upstream the
// first time. With force it replaces origin's copy, but only while origin
// still has what was last fetched of it: what anyone pushed since makes git
// refuse rather than be lost. It answers the branch.
func (g *Git) PushHead(dir string, force bool) (string, error) {
	branch, err := g.out(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("HEAD is detached - there is no branch to push")
	}
	tip, err := g.UpstreamTip(dir)
	switch {
	case err != nil:
		return branch, g.Push(dir, branch, true)
	case force:
		return branch, g.ForcePush(dir, branch, tip)
	}
	return branch, g.Push(dir, branch, false)
}

// Message is a commit's whole message, as it was written.
func (g *Git) Message(dir, sha string) (string, error) {
	return g.out(dir, "log", "-1", "--format=%B", sha)
}
