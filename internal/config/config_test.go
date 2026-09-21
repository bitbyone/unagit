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

func TestCycleGroupHasThreeStates(t *testing.T) {
	cfg := Default()
	g := Group{ID: 1, FullPath: "a"}

	if got := cfg.CycleGroup(g); got != ScopeGroup || len(cfg.Groups) != 1 {
		t.Fatalf("first press: scope=%q groups=%v", got, cfg.Groups)
	}
	if got := cfg.CycleGroup(g); got != ScopeSubgroups || len(cfg.Groups) != 1 {
		t.Fatalf("second press: scope=%q groups=%v", got, cfg.Groups)
	}
	if got := cfg.CycleGroup(g); got != "" || len(cfg.Groups) != 0 {
		t.Fatalf("third press: scope=%q groups=%v", got, cfg.Groups)
	}
}

func TestGroupOwns(t *testing.T) {
	direct := Group{FullPath: "acme/platform", Scope: ScopeGroup}
	tree := Group{FullPath: "acme/platform", Scope: ScopeSubgroups}
	legacy := Group{FullPath: "acme/platform"} // written before scopes existed

	cases := []struct {
		group Group
		path  string
		want  bool
	}{
		{direct, "acme/platform/api", true},
		{direct, "acme/platform/team/api", false},
		{direct, "acme/other/api", false},
		{direct, "acme/platform", false},
		{tree, "acme/platform/team/api", true},
		{legacy, "acme/platform/team/api", true},
	}
	for _, c := range cases {
		if got := c.group.Owns(c.path); got != c.want {
			t.Errorf("Group{%q, %q}.Owns(%q) = %v", c.group.FullPath, c.group.Scope, c.path, got)
		}
	}
}

func TestLoadDefaultsOldGroupsToSubgroups(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)
	cfg := Default()
	cfg.Groups = []Group{{ID: 1, FullPath: "acme"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupScope(1) != ScopeSubgroups {
		t.Errorf("scope = %q, want %q", got.GroupScope(1), ScopeSubgroups)
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
