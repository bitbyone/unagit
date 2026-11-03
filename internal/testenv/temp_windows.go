//go:build windows

package testenv

import (
	"os"

	"golang.org/x/sys/windows"
)

// longTemp is the temporary directory by its long name. A Windows account
// with a long name gets its temporary directory as an 8.3 short name
// (C:\Users\RUNNER~1\...), and git answers with the long one, so the same
// directory would read as two to every comparison.
func longTemp() string {
	dir := os.TempDir()
	short, err := windows.UTF16FromString(dir)
	if err != nil {
		return ""
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(&short[0], &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
