package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
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
	m := New(Options{Root: "/root", GitLabURL: "https://gl.example"}, nil)
	if got := m.ProjectDir("group/sub/app"); got != filepath.FromSlash("/root/group/sub/app") {
		t.Errorf("ProjectDir = %q", got)
	}
	if got := m.MRRoot("group/app"); got != filepath.FromSlash("/root/group/.unagit/app") {
		t.Errorf("MRRoot = %q", got)
	}
	if got := m.MRDir("group/app", 42, "feature/x"); got != filepath.FromSlash("/root/group/.unagit/app/42-feature-x") {
		t.Errorf("MRDir = %q", got)
	}
	if got := m.RemoteURL(forge.Project{PathWithNamespace: "group/app"}); got != "https://gl.example/group/app.git" {
		t.Errorf("RemoteURL = %q", got)
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

func newManager(t *testing.T, origin string) (*Manager, string, forge.Project) {
	t.Helper()
	root := t.TempDir()
	m := New(Options{Root: root, GitLabURL: "https://gl.example"}, func(string) {})
	p := forge.Project{
		ID:                1,
		Name:              "app",
		PathWithNamespace: "group/app",
		DefaultBranch:     "main",
		HTTPURLToRepo:     origin,
	}
	return m, root, p
}

func TestCloneProjectLeavesAnExistingCloneAlone(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))

	dir, err := m.CloneProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if !Exists(dir) {
		t.Fatalf("%s is not a repository", dir)
	}
	if got := m.Git().CurrentBranch(dir); got != "main" {
		t.Errorf("branch = %q, want main", got)
	}

	// A second call must leave the clone as it is, not clone it again.
	again, err := m.CloneProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if again != dir {
		t.Errorf("directory changed: %q -> %q", dir, again)
	}
}

func TestEnsureMRCreatesIndependentWorktree(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", TargetBranch: "main", SourceProjectID: 1, TargetProjectID: 1}

	wt, err := m.EnsureMR(mr, p)
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
	if _, err := m.EnsureMR(mr, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("local change was lost: %v", err)
	}
}

func TestEnsureMRFromForkUsesMergeRequestRef(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	// Different source project: the source branch does not exist on origin.
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 99, TargetProjectID: 1}

	wt, err := m.EnsureMR(mr, p)
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
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}

	wt, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatalf("worktree fallback failed: %v", err)
	}
	if got := m.Git().CurrentBranch(wt); got != "unagit-mr-1" {
		t.Errorf("branch = %q, want the unagit-mr-1 fallback", got)
	}
}

// TestPushFromAFallbackBranchGoesToTheSourceBranch: a merge request worktree
// on unagit-mr-<iid>, because the source branch is out elsewhere, must still
// take a plain git push - git's default refuses one whose upstream has
// another name - and that push must land on the source branch.
func TestPushFromAFallbackBranchGoesToTheSourceBranch(t *testing.T) {
	origin := newOrigin(t)
	m, _, p := newManager(t, origin)
	if _, err := m.SwitchBranch(p, "feature/login"); err != nil {
		t.Fatal(err)
	}
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	wt, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "answer.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, wt, "add", ".")
	git(t, wt, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "answer")
	git(t, wt, "push", "-q")
	if got, want := git(t, origin, "rev-parse", "feature/login"), git(t, wt, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin feature/login = %s, want the pushed %s", got, want)
	}
	if out := git(t, origin, "branch", "--list", "unagit-mr-1"); out != "" {
		t.Errorf("the fallback branch was pushed under its own name: %q", out)
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

// TestSwitchBranchTakesWhatOriginGotSinceTheClone: a branch pushed after the
// clone is fetched, alone, and checked out tracking origin.
func TestSwitchBranchTakesWhatOriginGotSinceTheClone(t *testing.T) {
	origin := newOrigin(t)
	m, _, p := newManager(t, origin)
	dir, err := m.CloneProject(p)
	if err != nil {
		t.Fatal(err)
	}
	git(t, origin, "branch", "late", "main")
	if _, err := m.SwitchBranch(p, "late"); err != nil {
		t.Fatal(err)
	}
	if got := m.Git().CurrentBranch(dir); got != "late" {
		t.Errorf("branch = %q", got)
	}
	if got := git(t, dir, "rev-parse", "--abbrev-ref", "late@{upstream}"); got != "origin/late" {
		t.Errorf("upstream = %q", got)
	}
}

func TestSwitchBranchRefusesDirtyTree(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	dir, err := m.CloneProject(p)
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
	dir, err := m.CloneProject(p)
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
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	wt, err := m.EnsureMR(mr, p)
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
	m, root, p := newManager(t, newOrigin(t))
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	if _, err := m.EnsureMR(mr, p); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveProject("group/app"); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{m.ProjectDir("group/app"), m.MRRoot("group/app"), filepath.Join(root, "group")} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s still exists", dir)
		}
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("root directory was removed: %v", err)
	}
}

