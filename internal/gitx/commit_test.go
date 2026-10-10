package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// identify gives a clone an author of its own, so commits made through Git
// do not depend on the machine's configuration.
func identify(t *testing.T, dir string) {
	t.Helper()
	sh(t, dir, "config", "user.name", "test")
	sh(t, dir, "config", "user.email", "test@example.com")
}

func write(t *testing.T, dir, file, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCommitVersionedLeavesUnversionedFilesAlone: a change is committed as
// it is on disk whether it was staged or not, and a file git does not track
// stays out of the commit and on disk.
func TestCommitVersionedLeavesUnversionedFilesAlone(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "b.txt", "second")
	write(t, clone, "a.txt", "changed\n")
	write(t, clone, "b.txt", "staged\n")
	sh(t, clone, "add", "b.txt")
	write(t, clone, "b.txt", "staged, then changed again\n")
	write(t, clone, "new.txt", "new\n")
	if v, u := g.EditCounts(clone); v != 2 || u != 1 {
		t.Fatalf("counts = %d/%d, want 2/1", v, u)
	}

	sha, err := g.CommitVersioned(clone, "Change both")
	if err != nil || sha == "" {
		t.Fatalf("commit = %q, %v", sha, err)
	}
	if got := sh(t, clone, "show", "--name-only", "--format=", "HEAD"); got != "a.txt\nb.txt" {
		t.Errorf("committed %q", got)
	}
	if got := sh(t, clone, "show", "HEAD:b.txt"); got != "staged, then changed again" {
		t.Errorf("b.txt was committed as %q, not as it is on disk", got)
	}
	if v, u := g.EditCounts(clone); v != 0 || u != 1 {
		t.Errorf("after the commit: %d/%d, want 0/1", v, u)
	}

	// Nothing versioned left: no commit, and no error either.
	if sha, err := g.CommitVersioned(clone, "Nothing"); sha != "" || err != nil {
		t.Errorf("an empty commit: %q, %v", sha, err)
	}
}

// TestCommitVersionedRefusesAReview: a review worktree's changes are the
// merge request's, and committing them would rewrite what is reviewed.
func TestCommitVersionedRefusesAReview(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	sh(t, clone, "config", "unagit.mr.mode", "review")
	write(t, clone, "a.txt", "changed\n")
	if _, err := New("", nil).CommitVersioned(clone, "No"); err == nil || !strings.Contains(err.Error(), "review") {
		t.Errorf("a review worktree was committed: %v", err)
	}
}

// TestRewordCommit: the newest commit is amended without what the index
// holds; an older one is written again, keeping its author and what it
// changed, and the commits after it follow with their edits untouched.
func TestRewordCommit(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	write(t, clone, "one.txt", "one\n")
	sh(t, clone, "add", "one.txt")
	sh(t, clone, "commit", "-q", "-m", "one", "--author", "someone else <else@example.com>")
	commit(t, clone, "two.txt", "two")
	write(t, clone, "a.txt", "staged\n")
	sh(t, clone, "add", "a.txt")

	head := sh(t, clone, "rev-parse", "HEAD")
	if _, err := g.RewordCommit(clone, head, "Two, better said"); err != nil {
		t.Fatal(err)
	}
	if got := sh(t, clone, "log", "-1", "--format=%s"); got != "Two, better said" {
		t.Errorf("subject = %q", got)
	}
	if got := sh(t, clone, "show", "--name-only", "--format=", "HEAD"); got != "two.txt" {
		t.Errorf("the amend took in what was staged: %q", got)
	}

	one := sh(t, clone, "rev-parse", "HEAD~1")
	rewritten, err := g.RewordCommit(clone, one, "One, with a body\n\nWhy it was done.")
	if err != nil {
		t.Fatal(err)
	}
	if got := sh(t, clone, "rev-parse", "HEAD~1"); got != rewritten {
		t.Errorf("HEAD~1 = %s, want the rewritten %s", got, rewritten)
	}
	if got := sh(t, clone, "log", "-1", "--format=%B", "HEAD~1"); got != "One, with a body\n\nWhy it was done." {
		t.Errorf("message = %q", got)
	}
	if got := sh(t, clone, "log", "-1", "--format=%an", "HEAD~1"); got != "someone else" {
		t.Errorf("author = %q", got)
	}
	if got := sh(t, clone, "log", "-1", "--format=%s", "HEAD"); got != "Two, better said" {
		t.Errorf("the commit after it = %q", got)
	}
	// Staged or not is the index's, which unagit does not keep; the edit
	// itself must survive the replay.
	if got := sh(t, clone, "status", "--porcelain"); !strings.HasSuffix(got, "M a.txt") && got != "M  a.txt" {
		t.Errorf("the edits around the replay: %q", got)
	}
	if got := sh(t, clone, "log", "--format=%s", "origin/main..HEAD"); got != "Two, better said\nOne, with a body" {
		t.Errorf("history = %q", got)
	}
}

