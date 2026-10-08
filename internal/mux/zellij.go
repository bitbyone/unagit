package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (c Connection) zellijCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, c.Binary, append([]string{"--session", c.Session, "action"}, args...)...)
}

func muxError(ctx context.Context, output []byte, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("zellij is not answering; check the session and try again")
	}
	return fmt.Errorf("zellij request failed: %s (%v); check the session and use Zellij 0.45.1 or newer", strings.TrimSpace(string(output)), err)
}

type pane struct {
	ID     int  `json:"id"`
	Plugin bool `json:"is_plugin"`
	Exited bool `json:"exited"`
	TabID  int  `json:"tab_id"`
}

func (c Connection) panes(ctx context.Context) ([]pane, error) {
	if c.Kind != Zellij || c.Binary == "" || c.Session == "" {
		return nil, fmt.Errorf("unknown multiplexer; open the editor again from Zellij")
	}
	cmd := c.zellijCommand(ctx, "list-panes", "--json")
	out, err := cmd.CombinedOutput()
	// An absent session is definitive. Other failures keep its records for a
	// later retry; a title in a JSON reply must never be mistaken for an error.
	if strings.HasPrefix(strings.TrimSpace(string(out)), "There is no active session") || strings.HasPrefix(strings.TrimSpace(string(out)), fmt.Sprintf("Session '%s' not found.", c.Session)) {
		return nil, nil
	}
	if err != nil {
		return nil, muxError(ctx, out, err)
	}
	var panes []pane
	if err := json.Unmarshal(out, &panes); err != nil {
		return nil, fmt.Errorf("cannot read zellij panes; use Zellij 0.45.1 or newer")
	}
	return panes, nil
}

func (c Connection) zellijPanes() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	panes, err := c.panes(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, p := range panes {
		if !p.Plugin && !p.Exited {
			live[fmt.Sprintf("terminal_%d", p.ID)] = true
		}
	}
	return live, nil
}

func (c *Client) openZellij(where Placement, dir, name string, command *exec.Cmd) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var args []string
	switch where {
	case Tab:
		args = []string{"new-tab", "--cwd", dir, "--name", name, "--layout-string", zellijLayout(dir, command.Args)}
	case Vertical, Horizontal:
		direction := "right"
		if where == Horizontal {
			direction = "down"
		}
		args = []string{"new-pane", "--direction", direction, "--cwd", dir, "--close-on-exit", "--near-current-pane"}
	default:
		return "", fmt.Errorf("unknown editor placement; choose a tab or split")
	}
	if where != Tab {
		args = append(append(args, "--"), command.Args...)
	}
	cmd := c.zellijCommand(ctx, args...)
	cmd.Dir = dir
	if c.SourcePane != "" {
		cmd.Env = append(os.Environ(), "ZELLIJ_PANE_ID="+c.SourcePane)
	}
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			out = exit.Stderr
		}
		return "", muxError(ctx, out, err)
	}
	id := strings.TrimSpace(string(out))
	tab := -1
	if where != Tab {
		if _, err := strconv.ParseUint(strings.TrimPrefix(id, "terminal_"), 10, 32); err != nil || !strings.HasPrefix(id, "terminal_") {
			return "", fmt.Errorf("zellij did not return a pane id; use Zellij 0.45.1 or newer")
		}
	} else {
		tab, err = strconv.Atoi(id)
		if err != nil || tab < 0 {
			return "", fmt.Errorf("zellij did not return a tab id; use Zellij 0.45.1 or newer")
		}
	}
	// Creation replies precede the pane list. Do not write a persistent record
	// until another reader can confirm its pane instead of sweeping it away.
	for {
		panes, err := c.panes(ctx)
		if err != nil {
			return "", err
		}
		for _, p := range panes {
			paneID := fmt.Sprintf("terminal_%d", p.ID)
			if !p.Plugin && !p.Exited && (where == Tab && p.TabID == tab || where != Tab && paneID == id) {
				return paneID, c.focus(ctx, paneID)
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("the new zellij pane has no running editor; check its command")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (c *Client) focusZellij(paneID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.focus(ctx, paneID)
}

func (c *Client) focus(ctx context.Context, paneID string) error {
	focus := c.zellijCommand(ctx, "focus-pane-id", paneID)
	if c.SourcePane != "" {
		focus.Env = append(os.Environ(), "ZELLIJ_PANE_ID="+c.SourcePane)
	}
	out, err := focus.CombinedOutput()
	// New tabs may already have the focus. Zellij reports that state as an
	// error even though the requested result has been reached.
	already := fmt.Sprintf("Pane Terminal(%s) is already focused", strings.TrimPrefix(paneID, "terminal_"))
	if err != nil && strings.TrimSpace(string(out)) != already {
		return muxError(ctx, out, err)
	}
	return nil
}

// KDL uses braced Unicode escapes for control characters. Arguments stay
// separate, so shell metacharacters in a directory or custom command are data.
func kdlQuote(s string) string {
	var text strings.Builder
	text.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			text.WriteByte('\\')
			text.WriteRune(r)
		case r < 32 || r == 127:
			fmt.Fprintf(&text, "\\u{%x}", r)
		default:
			text.WriteRune(r)
		}
	}
	text.WriteByte('"')
	return text.String()
}

func zellijLayout(dir string, args []string) string {
	var layout strings.Builder
	fmt.Fprintf(&layout, "layout {\n pane command=%s cwd=%s {\n", kdlQuote(args[0]), kdlQuote(dir))
	if len(args) > 1 {
		layout.WriteString("  args")
		for _, arg := range args[1:] {
			layout.WriteByte(' ')
			layout.WriteString(kdlQuote(arg))
		}
		layout.WriteByte('\n')
	}
	layout.WriteString("  close_on_exit true\n }\n}\n")
	return layout.String()
}
