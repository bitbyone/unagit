//go:build !windows

package platform

import "os"

// Replace puts from in to's place in one step: a reader sees the old file
// or the new one, never half of either.
func Replace(from, to string) error { return os.Rename(from, to) }

// SyncPath makes what was written to a file or a directory durable: a
// directory's entries too, so a rename survives a crash.
func SyncPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// RemoveAll removes a directory and what is in it; Unix removes a file
// another process has open, so there is nothing to wait for.
func RemoveAll(path string) error { return os.RemoveAll(path) }
