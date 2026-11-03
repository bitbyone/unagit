package testenv

import (
	"path/filepath"
	"runtime"
)

// Abs is a made-up absolute path, written the Unix way, as this system
// writes it: /work/app stays so elsewhere and is C:\work\app on Windows,
// where a path without a volume is not absolute.
func Abs(path string) string {
	if runtime.GOOS == "windows" {
		return `C:` + filepath.FromSlash(path)
	}
	return path
}

// Exe is the name a program built by a test is given, which Windows finds
// only with its extension.
func Exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
