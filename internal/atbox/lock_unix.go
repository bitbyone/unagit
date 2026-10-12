//go:build !windows

package atbox

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type FileLock struct{ f *os.File }

func LockFile(path string, shared, wait bool) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	op := syscall.LOCK_EX
	if shared {
		op = syscall.LOCK_SH
	}
	if !wait {
		op |= syscall.LOCK_NB
	}
	if err = syscall.Flock(int(f.Fd()), op); err != nil {
		f.Close()
		return nil, fmt.Errorf("resource is busy; wait for the active atbox operation to finish: %w", err)
	}
	return &FileLock{f}, nil
}
func (l *FileLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
func processAlive(pid int) bool { return pid > 0 && (syscall.Kill(pid, 0) == nil) }
