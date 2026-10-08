package mux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// LaunchVariable carries what `unagit launch` runs. herdr types a line into
// a shell and Ghostty takes a command line, and the user's shell may be fish
// and a path may hold anything: so the line is always the same, the binary
// and one word, and the command travels whole in the environment.
const LaunchVariable = "UNAGIT_LAUNCH"

type launch struct {
	Dir  string   `json:"dir"`
	Args []string `json:"args"`
	// CloseGhostty names a file that will hold the Ghostty terminal the
	// command runs in. Ghostty keeps a terminal whose command has ended
	// open until a key is pressed; the launcher closes it instead.
	CloseGhostty string `json:"close_ghostty,omitempty"`
	Osascript    string `json:"osascript,omitempty"`
}

func launchValue(l launch) (string, error) {
	if len(l.Args) == 0 {
		return "", fmt.Errorf("nothing to run; choose an editor or an agent")
	}
	b, err := json.Marshal(l)
	return string(b), err
}

// launchLine is what a shell or Ghostty is given to run: the unagit binary,
// quoted the one way sh, zsh, bash and fish all read alike.
func launchLine(self string, exec bool) (string, error) {
	if self == "" {
		return "", fmt.Errorf("cannot tell where the unagit binary is; start unagit by its full path")
	}
	if strings.ContainsAny(self, "'\\\n") {
		return "", fmt.Errorf("the unagit binary's path %q has a quote or a backslash; move it to a plainer one", self)
	}
	line := "'" + self + "' launch"
	if exec {
		line = "exec " + line
	}
	return line, nil
}

// RunLaunch is `unagit launch`: it runs what LaunchVariable says, in its
// directory. It replaces itself with the command, so the pane closes when
// the command ends - except in Ghostty, where it waits and closes the
// terminal itself.
func RunLaunch() error {
	value := os.Getenv(LaunchVariable)
	if value == "" {
		return fmt.Errorf("unagit launch runs what unagit opens in a pane; it is not for typing")
	}
	var l launch
	if err := json.Unmarshal([]byte(value), &l); err != nil || len(l.Args) == 0 {
		return fmt.Errorf("unagit launch was given nothing it can run; open it again from unagit")
	}
	os.Unsetenv(LaunchVariable)
	if l.Dir != "" {
		if err := os.Chdir(l.Dir); err != nil {
			return fmt.Errorf("cannot go to %s: %w", l.Dir, err)
		}
	}
	bin, err := exec.LookPath(l.Args[0])
	if err != nil {
		return fmt.Errorf("%s is not installed or not on PATH: %w", l.Args[0], err)
	}
	if l.CloseGhostty == "" {
		return syscall.Exec(bin, l.Args, os.Environ())
	}
	cmd := exec.Command(bin, l.Args[1:]...)
	cmd.Args[0] = l.Args[0]
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// The terminal's keys are the program's: Ctrl-C reaches it, not this
	// launcher waiting for it.
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)
	runErr := cmd.Run()
	closeGhostty(l)
	if runErr != nil {
		if _, exited := runErr.(*exec.ExitError); !exited {
			return runErr
		}
	}
	return nil
}

// closeGhostty closes the terminal unagit opened, once the program in it
// has ended. Its id is written by unagit after Ghostty answers, which a
// very short program can outrun, so it is waited for a little.
func closeGhostty(l launch) {
	defer os.Remove(l.CloseGhostty)
	var id string
	for i := 0; i < 20; i++ {
		b, _ := os.ReadFile(l.CloseGhostty)
		if id = strings.TrimSpace(string(b)); id != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if id == "" || l.Osascript == "" {
		return
	}
	_ = exec.Command(l.Osascript, "-e", ghosttyCloseScript, id).Run()
}
