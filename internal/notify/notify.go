// Package notify says something to the user outside unagit's screen: through
// the terminal, which shows it as a desktop notification, or through the
// system's own notifier when the terminal cannot.
package notify

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Modes of Settings › Integrations › Notifications. Auto, the default, is
// the terminal where it can and the system otherwise.
const (
	Auto     = ""
	Terminal = "terminal"
	System   = "system"
	Off      = "off"
)

// Protocol is the escape sequence a terminal shows a notification for.
type Protocol int

const (
	None Protocol = iota
	// OSC777 is rxvt's, taken up by Ghostty, WezTerm and foot.
	OSC777
	// OSC9 is iTerm2's.
	OSC9
	// OSC99 is kitty's.
	OSC99
)

// TerminalInfo is the terminal unagit runs in as far as notifications go.
type TerminalInfo struct {
	Name     string
	Protocol Protocol
	// Tmux is set inside tmux, which passes a sequence on to the terminal
	// only wrapped, and only with allow-passthrough on.
	Tmux bool
	// Muxer names a multiplexer that passes no notification on - Zellij,
	// herdr - so the system's notifier stands in for the terminal's.
	Muxer string
}

// Detect names the terminal from its environment, the way the Nerd Font
// guess does. Inside tmux TERM_PROGRAM is tmux's, so the outer terminal is
// told by what it leaves in the environment of the shells under it.
func Detect(getenv func(string) string) TerminalInfo {
	info := TerminalInfo{Tmux: getenv("TMUX") != ""}
	program := strings.ToLower(getenv("TERM_PROGRAM"))
	term := getenv("TERM")
	switch {
	case program == "ghostty" || getenv("GHOSTTY_RESOURCES_DIR") != "":
		info.Name, info.Protocol = "Ghostty", OSC777
	case program == "wezterm" || getenv("WEZTERM_EXECUTABLE") != "":
		info.Name, info.Protocol = "WezTerm", OSC777
	case program == "iterm.app" || getenv("LC_TERMINAL") == "iTerm2":
		info.Name, info.Protocol = "iTerm2", OSC9
	case term == "xterm-kitty" || getenv("KITTY_WINDOW_ID") != "":
		info.Name, info.Protocol = "kitty", OSC99
	case strings.HasPrefix(term, "foot"):
		info.Name, info.Protocol = "foot", OSC777
	}
	switch {
	case getenv("ZELLIJ") != "":
		info.Muxer = "Zellij"
	case getenv("HERDR_ENV") != "" || getenv("HERDR_PANE_ID") != "":
		info.Muxer = "herdr"
	}
	if info.Muxer != "" {
		info.Protocol = None
	}
	return info
}

// App is the terminal's application as macOS names it, to be told among
// the frontmost: "" where unagit cannot say.
func (t TerminalInfo) App(getenv func(string) string) string {
	if t.Name != "" {
		return t.Name
	}
	if getenv("TERM_PROGRAM") == "Apple_Terminal" {
		return "Terminal"
	}
	return ""
}

// FrontApp is the name of the application in front, on macOS; "" elsewhere
// or when it cannot be told. lsappinfo asks for no permission, unlike
// System Events.
func FrontApp(ctx context.Context) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	front, err := exec.CommandContext(ctx, "lsappinfo", "front").Output()
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "lsappinfo", "info", "-only", "name", strings.TrimSpace(string(front))).Output()
	if err != nil {
		return ""
	}
	// "LSDisplayName"="Ghostty"
	_, name, ok := strings.Cut(strings.TrimSpace(string(out)), "=")
	if !ok {
		return ""
	}
	return strings.Trim(name, `"`)
}

// Sequence is the bytes that make the terminal show title and body, nil
// when it has no way to.
func (t TerminalInfo) Sequence(title, body string) []byte {
	title, body = clean(title), clean(body)
	var seq string
	switch t.Protocol {
	case OSC777:
		// The fields are separated by semicolons, so the title has none.
		seq = "\x1b]777;notify;" + strings.ReplaceAll(title, ";", ",") + ";" + body + "\a"
	case OSC9:
		seq = "\x1b]9;" + title + ": " + body + "\a"
	case OSC99:
		// The title first, not yet shown (d=0), then the body that shows it.
		seq = "\x1b]99;i=1:d=0;" + title + "\x1b\\" + "\x1b]99;i=1:p=body;" + body + "\x1b\\"
	default:
		return nil
	}
	if t.Tmux {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return []byte(seq)
}

// clean keeps what a sequence can carry: no control characters, which
// would end it early or start another.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// SystemCommand is the program the system notifies through here, "" where
// there is none unagit knows.
func SystemCommand() string {
	switch runtime.GOOS {
	case "darwin":
		return "osascript"
	case "linux", "freebsd", "openbsd":
		return "notify-send"
	}
	return ""
}

// SystemNotify shows a notification through the system: Notification
// Centre on macOS, the desktop's notification daemon elsewhere.
func SystemNotify(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch SystemCommand() {
	case "osascript":
		script := fmt.Sprintf("display notification %s with title %s", appleString(body), appleString(title))
		cmd = exec.CommandContext(ctx, "osascript", "-e", script)
	case "notify-send":
		cmd = exec.CommandContext(ctx, "notify-send", "--app-name=unagit", title, body)
	default:
		return fmt.Errorf("this system has no notifier unagit knows; choose the terminal in Settings › Integrations")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v %s", cmd.Path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// appleString quotes s for AppleScript.
func appleString(s string) string {
	s = strings.ReplaceAll(clean(s), `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// Route is where a notification goes: the terminal (its sequence), the
// system, or nowhere. terminalFree is false while an editor has the
// terminal, when a sequence written would land in the editor's output.
// terminalAway is true when the terminal has said it is not in front: a
// terminal shows nothing for a window that is - Ghostty and iTerm2 leave
// that to the program inside - so automatic goes to the terminal only
// then, and to the system whenever the terminal may be in front.
func Route(mode string, t TerminalInfo, terminalFree, terminalAway bool) (useTerminal, useSystem bool) {
	switch mode {
	case Off:
		return false, false
	case Terminal:
		return terminalFree && t.Protocol != None, false
	case System:
		return false, true
	}
	if terminalFree && terminalAway && t.Protocol != None {
		return true, false
	}
	return false, true
}
