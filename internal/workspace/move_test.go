package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

func TestMoveLocalPreservesTheWholeFolder(t *testing.T) {
	t.Parallel()
	for _, copying := range []bool{false, true} {
		t.Run(map[bool]string{false: "rename", true: "copy"}[copying], func(t *testing.T) {
			p, f, m, journal := publicationFixture(t)
			publicationCommit(t, p.Directory)
			if err := m.PublishLocal(context.Background(), f, p, journal, func(forge.Project) error { return nil }); err != nil {
				t.Fatal(err)
			}
			git(t, p.Directory, "branch", "unpublished")
			os.WriteFile(filepath.Join(p.Directory, "file.txt"), []byte("stash me"), 0644)
			git(t, p.Directory, "stash", "push", "-m", "Local stash")
			os.WriteFile(filepath.Join(p.Directory, "file.txt"), []byte("dirty again"), 0644)
			os.WriteFile(filepath.Join(p.Directory, ".gitignore"), []byte("ignored\n"), 0644)
			os.WriteFile(filepath.Join(p.Directory, "ignored"), []byte("keep this too"), 0600)
			os.Symlink("file.txt", filepath.Join(p.Directory, "link"))
			before, err := folderContents(p.Directory)
			if err != nil {
				t.Fatal(err)
			}
			source := p.Directory
			target := filepath.Join(t.TempDir(), "destination")
			if copying {
				err = copyLocalFolder(source, target, func() error { return nil })
			} else {
				err = p.MoveLocal(target, journal)
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := folderContents(target)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("move changed files, modes or links")
			}
			if _, err := os.Stat(source); !os.IsNotExist(err) {
				t.Fatal("source remains")
			}
			if !copying {
				saved, err := ReadPublication(journal)
				if err != nil || saved.Directory != target || saved.MoveTo != "" {
					t.Fatalf("move progress lost: %#v %v", saved, err)
				}
			}
		})
	}
}
func TestMoveRefusesOccupiedNestedAndSharedDestinations(t *testing.T) {
	t.Parallel()
	p, _, _, _ := publicationFixture(t)
	publicationCommit(t, p.Directory)
	for _, target := range []string{p.Directory, filepath.Join(p.Directory, "child", "repo"), t.TempDir()} {
		if err := p.MoveLocal(target, PublicationPath(t.TempDir(), p.Directory)); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	os.Symlink(p.Directory, alias)
	if err := CheckMove(p.Directory, filepath.Join(alias, "nested")); err == nil {
		t.Fatal("symlink destination accepted")
	}
	git(t, p.Directory, "worktree", "add", "-b", "linked", filepath.Join(t.TempDir(), "linked"))
	if err := CheckMove(p.Directory, filepath.Join(t.TempDir(), "destination")); err == nil {
		t.Fatal("shared repository accepted")
	}
}
func TestCrossFilesystemMoveKeepsSourceWhenSavingProgressFails(t *testing.T) {
	t.Parallel()
	p, _, _, _ := publicationFixture(t)
	publicationCommit(t, p.Directory)
	target := filepath.Join(t.TempDir(), "destination")
	err := copyLocalFolder(p.Directory, target, func() error { return errors.New("disk full") })
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("the copy did not get as far as saving the progress: %v", err)
	}
	for _, dir := range []string{p.Directory, target} {
		if !Exists(dir) {
			t.Fatalf("lost %s (%v)", dir, err)
		}
	}
}
