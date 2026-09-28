package workspace

import (
	"path/filepath"
	"strings"
	"testing"
)

// pushToMR clones origin, lets change do something on the merge request branch,
// and pushes it back to both the branch and the merge request ref. It returns
// the working clone.
func pushToMR(t *testing.T, origin string, change func(work string)) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	git(t, filepath.Dir(work), "clone", "-q", origin, work)
	git(t, work, "checkout", "-q", "feature/login")
	change(work)
	git(t, work, "push", "-q", "--force", "origin", "feature/login")
	git(t, work, "push", "-q", "--force", "origin", "HEAD:refs/merge-requests/1/head")
	return work
}

func answerComments(t *testing.T, work string) {
	t.Helper()
	write(t, work, "mod.txt", "new, as asked\n")
	git(t, work, "commit", "-qam", "answer the review")
}

// TestReviewFromACommitPendsOnlyWhatFollows: narrowed to the answer to the
// comments, only that answer is pending; the commits before it are HEAD.
func TestReviewFromACommitPendsOnlyWhatFollows(t *testing.T) {
	origin, base, _ := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	mr := reviewMR()
	work := pushToMR(t, origin, func(work string) { answerComments(t, work) })
	head := git(t, work, "rev-parse", "HEAD")
	answer := head
	before := git(t, work, "rev-parse", "HEAD^")

	dir, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head, From: answer})
	if err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD = %s, want the commit before the chosen one %s", got, before)
	}
	if pending := git(t, dir, "diff", "--name-status"); pending != "M\tmod.txt" {
		t.Errorf("pending = %q, want only the answer to the comments", pending)
	}
	if staged := git(t, dir, "diff", "--cached", "--name-status"); staged != "" {
		t.Errorf("something is staged, so a gutter would not see it:\n%s", staged)
	}
	meta := m.ReadMeta(dir)
	if meta.Base != before || meta.Head != head || meta.From != answer {
		t.Errorf("meta = %+v, want base %s, head %s, from %s", meta, before, head, answer)
	}

	// Ctrl-R afterwards opens the whole change again and stops calling it narrowed.
	if _, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "rev-parse", "HEAD"); got != base {
		t.Errorf("HEAD = %s, want the merge base %s", got, base)
	}
	if meta := m.ReadMeta(dir); meta.From != "" || meta.Base != base {
		t.Errorf("after the whole review, meta = %+v", meta)
	}
}

// TestReviewFromRefusesACommitThatIsNotTheMergeRequests: the merge base and the
// target's own commits are not the merge request's work.
func TestReviewFromRefusesACommitThatIsNotTheMergeRequests(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	target := git(t, origin, "rev-parse", "main")
	for name, from := range map[string]string{"the merge base": base, "the target's commit": target} {
		if _, err := m.EnsureMRReview(reviewMR(), p, Review{BaseSHA: base, HeadSHA: head, From: from}); err == nil ||
			!strings.Contains(err.Error(), "not part of the merge request") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// TestReviewFromLeavesYourEditsAlone: narrowing resets the worktree, so with
// edits of the reviewer's in it, it refuses instead.
func TestReviewFromLeavesYourEditsAlone(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	dir, err := m.EnsureMRReview(reviewMR(), p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "keep.txt", "my own note\n")
	first := git(t, dir, "rev-list", "--reverse", base+".."+head)
	from := strings.Fields(first)[1]
	if _, err := m.EnsureMRReview(reviewMR(), p, Review{BaseSHA: base, HeadSHA: head, From: from}); err == nil ||
		!strings.Contains(err.Error(), "keep.txt") {
		t.Errorf("err = %v, want a refusal naming the edited file", err)
	}
	if got := readFile(t, dir, "keep.txt"); got != "my own note\n" {
		t.Errorf("keep.txt = %q, the edit was lost", got)
	}
}

// TestMRCommitsMarksWhatCameAfterTheLastReview, also across a rebase, which
// gives every commit a new id: the ones already reviewed must not come back as
// new.
func TestMRCommitsMarksWhatCameAfterTheLastReview(t *testing.T) {
	origin, base, head := newDivergedOrigin(t)
	m, p := newReviewManager(t, origin)
	mr := reviewMR()

	// Before any review nothing has been seen, and nothing is marked either.
	commits, err := m.MRCommits(mr, p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(commits); got != "the change, more" {
		t.Errorf("before a review: %s", got)
	}

	if _, err := m.EnsureMRReview(mr, p, Review{BaseSHA: base, HeadSHA: head}); err != nil {
		t.Fatal(err)
	}
	work := pushToMR(t, origin, func(work string) { answerComments(t, work) })
	head = git(t, work, "rev-parse", "HEAD")
	commits, err = m.MRCommits(mr, p, Review{BaseSHA: base, HeadSHA: head})
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(commits); got != "the change, more, answer the review (new)" {
		t.Errorf("after a new commit: %s", got)
	}

	// The author rebases onto the target; still only the answer is new.
	work = pushToMR(t, origin, func(work string) {
		git(t, work, "reset", "-q", "--hard", head)
		git(t, work, "rebase", "-q", "origin/main")
	})
	newBase := git(t, work, "rev-parse", "origin/main")
	newHead := git(t, work, "rev-parse", "HEAD")
	commits, err = m.MRCommits(mr, p, Review{BaseSHA: newBase, HeadSHA: newHead})
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(commits); got != "the change, more, answer the review (new)" {
		t.Errorf("after a rebase: %s", got)
	}
}

func describe(commits []MRCommit) string {
	var parts []string
	for _, c := range commits {
		s := c.Subject
		if c.New {
			s += " (new)"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
