//go:build !darwin && !linux

package credential

import (
	"errors"
	"net"
)

// peerOf cannot tell who is at the other end here, so nobody is answered.
func peerOf(*net.UnixConn) (uid, pid int, err error) {
	return -1, 0, errors.New("the peer of a socket cannot be checked on this platform")
}
