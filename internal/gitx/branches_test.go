package gitx

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBranchHousekeeping: OnlyHere counts what deleting a branch would lose,
// DeleteLocalBranch deletes it unmerged, and ForgetRemoteBranch drops the
// tracking of a branch origin no longer has.
func TestBranchHousekeeping(t *testing.T) {
	origin, clone := repos(t)
	g := New("", nil)

	sh(t, clone, "checkout", "-q", "-b", "pushed")
	commit(t, clone, "p.txt", "on origin")
	sh(t, clone, "push", "-q", "-u", "origin", "pushed")
	sh(t, clone, "checkout", "-q", "-b", "mine")
	commit(t, clone, "m.txt", "here alone")
	commit(t, clone, "n.txt", "here alone too")
	sh(t, clone, "checkout", "-q", "main")

	if n := g.OnlyHere(clone, "pushed"); n != 0 {
		t.Errorf("pushed has %d commits only here", n)
	}
	if n := g.OnlyHere(clone, "mine"); n != 2 {
		t.Errorf("mine has %d commits only here, want 2", n)
	}
	if err := g.DeleteLocalBranch(clone, "mine"); err != nil {
		t.Fatal(err)
	}

	// Deleted on origin by someone else - the API, say.
	sh(t, filepath.Dir(origin), "--git-dir", origin, "branch", "-D", "pushed")
	g.ForgetRemoteBranch(clone, "pushed")
	if refs := sh(t, clone, "branch", "-a"); strings.Contains(refs, "remotes/origin/pushed") || strings.Contains(refs, "mine") {
		t.Errorf("branches left: %s", refs)
	}
	if u := g.BranchUpstreams(clone)["pushed"]; u.Name != "" {
		t.Errorf("pushed still tracks %q", u.Name)
	}
}
