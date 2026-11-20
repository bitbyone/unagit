package incomm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// A grouped worktree holds several repositories in one folder, and an editor
// or an agent opened there keeps one Incomm store for all of them, at the
// folder: a comment on gateway/src/x.go is a comment on src/x.go in the
// gateway repository. A Place says where a merge request's comments are read
// from and how its files are named there.

// Place is a directory holding .incomm/, and the prefix that its comments on
// one repository's files carry: "" for a worktree of its own, "gateway/" in a
// grouped worktree.
type Place struct {
	Dir    string
	Prefix string
}

// ThreadsAt reads the comments of every place that exists. In a group, only
// the threads on the repository's own files are taken, their paths as the
// repository names them; their Dir stays the group's, which is where what
// happens to them is written back.
func ThreadsAt(places ...Place) []Thread {
	var threads []Thread
	seen := map[Place]bool{}
	for _, p := range places {
		if p.Dir == "" || seen[p] {
			continue
		}
		seen[p] = true
		for _, t := range ReadThreads(p.Dir) {
			if p.Prefix != "" {
				rest, ok := strings.CutPrefix(t.File, p.Prefix)
				if !ok {
					continue
				}
				t.File = rest
			}
			threads = append(threads, t)
		}
	}
	return threads
}

// PendingAt counts what waits to be published at a place.
func PendingAt(p Place) int {
	n := 0
	for _, t := range ThreadsAt(p) {
		n += t.PendingCount()
	}
	return n
}

// ImportAt is Import into a place: in a group, every comment lands on the
// repository's files under its folder.
func ImportAt(ctx context.Context, p Place, mr forge.MergeRequest, notes []forge.Note, log func(string)) error {
	if p.Prefix != "" {
		prefixed := make([]forge.Note, len(notes))
		for i, n := range notes {
			if n.Path != "" {
				n.Path = p.Prefix + n.Path
			}
			prefixed[i] = n
		}
		notes = prefixed
	}
	return Import(ctx, p.Dir, mr, notes, log)
}

// ErrNoLine is AddPending's answer when the line is not in the checkout:
// the comment is better kept where it was than put on the wrong code.
var ErrNoLine = errors.New("the line is not in this checkout")

// AddPending puts a comment written in unagit on a line of a place's
// checkout, waiting to be published - for the agent and the forge, and
// from no forge yet, as one written in the editor would be. author names
// who wrote it; empty, Incomm takes git's user.name.
func AddPending(ctx context.Context, p Place, path string, line int, body, author string) error {
	path = p.Prefix + path
	if !filepath.IsLocal(path) {
		return fmt.Errorf("incomm comment has an invalid file path: %q", path)
	}
	ok, err := hasLine(p.Dir, path, line)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoLine
	}
	if err := Prepare(p.Dir); err != nil {
		return err
	}
	c, err := newCLI(ctx, p.Dir)
	if err != nil {
		return err
	}
	args := []string{"add", "--file", path, "--line", strconv.Itoa(line),
		"--content", body, "--author", "user", "--audience", "agent+external"}
	if author != "" {
		args = append(args, "--author-title", author)
	}
	_, err = c.run(viewAgent, args...)
	return err
}

// Prepare gives a directory a store of its own, so that Incomm - which looks
// for .incomm/ upwards from where it starts - keeps its comments there and not
// in some folder above it.
func Prepare(dir string) error {
	return os.MkdirAll(filepath.Join(dir, ".incomm"), 0o755)
}
