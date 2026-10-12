//go:build integration

package testenv

import (
	"os/exec"
	"testing"
)

// The integration suite runs the programs unagit drives, in its image
// (make integration): these join the tools on PATH, the rest stays away.
func init() {
	Tools = append(Tools, "nvim", "incomm", "zellij", "herdr")
}

// Require is where a program is. Missing, it fails the test rather than
// skipping it: a suite that skipped what it is for would pass on nothing.
func Require(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is not installed; the integration suite runs in its image: make integration", name)
	}
	return path
}
