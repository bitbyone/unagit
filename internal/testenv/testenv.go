// Package testenv gives a test binary an environment of its own, so that what
// is installed and configured on the machine running the tests can neither
// change what they see nor be changed by them.
//
// Every package with tests runs through it (TestEveryPackageRunsInTheTestEnvironment
// checks that). It touches no file of the machine: it changes this process's
// environment only, for the length of the run, and everything it points to is
// in a temporary directory removed at the end.
package testenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// Tools are the programs the tests may find on PATH: git, which is what
// unagit drives and is not faked, go, which builds the stand-ins, and the
// shell with the utilities the stand-in scripts are written in. Everything
// else - an editor, chezmoi, a multiplexer, the system's notifier or browser
// opener - is absent unless a test puts a stand-in of its own on PATH. A
// tool really run belongs in the integration suite.
var Tools = []string{
	"git", "go",
	"sh", "cat", "printf", "echo", "sleep", "true", "false", "env",
	"mkdir", "rm", "mv", "cp", "ln", "touch", "chmod", "ls", "pwd",
	"head", "tail", "grep", "sed", "tr", "cut", "wc", "sort", "tee",
	"dirname", "basename", "readlink", "date", "kill", "du", "find",
}

// Main runs a package's tests in an environment of their own.
func Main(m *testing.M) {
	cleanup := Isolate()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// Isolate points this process at a throwaway home, git configuration and
// PATH, and returns what removes them. For a package whose TestMain does
// more than Main.
func Isolate() func() {
	root, err := os.MkdirTemp("", "unagit-test-")
	if err != nil {
		fail(err)
	}
	// Go keeps its caches and settings under the home: without them a test
	// that builds a stand-in would compile the world, or fetch it.
	// The C compiler is found on PATH too, and without it go quietly builds
	// without cgo - and without the keychain - so it is named by its path.
	goKeys := []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV", "GOTOOLCHAIN", "CGO_ENABLED", "CC", "CXX"}
	goEnv := map[string]string{}
	if out, err := exec.Command("go", append([]string{"env"}, goKeys...)...).Output(); err == nil {
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		for i, key := range goKeys {
			if i < len(lines) && lines[i] != "" {
				goEnv[key] = lines[i]
			}
		}
	}
	// Only the keychain is cgo, and only on macOS. Elsewhere the compiler
	// would look for its assembler on a PATH that has none.
	if runtime.GOOS != "darwin" {
		goEnv["CGO_ENABLED"] = "0"
	}
	for _, key := range []string{"CC", "CXX"} {
		if path, err := exec.LookPath(goEnv[key]); err == nil {
			goEnv[key] = path
		} else {
			delete(goEnv, key)
		}
	}
	bin := filepath.Join(root, "bin")
	must(os.MkdirAll(bin, 0o700))
	for _, tool := range Tools {
		if found, err := exec.LookPath(tool); err == nil {
			must(os.Symlink(found, filepath.Join(bin, tool)))
		}
	}

	// Nothing of git's from the machine: no identity, signing, hooks,
	// aliases or a repository the tests were started inside of.
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			os.Unsetenv(name)
		}
	}
	gitconfig := filepath.Join(root, "gitconfig")
	must(os.WriteFile(gitconfig, []byte(`[user]
	name = unagit tests
	email = tests@unagit.invalid
[init]
	defaultBranch = main
[commit]
	gpgsign = false
[tag]
	gpgsign = false
[advice]
	detachedHead = false
`), 0o600))

	home := filepath.Join(root, "home")
	set := map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, ".config"),
		"XDG_DATA_HOME":       filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":      filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME":      filepath.Join(home, ".cache"),
		"UNAGIT_CONFIG_DIR":   filepath.Join(root, "unagit"),
		"_ZO_DATA_DIR":        filepath.Join(root, "zoxide"),
		"PATH":                bin,
		"GIT_CONFIG_GLOBAL":   gitconfig,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
	}
	for key, value := range goEnv {
		set[key] = value
	}
	for key, value := range set {
		os.Setenv(key, value)
	}
	must(os.MkdirAll(home, 0o700))
	// Nor the terminal, multiplexer or agent the tests were started in:
	// what unagit does under one is a test's choice.
	for _, name := range []string{
		"SSH_AUTH_SOCK", "TMUX", "ZELLIJ", "ZELLIJ_SESSION_NAME", "ZELLIJ_PANE_ID",
		"HERDR_ENV", "HERDR_PANE_ID", "HERDR_SOCKET", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
		"GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR", "KITTY_WINDOW_ID", "WEZTERM_PANE",
		"VISUAL", "EDITOR", "BROWSER", "NO_COLOR",
	} {
		os.Unsetenv(name)
	}
	return func() { os.RemoveAll(root) }
}

func must(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "testenv:", err)
	os.Exit(1)
}

// WriteExecutable writes a stand-in program. A plain os.WriteFile followed by
// running the file fails now and then on Linux with "text file busy": a
// child another test forks while the file is still open for writing keeps
// that descriptor until it execs. Holding the fork lock while the file is
// open lets no child be forked with it.
func WriteExecutable(path string, content []byte) error {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	return os.WriteFile(path, content, 0o755)
}
