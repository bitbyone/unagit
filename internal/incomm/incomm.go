// Package incomm imports forge comments into Incomm review worktrees.
package incomm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// Import waits for every write so the editor sees a complete conversation.
func Import(ctx context.Context, dir string, mr forge.MergeRequest, notes []forge.Note, log func(string)) error {
	locations := make(map[string]forge.Note)
	for _, n := range notes {
		if n.Thread != "" && n.Path != "" && n.Line > 0 {
			locations[n.Thread] = n
		}
	}
	var located []forge.Note
	for _, n := range notes {
		if n.System {
			continue
		}
		if n.Path == "" && n.Line == 0 && n.Thread != "" {
			if root, ok := locations[n.Thread]; ok {
				n.Path, n.Line, n.Orphaned = root.Path, root.Line, root.Orphaned
			}
		}
		if n.Path == "" || n.Line <= 0 {
			continue
		}
		located = append(located, n)
	}
	if len(located) == 0 {
		return nil
	}
	binary, err := exec.LookPath("incomm")
	if err != nil {
		return fmt.Errorf("incomm is not on PATH; install it or disable it in Settings > Integrations")
	}
	// Incomm searches parents even with --root. Give this worktree its own
	// store before invoking it, so another checkout's notes stay separate.
	if err := os.MkdirAll(filepath.Join(dir, ".incomm"), 0o755); err != nil {
		return err
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, append([]string{"--root", dir, "--json"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("incomm %s failed; check the installation and retry: %w", args[0], err)
		}
		return out, nil
	}
	out, err := run("list")
	if err != nil {
		return err
	}
	var existing struct {
		Notes []struct {
			Content string `json:"content"`
			ID      string `json:"id"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(out, &existing); err != nil {
		return fmt.Errorf("read incomm comments: %w", err)
	}
	seen := make(map[string]bool)
	ids := make(map[string]string)
	for _, n := range existing.Notes {
		seen[n.Content] = true
		ids[n.Content] = n.ID
	}
	added, orphaned := 0, 0
	for _, n := range located {
		// A forge path must stay inside the worktree, including through symlinks.
		if !filepath.IsLocal(n.Path) {
			return fmt.Errorf("incomm comment has an invalid file path: %q", n.Path)
		}
		if !n.Orphaned {
			located, err := hasLine(dir, n.Path, n.Line)
			if err != nil {
				return err
			}
			n.Orphaned = !located
		}
		source := mr.WebURL
		if source == "" {
			source = fmt.Sprintf("%s/%s!%d", mr.Instance, mr.ProjectPath, mr.IID)
		}
		content := fmt.Sprintf("%s\n\nSource: %s (comment %d)", n.Body, source, n.ID)
		if seen[content] {
			// A comment imported on an earlier head may now belong to a
			// deleted line. Keep its replies and identity while orphaning it.
			if n.Orphaned && ids[content] != "" {
				if _, err := run("anchor", "set", ids[content], "--orphaned",
					"--line", strconv.Itoa(n.Line), "--no-recompute",
					"--start-prefix", "\n", "--end-prefix", "",
					"--context-before", "", "--context-after", "", "--checksum", ""); err != nil {
					return err
				}
			}
			continue
		}
		author := n.Author.Name
		if author == "" {
			author = n.Author.Username
		}
		if author == "" {
			author = "Merge request reviewer"
		}
		if n.Orphaned {
			if err := appendOrphan(ctx, dir, n, content, author); err != nil {
				return err
			}
			orphaned++
		} else if _, err := run("add", "--file", n.Path, "--line", strconv.Itoa(n.Line),
			"--content", content, "--author", "user", "--author-title", author); err != nil {
			return err
		}
		seen[content] = true
		added++
	}
	if log != nil {
		log(fmt.Sprintf("Incomm: imported %d comments", added))
		if orphaned > 0 {
			log(fmt.Sprintf("Incomm: imported %d orphaned comments", orphaned))
		}
	}
	return nil
}

// hasLine checks the current checkout without following a path outside it.
func hasLine(dir, path string, line int) (bool, error) {
	target, err := filepath.EvalSymlinks(filepath.Join(dir, path))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(rel) {
		return false, fmt.Errorf("incomm comment file is outside the review: %q", path)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return line <= len(lines), nil
}
