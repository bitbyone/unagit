package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobola/unagit/internal/session"
)

// TestPlacesListWhatIsOnDisk: a clone, the review and the branch worktree of
// a merge request, and another worktree, each with what tells it apart - and
// nothing for a repository that is not cloned.
func TestPlacesListWhatIsOnDisk(t *testing.T) {
	t.Parallel()
	cfg := writeTestConfig(t, "http://unused.test")
	off := false
	cfg.Integrations.Chezmoi = &off // the machine's own chezmoi is not this test's
	a := &App{cfg: cfg}
	id := cfg.Instances[0].ID

	clone := a.cloneDir(id, "acme/gateway")
	must(t, os.MkdirAll(clone, 0o755))
	gitIn(t, clone, "init", "-q", "--initial-branch=main")
	commitIn(t, clone, "a.txt", "initial")
	review := a.reviewDir(id, "acme/gateway", 7, "feat/rate")
	branch := a.mrDir(id, "acme/gateway", 7, "feat/rate")
	other := filepath.Join(a.pathManager(id, "acme/gateway").MRRoot("acme/gateway"), "wt-spike")
	gitIn(t, clone, "worktree", "add", "-q", "--detach", review)
	gitIn(t, clone, "worktree", "add", "-q", "-b", "feat/rate", branch)
	gitIn(t, clone, "worktree", "add", "-q", "-b", "spike", other)

	got := map[string]session.Record{}
	for _, p := range Places(cfg) {
		got[p.Dir] = p
	}
	if len(got) != 4 {
		t.Fatalf("places = %+v, want the clone and three worktrees", got)
	}
	for dir, want := range map[string]session.Record{
		clone:  {Project: "acme/gateway", Mode: session.ModeRepository, Title: "main"},
		review: {Project: "acme/gateway", Mode: session.ModeReview, IID: 7, Title: "Rate limiting"},
		branch: {Project: "acme/gateway", Mode: session.ModeBranch, IID: 7, Title: "Rate limiting"},
		other:  {Project: "acme/gateway", Mode: session.ModeBranch, Title: "spike"},
	} {
		p, ok := got[dir]
		if !ok {
			t.Errorf("%s is missing", dir)
			continue
		}
		if p.Project != want.Project || p.Mode != want.Mode || p.IID != want.IID || p.Title != want.Title {
			t.Errorf("%s = %+v, want %+v", dir, p, want)
		}
	}
}
