package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
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
	// No favourite on a first run: the first open asks which editor.
	if len(cfg.Instances) != 0 || cfg.FavouriteEditor != "" || cfg.Editor != "" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// TestOlderEditorBecomesTheFavourite: the one editor an older configuration
// had keeps being the one things open in.
func TestOlderEditorBecomesTheFavourite(t *testing.T) {
	for yaml, want := range map[string]struct{ favourite, custom string }{
		"editor: nvim\neditor_args: [.]\n":       {"nvim", ""},
		"editor: /opt/homebrew/bin/zed\n":        {"zed", ""},
		"editor: hx\neditor_args: [.]\n":         {"custom", "hx"},
		"editor: nvim\neditor_args: [-c, Git]\n": {"custom", "nvim"},
		"favourite_editor: code\neditor: hx\n":   {"code", "hx"},
		"root_dir: /tmp/x\n":                     {"", ""},
		"favourite_editor: ask\neditor: hx\n":    {"ask", "hx"},
	} {
		dir := t.TempDir()
		t.Setenv("UNAGIT_CONFIG_DIR", dir)
		if err := os.WriteFile(Path(), []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.FavouriteEditor != want.favourite || cfg.Editor != want.custom {
			t.Errorf("%q: favourite %q custom %q, want %q %q", yaml, cfg.FavouriteEditor, cfg.Editor, want.favourite, want.custom)
		}
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
	if f.Active() || f.Order(ListRepositories) != SortActivity {
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
	if f.Order(ListRepositories) != SortActivity {
		t.Errorf("empty order = %q", f.Order(ListRepositories))
	}
	f.Sort = SortName
	if f.Order(ListRepositories) != SortName {
		t.Errorf("order = %q", f.Order(ListRepositories))
	}
	f.Sort = "nonsense"
	if f.Order(ListRepositories) != SortActivity {
		t.Errorf("an unknown order should fall back to activity, got %q", f.Order(ListRepositories))
	}
}

// TestFiltersSurviveTheFile
func TestFiltersSurviveTheFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)

	cfg := Default()
	cfg.Filters.ClonedOnly = true
	cfg.Filters.Sort = SortName
	cfg.Filters.SetOrder(ListRepositories, SortSize)
	cfg.Filters.ToggleHidden("work", "acme/api")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Filters.ClonedOnly || got.Filters.Order(ListMergeRequests) != SortName {
		t.Fatalf("filters = %+v", got.Filters)
	}
	if got.Filters.Order(ListRepositories) != SortSize {
		t.Fatalf("the repositories' own order = %q", got.Filters.Order(ListRepositories))
	}
	if !got.Filters.IsHidden("work", "acme/api") {
		t.Fatalf("hidden = %+v", got.Filters.Hidden)
	}
}

func TestExactProjectDirectory(t *testing.T) {
	cfg := &Config{RootDir: "/base"}
	inst := &Instance{RootDir: "/server", Groups: []Group{{FullPath: "group/sub", RootDir: "/group", Scope: ScopeSubgroups}}, ProjectDirs: map[string]string{"group/sub/app": "/custom/renamed"}}
	if got := cfg.ProjectDir(inst, "group/sub/app"); got != "/custom/renamed" {
		t.Fatalf("override = %q", got)
	}
	if got := cfg.ProjectDir(inst, "group/sub/other"); got != "/group/group/sub/other" {
		t.Fatalf("inherited = %q", got)
	}
	if got := cfg.ProjectDir(&Instance{}, "group/sub/app"); got != "/base/group/sub/app" {
		t.Fatalf("another server = %q", got)
	}
}

// TestPathsAreKeptFromHome: a directory under the home directory is written
// as ~/… whatever form it was given in, so config.yaml can live in dotfiles
// and mean the same on another machine; anything else stays as it is.
func TestPathsAreKeptFromHome(t *testing.T) {
	t.Setenv("UNAGIT_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	c := Default()
	if c.RootDir != "~/unagit" {
		t.Errorf("default root = %q", c.RootDir)
	}
	c.RootDir = filepath.Join(home, "workspace")
	c.AddInstance(Instance{Name: "acme", URL: "https://gl.example",
		RootDir:     filepath.Join(home, "work", "acme"),
		ProjectDirs: map[string]string{"acme/api": filepath.Join(home, "code", "api"), "acme/web": "/srv/web"},
		Groups:      []Group{{ID: 1, FullPath: "acme/tools", RootDir: "tools"}, {ID: 2, FullPath: "acme/x", RootDir: home + "/x/"}}})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, home) {
		t.Errorf("the home directory is written out:\n%s", text)
	}
	for _, want := range []string{"root_dir: ~/workspace", "root_dir: ~/work/acme", "acme/api: ~/code/api",
		"acme/web: /srv/web", "root_dir: tools", "root_dir: ~/x"} {
		if !strings.Contains(text, want) {
			t.Errorf("config.yaml lacks %q:\n%s", want, text)
		}
	}

	// An absolute path written by an older unagit reads as ~ too.
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(Path(), []byte("root_dir: "+filepath.Join(home, "old")+"\n"), 0o600))
	loaded, err := Load()
	must(err)
	if loaded.RootDir != "~/old" {
		t.Errorf("loaded root = %q", loaded.RootDir)
	}
	if loaded.Root() != filepath.Join(home, "old") {
		t.Errorf("the root does not expand back: %q", loaded.Root())
	}
}

