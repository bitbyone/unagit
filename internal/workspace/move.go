package workspace

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/tobola/unagit/internal/gitx"
)

// CheckMove refuses layouts whose other checkouts or external git storage would
// still point at the old location after the folder moved.
func CheckMove(source, target string) error {
	f, err := InspectLocalFolder(source)
	if err != nil {
		return err
	}
	canonicalSource, _ := filepath.EvalSymlinks(source)
	if !f.Repository || f.Directory != canonicalSource {
		return fmt.Errorf("move the main repository directory itself")
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("choose an absolute destination")
	}
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return fmt.Errorf("the destination already exists or cannot be inspected; choose an unused path")
	}
	parent := filepath.Dir(target)
	for {
		resolved, e := filepath.EvalSymlinks(parent)
		if e == nil {
			if withinDirectory(canonicalSource, resolved) {
				return fmt.Errorf("the destination cannot be inside the source folder")
			}
			break
		}
		if !os.IsNotExist(e) {
			return e
		}
		next := filepath.Dir(parent)
		if next == parent {
			return e
		}
		parent = next
	}
	g := gitx.New("", nil)
	worktrees, err := g.Run(source, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	if strings.Count(worktrees, "worktree ") != 1 {
		return fmt.Errorf("other worktrees use this repository; remove them before moving it")
	}
	for _, name := range []string{"objects/info/alternates", "modules", "index.lock", "HEAD.lock", "config.lock"} {
		if _, e := os.Lstat(filepath.Join(source, ".git", name)); !os.IsNotExist(e) {
			return fmt.Errorf("this repository has git storage or a lock at %s; resolve it before moving", name)
		}
	}
	if worktree, _ := g.Run(source, "config", "--get", "core.worktree"); strings.TrimSpace(worktree) != "" {
		return fmt.Errorf("core.worktree points outside the ordinary layout; remove that override before moving")
	}
	return nil
}

func withinDirectory(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// MoveLocal keeps the journal ahead of the rename, so a restart can find the
// checkout at either end. Across filesystems the complete copy is checked before
// the source is removed; git's index and ignored files are ordinary files here.
func (p *Publication) MoveLocal(target, journal string) error {
	if !p.Complete || p.Project == nil {
		return fmt.Errorf("finish publishing the repository before moving it")
	}
	unlock, err := lockPublication(journal)
	if err != nil {
		return err
	}
	defer unlock()
	if p.Project != nil {
		if err := New(Options{LocalDirectory: p.Directory}, nil).CheckLocalProject(*p.Project); err != nil {
			return err
		}
	}
	if err := CheckMove(p.Directory, target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	p.MoveTo = target
	if err := p.Save(journal); err != nil {
		return err
	}
	source := p.Directory
	err = renameUnused(source, target)
	if errors.Is(err, syscall.EXDEV) {
		err = copyLocalFolder(source, target, func() error {
			p.Directory, p.MoveTo, p.MoveSource = target, "", source
			return p.Save(journal)
		})
	}
	if err != nil {
		return err
	}
	// From here on the repository is at target. Keep that true in memory even
	// if syncing a parent directory fails, so the caller can immediately repair
	// its machine state from the path that actually exists.
	p.Directory = target
	if err = syncPath(filepath.Dir(target)); err != nil {
		return fmt.Errorf("folder moved to %s, but its parent could not be synced: %w", target, err)
	}
	if filepath.Dir(source) != filepath.Dir(target) {
		if err = syncPath(filepath.Dir(source)); err != nil {
			return fmt.Errorf("folder moved to %s, but its old parent could not be synced: %w", target, err)
		}
	}
	p.MoveTo, p.MoveSource = "", ""
	if err = p.Save(journal); err != nil {
		return fmt.Errorf("folder moved to %s; progress could not be saved, reopen unagit to recover: %w", target, err)
	}
	return nil
}

type folderEntry struct {
	Mode fs.FileMode
	Size int64
	Hash [32]byte
	Link string
}

func folderContents(root string) (map[string]folderEntry, error) {
	entries := map[string]folderEntry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entry := folderEntry{Mode: st.Mode()}
		switch {
		case st.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			copy(entry.Hash[:], h.Sum(nil))
			entry.Size = st.Size()
		case st.Mode()&os.ModeSymlink != 0:
			entry.Link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		case st.IsDir():
		default:
			return fmt.Errorf("%s is not an ordinary file; move it separately before moving across filesystems", path)
		}
		entries[rel] = entry
		return nil
	})
	return entries, err
}

func copyLocalFolder(source, target string, installed func() error) error {
	before, err := folderContents(source)
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp(filepath.Dir(target), ".unagit-move-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	copyDir := filepath.Join(temp, "repository")
	// cp -a keeps filesystem metadata, symlinks and hard links, including git's
	// own files. The manifest separately checks the contents before any deletion.
	if output, err := exec.Command("cp", "-a", source, copyDir).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot copy the folder; source kept: %w: %s", err, strings.TrimSpace(string(output)))
	}
	copied, err := folderContents(copyDir)
	if err != nil {
		return err
	}
	after, err := folderContents(source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, copied) || !reflect.DeepEqual(before, after) {
		return fmt.Errorf("the folder changed during the copy; source kept, close its editors and retry")
	}
	if err = syncCopiedFolder(copyDir); err != nil {
		return err
	}
	if err = renameUnused(copyDir, target); err != nil {
		return err
	}
	if err = syncPath(filepath.Dir(target)); err != nil {
		return err
	}
	// Check once more after installing the destination. If something wrote to the
	// source meanwhile, keep both copies for the user to reconcile.
	after, err = folderContents(source)
	if err != nil || !reflect.DeepEqual(before, after) {
		return fmt.Errorf("source changed after copying; both folders were kept, reconcile them before continuing")
	}
	if err = installed(); err != nil {
		return fmt.Errorf("both folders were kept because move progress could not be saved: %w", err)
	}
	if err = os.RemoveAll(source); err != nil {
		return fmt.Errorf("copy is complete at %s, but the source could not be fully removed: %w", target, err)
	}
	if err = syncPath(filepath.Dir(source)); err != nil {
		return fmt.Errorf("copy is complete at %s and the source is gone, but its parent could not be synced: %w", target, err)
	}
	return nil
}

func syncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// A copied file must reach the destination disk before its source can go.
func syncCopiedFolder(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if d.Type().IsRegular() {
			return syncPath(path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := syncPath(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
