package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiffTexts is what changed from one text to the other as a unified diff,
// every line of the file in it as context, its hunks alone - no headers.
// Two texts alike are one hunk of context. It needs no repository: a forge
// that sends a large merge request's diff only in part still has every
// file, and what changed in one is worked out here.
func DiffTexts(old, new string) (string, error) {
	if old == new {
		return contextHunk(new), nil
	}
	dir, err := os.MkdirTemp("", "unagit-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	for name, text := range map[string]string{"a": old, "b": new} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			return "", err
		}
	}
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--no-ext-diff", "--unified=100000000", "a", "b")
	cmd.Dir = dir
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	// Exit status 1 is git saying the two differ.
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return "", fmt.Errorf("git diff: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	text := out.String()
	if at := strings.Index(text, "\n@@"); at >= 0 {
		return text[at+1:], nil
	}
	return text, nil
}

// contextHunk is a text as one hunk in which nothing changed.
func contextHunk(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(lines), len(lines))
	for _, l := range lines {
		b.WriteString(" " + l + "\n")
	}
	return b.String()
}
