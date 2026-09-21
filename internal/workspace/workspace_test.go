package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"feature/login":    "feature-login",
		"fix: crash":       "fix--crash",
		"plain":            "plain",
		"-leading-dashes-": "leading-dashes",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Sanitize(strings.Repeat("x", 100)); len(got) != 60 {
		t.Errorf("long branch not truncated: %d", len(got))
	}
}

func TestPaths(t *testing.T) {
	m := New(&config.Config{RootDir: "/root", GitLabURL: "https://gl.example"}, "", nil)
	if got := m.ProjectDir("group/sub/app"); got != filepath.FromSlash("/root/group/sub/app") {
		t.Errorf("ProjectDir = %q", got)
	}
	if got := m.MRRoot("group/app"); got != filepath.FromSlash("/root/group/app.mrs") {
		t.Errorf("MRRoot = %q", got)
	}
	if got := m.MRDir("group/app", 42, "feature/x"); got != filepath.FromSlash("/root/group/app.mrs/42-feature-x") {
		t.Errorf("MRDir = %q", got)
	}
	if got := m.cloneURL("group/app"); got != "https://gl.example/group/app.git" {
		t.Errorf("cloneURL = %q", got)
	}
}

// --- integration against a local bare repository -----------------------------

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newOrigin builds a bare repository with a main branch, a feature branch and
// a GitLab style refs/merge-requests/1/head ref.
func newOrigin(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	bare := filepath.Join(base, "origin.git")

	git(t, base, "init", "--bare", "--initial-branch=main", bare)
	git(t, base, "init", "--initial-branch=main", work)
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "initial")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "origin", "main")

	git(t, work, "checkout", "-b", "feature/login")
	if err := os.WriteFile(filepath.Join(work, "login.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "add login")
	git(t, work, "push", "origin", "feature/login")
	// GitLab exposes every merge request head under this ref.
	git(t, work, "push", "origin", "HEAD:refs/merge-requests/1/head")
	return bare
}

func newManager(t *testing.T, origin string) (*Manager, *config.Config, gitlab.Project) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{RootDir: root, GitLabURL: "https://gl.example", Editor: "true"}
	m := New(cfg, "", func(string) {})
	p := gitlab.Project{
		ID:                1,
		Name:              "app",
		PathWithNamespace: "group/app",
		DefaultBranch:     "main",
		HTTPURLToRepo:     origin,
	}
	return m, cfg, p
}

func TestEnsureProjectClonesThenUpdates(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))

	dir, err := m.EnsureProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if !Exists(dir) {
		t.Fatalf("%s is not a repository", dir)
	}
	if got := m.Git().CurrentBranch(dir); got != "main" {
		t.Errorf("branch = %q, want main", got)
	}

	// A second call must be a no-op update, not a re-clone.
	again, err := m.EnsureProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if again != dir {
		t.Errorf("directory changed: %q -> %q", dir, again)
	}
}

func TestEnsureMRCreatesIndependentWorktree(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	mr := gitlab.MergeRequest{IID: 1, SourceBranch: "feature/login", TargetBranch: "main", SourceProjectID: 1, TargetProjectID: 1}

	wt, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo)
	if err != nil {
		t.Fatal(err)
	}
	if !Exists(wt) {
		t.Fatalf("%s is not a worktree", wt)
	}
	if want := m.MRDir("group/app", 1, "feature/login"); wt != want {
		t.Errorf("worktree at %q, want %q", wt, want)
	}
	if _, err := os.Stat(filepath.Join(wt, "login.go")); err != nil {
		t.Errorf("merge request content missing: %v", err)
	}
	if got := m.Git().CurrentBranch(wt); got != "feature/login" {
		t.Errorf("branch = %q, want feature/login", got)
	}
	// The main clone is created as the shared object store and stays on main.
	main := m.ProjectDir("group/app")
	if !Exists(main) {
		t.Fatal("main clone missing")
	}
	if got := m.Git().CurrentBranch(main); got != "main" {
		t.Errorf("main clone moved to %q", got)
	}

	// Uncommitted changes in the worktree must survive a second open.
	scratch := filepath.Join(wt, "scratch.txt")
	if err := os.WriteFile(scratch, []byte("review notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("local change was lost: %v", err)
	}
}

