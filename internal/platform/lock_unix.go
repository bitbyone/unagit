//go:build !windows

package platform

import (
	"errors"
	"os"
	"syscall"
)

// Lock locks the whole of f for this process alone, waiting for it when
// wait is set and failing with ErrBusy otherwise. The kernel lets it go
// when the process ends, however it ends.
func Lock(f *os.File, wait bool) error {
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	err := syscall.Flock(int(f.Fd()), how)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrBusy
	}
	return err
}

// Unlock lets the lock of f go.
func Unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
