//go:build !windows

package testenv

// longTemp is the temporary directory by its long name, where it can have
// another; nothing to change here.
func longTemp() string { return "" }
