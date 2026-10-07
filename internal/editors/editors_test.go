package editors

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeMachine makes Detect see only the launchers and applications given.
func fakeMachine(t *testing.T, bins map[string]string, apps ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, app := range apps {
		if err := os.MkdirAll(filepath.Join(dir, app), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scripts := t.TempDir()
	savedLook, savedApps, savedScripts := lookPath, AppDirs, ScriptDirs
	lookPath = func(name string) (string, error) {
		if path, ok := bins[name]; ok {
			return path, nil
		}
		return "", errors.New("not found")
	}
	AppDirs = func() []string { return []string{dir} }
	ScriptDirs = func() []string { return []string{scripts} }
	t.Cleanup(func() { lookPath, AppDirs, ScriptDirs = savedLook, savedApps, savedScripts })
}

// TestDetectFindsToolboxScripts: JetBrains Toolbox's idea script is found
// where Toolbox keeps it even when it is not on PATH, and it is preferred to
// the application.
func TestDetectFindsToolboxScripts(t *testing.T) {
	fakeMachine(t, nil, "IntelliJ IDEA.app")
	script := filepath.Join(ScriptDirs()[0], "idea")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e, _ := Pick(Detect(CustomSpec{}), Idea)
	if !e.Found || e.Where != script {
		t.Fatalf("idea = %+v, want the Toolbox script", e)
	}
	cmd, err := e.Command("/work/app")
	if err != nil || cmd.Path != script || strings.Join(cmd.Args[1:], " ") != "/work/app" {
		t.Errorf("command = %v %v", cmd, err)
	}
}

func TestDetectFindsLaunchersAndApplications(t *testing.T) {
	fakeMachine(t, map[string]string{"nvim": "/bin/nvim", "zeditor": "/bin/zeditor"}, "IntelliJ IDEA CE.app")
	all := Detect(CustomSpec{})
	got := map[string]bool{}
	for _, e := range all {
		got[e.ID] = e.Found
	}
	want := map[string]bool{Nvim: true, Idea: true, Code: false, Zed: true}
	for id, found := range want {
		if got[id] != found {
			t.Errorf("%s found = %v, want %v", id, got[id], found)
		}
	}
	if _, ok := Pick(all, Custom); ok {
		t.Error("a custom editor is listed without a command")
	}
}

// TestCommandOpensTheDirectory: nvim is started in the directory, a window
// editor is told which one, and an application goes through open -a.
func TestCommandOpensTheDirectory(t *testing.T) {
	fakeMachine(t, map[string]string{"nvim": "/bin/nvim", "code": "/bin/code", "hx": "/bin/hx"}, "Zed.app")
	all := Detect(CustomSpec{Command: "hx", Args: []string{"-c", "x"}, Terminal: true})
	dir := "/work/app"
	for id, want := range map[string]string{
		Nvim:   "/bin/nvim .",
		Code:   "/bin/code /work/app",
		Zed:    "open -a " + filepath.Join(AppDirs()[0], "Zed.app") + " /work/app",
		Custom: "/bin/hx -c x",
	} {
		e, _ := Pick(all, id)
		cmd, err := e.Command(dir)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		args := append([]string{filepath.Base(cmd.Path)}, cmd.Args[1:]...)
		if cmd.Path == "/bin/nvim" || cmd.Path == "/bin/code" || cmd.Path == "/bin/hx" {
			args[0] = cmd.Path
		}
		if got := strings.Join(args, " "); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
		if cmd.Dir != dir {
			t.Errorf("%s runs in %q", id, cmd.Dir)
		}
	}
	if e, _ := Pick(all, Code); e.Terminal {
		t.Error("VS Code is not a terminal editor")
	}
	if e, _ := Pick(all, Custom); !e.Terminal {
		t.Error("the custom editor was said to be a terminal one")
	}
}

// TestFavouriteIsOnlyTheChosenOne: no favourite, or one that is not
// installed, is no favourite - the caller then asks rather than quietly
// opening something else.
func TestFavouriteIsOnlyTheChosenOne(t *testing.T) {
	fakeMachine(t, map[string]string{"code": "/bin/code"})
	all := Detect(CustomSpec{})
	if e, ok := Favourite(all, Code); !ok || e.ID != Code {
		t.Errorf("favourite = %+v, %v; want VS Code", e, ok)
	}
	for _, id := range []string{"", Zed} {
		if e, ok := Favourite(all, id); ok {
			t.Errorf("favourite %q gave %s", id, e.ID)
		}
	}
}

// TestCommandOfAMissingEditorIsRefused: nothing is looked up again, so a
// launcher that detection did not choose can never be started.
func TestCommandOfAMissingEditorIsRefused(t *testing.T) {
	fakeMachine(t, nil)
	e, _ := Pick(Detect(CustomSpec{}), Zed)
	if cmd, err := e.Command(t.TempDir()); err == nil || cmd != nil {
		t.Errorf("an editor that is not installed has a command: %v", cmd)
	}
}

func TestCommandAtKeepsTheProjectAndOpensTheFile(t *testing.T) {
	fakeMachine(t, map[string]string{"nvim": "/bin/nvim", "idea": "/bin/idea", "code": "/bin/code", "zed": "/bin/zed", "hx": "/bin/hx"})
	all := Detect(CustomSpec{Command: "hx", Args: []string{"--clean"}, Terminal: true})
	dir, file := "/work/app", "/work/app/a file's.go"
	for id, want := range map[string][]string{
		Nvim:   {"/bin/nvim", file},
		Idea:   {"/bin/idea", dir, "--line", "1", file},
		Code:   {"/bin/code", dir, file},
		Zed:    {"/bin/zed", dir, file},
		Custom: {"/bin/hx", "--clean", file},
	} {
		e, _ := Pick(all, id)
		cmd, err := e.CommandAt(dir, "a file's.go")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cmd.Args, want) || cmd.Dir != dir {
			t.Errorf("%s: %v in %s, want %v in %s", id, cmd.Args, cmd.Dir, want, dir)
		}
	}
	missing := Editor{Name: "missing"}
	if cmd, err := missing.CommandAt(dir, file); err == nil || cmd != nil {
		t.Fatal("missing editor can open a file")
	}
}

func TestCommandAtUsesTheMacApplication(t *testing.T) {
	fakeMachine(t, nil, "IntelliJ IDEA.app", "Visual Studio Code.app", "Zed.app")
	for _, id := range []string{Idea, Code, Zed} {
		e, _ := Pick(Detect(CustomSpec{}), id)
		cmd, err := e.CommandAt("/work/app", "/work/app/a.go")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"open", "-a", e.appPath, "/work/app", "/work/app/a.go"}
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Errorf("%s: %v, want %v", id, cmd.Args, want)
		}
	}
}
