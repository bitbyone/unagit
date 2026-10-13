package unibox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (m *Manager) Diff(ctx context.Context, name, runtime string, out io.Writer) error {
	p, err := m.profile(name)
	if err != nil {
		return err
	}
	r, err := m.Runtime(runtime)
	if err != nil {
		return err
	}
	s, err := m.Paths.ReadState()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "unibox-diff-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	before := filepath.Join(dir, "before")
	after := filepath.Join(dir, "after")
	for root, files := range map[string]map[string]SnapshotFile{before: s.Builds[buildKey(p, r)].Snapshot, after: p.Files} {
		if err = os.MkdirAll(root, 0o700); err != nil {
			return err
		}
		for name, file := range files {
			if !safeRelative(name) {
				return fmt.Errorf("invalid saved build path; inspect unibox state")
			}
			if err = AtomicWrite(filepath.Join(root, name), file.Data, os.FileMode(file.Mode)); err != nil {
				return err
			}
		}
	}
	cmd := hostCommand(ctx, "git", "diff", "--no-index", "--no-ext-diff", "--no-color", "--", before, after)
	b, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return err
	}
	text := strings.ReplaceAll(string(b), filepath.ToSlash(before)+"/", "")
	text = strings.ReplaceAll(text, filepath.ToSlash(after)+"/", "")
	_, err = io.WriteString(out, text)
	return err
}