func TestEnsureMRFromForkUsesMergeRequestRef(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	// Different source project: the source branch does not exist on origin.
	mr := gitlab.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 99, TargetProjectID: 1}

	wt, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Git().CurrentBranch(wt); got != "mr-1-feature-login" {
		t.Errorf("branch = %q, want mr-1-feature-login", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "login.go")); err != nil {
		t.Errorf("merge request content missing: %v", err)
	}
}

func TestEnsureMRWhenBranchIsCheckedOutInMainClone(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	if _, err := m.SwitchBranch(p, "feature/login"); err != nil {
		t.Fatal(err)
	}
	mr := gitlab.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}

	wt, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo)
	if err != nil {
		t.Fatalf("worktree fallback failed: %v", err)
	}
	if got := m.Git().CurrentBranch(wt); got != "unagit-mr-1" {
		t.Errorf("branch = %q, want the unagit-mr-1 fallback", got)
	}
}

func TestSwitchBranch(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	dir, err := m.SwitchBranch(p, "feature/login")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Git().CurrentBranch(dir); got != "feature/login" {
		t.Errorf("branch = %q", got)
	}
	if _, err := m.SwitchBranch(p, "does-not-exist"); err == nil {
		t.Error("switching to a missing branch should fail")
	}
}

func TestSwitchBranchRefusesDirtyTree(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	dir, err := m.EnsureProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SwitchBranch(p, "feature/login"); err == nil {
		t.Fatal("switching with uncommitted changes should fail")
	}
	if got := m.Git().CurrentBranch(dir); got != "main" {
		t.Errorf("branch changed to %q despite the error", got)
	}
}

func TestInspectReportsLocalWork(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	dir, err := m.EnsureProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if r := m.InspectProject("group/app"); len(r.Warnings) != 0 {
		t.Fatalf("clean clone reported %v", r.Warnings)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := m.InspectProject("group/app")
	if len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "uncommitted") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}

func TestRemoveMRKeepsTheMainClone(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	mr := gitlab.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	wt, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveMR("group/app", 1, "feature/login"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree still on disk: %v", err)
	}
	if !Exists(m.ProjectDir("group/app")) {
		t.Error("main clone was removed too")
	}
}

func TestRemoveProjectRemovesWorktreesAndEmptyParents(t *testing.T) {
	m, cfg, p := newManager(t, newOrigin(t))
	mr := gitlab.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	if _, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveProject("group/app"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{m.ProjectDir("group/app"), m.MRRoot("group/app"), filepath.Join(cfg.Root(), "group")} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still exists", dir)
		}
	}
	if _, err := os.Stat(cfg.Root()); err != nil {
		t.Errorf("root directory was removed: %v", err)
	}
}

// TestTokenNeverTouchesDisk guards the central promise of the tool: the token
// is handed to git through the environment of the child process only.
func TestTokenNeverTouchesDisk(t *testing.T) {
	const token = "glpat-super-secret-token-value"
	origin := newOrigin(t)
	root := t.TempDir()
	cfg := &config.Config{RootDir: root, GitLabURL: "https://gl.example", Editor: "true"}
	m := New(cfg, token, func(string) {})
	p := gitlab.Project{ID: 1, PathWithNamespace: "group/app", DefaultBranch: "main", HTTPURLToRepo: origin}

	if _, err := m.EnsureProject(p); err != nil {
		t.Fatal(err)
	}
	mr := gitlab.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	if _, err := m.EnsureMR(mr, p.PathWithNamespace, p.HTTPURLToRepo); err != nil {
		t.Fatal(err)
	}

	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(b), token) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("the token was written to %v", found)
	}
}
