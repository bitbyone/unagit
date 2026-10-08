// Package mux opens terminal programs beside their caller: in Zellij, in
// herdr, or in Ghostty. A connection names the session explicitly so its
// panes can be found from another shell.
package mux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type Placement int

const (
	Tab Placement = iota
	Vertical
	Horizontal
	// Window is a place of its own: a Ghostty window, a herdr workspace.
	Window
)

// The kinds of place a program can be opened in.
const (
	Zellij  = "zellij"
	Herdr   = "herdr"
	Ghostty = "ghostty"
)

// Connection is also the identity used when reading a session's panes once
// for several editor records. Binary is the launcher that was detected;
// Session is Zellij's session name, herdr's socket, and nothing for
// Ghostty, which has one of everything.
type Connection struct{ Kind, Binary, Session string }

type Client struct {
	Connection
	// SourcePane is where unagit itself runs, when it runs there: splits
	// open beside it.
	SourcePane string
	// Workspace is the herdr workspace unagit runs in, where tabs open.
	Workspace string
	// Self is the unagit binary, which herdr and Ghostty start to run the
	// command: they take a line of text, not separate arguments.
	Self string
}

// Name is how the kind reads in a sentence.
func (c Connection) Name() string {
	switch c.Kind {
	case Zellij:
		return "Zellij"
	case Ghostty:
		return "Ghostty"
	}
	return c.Kind
}

// Detect finds the multiplexer unagit runs in: Zellij, or herdr when herdr
// may be used.
func Detect(getenv func(string) string, lookPath func(string) (string, error), herdr bool) *Client {
	if getenv("ZELLIJ") != "" && getenv("ZELLIJ_SESSION_NAME") != "" {
		bin, err := lookPath(Zellij)
		if err != nil {
			return nil
		}
		return &Client{Connection: Connection{Kind: Zellij, Binary: bin, Session: getenv("ZELLIJ_SESSION_NAME")}, SourcePane: getenv("ZELLIJ_PANE_ID")}
	}
	if herdr && getenv("HERDR_ENV") == "1" && getenv("HERDR_PANE_ID") != "" && getenv("HERDR_WORKSPACE_ID") != "" {
		return DetectHerdr(getenv, lookPath)
	}
	return nil
}

// DetectHerdr connects to the herdr server from wherever unagit runs: from
// one of its panes, the server of that pane, with tabs and splits beside
// unagit; from anywhere else, the user's usual server, where only a
// workspace of its own can be opened.
func DetectHerdr(getenv func(string) string, lookPath func(string) (string, error)) *Client {
	bin, err := lookPath(Herdr)
	if err != nil {
		return nil
	}
	socket := getenv("HERDR_SOCKET_PATH")
	if socket == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		socket = filepath.Join(home, ".config", "herdr", "herdr.sock")
	}
	c := &Client{Connection: Connection{Kind: Herdr, Binary: bin, Session: socket}, Self: self()}
	if getenv("HERDR_ENV") == "1" && getenv("HERDR_SOCKET_PATH") == socket {
		c.SourcePane, c.Workspace = getenv("HERDR_PANE_ID"), getenv("HERDR_WORKSPACE_ID")
	}
	return c
}

// DetectGhostty finds Ghostty on a Mac, which is scripted through
// AppleScript. here is whether unagit runs in a Ghostty terminal of its
// own, which a split can then be made of.
func DetectGhostty(getenv func(string) string, lookPath func(string) (string, error), here bool) *Client {
	if runtime.GOOS != "darwin" || !ghosttyInstalled(getenv) {
		return nil
	}
	bin, err := lookPath("osascript")
	if err != nil {
		return nil
	}
	c := &Client{Connection: Connection{Kind: Ghostty, Binary: bin}, Self: self()}
	if here {
		// Found when a split is asked for: Ghostty knows its terminals by
		// ids unagit cannot see from inside one.
		c.SourcePane = ghosttyFindSelf
	}
	return c
}

func ghosttyInstalled(getenv func(string) string) bool {
	if getenv("TERM_PROGRAM") == "ghostty" {
		return true
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
		if _, err := os.Stat(filepath.Join(dir, "Ghostty.app")); err == nil {
			return true
		}
	}
	return false
}

// GhosttyApp is where Ghostty is installed, for saying so in Settings.
func GhosttyApp() string {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications")} {
		app := filepath.Join(dir, "Ghostty.app")
		if _, err := os.Stat(app); err == nil {
			return app
		}
	}
	return ""
}

func self() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

// Places are where this client can open something.
func (c *Client) Places() []Placement {
	switch c.Kind {
	case Zellij:
		return []Placement{Tab, Vertical, Horizontal}
	case Herdr:
		if c.SourcePane == "" {
			return []Placement{Window}
		}
		return []Placement{Tab, Vertical, Horizontal, Window}
	case Ghostty:
		if c.SourcePane == "" {
			return []Placement{Window, Tab}
		}
		return []Placement{Window, Tab, Vertical, Horizontal}
	}
	return nil
}

// Open runs command in dir in a new place of this kind, brings it forward,
// and returns the pane it runs in.
func (c *Client) Open(where Placement, dir, name string, command *exec.Cmd) (string, error) {
	switch c.Kind {
	case Zellij:
		return c.openZellij(where, dir, name, command)
	case Herdr:
		return c.openHerdr(where, dir, name, command.Args)
	case Ghostty:
		return c.openGhostty(where, dir, command.Args)
	}
	return "", fmt.Errorf("unknown multiplexer; open it again from Zellij, herdr or Ghostty")
}

// Focus brings one of the session's panes to the front: the editor already
// open in a directory, rather than a second one fighting over its files.
func (c *Client) Focus(paneID string) error {
	switch c.Kind {
	case Zellij:
		return c.focusZellij(paneID)
	case Herdr:
		return c.focusHerdr(paneID)
	case Ghostty:
		return c.focusGhostty(paneID)
	}
	return fmt.Errorf("unknown multiplexer; go to the editor yourself")
}

// Panes are the live panes of the session, by the ids Open returns. An
// absent session has none; a server that cannot be asked is an error, so
// records are kept for a later look rather than swept.
func (c Connection) Panes() (map[string]bool, error) {
	switch c.Kind {
	case Zellij:
		return c.zellijPanes()
	case Herdr:
		return c.herdrPanes()
	case Ghostty:
		return c.ghosttyPanes()
	}
	return nil, fmt.Errorf("unknown multiplexer; open the editor again")
}
