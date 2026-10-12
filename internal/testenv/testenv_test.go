package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

// executableWrite is an os.WriteFile of a file with the owner's execute
// bit, which on Linux can leave the program "text file busy".
var executableWrite = regexp.MustCompile(`os\.WriteFile\([^\n]*(,\n[^\n]*)?, 0o?7[0-7][0-7]\)`)

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
		if executableWrite.Match(b) {
			t.Errorf("%s writes a program with os.WriteFile; use testenv.WriteExecutable", path)
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
		if slices.Contains(Tools, name) {
			continue // the integration suite's
		}
		if path, err := exec.LookPath(name); err == nil {
			t.Errorf("%s is on PATH at %s", name, path)
		}
	}
	out, err := exec.Command("git", "config", "--get", "user.email").Output()
	if err != nil || strings.TrimSpace(string(out)) != "tests@unagit.invalid" {
		t.Errorf("git's identity is %q (%v), not the tests'", out, err)
	}
}

// TestGitVersionsAreRead: the versions git and its vendors print, against
// the oldest the tests run with.
func TestGitVersionsAreRead(t *testing.T) {
	for out, want := range map[string]bool{
		"git version 2.54.0 (Apple Git-157)\n": true,
		"git version 2.39.5\n":                 true,
		"git version 2.38.0":                   true,
		"git version 2.37.9":                   false,
		"git version 1.99.0":                   false,
		"git version 3.0.0":                    true,
		"something else":                       false,
	} {
		if _, got := gitAtLeast(out, [2]int{2, 38}); got != want {
			t.Errorf("gitAtLeast(%q) = %v, want %v", out, got, want)
		}
	}
}
