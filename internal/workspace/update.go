package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Update outcomes, for the log and the status line.
const (
	UpdateCurrent     = "up to date"
	UpdateFastForward = "fast-forwarded"
	UpdateRebased     = "rebased"
)

// ErrNotTracking marks a working tree with nothing to update from: a detached
// HEAD, or a branch without an upstream. Updating everything passes over those.
var ErrNotTracking = errors.New("nothing to update from")

// ErrNothingDone marks a refused update: the working tree was left exactly as
// it was, and the user has to sort it out by hand.
var ErrNothingDone = errors.New("nothing was changed")

// UpdateClone brings the checked out branch of a working tree up to its
// upstream. Without local work it fast-forwards; with local commits or edits it
// rebases them onto the upstream, edits carried over by --autostash. Anything
// that would end in a conflict is refused before or rolled back after, so the
// tree is either updated or untouched - never left half way.
func (m *Manager) UpdateClone(dir string) (string, error) { return m.UpdateBranch(dir, "") }

// UpdateBranch is UpdateClone for a worktree branch: one with an upstream
// follows it, one that was never pushed is rebased onto origin's copy of the
// branch it was made from. A pushed branch is never rebased onto its base -
// that would rewrite what origin has and need a force push, which unagit does
// not do.
func (m *Manager) UpdateBranch(dir, base string) (string, error) {
	if busy := m.OperationInProgress(dir); busy != "" {
		return "", fmt.Errorf("a %s is in progress here - finish or abort it first: %w", busy, ErrNothingDone)
	}
	branch := m.git.CurrentBranch(dir)
	if branch == "" {
		return "", fmt.Errorf("HEAD is detached, there is no branch to update: %w", ErrNotTracking)
	}
	if err := m.git.Fetch(dir); err != nil {
		return "", err
	}
	upstream, err := m.trimmed(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || upstream == "" {
		if base == "" {
			return "", fmt.Errorf("%s has no upstream to update from: %w", branch, ErrNotTracking)
		}
		if upstream = m.startOfGroupBranch(dir, base); upstream == "" {
			return "", fmt.Errorf("%s was made from %s, which is gone: %w", branch, base, ErrNothingDone)
		}
	}
	behind := m.count(dir, "HEAD.."+upstream)
	if behind == 0 {
		return UpdateCurrent, nil
	}
	ahead := m.count(dir, upstream+"..HEAD")
	dirty := m.dirtyPaths(dir)

	if ahead == 0 && len(dirty) == 0 {
		m.log("Fast-forwarding %s by %d commit(s)", branch, behind)
		if _, err := m.git.Run(dir, "merge", "--ff-only", upstream); err != nil {
			return "", err
		}
		return UpdateFastForward, nil
	}

	// Edits to a file the upstream changes too would conflict when the stash
	// comes back, after the rebase already moved HEAD: refuse those up front.
	if len(dirty) > 0 {
		base, err := m.trimmed(dir, "merge-base", "HEAD", upstream)
		if err != nil {
			return "", err
		}
		incoming := map[string]bool{}
		out, _ := m.trimmed(dir, "diff", "--name-only", base, upstream)
		for _, f := range strings.Split(out, "\n") {
			incoming[f] = true
		}
		var clash []string
		for _, f := range dirty {
			if incoming[f] {
				clash = append(clash, f)
			}
		}
		if len(clash) > 0 {
			return "", fmt.Errorf("your uncommitted changes to %s collide with what %s brings - "+
				"commit or stash them and update by hand: %w", strings.Join(clash, ", "), upstream, ErrNothingDone)
		}
	}

	m.log("Rebasing %d local commit(s) and %d edited file(s) onto %s (%d new)", ahead, len(dirty), upstream, behind)
	if _, err := m.git.Run(dir, "rebase", "--autostash", upstream); err != nil {
		// The abort puts HEAD, the index and the stashed edits back.
		if _, abortErr := m.git.Run(dir, "rebase", "--abort"); abortErr != nil {
			return "", fmt.Errorf("the rebase onto %s failed and could not be aborted - run git rebase --abort: %w", upstream, err)
		}
		return "", fmt.Errorf("your commits conflict with %s - rebase by hand (git pull --rebase): %w", upstream, ErrNothingDone)
	}
	return UpdateRebased, nil
}

// OperationInProgress names a merge, rebase, cherry-pick or revert that git is
// in the middle of, or "" when there is none.
func (m *Manager) OperationInProgress(dir string) string {
	for _, op := range []struct{ path, name string }{
		{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"},
	} {
		path, err := m.trimmed(dir, "rev-parse", "--git-path", op.path)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if _, err := os.Stat(path); err == nil {
			return op.name
		}
	}
	return ""
}

// dirtyPaths lists every file with uncommitted changes, untracked ones too:
// an untracked file the upstream adds would be just as much in the way.
func (m *Manager) dirtyPaths(dir string) []string {
	out, err := m.git.Run(dir, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil
	}
	var paths []string
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		paths = append(paths, e[3:])
		// A rename names its source in the next entry, which is touched too.
		if e[0] == 'R' || e[0] == 'C' {
			if i+1 < len(entries) && entries[i+1] != "" {
				paths = append(paths, entries[i+1])
			}
			i++
		}
	}
	return paths
}

func (m *Manager) count(dir, revRange string) int {
	out, err := m.trimmed(dir, "rev-list", "--count", revRange)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(out)
	return n
}

func (m *Manager) trimmed(dir string, args ...string) (string, error) {
	out, err := m.git.Run(dir, args...)
	return strings.TrimSpace(out), err
}
