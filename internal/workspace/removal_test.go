package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

func TestRemovalNotifiesOnlyAfterDirectoriesAreGone(t *testing.T) {
	t.Parallel()
	m, _, p := newManager(t, newOrigin(t))
	removed := map[string]int{}
	m.opts.OnRemoved = func(dir string) {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("notified while %s still exists: %v", dir, err)
		}
		removed[dir]++
	}
	mr := forge.MergeRequest{IID: 1, SourceBranch: "feature/login", SourceProjectID: 1, TargetProjectID: 1}
	branch, err := m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatal("creating worktree notified removal")
	}
	if err := m.RemoveMR(p.PathWithNamespace, 1, mr.SourceBranch); err != nil {
		t.Fatal(err)
	}
	if removed[branch] != 1 {
		t.Fatalf("branch notifications = %d", removed[branch])
	}
	if err := m.RemoveMR(p.PathWithNamespace, 1, mr.SourceBranch); err != nil {
		t.Fatal(err)
	}
	if removed[branch] != 1 {
		t.Fatal("missing worktree notified twice")
	}
	branch, err = m.EnsureMR(mr, p)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(m.ProjectDir(p.PathWithNamespace)+".reviews", "review-legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveProject(p.PathWithNamespace); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{branch, legacy, m.ProjectDir(p.PathWithNamespace)} {
		if removed[dir] == 0 {
			t.Errorf("%s was never reported removed", dir)
		}
	}
}

func TestGroupAndManagedRemovalNotifications(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	removed := map[string]int{}
	m := New(Options{Root: root, ManagedDirectory: managed, OnRemoved: func(dir string) { removed[dir]++ }}, nil)
	group := filepath.Join(root, "group")
	member := filepath.Join(group, "app")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveGroupMember("acme/app", member); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDirectory(group); err != nil {
		t.Fatal(err)
	}
	if removed[member] != 1 || removed[group] != 1 {
		t.Fatalf("group notifications = %v", removed)
	}
	if err := m.RemoveProject("acme/app"); err != nil {
		t.Fatal(err)
	}
	if removed[managed] != 0 {
		t.Fatal("reported the managed clone removed")
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatal(err)
	}
}
