//go:build windows

package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Lock locks the whole of f for this process alone, as flock does
// elsewhere: waiting for it when wait is set, failing with ErrBusy
// otherwise. Windows lets it go when the handle is closed or the process
// ends.
func Lock(f *os.File, wait bool) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return ErrBusy
	}
	return err
}

// Unlock lets the lock of f go.
func Unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}
