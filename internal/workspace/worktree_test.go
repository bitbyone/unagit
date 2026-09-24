package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

func TestEnsureWorktreeNewBranchFromMainHEAD(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	main, err := m.EnsureProject(p)
	if err != nil {
		t.Fatal(err)
	}
	head := m.Git().CurrentBranch(main)
	if head != "main" {
		t.Fatalf("main clone on %q, want main", head)
	}

	wt, err := m.EnsureWorktree(p, "feature-brand-new", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := m.WorktreeDir("group/app", "feature-brand-new"); wt != want {
		t.Errorf("worktree at %q, want %q", wt, want)
	}
	if got := m.Git().CurrentBranch(wt); got != "feature-brand-new" {
		t.Errorf("branch = %q", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Errorf("did not branch from main's content: %v", err)
	}
	// login.go only exists on feature/login, not on main.
	if _, err := os.Stat(filepath.Join(wt, "login.go")); !os.IsNotExist(err) {
		t.Errorf("branched from something other than main's HEAD: %v", err)
	}
}

func TestEnsureWorktreeExistingRemoteBranch(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))

	wt, err := m.EnsureWorktree(p, "feature/login", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Git().CurrentBranch(wt); got != "feature/login" {
		t.Errorf("branch = %q, want feature/login", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "login.go")); err != nil {
		t.Errorf("remote branch content missing: %v", err)
	}
	if upstream, err := m.Git().Run(wt, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil {
		t.Errorf("no upstream set: %v", err)
	} else if got := strings.TrimSpace(upstream); got != "origin/feature/login" {
		t.Errorf("upstream = %q, want origin/feature/login", got)
	}
}

func TestEnsureWorktreeExistingLocalOnlyBranch(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	main, err := m.EnsureProject(p)
	if err != nil {
		t.Fatal(err)
	}
	git(t, main, "branch", "local-only")

	wt, err := m.EnsureWorktree(p, "local-only", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Git().CurrentBranch(wt); got != "local-only" {
		t.Errorf("branch = %q, want local-only", got)
	}
}

func TestEnsureWorktreeUpdatesInPlace(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	wt, err := m.EnsureWorktree(p, "feature/login", false)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(wt, "scratch.txt")
	if err := os.WriteFile(scratch, []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := m.EnsureWorktree(p, "feature/login", false)
	if err != nil {
		t.Fatal(err)
	}
	if again != wt {
		t.Errorf("directory changed: %q -> %q", wt, again)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Errorf("local change was lost: %v", err)
	}
}

func TestWorktreeEntriesListsMergeRequestAndPlainWorktrees(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	if _, err := m.EnsureMR(mr, p); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureWorktree(p, "feature-brand-new", true); err != nil {
		t.Fatal(err)
	}

	entries := m.WorktreeEntries("group/app")
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	var sawMR, sawBranch bool
	for _, e := range entries {
		switch e.Kind {
		case "merge request":
			sawMR = true
			if e.Label != "!1 feature/login" {
				t.Errorf("mr label = %q", e.Label)
			}
			if len(e.Dirs) != 1 {
				t.Errorf("mr dirs = %v", e.Dirs)
			}
		case "branch":
			sawBranch = true
			if e.Label != "feature-brand-new" {
				t.Errorf("branch label = %q", e.Label)
			}
		default:
			t.Errorf("unexpected kind %q", e.Kind)
		}
	}
	if !sawMR || !sawBranch {
		t.Fatalf("missing an entry: mr=%v branch=%v", sawMR, sawBranch)
	}
}

func TestRemoveWorktreeDirKeepsEverythingElse(t *testing.T) {
	m, _, p := newManager(t, newOrigin(t))
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	mrDir, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := m.EnsureWorktree(p, "feature-brand-new", true)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.RemoveWorktreeDir("group/app", wt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("plain worktree still on disk: %v", err)
	}
	if !Exists(mrDir) {
		t.Error("unrelated merge request worktree was removed too")
	}
	if !Exists(m.ProjectDir("group/app")) {
		t.Error("main clone was removed too")
	}
}
