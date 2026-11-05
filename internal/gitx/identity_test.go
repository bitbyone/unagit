package gitx

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// committer is who git would say made a commit in dir: the identity as git
// resolves it, local over global.
func committer(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "var", "GIT_COMMITTER_IDENT").Output()
	if err != nil {
		t.Fatal(err)
	}
	ident := string(out)
	return strings.TrimSpace(ident[:strings.Index(ident, ">")+1])
}

// TestAnIdentityReachesEveryWorktree: the clone's own user.name and
// user.email win over the global ones in the clone and in its worktrees,
// and taking the identity away gives the global one back - but only what
// unagit wrote is taken.
func TestAnIdentityReachesEveryWorktree(t *testing.T) {
	t.Parallel()
	_, clone := repos(t)
	g := New("", nil)
	global := committer(t, clone)
	wt := filepath.Join(filepath.Dir(clone), "wt")
	sh(t, clone, "worktree", "add", "-q", "-b", "feat", wt)
	sh(t, clone, "config", "extensions.worktreeConfig", "true")

	if changed, err := g.SetIdentity(clone, "Jan Work", "jan@work.example"); err != nil || !changed {
		t.Fatalf("set: %v %v", changed, err)
	}
	for _, dir := range []string{clone, wt} {
		if got := committer(t, dir); got != "Jan Work <jan@work.example>" {
			t.Errorf("%s commits as %q", filepath.Base(dir), got)
		}
	}
	if changed, _ := g.SetIdentity(clone, "Jan Work", "jan@work.example"); changed {
		t.Error("the same identity again changed something")
	}

	// Only the address set: the name falls back to the global one.
	if _, err := g.SetIdentity(wt, "", "jan@work.example"); err != nil {
		t.Fatal(err)
	}
	if name, email, _ := g.LocalIdentity(clone); name != "" || email != "jan@work.example" {
		t.Errorf("the clone has %q %q", name, email)
	}

	if _, err := g.SetIdentity(clone, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := committer(t, wt); got != global {
		t.Errorf("with no identity the worktree commits as %q, want the global %q", got, global)
	}

	// A clone's own identity, set by hand, is not unagit's to take away...
	sh(t, clone, "config", "user.email", "mine@example.com")
	if changed, _ := g.SetIdentity(clone, "", ""); changed {
		t.Error("nothing to set took away the clone's own address")
	}
	// ...but one set in Settings writes over it.
	if _, err := g.SetIdentity(clone, "", "set@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, email, unagits := g.LocalIdentity(clone); email != "set@example.com" || !unagits {
		t.Errorf("the clone has %q, unagit's: %v", email, unagits)
	}
}
