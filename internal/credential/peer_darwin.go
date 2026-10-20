package credential

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerOf is the uid and pid of the process at the other end.
func peerOf(c *net.UnixConn) (uid, pid int, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, 0, err
	}
	var inner error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e != nil {
			inner = e
			return
		}
		uid = int(cred.Uid)
		pid, _ = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if err == nil {
		err = inner
	}
	return uid, pid, err
}