// TestLocalCommits are those no remote branch has.
func TestLocalCommits(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	pushed := sh(t, clone, "rev-parse", "HEAD")
	commit(t, clone, "l.txt", "local")
	local := g.LocalCommits(clone)
	if !local[sh(t, clone, "rev-parse", "HEAD")] || local[pushed] || len(local) != 1 {
		t.Errorf("local = %v", local)
	}
	if tip, err := g.UpstreamTip(clone); err != nil || tip != pushed {
		t.Errorf("upstream tip = %q, %v; want %s", tip, err, pushed)
	}
}

// TestUpstreamOnly: in step, origin has nothing HEAD lacks; once a pushed
// commit is rewritten, origin's copy of it is what only origin has.
func TestUpstreamOnly(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "b.txt", "two")
	sh(t, clone, "push", "-q", "origin", "HEAD")
	name, theirs, err := g.UpstreamOnly(clone, 50)
	must(t, err)
	if name != "origin/main" || len(theirs) != 0 {
		t.Fatalf("in step: %q, %v", name, theirs)
	}
	pushed := sh(t, clone, "rev-parse", "HEAD")
	sh(t, clone, "commit", "-q", "--amend", "-m", "two, rewritten")
	_, theirs, err = g.UpstreamOnly(clone, 50)
	must(t, err)
	if len(theirs) != 1 || theirs[0].SHA != pushed {
		t.Errorf("only on origin: %v, want %s", theirs, pushed)
	}
	sh(t, clone, "checkout", "-q", "-b", "alone")
	if name, theirs, err := g.UpstreamOnly(clone, 50); name != "" || theirs != nil || err != nil {
		t.Errorf("a branch with no upstream: %q, %v, %v", name, theirs, err)
	}
}

// TestOutgoingAndUndoCommit: a push would send the commits the upstream
// lacks; undoing the newest leaves its changes on disk, not committed.
func TestOutgoingAndUndoCommit(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	commit(t, clone, "b.txt", "two")
	commit(t, clone, "c.txt", "three")
	out, err := g.Outgoing(clone)
	must(t, err)
	if len(out) != 2 || !strings.HasSuffix(out[0], " three") || !strings.HasSuffix(out[1], " two") {
		t.Errorf("outgoing = %q", out)
	}
	must(t, g.UndoCommit(clone))
	if got := sh(t, clone, "log", "-1", "--format=%s"); got != "two" {
		t.Errorf("HEAD is %q after the undo", got)
	}
	if got := sh(t, clone, "status", "--porcelain"); got != "A  c.txt" {
		t.Errorf("the undone commit's changes: %q", got)
	}
	sh(t, clone, "reset", "-q", "--hard", "HEAD~1")
	if err := g.UndoCommit(clone); err == nil {
		t.Error("the first commit was undone")
	}
}

// TestSquashCommits: the middle three of five become one commit with the
// message given and the oldest's author; the tree is what it was, the two
// after it keep their own messages, and an uncommitted edit survives.
func TestSquashCommits(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	write(t, clone, "2.txt", "two\n")
	sh(t, clone, "add", "2.txt")
	sh(t, clone, "commit", "-q", "-m", "two", "--author", "someone else <else@example.com>")
	commit(t, clone, "3.txt", "three")
	commit(t, clone, "4.txt", "four")
	commit(t, clone, "5.txt", "five")
	write(t, clone, "a.txt", "an edit\n")
	tree := sh(t, clone, "rev-parse", "HEAD^{tree}")

	squashed, err := g.SquashCommits(clone, sh(t, clone, "rev-parse", "HEAD~3"), sh(t, clone, "rev-parse", "HEAD~1"), "Two to four\n\nAll at once.")
	must(t, err)
	if got := sh(t, clone, "log", "--format=%s", "origin/main..HEAD"); got != "five\nTwo to four\none" {
		t.Errorf("history = %q", got)
	}
	if got := sh(t, clone, "rev-parse", "HEAD~1"); got != squashed {
		t.Errorf("HEAD~1 = %s, want the squashed %s", got, squashed)
	}
	if got := sh(t, clone, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Error("the tree changed")
	}
	if got := sh(t, clone, "log", "-1", "--format=%an|%B", squashed); got != "someone else|Two to four\n\nAll at once." {
		t.Errorf("author and message = %q", got)
	}
	if got := sh(t, clone, "show", "--name-only", "--format=", squashed); got != "2.txt\n3.txt\n4.txt" {
		t.Errorf("the squashed commit changed %q", got)
	}
	if got := sh(t, clone, "status", "--porcelain"); got != "M a.txt" {
		t.Errorf("the edit around the replay: %q", got)
	}

	// The newest two: the branch moves, nothing is replayed.
	if _, err := g.SquashCommits(clone, "HEAD~1", "HEAD", "Two to five"); err != nil {
		t.Fatal(err)
	}
	if got := sh(t, clone, "log", "--format=%s", "origin/main..HEAD"); got != "Two to five\none" {
		t.Errorf("history = %q", got)
	}
	if got := sh(t, clone, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Error("the tree changed")
	}
}

// TestSquashRefusesAMerge: a merge among the commits leaves the branch alone.
func TestSquashRefusesAMerge(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	sh(t, clone, "checkout", "-q", "-b", "side")
	commit(t, clone, "s.txt", "side")
	sh(t, clone, "checkout", "-q", "main")
	commit(t, clone, "2.txt", "two")
	sh(t, clone, "merge", "-q", "--no-edit", "side")
	commit(t, clone, "3.txt", "three")
	head := sh(t, clone, "rev-parse", "HEAD")
	if _, err := g.SquashCommits(clone, sh(t, clone, "rev-parse", "HEAD~2"), head, "all"); err == nil || !strings.Contains(err.Error(), "merge") {
		t.Errorf("a merge was squashed: %v", err)
	}
	if sh(t, clone, "rev-parse", "HEAD") != head {
		t.Error("the branch moved")
	}
}

// TestRewritingNotesWhereOriginStood: a rewrite of pushed commits notes
// origin's copy as the lease; one of commits not pushed notes nothing.
func TestRewritingNotesWhereOriginStood(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	_, err := g.Rewriting(clone, RewriteChange{Kind: RewriteUndoCommit}, func() error { return g.UndoCommit(clone) })
	must(t, err)
	if mark := g.RebasedFrom(clone)["main"]; mark != "" {
		t.Errorf("a commit not pushed noted %q", mark)
	}
	sh(t, clone, "commit", "-q", "-m", "one")
	sh(t, clone, "push", "-q")
	pushed := sh(t, clone, "rev-parse", "HEAD")
	_, err = g.Rewriting(clone, RewriteChange{Kind: RewriteReword}, func() error { _, err := g.RewordCommit(clone, pushed, "One"); return err })
	must(t, err)
	if mark := g.RebasedFrom(clone)["main"]; mark != pushed {
		t.Errorf("noted %q, origin had %s", mark, pushed)
	}
}
