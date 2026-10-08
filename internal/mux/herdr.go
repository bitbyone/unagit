package mux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// herdrReply is what every herdr command prints: a result, or an error with
// a code to tell the failures apart.
type herdrReply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// herdrError is a failure herdr explained.
type herdrError struct{ Code, Message string }

func (e *herdrError) Error() string {
	return "herdr: " + e.Message
}

func (c Connection) herdr(ctx context.Context, args ...string) (json.RawMessage, error) {
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Env = os.Environ()
	if c.Session != "" {
		cmd.Env = append(cmd.Env, "HERDR_SOCKET_PATH="+c.Session)
	}
	out, err := cmd.Output()
	// Some commands - pane run - say nothing when they have done it.
	if err == nil && len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}
	var reply herdrReply
	if json.Unmarshal(out, &reply) == nil {
		if reply.Error != nil {
			return nil, &herdrError{reply.Error.Code, reply.Error.Message}
		}
		if err == nil {
			return reply.Result, nil
		}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("herdr is not answering; check that its server runs and try again")
	}
	if exit, ok := err.(*exec.ExitError); ok {
		out = exit.Stderr
	}
	return nil, fmt.Errorf("herdr request failed: %s (%v); use herdr 0.9 or newer", strings.TrimSpace(string(out)), err)
}

type herdrPane struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
	Cwd         string `json:"cwd"`
}

func (c Connection) herdrPaneList(ctx context.Context) ([]herdrPane, error) {
	result, err := c.herdr(ctx, "pane", "list")
	if err != nil {
		var he *herdrError
		// A server that is gone took its panes with it.
		if errors.As(err, &he) && he.Code == "server_not_running" {
			return nil, nil
		}
		return nil, err
	}
	var list struct {
		Panes []herdrPane `json:"panes"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("cannot read herdr's panes; use herdr 0.9 or newer")
	}
	return list.Panes, nil
}

func (c Connection) herdrPanes() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	panes, err := c.herdrPaneList(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, p := range panes {
		live[p.PaneID] = true
	}
	return live, nil
}

// openHerdr opens a shell where asked and has it become the command.
func (c *Client) openHerdr(where Placement, dir, name string, args []string) (string, error) {
	value, err := launchValue(launch{Dir: dir, Args: args})
	if err != nil {
		return "", err
	}
	line, err := launchLine(c.Self, true)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pane, err := c.herdrShell(ctx, where, dir, name, LaunchVariable+"="+value)
	if err != nil {
		return "", err
	}
	if _, err := c.herdr(ctx, "pane", "run", pane, line); err != nil {
		return "", err
	}
	return pane, nil
}

// OpenShell opens a shell where asked, for an agent herdr is to start in it.
func (c *Client) OpenShell(where Placement, dir, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.herdrShell(ctx, where, dir, name, "")
}

// herdrShell makes the place and brings it forward. A split does not take
// the focus in herdr, so the focus is moved from unagit towards it - the
// split is unagit's neighbour that way.
func (c *Client) herdrShell(ctx context.Context, where Placement, dir, name, env string) (string, error) {
	var args []string
	switch where {
	case Tab:
		if c.Workspace == "" {
			return "", fmt.Errorf("a herdr tab opens beside unagit; run unagit inside herdr, or open a workspace")
		}
		args = []string{"tab", "create", "--workspace", c.Workspace, "--cwd", dir, "--label", name, "--focus"}
	case Vertical, Horizontal:
		if c.SourcePane == "" {
			return "", fmt.Errorf("a herdr split opens beside unagit; run unagit inside herdr, or open a workspace")
		}
		args = []string{"pane", "split", c.SourcePane, "--direction", herdrDirection(where), "--cwd", dir}
	case Window:
		// Everything opened this way goes to one workspace, a tab each, so
		// herdr's sidebar does not fill up with them.
		workspace, err := c.agentsWorkspace(ctx)
		if err != nil {
			return "", err
		}
		if workspace == "" {
			args = []string{"workspace", "create", "--cwd", dir, "--label", AgentsWorkspace, "--focus"}
		} else {
			args = []string{"tab", "create", "--workspace", workspace, "--cwd", dir, "--label", name, "--focus"}
		}
	default:
		return "", fmt.Errorf("unknown placement; choose a tab, a split or a workspace")
	}
	if env != "" {
		args = append(args, "--env", env)
	}
	result, err := c.herdr(ctx, args...)
	if err != nil {
		return "", err
	}
	var created struct {
		RootPane herdrPane `json:"root_pane"`
		Pane     herdrPane `json:"pane"`
	}
	if err := json.Unmarshal(result, &created); err != nil {
		return "", fmt.Errorf("cannot read what herdr made; use herdr 0.9 or newer")
	}
	pane := created.RootPane.PaneID
	if args[0] == "workspace" {
		// A new workspace comes with a tab; it is named as the others are.
		if _, err := c.herdr(ctx, "tab", "rename", created.RootPane.TabID, name); err != nil {
			return pane, err
		}
	}
	if where == Vertical || where == Horizontal {
		pane = created.Pane.PaneID
		if _, err := c.herdr(ctx, "pane", "focus", "--pane", c.SourcePane, "--direction", herdrDirection(where)); err != nil {
			return pane, err
		}
	}
	if pane == "" {
		return "", fmt.Errorf("herdr did not say which pane it made; use herdr 0.9 or newer")
	}
	return pane, nil
}

// AgentsWorkspace is the herdr workspace a place of its own opens a tab of.
const AgentsWorkspace = "Unagit Agents"

// agentsWorkspace finds that workspace, "" when there is none yet.
func (c *Client) agentsWorkspace(ctx context.Context) (string, error) {
	result, err := c.herdr(ctx, "workspace", "list")
	if err != nil {
		return "", err
	}
	var list struct {
		Workspaces []struct {
			ID    string `json:"workspace_id"`
			Label string `json:"label"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return "", fmt.Errorf("cannot read herdr's workspaces; use herdr 0.9 or newer")
	}
	for _, w := range list.Workspaces {
		if w.Label == AgentsWorkspace {
			return w.ID, nil
		}
	}
	return "", nil
}

func herdrDirection(where Placement) string {
	if where == Horizontal {
		return "down"
	}
	return "right"
}

// focusHerdr brings forward the tab a pane is in; herdr has no way to
// focus a pane by its id, and a tab of its own is how unagit opens most.
func (c *Client) focusHerdr(paneID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	panes, err := c.herdrPaneList(ctx)
	if err != nil {
		return err
	}
	for _, p := range panes {
		if p.PaneID == paneID {
			_, err := c.herdr(ctx, "tab", "focus", p.TabID)
			return err
		}
	}
	return fmt.Errorf("the herdr pane is gone; open it again")
}

// StartAgent has herdr start an agent of a kind it knows in a pane at its
// shell prompt, named so herdr can be asked about it. herdr waits until it
// sees the agent ready; one that starts by asking something - whether to
// trust the folder - is started all the same, and waits for the user.
func (c *Client) StartAgent(pane, name, kind string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := []string{"agent", "start", name, "--kind", kind, "--pane", pane}
	if len(args) > 0 {
		cmd = append(append(cmd, "--"), args...)
	}
	_, err := c.herdr(ctx, cmd...)
	var he *herdrError
	if errors.As(err, &he) && he.Code == "agent_not_ready" {
		return nil
	}
	return err
}

// FocusAgent brings an agent forward in herdr.
func (c *Client) FocusAgent(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.herdr(ctx, "agent", "focus", name)
	return err
}
