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
	cfg.RootDir = filepath.Join(dir, "repos")
	cfg.AddInstance(Instance{
		Name:   "Work",
		URL:    "https://gitlab.example.com/",
		Groups: []Group{{ID: 7, FullPath: "acme/platform", Name: "platform"}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Instances) != 1 {
		t.Fatalf("instances = %+v", got.Instances)
	}
	inst := got.Instances[0]
	if inst.URL != "https://gitlab.example.com" {
		t.Errorf("trailing slash not trimmed: %q", inst.URL)
	}
	if inst.ID != "gitlab-example-com" {
		t.Errorf("id = %q", inst.ID)
	}
	if !inst.HasGroup(7) || inst.HasGroup(8) {
		t.Errorf("groups = %v", inst.Groups)
	}
	if fi, err := os.Stat(Path()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %v (%v)", fi.Mode().Perm(), err)
	}
}

func TestMissingConfigIsNotAnError(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	// Everything is configurable from the Settings tab, so a first run must
	// start rather than fail.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Instances) != 0 || cfg.Editor != "nvim" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLegacySingleServerConfigIsMigrated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)
	old := []byte("gitlab_url: https://gitlab.example.com\n" +
		"root_dir: /tmp/repos\n" +
		"editor: nvim\n" +
		"groups:\n" +
		"  - id: 7\n" +
		"    full_path: acme/platform\n")
	if err := os.WriteFile(Path(), old, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Instances) != 1 {
		t.Fatalf("instances = %+v", cfg.Instances)
	}
	inst := cfg.Instances[0]
	if inst.ID != LegacyInstanceID {
		t.Errorf("id = %q, want %q so the cached indexes still match", inst.ID, LegacyInstanceID)
	}
	if inst.URL != "https://gitlab.example.com" || inst.Name != "gitlab.example.com" {
		t.Errorf("instance = %+v", inst)
	}
	if inst.GroupScope(7) != ScopeSubgroups {
		t.Errorf("scope = %q", inst.GroupScope(7))
	}
	if cfg.RootDir != "/tmp/repos" {
		t.Errorf("root = %q", cfg.RootDir)
	}
	// Saving it again writes the current layout, without the old keys.
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	if string(b) == "" || containsAny(string(b), "gitlab_url:") {
		t.Errorf("legacy key survived the save:\n%s", b)
	}
}

func containsAny(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && index(s, sub) >= 0 }

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestInstanceIDsAreUnique(t *testing.T) {
	cfg := Default()
	a := cfg.AddInstance(Instance{URL: "https://gitlab.example.com"})
	b := cfg.AddInstance(Instance{URL: "https://gitlab.example.com"})
	if a.ID == b.ID {
		t.Fatalf("both instances got %q", a.ID)
	}
	if b.ID != "gitlab-example-com-2" {
		t.Errorf("second id = %q", b.ID)
	}
}

func TestCycleGroupHasThreeStates(t *testing.T) {
	inst := &Instance{}
	g := Group{ID: 1, FullPath: "a"}

	if got := inst.CycleGroup(g); got != ScopeGroup || len(inst.Groups) != 1 {
		t.Fatalf("first press: scope=%q groups=%v", got, inst.Groups)
	}
	if got := inst.CycleGroup(g); got != ScopeSubgroups || len(inst.Groups) != 1 {
		t.Fatalf("second press: scope=%q groups=%v", got, inst.Groups)
	}
	if got := inst.CycleGroup(g); got != "" || len(inst.Groups) != 0 {
		t.Fatalf("third press: scope=%q groups=%v", got, inst.Groups)
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

// TestRootFor walks the three levels a clone directory can come from.
func TestRootFor(t *testing.T) {
	cfg := &Config{RootDir: "/base"}
	inst := cfg.AddInstance(Instance{URL: "https://gl.example", Groups: []Group{
		{ID: 1, FullPath: "acme", Scope: ScopeSubgroups},
		{ID: 2, FullPath: "acme/platform", Scope: ScopeSubgroups, RootDir: "platform"},
		{ID: 3, FullPath: "acme/secret", Scope: ScopeSubgroups, RootDir: "/elsewhere/secret"},
	}})

	cases := []struct{ path, want string }{
		{"acme/tools/cli", "/base"},                 // nothing overrides
		{"acme/platform/api", "/base/platform"},     // relative, from the base
		{"acme/platform/sub/api", "/base/platform"}, // subgroups inherit it
		{"acme/secret/vault", "/elsewhere/secret"},  // absolute replaces it
		{"other/thing", "/base"},                    // not ours
	}
	for _, c := range cases {
		if got := cfg.RootFor(inst, c.path); got != c.want {
			t.Errorf("RootFor(%q) = %q, want %q", c.path, got, c.want)
		}
	}

	// An instance level root sits between the two.
	inst.RootDir = "/work"
	if got := cfg.RootFor(inst, "acme/tools/cli"); got != "/work" {
		t.Errorf("instance root ignored: %q", got)
	}
	if got := cfg.RootFor(inst, "acme/platform/api"); got != "/work/platform" {
		t.Errorf("group root should be relative to the instance root: %q", got)
	}
	if got := cfg.RootFor(inst, "acme/secret/vault"); got != "/elsewhere/secret" {
		t.Errorf("absolute group root should win outright: %q", got)
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

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"gitlab.example.com": "gitlab-example-com",
		"GitLab.COM":         "gitlab-com",
		"":                   "",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGitHubInstancesAreAlwaysGithubCom: there is no self hosted GitHub here.
func TestGitHubInstancesAreAlwaysGithubCom(t *testing.T) {
	cfg := Default()
	inst := cfg.AddInstance(Instance{Kind: KindGitHub, Name: "Personal", URL: "https://ghe.internal"})
	if inst.URL != GitHubURL {
		t.Errorf("url = %q, want %q", inst.URL, GitHubURL)
	}
	if !inst.IsGitHub() {
		t.Error("kind was lost")
	}
	if inst.ID != "github-com" {
		t.Errorf("id = %q", inst.ID)
	}
}

func TestInstancesDefaultToGitLab(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)
	old := []byte("instances:\n  - id: work\n    name: Work\n    url: https://gitlab.example.com\n    groups: []\n")
	if err := os.WriteFile(Path(), old, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Instances[0].Kind != KindGitLab {
		t.Errorf("kind = %q, want %q", cfg.Instances[0].Kind, KindGitLab)
	}
}

func TestInstancesOfKind(t *testing.T) {
	cfg := Default()
	cfg.AddInstance(Instance{Kind: KindGitLab, URL: "https://gitlab.example.com"})
	cfg.AddInstance(Instance{Kind: KindGitHub})
	cfg.AddInstance(Instance{Kind: KindGitHub, Name: "Second"})

	if got := len(cfg.InstancesOfKind(KindGitLab)); got != 1 {
		t.Errorf("gitlab = %d", got)
	}
	gh := cfg.InstancesOfKind(KindGitHub)
	if len(gh) != 2 {
		t.Fatalf("github = %+v", gh)
	}
	if gh[0].ID == gh[1].ID {
		t.Errorf("two accounts share the id %q", gh[0].ID)
	}
}

// TestToggleGroupIsTwoStates covers what GitHub needs: on or off.
func TestToggleGroupIsTwoStates(t *testing.T) {
	inst := &Instance{Kind: KindGitHub}
	g := Group{ID: 10, FullPath: "widgets"}
	if got := inst.ToggleGroup(g); got != ScopeGroup || len(inst.Groups) != 1 {
		t.Fatalf("on: %q %v", got, inst.Groups)
	}
	if got := inst.ToggleGroup(g); got != "" || len(inst.Groups) != 0 {
		t.Fatalf("off: %q %v", got, inst.Groups)
	}
}

// TestGitHubGroupOwnsItsRepos: an organisation's repositories are exactly one
// segment below it.
func TestGitHubGroupOwnsItsRepos(t *testing.T) {
	g := Group{FullPath: "widgets", Scope: ScopeGroup}
	if !g.Owns("widgets/api") {
		t.Error("widgets/api should belong to widgets")
	}
	if g.Owns("other/api") {
		t.Error("other/api should not belong to widgets")
	}
}

func TestFiltersHideAndShow(t *testing.T) {
	var f Filters
	if f.Active() || f.Order() != SortActivity {
		t.Fatalf("a fresh filter set is %+v", f)
	}
	if hidden := f.ToggleHidden("work", "acme/api"); !hidden || !f.IsHidden("work", "acme/api") {
		t.Fatalf("hiding failed: %+v", f.Hidden)
	}
	// The same path on another server is a different project.
	if f.IsHidden("personal", "acme/api") {
		t.Error("hidden on the wrong server")
	}
	if !f.Active() {
		t.Error("Active should notice a hidden project")
	}
	if hidden := f.ToggleHidden("work", "acme/api"); hidden || len(f.Hidden) != 0 {
		t.Fatalf("unhiding failed: %+v", f.Hidden)
	}
}

func TestFiltersShowAll(t *testing.T) {
	var f Filters
	f.ToggleHidden("work", "a")
	f.ToggleHidden("work", "b")
	if n := f.ShowAll(); n != 2 || len(f.Hidden) != 0 {
		t.Fatalf("ShowAll returned %d, left %+v", n, f.Hidden)
	}
	if n := f.ShowAll(); n != 0 {
		t.Errorf("ShowAll on an empty set returned %d", n)
	}
}

func TestFiltersOrderDefaults(t *testing.T) {
	var f Filters
	if f.Order() != SortActivity {
		t.Errorf("empty order = %q", f.Order())
	}
	f.Sort = SortName
	if f.Order() != SortName {
		t.Errorf("order = %q", f.Order())
	}
	f.Sort = "nonsense"
	if f.Order() != SortActivity {
		t.Errorf("an unknown order should fall back to activity, got %q", f.Order())
	}
}

// TestFiltersSurviveTheFile
func TestFiltersSurviveTheFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)

	cfg := Default()
	cfg.Filters.ClonedOnly = true
	cfg.Filters.Sort = SortName
	cfg.Filters.ToggleHidden("work", "acme/api")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Filters.ClonedOnly || got.Filters.Order() != SortName {
		t.Fatalf("filters = %+v", got.Filters)
	}
	if !got.Filters.IsHidden("work", "acme/api") {
		t.Fatalf("hidden = %+v", got.Filters.Hidden)
	}
}
