//go:build windows

package platform

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"

	"golang.org/x/sys/windows"
)

// stillActive is what GetExitCodeProcess says of a process still running.
const stillActive = 259

// Alive says whether a process still runs: one that can be opened and has
// no exit code yet.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Denied is a process of someone else's, which runs all the same.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// Exec runs the program at path in this terminal and ends with its exit
// code. Windows cannot put another program in this process's place, so
// unagit stays behind it, waiting, and passes on how it ended.
func Exec(path string, args, env []string) error {
	cmd := exec.Command(path, args[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	IgnoreInterrupts()
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// IgnoreInterrupts leaves Ctrl-C to the program in front, for a process
// that only waits for it.
func IgnoreInterrupts() {
	signal.Ignore(os.Interrupt)
}

// CrossDevice says whether a rename failed for going from one volume to
// another, which a copy can still do.
func CrossDevice(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }
