//go:build windows

package platform

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Replace puts from in to's place in one step: a reader sees the old file
// or the new one, never half of either. Windows refuses to replace a file
// while another program - or another unagit - has it open for a moment,
// so it tries again for a while before giving up.
func Replace(from, to string) error {
	var err error
	for wait := time.Millisecond; wait < time.Second; wait *= 2 {
		if err = os.Rename(from, to); err == nil || !busy(err) {
			return err
		}
		time.Sleep(wait)
	}
	return err
}

// busy is a file another process has open.
func busy(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// SyncPath makes what was written to a file durable. Windows flushes only
// a file opened for writing, and no directory: NTFS keeps its entries in
// its journal. A file it will not open for writing - one git made read-only
// - is left to the system's cache, which writes it all the same.
func SyncPath(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if busy(err) {
			return nil
		}
		return err
	}
	err = f.Sync()
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// RemoveAll removes a directory and what is in it. Windows refuses while
// another process has a file in it open or its directory as its own - a
// git that unagit's background read started a moment before - so it tries
// again for a few seconds before giving up.
func RemoveAll(path string) error {
	var err error
	for wait := 10 * time.Millisecond; wait < 4*time.Second; wait *= 2 {
		if err = os.RemoveAll(path); err == nil || !busy(err) {
			return err
		}
		time.Sleep(wait)
	}
	return err
}
