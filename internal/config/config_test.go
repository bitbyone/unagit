package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)

	cfg := Default()
	cfg.GitLabURL = "https://gitlab.example.com/"
	cfg.RootDir = filepath.Join(dir, "repos")
	cfg.Groups = []Group{{ID: 7, FullPath: "acme/platform", Name: "platform"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.GitLabURL != "https://gitlab.example.com" {
		t.Errorf("trailing slash not trimmed: %q", got.GitLabURL)
	}
	if !got.HasGroup(7) || got.HasGroup(8) {
		t.Errorf("groups = %v", got.Groups)
	}
	if fi, err := os.Stat(Path()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %v (%v)", fi.Mode().Perm(), err)
	}
}

func TestToggleGroup(t *testing.T) {
	cfg := Default()
	g := Group{ID: 1, FullPath: "a"}
	if on := cfg.ToggleGroup(g); !on || len(cfg.Groups) != 1 {
		t.Fatalf("adding failed: on=%v groups=%v", on, cfg.Groups)
	}
	if on := cfg.ToggleGroup(g); on || len(cfg.Groups) != 0 {
		t.Fatalf("removing failed: on=%v groups=%v", on, cfg.Groups)
	}
}

func TestExpand(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := Expand("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("Expand(~/x) = %q", got)
	}
	if got := Expand("/abs/path"); got != "/abs/path" {
		t.Errorf("Expand mangled an absolute path: %q", got)
	}
}

func TestDirHonoursXDG(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := Dir(); got != "/tmp/xdg/unagit" {
		t.Errorf("Dir() = %q", got)
	}
}
