package editors

import (
	"errors"
	"os"
	"path/filepath"
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
	savedLook, savedApps := lookPath, AppDirs
	lookPath = func(name string) (string, error) {
		if path, ok := bins[name]; ok {
			return path, nil
		}
		return "", errors.New("not found")
	}
	AppDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { lookPath, AppDirs = savedLook, savedApps })
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

// TestFavouriteFallsBackToWhatIsThere: an uninstalled favourite must not
// leave every open failing.
func TestFavouriteFallsBackToWhatIsThere(t *testing.T) {
	fakeMachine(t, map[string]string{"code": "/bin/code"})
	all := Detect(CustomSpec{})
	if e, ok := Favourite(all, Zed); !ok || e.ID != Code {
		t.Errorf("favourite = %+v, %v; want VS Code, the one installed", e, ok)
	}
	fakeMachine(t, nil)
	if _, ok := Favourite(Detect(CustomSpec{}), Nvim); ok {
		t.Error("a favourite with nothing installed")
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
