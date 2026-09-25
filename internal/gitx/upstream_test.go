package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(message+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, dir, "add", ".")
	sh(t, dir, "commit", "-q", "-m", message)
}

// repos is a bare origin with a main branch, and a clone of it.
func repos(t *testing.T) (origin, clone string) {
	t.Helper()
	base := t.TempDir()
	origin = filepath.Join(base, "origin.git")
	clone = filepath.Join(base, "clone")
	sh(t, base, "init", "-q", "--bare", "--initial-branch=main", origin)
	sh(t, base, "clone", "-q", origin, clone)
	commit(t, clone, "a.txt", "initial")
	sh(t, clone, "branch", "-M", "main")
	sh(t, clone, "push", "-q", "-u", "origin", "main")
	return origin, clone
}

func TestBranchUpstreamsSaysWhereEveryBranchStands(t *testing.T) {
	origin, clone := repos(t)
	g := New("", nil)

	// in sync
	sh(t, clone, "checkout", "-q", "-b", "in-sync")
	commit(t, clone, "s.txt", "in sync")
	sh(t, clone, "push", "-q", "-u", "origin", "in-sync")
	// ahead by two
	sh(t, clone, "checkout", "-q", "-b", "ahead")
	commit(t, clone, "a1.txt", "one")
	sh(t, clone, "push", "-q", "-u", "origin", "ahead")
	commit(t, clone, "a2.txt", "two")
	commit(t, clone, "a3.txt", "three")
	// no upstream at all
	sh(t, clone, "checkout", "-q", "-b", "local/only")
	commit(t, clone, "l.txt", "local")
	// behind: another clone pushes to the branch
	sh(t, clone, "checkout", "-q", "-b", "behind")
	commit(t, clone, "b.txt", "b")
	sh(t, clone, "push", "-q", "-u", "origin", "behind")
	other := filepath.Join(t.TempDir(), "other")
	sh(t, filepath.Dir(other), "clone", "-q", "-b", "behind", origin, other)
	commit(t, other, "b2.txt", "elsewhere")
	sh(t, other, "push", "-q", "origin", "behind")
	// diverged: local commit on top of the old head, origin moved too
	sh(t, clone, "checkout", "-q", "behind")
	commit(t, clone, "b3.txt", "mine")
	// gone: the branch is deleted on origin and pruned
	sh(t, clone, "checkout", "-q", "-b", "gone")
	commit(t, clone, "g.txt", "g")
	sh(t, clone, "push", "-q", "-u", "origin", "gone")
	sh(t, other, "push", "-q", "origin", "--delete", "gone")
	sh(t, clone, "fetch", "-q", "--prune")

	got := g.BranchUpstreams(clone)
	want := map[string]Upstream{
		"main":       {Name: "origin/main"},
		"in-sync":    {Name: "origin/in-sync"},
		"ahead":      {Name: "origin/ahead", Ahead: 2},
		"local/only": {},
		"behind":     {Name: "origin/behind", Ahead: 1, Behind: 1},
		"gone":       {Name: "origin/gone", Gone: true},
	}
	for branch, w := range want {
		if got[branch] != w {
			t.Errorf("%s = %+v, want %+v", branch, got[branch], w)
		}
	}
}

func TestBranchUpstreamsOfSomethingThatIsNotARepo(t *testing.T) {
	if got := New("", nil).BranchUpstreams(t.TempDir()); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestPushSetsTheUpstreamThenSendsMore(t *testing.T) {
	origin, clone := repos(t)
	g := New("", nil)
	sh(t, clone, "checkout", "-q", "-b", "feat/x")
	commit(t, clone, "x.txt", "first")

	if err := g.Push(clone, "feat/x", true); err != nil {
		t.Fatal(err)
	}
	if got := g.BranchUpstreams(clone)["feat/x"]; got != (Upstream{Name: "origin/feat/x"}) {
		t.Errorf("after the first push: %+v", got)
	}
	if got := sh(t, origin, "rev-parse", "feat/x"); got != sh(t, clone, "rev-parse", "HEAD") {
		t.Errorf("origin has %s", got)
	}
	commit(t, clone, "x2.txt", "second")
	if err := g.Push(clone, "feat/x", false); err != nil {
		t.Fatal(err)
	}
	if got := g.BranchUpstreams(clone)["feat/x"]; got.Ahead != 0 || got.Behind != 0 {
		t.Errorf("after the second push: %+v", got)
	}
}

func TestPushNeverForces(t *testing.T) {
	origin, clone := repos(t)
	g := New("", nil)
	sh(t, clone, "checkout", "-q", "-b", "feat/x")
	commit(t, clone, "x.txt", "first")
	sh(t, clone, "push", "-q", "-u", "origin", "feat/x")
	other := filepath.Join(t.TempDir(), "other")
	sh(t, filepath.Dir(other), "clone", "-q", "-b", "feat/x", origin, other)
	commit(t, other, "o.txt", "theirs")
	sh(t, other, "push", "-q", "origin", "feat/x")
	commit(t, clone, "c.txt", "mine")

	if err := g.Push(clone, "feat/x", false); err == nil {
		t.Fatal("pushing a branch origin has moved past must be refused")
	}
	if got := sh(t, origin, "rev-parse", "feat/x"); got != sh(t, other, "rev-parse", "HEAD") {
		t.Error("origin's branch was overwritten")
	}
}

func TestCommitsAheadListsSubjectsAndBodiesOldestFirst(t *testing.T) {
	_, clone := repos(t)
	sh(t, clone, "checkout", "-q", "-b", "feat/x")
	commit(t, clone, "1.txt", "First change")
	if err := os.WriteFile(filepath.Join(clone, "2.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, clone, "add", ".")
	sh(t, clone, "commit", "-q", "-m", "Second change", "-m", "It has a body.\n\nWith two paragraphs.")

	got, err := New("", nil).CommitsAhead(clone, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Subject != "First change" || got[0].Body != "" ||
		got[1].Subject != "Second change" || got[1].Body != "It has a body.\n\nWith two paragraphs." {
		t.Errorf("commits = %+v", got)
	}
	if _, err := New("", nil).CommitsAhead(clone, "origin/nope"); err == nil {
		t.Error("an unknown base should be an error, so the caller can leave the defaults empty")
	}
}
