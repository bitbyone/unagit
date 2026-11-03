//go:build windows

package platform

import "os"

// OpenTTY opens the console itself, whatever standard input and output
// are: the one a passphrase is typed on and a picker drawn on while
// standard output is a pipe. Windows keeps its input and its screen apart,
// as CONIN$ and CONOUT$.
func OpenTTY() (in, out *os.File, err error) {
	in, err = os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err = os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}
