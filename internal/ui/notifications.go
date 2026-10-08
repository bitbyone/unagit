package ui

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/tobola/unagit/internal/notify"
)

// The Notifications card: where a watched pipeline's news goes when no
// unagit is in front - the terminal, which shows a desktop notification
// for an escape sequence, or the system's notifier. Its command is the
// system's, which is there on every Mac.

// notificationModes are the modes m goes through, after the default.
var notificationModes = []string{notify.Auto, notify.Terminal, notify.System}

func notificationModeName(mode string) string {
	switch mode {
	case notify.Terminal:
		return "the terminal only"
	case notify.System:
		return "the system only"
	case notify.Off:
		return "off"
	}
	return "the terminal, or the system where it cannot"
}

func (a *App) notificationsCard() *integrationCard {
	cfg := &a.cfg.Integrations
	// passes is tmux's allow-passthrough as last checked: asking runs tmux,
	// which is not for every paint of the card.
	passes := true
	return &integrationCard{
		name: "Notifications", command: notify.SystemCommand(),
		description: "When no unagit is in front, say a watched pipeline's news on the desktop.",
		enabled:     func() bool { return cfg.Notifications != notify.Off },
		toggle: func() {
			if cfg.Notifications == notify.Off {
				cfg.Notifications = notify.Auto
			} else {
				cfg.Notifications = notify.Off
			}
		},
		found: func() string {
			term := notify.Detect(os.Getenv)
			line := "Through " + notificationModeName(cfg.Notifications) + "."
			switch {
			case term.Muxer != "":
				line += " " + term.Muxer + " passes none on; the system shows them."
			case term.Protocol == notify.None:
				line += " This terminal shows none; the system does."
			case term.Tmux && !passes:
				line += " " + term.Name + " under tmux: set allow-passthrough on."
			default:
				line += " " + term.Name + " shows them."
			}
			return line
		},
		check: func() {
			if os.Getenv("TMUX") != "" {
				passes = tmuxPassesThrough()
			}
		},
		keys: "m mode · t test",
		onKey: func(r rune) bool {
			switch r {
			case 'm':
				next := notify.Auto
				for i, m := range notificationModes {
					if m == cfg.Notifications {
						next = notificationModes[(i+1)%len(notificationModes)]
					}
				}
				cfg.Notifications = next
				a.saveConfig()
				return true
			case 't':
				go a.sendNotification(context.Background(), "unagit", "a test notification, as a watched pipeline's would come")
				a.done("sent a test notification")
				return true
			}
			return false
		},
	}
}

// tmuxPassesThrough reports whether tmux passes escape sequences on to the
// terminal, which a notification needs.
func tmuxPassesThrough() bool {
	out, err := exec.Command("tmux", "show", "-gv", "allow-passthrough").Output()
	return err == nil && strings.TrimSpace(string(out)) != "off"
}
