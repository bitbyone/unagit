package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/testenv"
)

type publishingForge struct {
	project       forge.Project
	creates       int
	defaultBranch string
	failDefault   bool
	failCreate    bool
}

func (f *publishingForge) CreateProject(_ context.Context, _ forge.Group, req forge.NewProject) (*forge.Project, error) {
	f.creates++
	if req.Readme || req.License != "" || req.Gitignore != "" {
		return nil, errors.New("remote must be empty")
	}
	if f.failCreate {
		return nil, errors.New("connection lost")
	}
	p := f.project
	return &p, nil
}
func (f *publishingForge) SetDefaultBranch(_ context.Context, _ forge.Project, branch string) error {
	if f.failDefault {
		return errors.New("connection lost")
	}
	f.defaultBranch = branch
	return nil
}
func publicationFixture(t *testing.T) (*Publication, *publishingForge, *Manager, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "local folder")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	dir, _ = filepath.EvalSymlinks(dir)
	if err := InitLocalFolder(dir, "trunk"); err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, "", "init", "--bare", origin)
	p := &Publication{Directory: dir, Instance: "server", Group: forge.Group{FullPath: "acme"}, Request: forge.NewProject{Name: "tool", Readme: true}, Branch: "trunk"}
	f := &publishingForge{project: forge.Project{Instance: "server", PathWithNamespace: "acme/tool", HTTPURLToRepo: origin}}
	return p, f, New(Options{LocalDirectory: dir}, nil), PublicationPath(t.TempDir(), dir)
}
func publicationCommit(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "file.txt")
	git(t, dir, "commit", "-m", "First")
}
func TestPublishLocalHistoryAndResume(t *testing.T) {
	t.Parallel()
	p, f, m, journal := publicationFixture(t)
	publicationCommit(t, p.Directory)
	git(t, p.Directory, "branch", "other")
	git(t, p.Directory, "tag", "v1")
	p.AllBranches, p.Tags = true, true
	head := git(t, p.Directory, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(p.Directory, "file.txt"), []byte("uncommitted"), 0644)
	os.WriteFile(filepath.Join(p.Directory, ".gitignore"), []byte("private.txt\n"), 0644)
	os.WriteFile(filepath.Join(p.Directory, "private.txt"), []byte("local only"), 0600)
	f.failDefault = true
	adopted := 0
	adopt := func(pr forge.Project) error {
		adopted++
		if pr.Instance != "server" {
			t.Error("missing instance")
		}
		return nil
	}
	if err := m.PublishLocal(context.Background(), f, p, journal, adopt); err == nil {
		t.Fatal("expected interrupted publication")
	}
	saved, err := ReadPublication(journal)
	if err != nil || saved.Project == nil || saved.Complete {
		t.Fatalf("progress lost: %v %#v", err, saved)
	}
	f.failDefault = false
	if err := m.PublishLocal(context.Background(), f, saved, journal, adopt); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || !saved.Complete || f.defaultBranch != "trunk" || adopted < 2 {
		t.Fatalf("not resumed: %#v %#v", f, saved)
	}
	for _, ref := range []string{"refs/heads/trunk", "refs/heads/other", "refs/tags/v1"} {
		if got := git(t, f.project.HTTPURLToRepo, "rev-parse", ref); got != head {
			t.Fatalf("%s: %s != %s", ref, got, head)
		}
	}
	if got := git(t, p.Directory, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/trunk" {
		t.Fatal(got)
	}
	if got := git(t, p.Directory, "show", "HEAD:file.txt"); got != "hello" {
		t.Fatal("local edits committed")
	}
	if got, _ := os.ReadFile(filepath.Join(p.Directory, "private.txt")); string(got) != "local only" {
		t.Fatal("ignored file lost")
	}
	if err := m.CheckLocalProject(*saved.Project); err != nil {
		t.Fatal(err)
	}
	git(t, p.Directory, "remote", "set-url", "origin", "/another/repository")
	if err := m.CheckLocalProject(*saved.Project); err == nil {
		t.Fatal("replaced origin accepted")
	}
}
func TestPublicationDoesNotRepeatAnUnconfirmedCreate(t *testing.T) {
	t.Parallel()
	p, f, m, journal := publicationFixture(t)
	f.failCreate = true
	adopt := func(forge.Project) error { return nil }
	for range 2 {
		if err := m.PublishLocal(context.Background(), f, p, journal, adopt); err == nil {
			t.Fatal("failure expected")
		}
	}
	if f.creates != 1 {
		t.Fatal("create repeated")
	}
}
func TestPublishEmptyRepositoryAndRejectExistingRemote(t *testing.T) {
	t.Parallel()
	p, f, m, journal := publicationFixture(t)
	if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !p.Complete || f.defaultBranch != "" {
		t.Fatal("empty repository was pushed")
	}
	p.Project = nil
	p.Requested = false
	journal = PublicationPath(t.TempDir(), p.Directory)
	if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err == nil {
		t.Fatal("existing remote accepted")
	}
}
func TestInspectLocalFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f, err := InspectLocalFolder(dir)
	if err != nil || f.Repository {
		t.Fatalf("plain folder: %#v %v", f, err)
	}
	if err := InitLocalFolder(f.Directory, "bad branch"); err == nil {
		t.Fatal("invalid branch accepted")
	}
	if err := InitLocalFolder(f.Directory, "main"); err != nil {
		t.Fatal(err)
	}
	dir = f.Directory
	os.Mkdir(filepath.Join(dir, "child"), 0755)
	f, err = InspectLocalFolder(filepath.Join(dir, "child"))
	if err != nil || f.Directory != dir || f.HasCommits {
		t.Fatalf("root: %#v %v", f, err)
	}
	publicationCommit(t, dir)
	wt := filepath.Join(t.TempDir(), "linked")
	git(t, dir, "worktree", "add", "-b", "linked", wt)
	if _, err = InspectLocalFolder(wt); err == nil {
		t.Fatal("linked worktree accepted")
	}
	git(t, dir, "checkout", "--detach")
	if _, err = InspectLocalFolder(dir); err == nil {
		t.Fatal("detached repository accepted")
	}
	bare := filepath.Join(t.TempDir(), "bare")
	git(t, "", "init", "--bare", bare)
	if _, err = InspectLocalFolder(bare); err == nil {
		t.Fatal("bare repository accepted")
	}
}
func TestFirstCommitIncludesOnlyChosenFiles(t *testing.T) {
	t.Parallel()
	p, _, _, _ := publicationFixture(t)
	for _, name := range []string{"chosen", "leave"} {
		os.WriteFile(filepath.Join(p.Directory, name), []byte(name), 0644)
	}
	g := gitx.New("", nil)
	if _, err := g.CommitPaths(p.Directory, "First", []gitx.Change{{Path: "chosen", Kind: gitx.Unversioned}}); err != nil {
		t.Fatal(err)
	}
	if got := git(t, p.Directory, "ls-tree", "--name-only", "HEAD"); got != "chosen" {
		t.Fatal(got)
	}
	if !strings.Contains(git(t, p.Directory, "status", "--porcelain"), "leave") {
		t.Fatal("unselected file lost")
	}
}

