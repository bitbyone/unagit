package mux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ghosttyFindSelf stands for unagit's own Ghostty terminal until it is
// found: Ghostty names its terminals by ids a program inside one cannot see,
// so unagit finds itself by a title it sets for the moment.
const ghosttyFindSelf = "self"

// Every value goes to a script as an argument, never into its text.
const ghosttyOpenScript = `on run argv
	set {workdir, commandline, launchvar, place, here} to argv
	tell application "Ghostty"
		set cfg to new surface configuration
		set initial working directory of cfg to workdir
		set command of cfg to commandline
		set environment variables of cfg to {launchvar}
		if place is "window" then
			set w to new window with configuration cfg
			return id of focused terminal of selected tab of w
		else if place is "tab" then
			set t to new tab in front window with configuration cfg
			return id of focused terminal of t
		else if place is "right" then
			return id of (split (first terminal whose id is here) direction right with configuration cfg)
		else
			return id of (split (first terminal whose id is here) direction down with configuration cfg)
		end if
	end tell
end run`

const ghosttyFocusScript = `on run argv
	tell application "Ghostty" to focus (first terminal whose id is (item 1 of argv))
end run`

const ghosttyCloseScript = `on run argv
	tell application "Ghostty" to close (first terminal whose id is (item 1 of argv))
end run`

const ghosttyListScript = `tell application "Ghostty"
	set out to ""
	repeat with t in terminals
		set out to out & (id of t) & linefeed
	end repeat
	return out
end tell`

const ghosttyNamedScript = `on run argv
	tell application "Ghostty" to return id of (first terminal whose name is (item 1 of argv))
end run`

func (c Connection) osascript(ctx context.Context, script string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.Binary, append([]string{"-e", script}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ghostty is not answering; check that it runs and try again")
		}
		return "", fmt.Errorf("ghostty: %s", ghosttyHint(strings.TrimSpace(string(out))))
	}
	return strings.TrimSpace(string(out)), nil
}

// ghosttyHint turns AppleScript's words into what to do about them.
func ghosttyHint(out string) string {
	switch {
	case strings.Contains(out, "-1743"), strings.Contains(out, "Not authorized"):
		return "unagit may not control Ghostty; allow it in System Settings › Privacy & Security › Automation"
	case strings.Contains(out, "Can’t continue new tab"), strings.Contains(out, "Can't continue new tab"):
		return "Ghostty would not open a tab; tabs may be turned off in its configuration - open a window instead"
	}
	return out
}

func (c *Client) openGhostty(where Placement, dir string, args []string) (string, error) {
	place, here := "", ""
	switch where {
	case Window:
		place = "window"
	case Tab:
		place = "tab"
	case Vertical, Horizontal:
		if c.SourcePane == "" || c.SourcePane == ghosttyFindSelf {
			return "", fmt.Errorf("a Ghostty split opens beside unagit; run unagit in Ghostty")
		}
		place, here = "right", c.SourcePane
		if where == Horizontal {
			place = "down"
		}
	default:
		return "", fmt.Errorf("unknown placement; choose a window, a tab or a split")
	}
	closeFile, err := os.CreateTemp("", "unagit-ghostty-")
	if err != nil {
		return "", err
	}
	closeFile.Close()
	value, err := launchValue(launch{Dir: dir, Args: args, CloseGhostty: closeFile.Name(), Osascript: c.Binary})
	if err == nil {
		var line string
		line, err = launchLine(c.Self, false)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var id string
			id, err = c.osascript(ctx, ghosttyOpenScript, dir, line, LaunchVariable+"="+value, place, here)
			if err == nil {
				// The launcher reads it once its program ends, to close
				// the terminal Ghostty would otherwise keep.
				err = os.WriteFile(closeFile.Name(), []byte(id), 0o600)
				if err == nil {
					return id, nil
				}
			}
		}
	}
	os.Remove(closeFile.Name())
	return "", err
}

func (c *Client) focusGhostty(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.osascript(ctx, ghosttyFocusScript, id)
	return err
}

func (c Connection) ghosttyPanes() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := c.osascript(ctx, ghosttyListScript)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, id := range strings.Fields(out) {
		live[id] = true
	}
	return live, nil
}

// CanSplit reports whether unagit runs in a Ghostty terminal it can find.
func (c *Client) CanSplit() bool { return c.Kind != Ghostty || c.SourcePane != "" }

// FindSelf finds unagit's own Ghostty terminal by the title it has just
// given it, and returns a client whose splits open beside that terminal.
// Ghostty takes the new title in a moment, so it is asked a few times.
func (c *Client) FindSelf(title string) (*Client, error) {
	if c.Kind != Ghostty || c.SourcePane != ghosttyFindSelf {
		return c, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		id, err := c.osascript(ctx, ghosttyNamedScript, title)
		if err == nil && id != "" {
			found := *c
			found.SourcePane = id
			return &found, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cannot find unagit's own Ghostty terminal; open a window instead")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
