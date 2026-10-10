package gitx

import (
	"strings"
	"testing"
)

// squashTwo squashes the newest two commits of clone, written down.
func squashTwo(t *testing.T, g *Git, clone string) Rewrite {
	t.Helper()
	r, err := g.Rewriting(clone, RewriteChange{Kind: RewriteSquash, What: "squashed 2 commits"}, func() error {
		_, err := g.SquashCommits(clone, "HEAD~1", "HEAD", "both")
		return err
	})
	must(t, err)
	if r.ID == "" {
		t.Fatal("the squash was not written down")
	}
	return r
}

// TestUndoASquash: the squash is written down; undone, the two commits are
// back and nothing on disk moved, an edit included; the undo is written
// down too, and undoing it brings the squash back.
func TestUndoASquash(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	commit(t, clone, "2.txt", "two")
	before := sh(t, clone, "rev-parse", "HEAD")
	write(t, clone, "a.txt", "an edit\n")
	r := squashTwo(t, g, clone)
	if r.Before != before || r.After != sh(t, clone, "rev-parse", "HEAD") || r.Branch != "main" || r.Files {
		t.Fatalf("written down as %+v", r)
	}
	if got := sh(t, clone, "for-each-ref", "--format=%(objectname)", "refs/unagit/rewrites/"+r.ID+"/before"); got != before {
		t.Errorf("no ref holds the state before: %q", got)
	}

	plan, err := g.PlanUndo(clone, r.ID)
	must(t, err)
	if plan.Blocked != "" || plan.To != before || plan.Since != 0 || plan.Files || len(plan.Chain) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	undo, err := g.UndoRewrite(clone, r.ID)
	must(t, err)
	if got := sh(t, clone, "log", "--format=%s", "origin/main..HEAD"); got != "two\none" {
		t.Errorf("history after the undo = %q", got)
	}
	if got := sh(t, clone, "status", "--porcelain"); got != "M a.txt" {
		t.Errorf("what is not committed = %q", got)
	}
	all := g.Rewrites(clone)
	if len(all) != 2 || all[0].ID != undo.ID || all[0].Kind != RewriteUndo || !Undone(all, all[1]) {
		t.Fatalf("record = %+v", all)
	}
	if _, err := g.UndoRewrite(clone, r.ID); err != ErrUndone {
		t.Errorf("undoing it twice: %v", err)
	}

	// Undoing the undo is the squash again.
	_, err = g.UndoRewrite(clone, undo.ID)
	must(t, err)
	if got := sh(t, clone, "log", "--format=%s", "origin/main..HEAD"); got != "both" {
		t.Errorf("history after undoing the undo = %q", got)
	}
	if all := g.Rewrites(clone); Undone(all, all[len(all)-1]) {
		t.Error("the squash still reads as undone")
	}
}

// TestUndoTakesTheLaterRewritesAlong: undoing the first of two rewrites of
// a branch undoes both, and says how many commits made since leave too.
func TestUndoTakesTheLaterRewritesAlong(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	commit(t, clone, "2.txt", "two")
	before := sh(t, clone, "rev-parse", "HEAD")
	first := squashTwo(t, g, clone)
	_, err := g.Rewriting(clone, RewriteChange{Kind: RewriteReword, What: "edited"}, func() error {
		_, err := g.RewordCommit(clone, "HEAD", "Both, said better")
		return err
	})
	must(t, err)
	commit(t, clone, "3.txt", "three")

	plan, err := g.PlanUndo(clone, first.ID)
	must(t, err)
	if len(plan.Chain) != 2 || plan.Since != 1 || plan.To != before {
		t.Fatalf("plan = %+v", plan)
	}
	_, err = g.UndoRewrite(clone, first.ID)
	must(t, err)
	if got := sh(t, clone, "rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	// The commit made since is not committed any more, but it is on disk.
	if got := sh(t, clone, "status", "--porcelain"); got != "A  3.txt" {
		t.Errorf("what is not committed = %q", got)
	}
}

// TestUndoARebasePutsTheFilesBack: a rebase changed the files, so its undo
// puts them back, an edit elsewhere kept.
func TestUndoARebasePutsTheFilesBack(t *testing.T) {
	t.Parallel()
	origin, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	other := t.TempDir() + "/other"
	sh(t, t.TempDir(), "clone", "-q", origin, other)
	identify(t, other)
	commit(t, other, "theirs.txt", "theirs")
	sh(t, other, "push", "-q")
	commit(t, clone, "mine.txt", "mine")
	before := sh(t, clone, "rev-parse", "HEAD")
	sh(t, clone, "fetch", "-q")
	write(t, clone, "a.txt", "an edit\n")
	r, err := g.Rewriting(clone, RewriteChange{Kind: RewriteRebase, What: "rebased", Files: true}, func() error {
		_, err := g.Run(clone, "rebase", "--autostash", "origin/main")
		return err
	})
	must(t, err)
	if r.ID == "" || !r.Files {
		t.Fatalf("written down as %+v", r)
	}
	_, err = g.UndoRewrite(clone, r.ID)
	must(t, err)
	if got := sh(t, clone, "rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want %s", got, before)
	}
	if got := sh(t, clone, "status", "--porcelain"); got != "M a.txt" {
		t.Errorf("after the undo: %q, want the edit alone", got)
	}
}