func TestPublicationRefusesAReplacedSourceAfterInterruption(t *testing.T) {
	t.Parallel()
	p, f, m, journal := publicationFixture(t)
	f.failCreate = true
	if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err == nil {
		t.Fatal("expected interrupted create")
	}
	mustRename := func(from, to string) {
		t.Helper()
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}
	mustRename(filepath.Join(p.Directory, ".git"), filepath.Join(t.TempDir(), "saved.git"))
	git(t, p.Directory, "init", "--initial-branch=trunk")
	if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("replacement accepted: %v", err)
	}
	if f.creates != 1 {
		t.Fatal("remote create repeated")
	}
}

func TestPublicationNeverForcesOverRemoteHistory(t *testing.T) {
	t.Parallel()
	p, f, m, journal := publicationFixture(t)
	publicationCommit(t, p.Directory)
	f.failDefault = true
	if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err == nil {
		t.Fatal("expected interrupted default branch update")
	}
	other := filepath.Join(t.TempDir(), "other")
	git(t, "", "clone", "--branch", "trunk", f.project.HTTPURLToRepo, other)
	os.WriteFile(filepath.Join(other, "theirs"), []byte("remote change"), 0644)
	git(t, other, "add", "theirs")
	git(t, other, "commit", "-m", "Their work")
	git(t, other, "push", "origin", "trunk")
	theirs := git(t, other, "rev-parse", "HEAD")
	f.failDefault = false
	if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err == nil {
		t.Fatal("diverged push should be refused")
	}
	if got := git(t, f.project.HTTPURLToRepo, "rev-parse", "trunk"); got != theirs {
		t.Fatal("remote history overwritten")
	}
}

func TestPublicationLockStopsASecondPublisher(t *testing.T) {
	t.Parallel()
	p, f, m, journal := publicationFixture(t)
	unlock, err := lockPublication(journal)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err == nil {
		t.Fatal("concurrent publication accepted")
	}
	if f.creates != 0 {
		t.Fatal("created a second remote")
	}
}

func TestFailedFirstCommitLeavesChosenFilesUnversioned(t *testing.T) {
	t.Parallel()
	p, _, _, _ := publicationFixture(t)
	if err := os.WriteFile(filepath.Join(p.Directory, "chosen"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := testenv.WriteExecutable(filepath.Join(p.Directory, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 1\n")); err != nil {
		t.Fatal(err)
	}
	_, err := gitx.New("", nil).CommitPaths(p.Directory, "First", []gitx.Change{{Path: "chosen", Kind: gitx.Unversioned}})
	if err == nil {
		t.Fatal("hook should reject commit")
	}
	if got := git(t, p.Directory, "status", "--porcelain"); got != "?? chosen" {
		t.Fatalf("failed commit left %q", got)
	}
}
