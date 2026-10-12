package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

// TestEveryPackageRunsInTheTestEnvironment: a package whose tests skip
// Isolate sees the machine's git, home and programs again.
func TestEveryPackageRunsInTheTestEnvironment(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "..", "..")
	tests := map[string]bool{}
	isolated := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir, _ := filepath.Rel(root, filepath.Dir(path))
		tests[dir] = true
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "testenv.Main(m)") || strings.Contains(string(b), "testenv.Isolate()") ||
			dir == filepath.Join("internal", "testenv") {
			isolated[dir] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for dir := range tests {
		if !isolated[dir] {
			t.Errorf("%s has tests but no TestMain through testenv", dir)
		}
	}
}

// TestTheMachineIsOutOfReach: the home, git's configuration and PATH are
// the run's own, and only the listed tools are on PATH.
func TestTheMachineIsOutOfReach(t *testing.T) {
	temp := os.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "GIT_CONFIG_GLOBAL", "UNAGIT_CONFIG_DIR", "PATH"} {
		if value := os.Getenv(key); !strings.HasPrefix(value, temp) {
			t.Errorf("%s = %q, outside the run's directory %s", key, value, temp)
		}
	}
	if os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" {
		t.Error("git still reads the system's configuration")
	}
	for _, name := range []string{"nvim", "chezmoi", "zoxide", "osascript", "open", "security"} {
		if path, err := exec.LookPath(name); err == nil {
			t.Errorf("%s is on PATH at %s", name, path)
		}
	}
	out, err := exec.Command("git", "config", "--get", "user.email").Output()
	if err != nil || strings.TrimSpace(string(out)) != "tests@unagit.invalid" {
		t.Errorf("git's identity is %q (%v), not the tests'", out, err)
	}
}
