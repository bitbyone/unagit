package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The Changes dialog's side of git: what is not committed, file by file and
// against HEAD, the diff of one, and the three things done with a choice of
// them - commit, roll back, delete. Paths go to git literally, so a file
// named with a star is that file and not a pattern.

// ChangeKind is what happened to a file since HEAD, as IntelliJ colours it.
type ChangeKind int

const (
	Modified ChangeKind = iota
	Added
	Deleted
	Renamed
	Conflicted
	Unversioned
)

// Change is one file not committed. From is where a renamed file was.
type Change struct {
	Path string
	From string
	Kind ChangeKind
}

// Versioned reports whether git tracks the file, or is about to.
func (c Change) Versioned() bool { return c.Kind != Unversioned }

// paths are the file's paths a commit or a rollback has to name: both ends
// of a rename.
func (c Change) paths() []string {
	if c.From != "" {
		return []string{c.Path, c.From}
	}
	return []string{c.Path}
}

// emptyTree is git's tree with nothing in it: what a repository without a
// commit yet is compared with.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Changes lists the files not committed, the versioned first, each part by
// path. An unversioned directory is listed file by file, so each can be
// chosen.
func (g *Git) Changes(dir string) ([]Change, error) {
	out, err := g.Run(dir, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var changes []Change
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		x, y, path := f[0], f[1], f[3:]
		c := Change{Path: path}
		switch {
		case x == '?':
			c.Kind = Unversioned
		case x == 'U' || y == 'U' || x == 'A' && y == 'A' || x == 'D' && y == 'D':
			c.Kind = Conflicted
		case x == 'R' || x == 'C':
			// The other end of a rename comes as a field of its own.
			c.Kind = Renamed
			if i+1 < len(fields) {
				c.From = fields[i+1]
				i++
			}
			if x == 'C' {
				c.Kind, c.From = Added, ""
			}
		case x == 'A':
			c.Kind = Added
		case x == 'D' || y == 'D':
			c.Kind = Deleted
		default:
			c.Kind = Modified
		}
		changes = append(changes, c)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		vi, vj := changes[i].Versioned(), changes[j].Versioned()
		if vi != vj {
			return vi
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, nil
}

// head is HEAD, or the empty tree in a repository with no commit yet.
func (g *Git) head(dir string) string {
	if _, err := g.out(dir, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return emptyTree
	}
	return "HEAD"
}

// ChangeDiff is the unified diff of one file against HEAD as it is on disk,
// staged or not; an unversioned file's is all of it, added.
func (g *Git) ChangeDiff(dir string, c Change) (string, error) {
	if c.Kind == Unversioned {
		// --no-index says there are differences by failing; the diff is
		// still what it printed.
		out, err := g.Run(dir, "diff", "--no-index", "--no-color", "--no-ext-diff", "--", os.DevNull, c.Path)
		if strings.HasPrefix(out, "diff ") {
			return out, nil
		}
		return out, err
	}
	args := []string{"--literal-pathspecs", "diff", "--no-color", "--no-ext-diff", "-M", g.head(dir), "--"}
	return g.Run(dir, append(args, c.paths()...)...)
}

// CommitPaths commits exactly the files chosen, each as it is on disk -
// what the index holds of any other is left there - adding the unversioned
// among them first. A failed commit takes those additions back. It answers
// the new commit's short id.
func (g *Git) CommitPaths(dir, message string, changes []Change) (string, error) {
	if mode, _ := g.out(dir, "config", "--get", "unagit.mr.mode"); mode == "review" {
		return "", fmt.Errorf("this is a review worktree: its changes are the merge request's, not yours to commit")
	}
	var added, paths []string
	for _, c := range changes {
		if c.Kind == Unversioned {
			added = append(added, c.Path)
		}
		paths = append(paths, c.paths()...)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no file is chosen")
	}
	if len(added) > 0 {
		if _, err := g.Run(dir, append([]string{"--literal-pathspecs", "add", "--"}, added...)...); err != nil {
			return "", err
		}
	}
	args := append([]string{"--literal-pathspecs", "commit", "--quiet", "--only", "--message", message, "--"}, paths...)
	if _, err := g.Run(dir, args...); err != nil {
		if len(added) > 0 {
			_, _ = g.Run(dir, append([]string{"--literal-pathspecs", "reset", "--quiet", "--"}, added...)...)
		}
		return "", err
	}
	return g.out(dir, "rev-parse", "--short", "HEAD")
}

// Rollback puts versioned files back as HEAD has them: a change undone, a
// deleted file brought back, an added one made unversioned again - kept on
// disk, never deleted - and a renamed one back under its old name, the new
// name left behind unversioned. Unversioned files are not git's to roll
// back, and are refused.
func (g *Git) Rollback(dir string, changes []Change) error {
	var restore, unadd []string
	for _, c := range changes {
		switch c.Kind {
		case Unversioned:
			return fmt.Errorf("%s is unversioned: there is nothing to roll back to - delete it instead", c.Path)
		case Added:
			unadd = append(unadd, c.Path)
		case Renamed:
			restore = append(restore, c.From)
			unadd = append(unadd, c.Path)
		default:
			restore = append(restore, c.Path)
		}
	}
	if len(unadd) > 0 {
		if _, err := g.Run(dir, append([]string{"--literal-pathspecs", "rm", "--quiet", "--cached", "--force", "--"}, unadd...)...); err != nil {
			return err
		}
	}
	if len(restore) > 0 {
		args := []string{"--literal-pathspecs", "restore", "--source=" + g.head(dir), "--staged", "--worktree", "--"}
		if _, err := g.Run(dir, append(args, restore...)...); err != nil {
			return err
		}
	}
	return nil
}

// DeleteUnversioned deletes files git does not track from disk. Each is
// asked about first: a versioned file is refused rather than lost, since
// only a rollback may change one.
func (g *Git) DeleteUnversioned(dir string, paths []string) error {
	for _, p := range paths {
		if _, err := g.Run(dir, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", p); err == nil {
			return fmt.Errorf("%s is versioned - roll it back instead of deleting it", p)
		}
	}
	for _, p := range paths {
		if err := os.Remove(filepath.Join(dir, p)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
