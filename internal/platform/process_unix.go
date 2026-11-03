//go:build !windows

package platform

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// Alive says whether a process still runs: signal 0 asks the kernel
// without disturbing it.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// Exec replaces this process with the program at path: what the shell
// runs next is that program, in the same terminal, with this pid.
func Exec(path string, args, env []string) error {
	return syscall.Exec(path, args, env)
}

// IgnoreInterrupts leaves Ctrl-C and Ctrl-\ to the program in front, for
// a process that only waits for it.
func IgnoreInterrupts() {
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)
}

// CrossDevice says whether a rename failed for going from one file system
// to another, which a copy can still do.
func CrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