// TestARebaseThatOnlyFastForwardsIsNotWrittenDown: nothing was rewritten.
func TestARebaseThatOnlyFastForwardsIsNotWrittenDown(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	sh(t, clone, "branch", "behind", "HEAD~1")
	sh(t, clone, "checkout", "-q", "behind")
	r, err := g.Rewriting(clone, RewriteChange{Kind: RewriteRebase}, func() error {
		_, err := g.Run(clone, "rebase", "main")
		return err
	})
	must(t, err)
	if r.ID != "" || len(g.Rewrites(clone)) != 0 {
		t.Errorf("a fast-forward was written down: %+v", g.Rewrites(clone))
	}
}

// TestUndoAnUndoneCommit: the commit comes back, its changes committed again.
func TestUndoAnUndoneCommit(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	head := sh(t, clone, "rev-parse", "HEAD")
	r, err := g.Rewriting(clone, RewriteChange{Kind: RewriteUndoCommit}, func() error { return g.UndoCommit(clone) })
	must(t, err)
	_, err = g.UndoRewrite(clone, r.ID)
	must(t, err)
	if got := sh(t, clone, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want %s", got, head)
	}
	if got := sh(t, clone, "status", "--porcelain"); got != "" {
		t.Errorf("not committed after the undo: %q", got)
	}
}

// TestUndoADeletion: the branch is made again where it was, tracking what
// it tracked; one of the same name made since blocks the undo.
func TestUndoADeletion(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	sh(t, clone, "checkout", "-q", "-b", "feat/x")
	commit(t, clone, "x.txt", "x")
	sh(t, clone, "push", "-q", "-u", "origin", "feat/x")
	tip := sh(t, clone, "rev-parse", "HEAD")
	sh(t, clone, "checkout", "-q", "main")
	must(t, g.DeleteLocalBranch(clone, "feat/x"))
	r := g.Rewrites(clone)[0]
	if r.Kind != RewriteDelete || r.Before != tip || r.After != "" {
		t.Fatalf("written down as %+v", r)
	}
	_, err := g.UndoRewrite(clone, r.ID)
	must(t, err)
	if got := sh(t, clone, "rev-parse", "feat/x"); got != tip {
		t.Errorf("feat/x = %s, want %s", got, tip)
	}
	if got := sh(t, clone, "rev-parse", "--abbrev-ref", "feat/x@{upstream}"); got != "origin/feat/x" {
		t.Errorf("upstream = %q", got)
	}

	must(t, g.DeleteLocalBranch(clone, "feat/x"))
	sh(t, clone, "branch", "feat/x", "main")
	plan, err := g.PlanUndo(clone, g.Rewrites(clone)[0].ID)
	must(t, err)
	if !strings.Contains(plan.Blocked, "is here again") {
		t.Errorf("plan = %+v", plan)
	}
}

// TestUndoAfterAForcePushAsksForAnother: once origin has the squash, the
// undo moves the branch off it, so a force push is needed and its lease is
// noted.
func TestUndoAfterAForcePushAsksForAnother(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	identify(t, clone)
	g := New("", nil)
	commit(t, clone, "1.txt", "one")
	commit(t, clone, "2.txt", "two")
	sh(t, clone, "push", "-q")
	r := squashTwo(t, g, clone)
	must(t, g.ForcePush(clone, "main", r.Before))
	squashed := sh(t, clone, "rev-parse", "HEAD")
	plan, err := g.PlanUndo(clone, r.ID)
	must(t, err)
	if !plan.Force {
		t.Error("the plan does not say a force push will be needed")
	}
	_, err = g.UndoRewrite(clone, r.ID)
	must(t, err)
	if mark := g.RebasedFrom(clone)["main"]; mark != squashed {
		t.Errorf("lease noted %q, origin has %s", mark, squashed)
	}
}