// TestTokenNeverTouchesDisk guards the central promise of the tool: the token
// is handed to git through the environment of the child process only.
func TestTokenNeverTouchesDisk(t *testing.T) {
	const token = "glpat-super-secret-token-value"
	origin := newOrigin(t)
	root := t.TempDir()
	m := New(Options{Root: root, GitLabURL: "https://gl.example", Token: token}, func(string) {})
	p := forge.Project{ID: 1, PathWithNamespace: "group/app", DefaultBranch: "main", HTTPURLToRepo: origin}

	if _, err := m.CloneProject(p); err != nil {
		t.Fatal(err)
	}
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	if _, err := m.EnsureMR(mr, p); err != nil {
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

// TestRemoteURLFollowsTheProtocol: over SSH the address the forge reported
// wins, because it knows about custom ports and hosts.
func TestRemoteURLFollowsTheProtocol(t *testing.T) {
	p := forge.Project{
		PathWithNamespace: "group/app",
		HTTPURLToRepo:     "https://gl.example/group/app.git",
		SSHURLToRepo:      "ssh://git@gl.example:2222/group/app.git",
	}
	https := New(Options{Root: "/r", GitLabURL: "https://gl.example"}, nil)
	ssh := New(Options{Root: "/r", GitLabURL: "https://gl.example", CloneProtocol: ProtocolSSH}, nil)

	if got := https.RemoteURL(p); got != p.HTTPURLToRepo {
		t.Errorf("https = %q", got)
	}
	if got := ssh.RemoteURL(p); got != p.SSHURLToRepo {
		t.Errorf("ssh = %q, want the address the forge gave", got)
	}

	// Without the forge's answer it is built from the server address.
	bare := forge.Project{PathWithNamespace: "group/app"}
	if got := https.RemoteURL(bare); got != "https://gl.example/group/app.git" {
		t.Errorf("built https = %q", got)
	}
	if got := ssh.RemoteURL(bare); got != "git@gl.example:group/app.git" {
		t.Errorf("built ssh = %q", got)
	}
}

// TestSetRemoteSwitchesAnExistingClone
func TestSetRemoteSwitchesAnExistingClone(t *testing.T) {
	origin := newOrigin(t)
	root := t.TempDir()
	p := forge.Project{ID: 1, PathWithNamespace: "group/app", DefaultBranch: "main",
		HTTPURLToRepo: origin, SSHURLToRepo: "ssh://git@gl.example/group/app.git"}

	m := New(Options{Root: root, GitLabURL: "https://gl.example"}, func(string) {})
	dir, err := m.CloneProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Git().RemoteURL(dir, "origin"); got != origin {
		t.Fatalf("cloned from %q", got)
	}

	ssh := New(Options{Root: root, GitLabURL: "https://gl.example",
		CloneProtocol: ProtocolSSH}, func(string) {})
	changed, err := ssh.SetRemote(p)
	if err != nil {
		t.Fatal(err)
	}
	if changed != p.SSHURLToRepo {
		t.Errorf("reported %q", changed)
	}
	if got, _ := ssh.Git().RemoteURL(dir, "origin"); got != p.SSHURLToRepo {
		t.Errorf("remote = %q", got)
	}
	// Running it again is a no-op and says so.
	if changed, err := ssh.SetRemote(p); err != nil || changed != "" {
		t.Errorf("second run: %q %v", changed, err)
	}
	// And a project that is not on disk is left alone.
	missing := forge.Project{PathWithNamespace: "group/nope"}
	if changed, err := ssh.SetRemote(missing); err != nil || changed != "" {
		t.Errorf("missing project: %q %v", changed, err)
	}
}

func TestCloneProjectLeavesExistingCheckoutAlone(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	dir, err := m.CloneProject(p)
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "checkout", "-b", "local-work")
	dirty := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dirty, []byte("local edits\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unreachable remote proves repeating clone does not fetch or pull.
	git(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	again, err := m.CloneProject(p)
	if err != nil || again != dir {
		t.Fatalf("repeat clone: %q, %v", again, err)
	}
	if branch := git(t, dir, "branch", "--show-current"); branch != "local-work" {
		t.Fatalf("branch changed to %q", branch)
	}
	data, err := os.ReadFile(dirty)
	if err != nil || string(data) != "local edits\n" {
		t.Fatalf("local edits changed: %q, %v", data, err)
	}
}

func TestExactDestinationAndLegacyWorktrees(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	parent := t.TempDir()
	m.opts.ProjectDirectory = filepath.Join(parent, "renamed")
	dir, err := m.CloneProject(p)
	if err != nil {
		t.Fatal(err)
	}
	if dir != m.opts.ProjectDirectory {
		t.Fatalf("clone = %q", dir)
	}
	branch := m.MRDir(p.PathWithNamespace, 42, "feature/x")
	review := m.ReviewDir(p.PathWithNamespace, 42, "feature/x")
	if branch != filepath.Join(parent, ".unagit", "renamed", "42-feature-x") {
		t.Fatalf("branch = %q", branch)
	}
	if review != filepath.Join(parent, ".unagit", "renamed", "review-42-feature-x") {
		t.Fatalf("review = %q", review)
	}
	legacy := filepath.Join(dir+".mrs", "42-feature-x")
	git(t, dir, "worktree", "add", "--detach", legacy, "HEAD")
	if got := m.MRDir(p.PathWithNamespace, 42, "feature/x"); got != legacy {
		t.Fatalf("lost existing worktree: %q", got)
	}
	if got := m.InspectProject(p.PathWithNamespace); len(got.MRDirs) != 1 {
		t.Fatalf("worktrees = %v", got.MRDirs)
	}
	if err := m.RemoveProject(p.PathWithNamespace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Fatalf("removed destination parent: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy worktree left behind: %v", err)
	}
}

// TestManagedCheckoutIsUsedAndKept: chezmoi's checkout is the main clone, so
// nothing is cloned; its worktrees go under the root, where the clone would
// have been; deleting the repository takes them and leaves the checkout and
// its remote as they were.
func TestManagedCheckoutIsUsedAndKept(t *testing.T) {
	origin := newOrigin(t)
	managed := filepath.Join(t.TempDir(), "chezmoi")
	git(t, filepath.Dir(managed), "clone", "-q", origin, managed)
	root := t.TempDir()
	m := New(Options{Root: root, GitLabURL: "https://gl.example", ManagedDirectory: managed}, func(string) {})
	p := forge.Project{ID: 1, PathWithNamespace: "me/dotfiles", DefaultBranch: "main", HTTPURLToRepo: "https://gl.example/me/dotfiles.git"}

	if dir, err := m.CloneProject(p); err != nil || dir != managed {
		t.Fatalf("CloneProject = %q, %v; want the managed checkout", dir, err)
	}
	if _, err := os.Stat(filepath.Join(root, "me", "dotfiles")); !os.IsNotExist(err) {
		t.Error("a second clone was made under the root")
	}
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	wt, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "me", ".unagit", "dotfiles"); filepath.Dir(wt) != want {
		t.Errorf("worktree at %s, want it under %s", wt, want)
	}

	if url, err := m.SetRemote(p); err != nil || url != "" {
		t.Errorf("SetRemote = %q, %v; it must leave the checkout's remote alone", url, err)
	}
	if got := git(t, managed, "remote", "get-url", "origin"); got != origin {
		t.Errorf("origin = %q, want %q", got, origin)
	}

	if err := m.RemoveProject("me/dotfiles"); err != nil {
		t.Fatal(err)
	}
	if !Exists(managed) {
		t.Fatal("the managed checkout was deleted")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree still on disk: %v", err)
	}
	if out := git(t, managed, "worktree", "list"); strings.Count(out, "\n") != 0 {
		t.Errorf("git still lists a removed worktree:\n%s", out)
	}
}
