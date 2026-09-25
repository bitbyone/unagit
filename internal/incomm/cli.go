package incomm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// MinFormat is the notes format the integration needs from the incomm CLI:
// version 2 is the first that knows an audience and a source. An older CLI
// would drop both when it rewrites the file, so nothing is imported or
// published through one.
const MinFormat = 2

// The two ways of looking at a worktree's comments. The agent view shows what
// the agent may see; the external view what belongs on the forge. Private
// comments are in neither.
const (
	viewAgent    = "agent"
	viewExternal = "external"
)

// CheckCLI finds the incomm binary and makes sure it is new enough.
func CheckCLI(ctx context.Context) (string, error) {
	binary, err := exec.LookPath("incomm")
	if err != nil {
		return "", fmt.Errorf("incomm is not on PATH; install it or disable it in Settings > Integrations")
	}
	out, err := exec.CommandContext(ctx, binary, "version", "--json").Output()
	var info struct {
		Version       string `json:"version"`
		FormatVersion int    `json:"formatVersion"`
	}
	if err != nil || json.Unmarshal(out, &info) != nil || info.FormatVersion < MinFormat {
		// An old CLI has no version command at all, which lands here too.
		return "", fmt.Errorf("incomm is too old for merge request comments (it needs notes format v%d); update incomm", MinFormat)
	}
	return binary, nil
}

// cli runs incomm against one worktree.
type cli struct {
	ctx    context.Context
	binary string
	dir    string
}

func newCLI(ctx context.Context, dir string) (*cli, error) {
	binary, err := CheckCLI(ctx)
	if err != nil {
		return nil, err
	}
	return &cli{ctx: ctx, binary: binary, dir: dir}, nil
}

// run executes one incomm command in the worktree, through the given view.
// Incomm searches parents even with --root, so the worktree gets a store of its
// own before anything is written (see Import).
func (c *cli) run(view string, args ...string) ([]byte, error) {
	full := []string{"--root", c.dir, "--json"}
	if view == viewExternal {
		full = append(full, "--view", viewExternal)
	}
	cmd := exec.CommandContext(c.ctx, c.binary, append(full, args...)...)
	cmd.Dir = c.dir
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			detail = strings.TrimSpace(string(exit.Stderr))
		}
		if detail != "" {
			return nil, fmt.Errorf("incomm %s failed: %s", args[0], detail)
		}
		return nil, fmt.Errorf("incomm %s failed; check the installation and retry: %w", args[0], err)
	}
	return out, nil
}

// SetSource records, through the CLI, where a comment or reply went once it was
// published, and optionally its audience. It works in the external view because
// that is where a pending comment is visible. reply is empty for a comment.
func SetSource(ctx context.Context, dir, id, reply string, src Source, audience string) error {
	c, err := newCLI(ctx, dir)
	if err != nil {
		return err
	}
	return c.setSource(viewExternal, id, reply, src, audience)
}

func (c *cli) setSource(view, id, reply string, src Source, audience string) error {
	args := []string{"set", id}
	if reply != "" {
		args = append(args, "--reply", reply)
	}
	if audience != "" {
		args = append(args, "--audience", audience)
	}
	if src.URL != "" {
		args = append(args, "--source-url", src.URL)
	}
	if src.ID != 0 {
		args = append(args, "--source-id", fmt.Sprint(src.ID))
	}
	if src.Thread != "" && reply == "" {
		args = append(args, "--source-thread", src.Thread)
	}
	_, err := c.run(view, args...)
	return err
}