// TestHiddenAuthors: hiding is per server, cumulative, undone one by one, and
// the filter can be turned off without forgetting anyone; hiding another
// author turns it on again.
func TestHiddenAuthors(t *testing.T) {
	var f Filters
	if !f.ToggleAuthor("gl", "renovate") || !f.ToggleAuthor("gl", "dependabot") {
		t.Fatal("hiding did not report hidden")
	}
	if !f.HidesAuthor("gl", "renovate") || f.HidesAuthor("gh", "renovate") || f.HidesAuthor("gl", "jane") {
		t.Error("hiding is not per server and per author")
	}
	f.ShowHiddenAuthors = true
	if f.HidesAuthor("gl", "renovate") || len(f.HiddenAuthors) != 2 {
		t.Error("turning the filter off hid or forgot someone")
	}
	f.ToggleAuthor("gl", "ci-bot")
	if f.ShowHiddenAuthors || !f.HidesAuthor("gl", "renovate") {
		t.Error("hiding another author did not turn the filter on")
	}
	if f.ToggleAuthor("gl", "renovate") || f.HidesAuthor("gl", "renovate") || !f.HidesAuthor("gl", "dependabot") {
		t.Error("showing one author again touched the others")
	}
}

// TestHiddenColumns: a column is hidden and shown again per list, a list
// with nothing hidden leaves no entry behind, and the tags hidden before
// there were columns count as their column hidden.
func TestHiddenColumns(t *testing.T) {
	var f Filters
	f.ToggleColumn(ListRepositories, "path")
	if !f.HidesColumn(ListRepositories, "path") || f.HidesColumn(ListWorktrees, "path") {
		t.Fatalf("path hidden in the wrong list: %+v", f.HiddenColumns)
	}
	f.ToggleColumn(ListRepositories, "path")
	if f.HidesColumn(ListRepositories, "path") || len(f.HiddenColumns) != 0 {
		t.Fatalf("path not shown again: %+v", f.HiddenColumns)
	}
	f.HideTags = true
	if !f.HidesColumn(ListRepositories, "tags") {
		t.Fatal("the old hide_tags is not read as the tags column hidden")
	}
	f.ToggleColumn(ListRepositories, "tags")
	if f.HidesColumn(ListRepositories, "tags") || f.HideTags {
		t.Fatal("the tags did not come back")
	}
}

// TestAnOrderAListCannotHaveFallsBack: a size is no merge request's, so a
// configuration that says so is read as the shared order.
func TestAnOrderAListCannotHaveFallsBack(t *testing.T) {
	f := Filters{Sort: SortName}
	if got := f.Order(ListWorktrees); got != SortName {
		t.Errorf("without one of its own = %q", got)
	}
	f.SetOrder(ListMergeRequests, SortSize)
	if got := f.Order(ListMergeRequests); got != SortName {
		t.Errorf("an order the list cannot have = %q", got)
	}
	f.SetOrder(ListMergeRequests, SortComments)
	if got := f.Order(ListMergeRequests); got != SortComments {
		t.Errorf("its own = %q", got)
	}
	if got := f.Order(ListRepositories); got != SortName {
		t.Errorf("another list's order leaked: %q", got)
	}
}

