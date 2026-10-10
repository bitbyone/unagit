package gitx

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The shelf is IntelliJ's name for changes put aside to come back to: git's
// stash, read as a list of named things. It is the repository's, shared by
// its worktrees, and what lazygit or a terminal stashed is on it too. A
// shelf is shelved from what is not committed, versioned and unversioned,
// and unshelved only where it goes in cleanly; one deleted is written down
// (Rewrite History), so it can be put back.

// Shelf is one entry of the shelf.
type Shelf struct {
	SHA string
	// Ref is where git has it now, stash@{n}: it moves as others come.
	Ref string
	At  time.Time
	// Name is what it was shelved as; Branch where, "" when git did not say.
	Name, Branch string
	// Message is git's whole line for it, "On main: name".
	Message string
}

// Shelves is the shelf, newest first.
func (g *Git) Shelves(dir string) ([]Shelf, error) {
	out, err := g.out(dir, "stash", "list", "--format=%H%x1f%gd%x1f%ct%x1f%gs")
	if err != nil || out == "" {
		return nil, err
	}
	var all []Shelf
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x1f", 4)
		if len(f) < 4 {
			continue
		}
		unix, _ := strconv.ParseInt(f[2], 10, 64)
		s := Shelf{SHA: f[0], Ref: f[1], At: time.Unix(unix, 0), Message: f[3], Name: f[3]}
		// "On main: name" from a push with a message, "WIP on main: abc
		// subject" from one without.
		for _, prefix := range []string{"On ", "WIP on "} {
			if rest, ok := strings.CutPrefix(f[3], prefix); ok {
				if branch, name, ok := strings.Cut(rest, ": "); ok {
					s.Branch, s.Name = branch, name
				}
				break
			}
		}
		all = append(all, s)
	}
	return all, nil
}

// ErrNothingToShelve marks a shelve with nothing to put aside.
var ErrNothingToShelve = errors.New("nothing to shelve: every file is as the last commit has it")

// Shelve puts what is not committed aside under name - only paths when
// some are given, unversioned files among them - leaving the files as the
// last commit has them.
func (g *Git) Shelve(dir, name string, paths []string) error {
	if mode, _ := g.out(dir, "config", "--get", "unagit.mr.mode"); mode == "review" {
		return fmt.Errorf("this is a review worktree: its changes are the merge request's, not yours to shelve")
	}
	before, _ := g.Shelves(dir)
	args := []string{"stash", "push", "--include-untracked", "--quiet", "--message", name}
	if len(paths) > 0 {
		args = append(append(args, "--"), paths...)
	}
	if _, err := g.Run(dir, args...); err != nil {
		return err
	}
	if after, _ := g.Shelves(dir); len(after) == len(before) {
		return ErrNothingToShelve
	}
	return nil
}

// ShelfFiles is what a shelf holds, each file with its lines added and
// deleted, unversioned ones too.
func (g *Git) ShelfFiles(dir, sha string) ([]FileStat, error) {
	out, err := g.out(dir, "stash", "show", "--numstat", "--include-untracked", sha)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

// parseNumstat reads git's --numstat: added, deleted, path; a binary
// file's counts are dashes.
func parseNumstat(out string) []FileStat {
	var files []FileStat
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		st := FileStat{Path: f[2], Binary: f[0] == "-"}
		st.Added, _ = strconv.Atoi(f[0])
		st.Deleted, _ = strconv.Atoi(f[1])
		files = append(files, st)
	}
	return files
}

// ShelfPatch is a shelf as a patch, for Hunk.
func (g *Git) ShelfPatch(dir, sha string) (string, error) {
	return g.Run(dir, "stash", "show", "--patch", "--include-untracked", sha)
}

// UnshelveBlocked says why a shelf cannot go into the checkout in dir as
// it stands, "" when it can: a file it holds that is not committed here
// either, an unversioned one it would write over, or a change that would
// conflict with what the branch has now, as git forecasts it in memory.
func (g *Git) UnshelveBlocked(dir, sha string) string {
	files, err := g.ShelfFiles(dir, sha)
	if err != nil {
		return err.Error()
	}
	dirty := map[string]bool{}
	if out, err := g.Run(dir, "status", "--porcelain", "-z", "--untracked-files=all"); err == nil {
		for _, e := range strings.Split(out, "\x00") {
			if len(e) > 3 {
				dirty[e[3:]] = true
			}
		}
	}
	var clash []string
	for _, f := range files {
		if dirty[f.Path] {
			clash = append(clash, f.Path)
		}
	}
	if len(clash) > 0 {
		return "not committed here either: " + strings.Join(clash, ", ") + " - commit or shelve them first"
	}
	conflicts, ok := g.mergeConflicts(dir, sha+"^1", "HEAD", sha)
	if ok && len(conflicts) > 0 {
		return "it would conflict with what the branch has now in " + strings.Join(conflicts, ", ")
	}
	return ""
}

// mergeConflicts forecasts the files merging theirs into ours would stop
// at, from base or, "", from where the two parted; ok false when git
// cannot tell.
func (g *Git) mergeConflicts(dir, base, ours, theirs string) ([]string, bool) {
	args := []string{"merge-tree", "--write-tree", "--name-only", "--no-messages"}
	if base != "" {
		args = append(args, "--merge-base="+base)
	}
	out, _ := g.out(dir, append(args, ours, theirs)...)
	lines := strings.Split(out, "\n")
	if len(lines) == 0 || len(lines[0]) < 40 || strings.ContainsAny(lines[0], " :") {
		return nil, false
	}
	var files []string
	for _, f := range lines[1:] {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	return files, true
}

// Unshelve puts a shelf's changes into the checkout in dir, and takes it
// off the shelf unless keep. Refused, it changes nothing.
func (g *Git) Unshelve(dir, sha string, keep bool) error {
	if why := g.UnshelveBlocked(dir, sha); why != "" {
		return errors.New(why)
	}
	if _, err := g.Run(dir, "stash", "apply", "--quiet", sha); err != nil {
		return err
	}
	if keep {
		return nil
	}
	ref, err := g.shelfRef(dir, sha)
	if err != nil {
		return err
	}
	_, err = g.Run(dir, "stash", "drop", "--quiet", ref)
	return err
}

// DeleteShelf takes a shelf off the shelf, written down so that it can be
// put back.
func (g *Git) DeleteShelf(dir, sha string) (Rewrite, error) {
	all, _ := g.Shelves(dir)
	at := slices.IndexFunc(all, func(s Shelf) bool { return s.SHA == sha })
	if at < 0 {
		return Rewrite{}, fmt.Errorf("the shelf no longer has %s", shortID(sha))
	}
	s := all[at]
	if _, err := g.Run(dir, "stash", "drop", "--quiet", s.Ref); err != nil {
		return Rewrite{}, err
	}
	return g.record(dir, Rewrite{Kind: RewriteDropShelf, Dir: dir, What: "deleted shelf " + s.Name,
		Before: sha, Shelf: s.Message})
}

// shelfRef is where git has a shelf now.
func (g *Git) shelfRef(dir, sha string) (string, error) {
	all, _ := g.Shelves(dir)
	for _, s := range all {
		if s.SHA == sha {
			return s.Ref, nil
		}
	}
	return "", fmt.Errorf("the shelf no longer has %s", shortID(sha))
}
