package gitx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// Change is one file not committed. From is where a renamed file was;
// Added and Deleted count its lines that differ from HEAD, and Binary says
// a file has no lines to count.
type Change struct {
	Path           string
	From           string
	Kind           ChangeKind
	Added, Deleted int
	Binary         bool
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
	g.countLines(dir, changes)
	sort.SliceStable(changes, func(i, j int) bool {
		vi, vj := changes[i].Versioned(), changes[j].Versioned()
		if vi != vj {
			return vi
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, nil
}

// countLines fills in how many lines of each file differ from HEAD: git's
// numstat for the versioned, the lines of the file for an unversioned one.
// A count git cannot give is left at nothing rather than failing the list.
func (g *Git) countLines(dir string, changes []Change) {
	out, _ := g.Run(dir, "--no-optional-locks", "diff", "--numstat", "-z", "-M", "--no-ext-diff", g.head(dir))
	type count struct {
		added, deleted int
		binary         bool
	}
	counts := map[string]count{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) < 3 {
			continue
		}
		path := parts[2]
		// A rename leaves the path empty and gives both ends after it.
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			i += 2
		}
		added, errA := strconv.Atoi(parts[0])
		deleted, errD := strconv.Atoi(parts[1])
		counts[path] = count{added, deleted, errA != nil || errD != nil}
	}
	for i := range changes {
		c := &changes[i]
		if c.Kind == Unversioned {
			c.Added, c.Binary = fileLines(filepath.Join(dir, c.Path))
			continue
		}
		n := counts[c.Path]
		c.Added, c.Deleted, c.Binary = n.added, n.deleted, n.binary
	}
}

// fileLines counts a file's lines, or says it is binary: a zero byte in it,
// or more than is worth reading to count.
func fileLines(path string) (int, bool) {
	const most = 4 << 20
	info, err := os.Stat(path)
	if err != nil || info.Size() > most {
		return 0, err == nil
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return 0, err == nil
	}
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n, false
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

// AddFiles puts unversioned files under git, as IntelliJ's Add to VCS
// does: each becomes an added file, a change of its own to commit.
func (g *Git) AddFiles(dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := g.Run(dir, append([]string{"--literal-pathspecs", "add", "--"}, paths...)...)
	return err
}

// Ignore writes files into the repository's .gitignore, each anchored to
// its own path - "/docs/notes.md", not every notes.md - with what git would
// read as a pattern escaped. The file is made when there is none.
func (g *Git) Ignore(dir string, paths []string) error {
	file := filepath.Join(dir, ".gitignore")
	old, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var b strings.Builder
	b.Write(old)
	if len(old) > 0 && old[len(old)-1] != '\n' {
		b.WriteByte('\n')
	}
	for _, p := range paths {
		b.WriteString("/" + ignorePattern(p) + "\n")
	}
	return os.WriteFile(file, []byte(b.String()), 0o644)
}

// ignorePattern is a path as .gitignore reads it literally: its pattern
// characters, and a trailing space git would drop, escaped.
func ignorePattern(path string) string {
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune(`\*?[!#`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if strings.HasSuffix(out, " ") {
		out = strings.TrimSuffix(out, " ") + "\\ "
	}
	return out
}

// Patch is the changes of files as one patch against HEAD - a new file for
// an unversioned one - that another checkout can apply: git apply, or
// IntelliJ's Apply Patch.
func (g *Git) Patch(dir string, changes []Change) (string, error) {
	var b strings.Builder
	for _, c := range changes {
		part, err := g.ChangeDiff(dir, c)
		if err != nil {
			return "", err
		}
		b.WriteString(part)
	}
	return b.String(), nil
}
