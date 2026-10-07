// Package editors knows the editors unagit can open a directory in, finds
// which of them this machine has, and builds the command that opens one.
//
// There are two kinds, and the difference matters to the caller. A terminal
// editor (nvim) takes over the terminal until it exits, so unagit suspends
// itself and waits. A window editor (IntelliJ IDEA, VS Code, Zed) is started
// by a launcher that returns at once while the editor runs on its own, so
// unagit only starts it and carries on.
package editors

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The editors by id; the id is what the configuration stores.
const (
	Nvim   = "nvim"
	Idea   = "idea"
	Code   = "code"
	Zed    = "zed"
	Custom = "custom"
)

// Editor is one editor and whether, and how, it can be started here.
type Editor struct {
	ID       string
	Name     string
	Terminal bool
	// Found says it can be started; Where is what was found - a launcher on
	// PATH or an application - for the settings to show.
	Found bool
	Where string

	argv    []string // the command before the directory
	dirArg  bool     // the directory is passed as an argument, not only as the working directory
	appPath string   // a macOS application, opened with open -a
}

// CustomSpec is the editor the user described by hand.
type CustomSpec struct {
	Command  string
	Args     []string
	Terminal bool
}

// known are the editors unagit recognises, in the order they are listed.
var known = []struct {
	id, name string
	terminal bool
	bins     []string // launchers to look for on PATH
	apps     []string // macOS application bundles, when the launcher is not installed
}{
	{Nvim, "Neovim", true, []string{"nvim"}, nil},
	{Idea, "IntelliJ IDEA", false, []string{"idea"},
		[]string{"IntelliJ IDEA.app", "IntelliJ IDEA Ultimate.app", "IntelliJ IDEA CE.app"}},
	{Code, "VS Code", false, []string{"code"}, []string{"Visual Studio Code.app"}},
	{Zed, "Zed", false, []string{"zed", "zeditor"}, []string{"Zed.app"}},
}

// lookPath finds a launcher; the tests replace it.
var lookPath = exec.LookPath

// AppDirs are where macOS applications are looked for. Tests elsewhere
// replace it, so that what is installed on the machine running them does not
// decide what they see.
var AppDirs = func() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	home, _ := os.UserHomeDir()
	return []string{"/Applications", filepath.Join(home, "Applications"),
		filepath.Join(home, "Applications", "JetBrains Toolbox")}
}

// ScriptDirs are where launchers are looked for when they are not on PATH:
// JetBrains Toolbox writes its shell scripts (idea, ...) there, and only
// puts them on PATH if asked to. A script is better than the application,
// because it opens the folder in the IDE Toolbox keeps up to date. Tests
// replace it, like AppDirs.
var ScriptDirs = func() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "JetBrains", "Toolbox", "scripts")}
	case "linux":
		return []string{filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "scripts")}
	}
	return nil
}

// findLauncher looks for a launcher on PATH, then in ScriptDirs.
func findLauncher(name string) (string, bool) {
	if path, err := lookPath(name); err == nil {
		return path, true
	}
	for _, dir := range ScriptDirs() {
		path := filepath.Join(dir, name)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return path, true
		}
	}
	return "", false
}

// Detect lists every editor unagit knows, found or not, and the custom one
// when it has a command.
func Detect(custom CustomSpec) []Editor {
	var out []Editor
	for _, k := range known {
		e := Editor{ID: k.id, Name: k.name, Terminal: k.terminal}
		for _, bin := range k.bins {
			if path, ok := findLauncher(bin); ok {
				e.Found, e.Where, e.argv = true, path, []string{path}
				break
			}
		}
		if !e.Found {
			for _, dir := range AppDirs() {
				for _, app := range k.apps {
					path := filepath.Join(dir, app)
					if _, err := os.Stat(path); err == nil && !e.Found {
						e.Found, e.Where, e.appPath = true, path, path
					}
				}
			}
		}
		// nvim reads the directory it is started in; the window editors are
		// told which folder to open.
		if e.Found && k.terminal {
			e.argv = append(e.argv, ".")
		}
		e.dirArg = !k.terminal
		out = append(out, e)
	}
	if strings.TrimSpace(custom.Command) != "" {
		e := Editor{ID: Custom, Name: "Custom: " + custom.Command, Terminal: custom.Terminal}
		if path, err := lookPath(custom.Command); err == nil {
			e.Found, e.Where = true, path
			e.argv = append([]string{path}, custom.Args...)
			if len(custom.Args) == 0 {
				// What unagit always did: the editor opens the directory it
				// is started in.
				e.argv = append(e.argv, ".")
			}
		}
		out = append(out, e)
	}
	return out
}

// Pick returns the editor with the id, from a detected list.
func Pick(all []Editor, id string) (Editor, bool) {
	for _, e := range all {
		if e.ID == id {
			return e, true
		}
	}
	return Editor{}, false
}

// Favourite is the editor to open with when nobody asked for another: the
// chosen one, when there is one and this machine has it. There is no
// fallback to some other editor - without a usable favourite the caller asks,
// the same as when another editor was asked for.
func Favourite(all []Editor, id string) (Editor, bool) {
	if e, ok := Pick(all, id); ok && e.Found {
		return e, true
	}
	return Editor{}, false
}

// Command is what opens dir in the editor. A terminal editor's command has
// to be run with the terminal attached; a window editor's only started. One
// that was not found has no command at all - looking its name up again on
// PATH could start something else.
func (e Editor) Command(dir string) (*exec.Cmd, error) {
	if !e.Found {
		return nil, fmt.Errorf("%s is not installed - pick another editor in Settings › Integrations", e.Name)
	}
	var cmd *exec.Cmd
	switch {
	case e.appPath != "":
		cmd = exec.Command("open", "-a", e.appPath, dir)
	case e.dirArg:
		cmd = exec.Command(e.argv[0], append(e.argv[1:], dir)...)
	default:
		cmd = exec.Command(e.argv[0], e.argv[1:]...)
	}
	cmd.Dir = dir
	return cmd, nil
}

// CommandAt opens a chosen file while keeping the repository as the editor's
// working directory. Window editors also get the folder as their project.
func (e Editor) CommandAt(dir, file string) (*exec.Cmd, error) {
	if file == "" {
		return e.Command(dir)
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	cmd, err := e.Command(dir)
	if err != nil {
		return nil, err
	}
	switch {
	case e.appPath != "":
		cmd.Args = append(cmd.Args, file)
	case e.ID == Nvim:
		args := append([]string(nil), e.argv[1:]...)
		if len(args) > 0 && args[len(args)-1] == "." {
			args = args[:len(args)-1]
		}
		cmd.Args = append([]string{e.argv[0]}, append(args, file)...)
	case e.ID == Idea:
		cmd.Args = append(cmd.Args, "--line", "1", file)
	default:
		cmd.Args = append(cmd.Args, file)
	}
	return cmd, nil
}