// TestSomeColumnsWaitToBeShown: the people columns of the merge requests
// are hidden until shown, and showing one is kept in shown_columns; the
// rest are shown until hidden.
func TestSomeColumnsWaitToBeShown(t *testing.T) {
	var f Filters
	if !f.HidesColumn(ListMergeRequests, "assignees") || f.HidesColumn(ListMergeRequests, "author") {
		t.Fatal("the defaults are wrong")
	}
	f.ToggleColumn(ListMergeRequests, "assignees")
	f.ToggleColumn(ListMergeRequests, "author")
	if f.HidesColumn(ListMergeRequests, "assignees") || !f.HidesColumn(ListMergeRequests, "author") {
		t.Fatalf("toggled: %+v", f)
	}
	if len(f.ShownColumns[ListMergeRequests]) != 1 || len(f.HiddenColumns[ListMergeRequests]) != 1 {
		t.Fatalf("kept as %+v / %+v", f.ShownColumns, f.HiddenColumns)
	}
	f.ToggleColumn(ListMergeRequests, "assignees")
	f.ToggleColumn(ListMergeRequests, "author")
	if f.ShownColumns != nil || f.HiddenColumns != nil {
		t.Fatalf("toggled back, still kept: %+v / %+v", f.ShownColumns, f.HiddenColumns)
	}
	f.HideTags = true
	f.ToggleColumn(ListRepositories, "tags")
	if f.HidesColumn(ListRepositories, "tags") {
		t.Fatal("tags hidden the old way do not show again")
	}
}

// TestWhatUseTeachesIsNotConfiguration: where things were opened, the last
// Open…, whom merge requests went to are kept in state.json, never in
// config.yaml - which is portable and versioned, and must not change
// because unagit was used. What an older version wrote into config.yaml
// moves out on load.
func TestWhatUseTeachesIsNotConfiguration(t *testing.T) {
	dir := t.TempDir()
	legacy := `root_dir: /src
instances:
  - id: gl
    name: GitLab
    url: https://gitlab.example.com
    people_uses:
      assignee: {mike: 2}
integrations:
  agent_place: zellij-right
  place_uses:
    agent: {zellij-right: 3}
  open_form:
    repository: {with: "agent:claude", where: zellij-right}
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.State
	if s.AgentPlace != "zellij-right" || s.PlaceUses["agent"]["zellij-right"] != 3 ||
		s.OpenForm["repository"].With != "agent:claude" || s.PersonUses("gl", RoleAssignee)["mike"] != 2 {
		t.Fatalf("the state did not move: %+v", s)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	for _, key := range []string{"people_uses", "agent_place", "place_uses", "open_form"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("config.yaml still has %s:\n%s", key, raw)
		}
	}
	if !strings.Contains(string(raw), "/src") {
		t.Errorf("the configuration itself was lost:\n%s", raw)
	}

	// Used and saved again, only state.json changes.
	before, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	cfg.State.UsePerson("gl", RoleAssignee, "mike")
	cfg.State.UsePlace("agent", "ghostty-tab")
	if err := cfg.SaveState(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if string(before) != string(after) {
		t.Errorf("config.yaml changed with use:\n%s\n---\n%s", before, after)
	}
	again, err := LoadFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.State.PersonUses("gl", RoleAssignee)["mike"] != 3 || again.State.PlaceUses["agent"]["ghostty-tab"] != 1 {
		t.Errorf("state.json was not read back: %+v", again.State)
	}
}

// TestOneToastLengthOfAnOlderFileIsEachSeverity: toast_seconds was once one
// number for every toast; read now, it is each severity's.
func TestOneToastLengthOfAnOlderFileIsEachSeverity(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("toast_seconds: 8\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.ToastSeconds != (ToastSeconds{8, 8, 8, 8}) || c.ToastLife(ToastDanger) != 8*time.Second {
		t.Fatalf("read %+v", c.ToastSeconds)
	}
	c = Config{}
	if err := yaml.Unmarshal([]byte("toast_seconds:\n  danger: 20\n"), &c); err != nil {
		t.Fatal(err)
	}
	if c.ToastLife(ToastDanger) != 20*time.Second || c.ToastLife(ToastInfo) != DefaultToastSeconds*time.Second {
		t.Fatalf("read %+v", c.ToastSeconds)
	}
}
