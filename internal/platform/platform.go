// Package platform is what unagit does differently on each system: locking
// a file, asking whether a process runs, handing the terminal to another
// program, talking to the terminal itself, opening a link and filling the
// clipboard. Everything above it is written once; what a system lacks is
// said here, as an error with what to do, never as a crash.
package platform

import "errors"

// ErrBusy is a lock another process holds, when not waiting for it.
var ErrBusy = errors.New("the lock is held by another process")
