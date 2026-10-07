// Package editortest supplies a Neovim stand-in for tests of the UI and CLI.
package editortest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Install keeps each fake editor and its processes owned by this test.
// Callers are serial because PATH is process state.
func Install(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "nvim")
	_, here, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(here), "..", "editors", "testdata", "fake_nvim.go")
	if out, err := exec.Command("go", "build", "-o", bin, source).CombinedOutput(); err != nil {
		t.Fatalf("build fake nvim: %v: %s", err, out)
	}
	log := filepath.Join(dir, "opened")
	t.Setenv("UNAGIT_FAKE_NVIM_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		data, _ := os.ReadFile(log)
		for _, line := range strings.Split(string(data), "\n") {
			if socket, ok := strings.CutPrefix(line, "start|"); ok && socket != "" {
				exec.Command(bin, "--server", socket, "--remote-expr", "execute('qa!')").Run()
			}
		}
	})
	return bin, log
}

// ShortDir leaves room for a Unix socket even on macOS's long temp root.
func ShortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ug-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
