package mux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Rex is scripted through its `rex` command, which takes a command as
// separate arguments and runs it without a shell: no launcher is needed,
// and the panes are Rex's blocks, by their full ids.

// RexSession is the Rex session a place of its own opens a tab of, as
// herdr's workspace is for herdr.
const RexSession = "Unagit"

// rexUnreachable is the exit status of a rex command that found no server.
const rexUnreachable = 4

// DetectRex finds the rex command. here is whether unagit runs in a Rex
// terminal of its own, whose session tabs open in and whose block splits
// open beside.
func DetectRex(getenv func(string) string, lookPath func(string) (string, error), here bool) *Client {
	bin := ""
	// Rex puts the copy of rex that matches its server first on a
	// terminal's PATH and names its directory; a shell's startup files can
	// reorder PATH since.
	if d := getenv("REX_BIN_DIR"); d != "" {
		if _, err := os.Stat(filepath.Join(d, "rex")); err == nil {
			bin = filepath.Join(d, "rex")
		}
	}
	if bin == "" {
		found, err := lookPath("rex")
		if err != nil {
			return nil
		}
		bin = found
	}
	c := &Client{Connection: Connection{Kind: Rex, Binary: bin, Session: getenv("REX_SERVER")}}
	if here && getenv("REX_SESSION") != "" && getenv("REX_BLOCK") != "" {
		c.SourcePane, c.Workspace = getenv("REX_BLOCK"), getenv("REX_SESSION")
	}
	return c
}

func (c Connection) rex(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Env = os.Environ()
	if c.Session != "" {
		cmd.Env = append(cmd.Env, "REX_SERVER="+c.Session)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("rex is not answering; check that Rex runs and try again")
	}
	said := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stderr.String()), "Error:"))
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == rexUnreachable {
		return "", &rexUnreachableError{said}
	}
	if said == "" {
		said = err.Error()
	}
	return "", fmt.Errorf("rex: %s", said)
}

// rexUnreachableError is a server rex could not reach: its blocks may live
// on, so nothing is taken for gone.
type rexUnreachableError struct{ said string }

func (e *rexUnreachableError) Error() string {
	return "Rex's server cannot be reached (" + e.said + "); start Rex and try again"
}

// rexCreated is what rex run, split and new print with --json.
type rexCreated struct {
	SessionID string   `json:"session_id"`
	WindowID  string   `json:"window_id"`
	BlockIDs  []string `json:"block_ids"`
}

func (made rexCreated) block() (string, error) {
	if len(made.BlockIDs) == 0 || made.BlockIDs[0] == "" {
		return "", fmt.Errorf("rex did not say which block it made; update Rex")
	}
	return made.BlockIDs[0], nil
}

func (c Connection) rexCreate(ctx context.Context, args ...string) (rexCreated, error) {
	out, err := c.rex(ctx, args...)
	if err != nil {
		return rexCreated{}, err
	}
	var made rexCreated
	if err := json.Unmarshal([]byte(out), &made); err != nil {
		return rexCreated{}, fmt.Errorf("cannot read what rex made; update Rex")
	}
	return made, nil
}

// openRex runs the command in a new block where asked. A Rex terminal runs
// its command with the login shell's environment, whose PATH may lack what
// unagit found the program on - Homebrew's, say, set up in .zshrc - so it
// runs through env with unagit's PATH, as the launcher does elsewhere.
func (c *Client) openRex(where Placement, dir, name string, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("nothing to run; choose an editor or an agent")
	}
	command := append([]string{"--", "env", "PATH=" + os.Getenv("PATH")}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch where {
	case Tab:
		if c.Workspace == "" {
			return "", fmt.Errorf("a Rex tab opens beside unagit; run unagit in Rex, or open a Rex session")
		}
		return c.rexTab(ctx, c.Workspace, dir, name, command)
	case Vertical, Horizontal:
		if c.SourcePane == "" {
			return "", fmt.Errorf("a Rex split opens beside unagit; run unagit in Rex")
		}
		side := "right"
		if where == Horizontal {
			side = "below"
		}
		made, err := c.rexCreate(ctx, append([]string{"split", "-b", c.SourcePane, "--split", side, "--cwd", dir, "--label", name, "--json"}, command...)...)
		if err != nil {
			return "", err
		}
		return made.block()
	case Window:
		session, err := c.rexSession(ctx)
		if err != nil {
			return "", err
		}
		if session != "" {
			pane, err := c.rexTab(ctx, session, dir, name, command)
			if err != nil {
				return pane, err
			}
			return pane, c.focusRexIn(ctx, pane)
		}
		made, err := c.rexCreate(ctx, append([]string{"new", RexSession, "--window", name, "--cwd", dir, "--json"}, command...)...)
		if err != nil {
			return "", err
		}
		if len(made.BlockIDs) == 0 && made.SessionID != "" {
			// A new session says what it is, and its one block is asked for.
			out, err := c.rex(ctx, "block", "ls", "-s", made.SessionID, "-q", "--short=false")
			if err != nil {
				return "", err
			}
			made.BlockIDs = strings.Fields(out)
		}
		pane, err := made.block()
		if err != nil {
			return "", err
		}
		return pane, c.focusRexIn(ctx, pane)
	}
	return "", fmt.Errorf("unknown placement; choose a tab, a split or a session")
}

// rexTab opens a window of a session - a tab, in Rex's words - named after
// what it opens.
func (c *Client) rexTab(ctx context.Context, session, dir, name string, command []string) (string, error) {
	made, err := c.rexCreate(ctx, append([]string{"run", "-s", session, "--cwd", dir, "--label", name, "--json"}, command...)...)
	if err != nil {
		return "", err
	}
	pane, err := made.block()
	if err != nil {
		return "", err
	}
	if made.WindowID != "" {
		if _, err := c.rex(ctx, "window", "rename", "-s", session, made.WindowID, name); err != nil {
			return pane, err
		}
	}
	return pane, nil
}

// rexSession finds the session places of their own open in, "" when there
// is none yet.
func (c *Client) rexSession(ctx context.Context) (string, error) {
	out, err := c.rex(ctx, "ls", "--json")
	if err != nil {
		return "", err
	}
	var list struct {
		Sessions []struct {
			ID    string `json:"session_id"`
			Label string `json:"label"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return "", fmt.Errorf("cannot read Rex's sessions; update Rex")
	}
	for _, s := range list.Sessions {
		if s.Label == RexSession {
			return s.ID, nil
		}
	}
	return "", nil
}

func (c *Client) focusRex(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.focusRexIn(ctx, id)
}

func (c *Client) focusRexIn(ctx context.Context, id string) error {
	_, err := c.rex(ctx, "block", "focus", id)
	return err
}

// rexPanes are the blocks of every session. The server is never started
// for asking: one that is not running cannot say its blocks are gone.
func (c Connection) rexPanes() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := c.rex(ctx, "block", "ls", "-A", "-q", "--short=false", "--autostart=false")
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, id := range strings.Fields(out) {
		live[id] = true
	}
	return live, nil
}
