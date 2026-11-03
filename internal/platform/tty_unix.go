//go:build !windows

package platform

import "os"

// OpenTTY opens the terminal itself, whatever standard input and output
// are: the one a passphrase is typed on and a picker drawn on while
// standard output is a pipe. in and out are the same file here.
func OpenTTY() (in, out *os.File, err error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	return f, f, nil
}
