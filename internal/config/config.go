// Package config loads and stores the unagit configuration and index files.
//
// Everything in here is editable from the Settings tab; the file is only the
// place it ends up.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Scopes a selected group can have.
const (
	// ScopeGroup takes only the projects that sit directly in the group.
	ScopeGroup = "group"
	// ScopeSubgroups takes the whole tree below the group.
	ScopeSubgroups = "subgroups"
)

// Group is a GitLab group the user selected in the settings view.
type Group struct {
	ID       int    `yaml:"id" json:"id"`
	FullPath string `yaml:"full_path" json:"full_path"`
	Name     string `yaml:"name" json:"name"`
	// Scope is ScopeGroup or ScopeSubgroups; an empty value means subgroups,
	// which is what older configurations did.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
	// RootDir overrides where this group's projects are cloned. A relative
	// path is taken from the root it would otherwise inherit.
	RootDir string `yaml:"root_dir,omitempty" json:"root_dir,omitempty"`
}

// IncludesSubgroups reports whether the whole tree below the group is wanted.
func (g Group) IncludesSubgroups() bool { return g.Scope != ScopeGroup }

// Owns reports whether projectPath belongs to this group under its scope.
func (g Group) Owns(projectPath string) bool {
	rest, ok := strings.CutPrefix(projectPath, g.FullPath+"/")
	if !ok {
		return false
	}
	if g.IncludesSubgroups() {
		return true
	}
	return !strings.Contains(rest, "/")
}

// Instance is one server - a GitLab installation or a GitHub account - with
// its own token and its own group selection.
type Instance struct {
	// ID is a stable key: it ties the cached indexes and the stored token to
	// this instance and never changes once assigned.
	ID string `yaml:"id" json:"id"`
	// Kind is forge.KindGitLab or forge.KindGitHub; empty means GitLab, which
	// is all unagit spoke to at first.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url"`
	// RootDir overrides the global root for everything on this instance.
	RootDir string `yaml:"root_dir,omitempty" json:"root_dir,omitempty"`
	// CloneProtocol is ProtocolHTTPS or ProtocolSSH. Over SSH git uses your
	// key and the token is only ever spent on the API.
	CloneProtocol string  `yaml:"clone_protocol,omitempty" json:"clone_protocol,omitempty"`
	Groups        []Group `yaml:"groups" json:"groups"`
}

// Clone protocols. They mirror the workspace's, which cannot be imported here
// without a cycle.
const (
	ProtocolHTTPS = "https"
	ProtocolSSH   = "ssh"
)

// Protocol is how this instance's repositories are cloned, defaulting to
// HTTPS, which is what unagit did before it could do anything else.
func (i Instance) Protocol() string {
	if i.CloneProtocol == ProtocolSSH {
		return ProtocolSSH
	}
	return ProtocolHTTPS
}

// Label is what the instance is called in the interface.
func (i Instance) Label() string {
	if i.Name != "" {
		return i.Name
	}
	return Host(i.URL)
}

// IsGitHub reports whether this is a github.com account.
func (i Instance) IsGitHub() bool { return i.Kind == KindGitHub }

// Kinds of server. They mirror forge's, which cannot be imported here without
// a cycle.
const (
	KindGitLab = "gitlab"
	KindGitHub = "github"
)

// GitHubURL is the only address a GitHub instance can have: github.com is not
// self hosted.
const GitHubURL = "https://github.com"

// GroupScope returns the scope a group is selected with, or "" when it is not
// selected at all.
func (i *Instance) GroupScope(id int) string {
	for _, g := range i.Groups {
		if g.ID == id {
			if g.Scope == "" {
				return ScopeSubgroups
			}
			return g.Scope
		}
	}
	return ""
}

// HasGroup reports whether the group id is selected.
func (i *Instance) HasGroup(id int) bool { return i.GroupScope(id) != "" }

// Group returns the selected group with this id, or nil.
func (i *Instance) Group(id int) *Group {
	for idx := range i.Groups {
		if i.Groups[idx].ID == id {
			return &i.Groups[idx]
		}
	}
	return nil
}

// ToggleGroup selects or unselects a group outright. GitHub has no subgroups,
// so there is nothing to cycle through there.
func (i *Instance) ToggleGroup(g Group) string {
	if i.GroupScope(g.ID) != "" {
		for idx, existing := range i.Groups {
			if existing.ID == g.ID {
				i.Groups = append(i.Groups[:idx], i.Groups[idx+1:]...)
				break
			}
		}
		return ""
	}
	g.Scope = ScopeGroup
	i.Groups = append(i.Groups, g)
	return ScopeGroup
}

// CycleGroup steps a group through "not selected" -> "this group only" ->
// "including subgroups" -> "not selected" and returns the new scope.
func (i *Instance) CycleGroup(g Group) string {
	switch i.GroupScope(g.ID) {
	case "":
		g.Scope = ScopeGroup
		i.Groups = append(i.Groups, g)
		return ScopeGroup
	case ScopeGroup:
		if existing := i.Group(g.ID); existing != nil {
			existing.Scope = ScopeSubgroups
		}
		return ScopeSubgroups
	default:
		for idx, existing := range i.Groups {
			if existing.ID == g.ID {
				i.Groups = append(i.Groups[:idx], i.Groups[idx+1:]...)
				break
			}
		}
		return ""
	}
}

// Orders the lists can be sorted in.
const (
	// SortActivity puts what moved most recently first.
	SortActivity = "activity"
	// SortName sorts by path, and merge requests by project then number.
	SortName = "name"
)

// Hidden is one project kept out of the lists.
type Hidden struct {
	Instance string `yaml:"instance" json:"instance"`
	Path     string `yaml:"path" json:"path"`
}

// Filters are the view settings the project and merge request lists share.
// They are a lasting preference, so they live in the configuration rather
// than in the session.
type Filters struct {
	// ClonedOnly narrows both lists to projects that are on disk.
	ClonedOnly bool `yaml:"cloned_only,omitempty" json:"cloned_only,omitempty"`
	// Sort is SortActivity or SortName; empty means activity.
	Sort string `yaml:"sort,omitempty" json:"sort,omitempty"`
	// Hidden are the projects kept out of both lists.
	Hidden []Hidden `yaml:"hidden,omitempty" json:"hidden,omitempty"`
	// GroupByProject gathers the merge requests under the project they
	// belong to. It means nothing to the project list.
	GroupByProject bool `yaml:"group_by_project,omitempty" json:"group_by_project,omitempty"`
}

// Order is the sort to apply, normalised.
func (f *Filters) Order() string {
	if f.Sort == SortName {
		return SortName
	}
	return SortActivity
}

// IsHidden reports whether a project is kept out of the lists.
func (f *Filters) IsHidden(instance, path string) bool {
	for _, h := range f.Hidden {
		if h.Instance == instance && h.Path == path {
			return true
		}
	}
	return false
}

// ToggleHidden hides or unhides a project and reports the new state.
func (f *Filters) ToggleHidden(instance, path string) bool {
	for i, h := range f.Hidden {
		if h.Instance == instance && h.Path == path {
			f.Hidden = append(f.Hidden[:i], f.Hidden[i+1:]...)
			return false
		}
	}
	f.Hidden = append(f.Hidden, Hidden{Instance: instance, Path: path})
	return true
}

// ShowAll unhides everything and reports how many were hidden.
func (f *Filters) ShowAll() int {
	n := len(f.Hidden)
	f.Hidden = nil
	return n
}

// Active reports whether anything is narrowing the lists.
func (f *Filters) Active() bool { return f.ClonedOnly || len(f.Hidden) > 0 }

// Config is the on-disk configuration (~/.config/unagit/config.yaml).
// Tokens are not stored here; they live encrypted in the vault.
type Config struct {
	RootDir    string     `yaml:"root_dir"`
	Editor     string     `yaml:"editor"`
	EditorArgs []string   `yaml:"editor_args"`
	Filters    Filters    `yaml:"filters,omitempty"`
	Instances  []Instance `yaml:"instances"`

	// Written by unagit before it grew multiple instances; read once and
	// folded into Instances.
	LegacyURL    string  `yaml:"gitlab_url,omitempty"`
	LegacyGroups []Group `yaml:"groups,omitempty"`
}

// Default returns a configuration with sane defaults filled in.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		RootDir:    filepath.Join(home, "unagit"),
		Editor:     "nvim",
		EditorArgs: []string{"."},
	}
}

// Dir is the configuration directory, honouring XDG_CONFIG_HOME.
func Dir() string {
	if d := os.Getenv("UNAGIT_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "unagit")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "unagit")
}

// Path is the location of config.yaml.
func Path() string { return filepath.Join(Dir(), "config.yaml") }

// VaultPath is the location of the encrypted tokens.
func VaultPath() string { return filepath.Join(Dir(), "tokens.enc") }

// LegacyTokenPath is where a single token lived before the vault.
func LegacyTokenPath() string { return filepath.Join(Dir(), "token.enc") }

// IndexPath is the location of a cached index file (projects, mrs, groups).
func IndexPath(name string) string { return filepath.Join(Dir(), "index-"+name+".json") }

// Load reads config.yaml, applies defaults and folds any legacy layout into
// the current one. A missing file yields the defaults rather than an error:
// everything can be set up from the Settings tab.
func Load() (*Config, error) {
	cfg := Default()
	b, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(), err)
	}
	cfg.normalise()
	return cfg, nil
}

// LegacyInstanceID is the id given to the instance migrated from a
// single-server configuration, so its cached indexes keep working.
const LegacyInstanceID = "default"

func (c *Config) normalise() {
	if c.Editor == "" {
		c.Editor = "nvim"
	}
	if c.RootDir == "" {
		c.RootDir = Default().RootDir
	}
	// One server, no instances: that is the old layout.
	if len(c.Instances) == 0 && c.LegacyURL != "" {
		c.Instances = []Instance{{
			ID:     LegacyInstanceID,
			Kind:   KindGitLab,
			Name:   Host(c.LegacyURL),
			URL:    c.LegacyURL,
			Groups: c.LegacyGroups,
		}}
	}
	c.LegacyURL, c.LegacyGroups = "", nil

	for i := range c.Instances {
		inst := &c.Instances[i]
		if inst.Kind == "" {
			inst.Kind = KindGitLab
		}
		if inst.Kind == KindGitHub {
			inst.URL = GitHubURL
		}
		if inst.CloneProtocol != ProtocolSSH {
			inst.CloneProtocol = ProtocolHTTPS
		}
		inst.URL = strings.TrimRight(inst.URL, "/")
		if inst.ID == "" {
			inst.ID = c.freeID(Slug(Host(inst.URL)), inst.ID)
		}
		for g := range inst.Groups {
			if inst.Groups[g].Scope == "" {
				inst.Groups[g].Scope = ScopeSubgroups
			}
		}
	}
}

// Save writes config.yaml, creating the config directory when needed.
func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), b, 0o600)
}

// Instance returns the instance with this id, or nil.
func (c *Config) Instance(id string) *Instance {
	for i := range c.Instances {
		if c.Instances[i].ID == id {
			return &c.Instances[i]
		}
	}
	return nil
}

// AddInstance appends an instance, giving it a unique id derived from its URL.
func (c *Config) AddInstance(inst Instance) *Instance {
	if inst.Kind == "" {
		inst.Kind = KindGitLab
	}
	if inst.Kind == KindGitHub {
		inst.URL = GitHubURL
	}
	inst.URL = strings.TrimRight(inst.URL, "/")
	inst.ID = c.freeID(Slug(Host(inst.URL)), "")
	c.Instances = append(c.Instances, inst)
	return &c.Instances[len(c.Instances)-1]
}

// RemoveInstance drops an instance and everything selected on it.
func (c *Config) RemoveInstance(id string) {
	for i := range c.Instances {
		if c.Instances[i].ID == id {
			c.Instances = append(c.Instances[:i], c.Instances[i+1:]...)
			return
		}
	}
}

// freeID returns a unique instance id based on want, ignoring the instance
// called self.
func (c *Config) freeID(want, self string) string {
	if want == "" {
		want = "gitlab"
	}
	taken := func(id string) bool {
		for _, inst := range c.Instances {
			if inst.ID == id && inst.ID != self {
				return true
			}
		}
		return false
	}
	if !taken(want) {
		return want
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", want, n)
		if !taken(candidate) {
			return candidate
		}
	}
}

// Root is the default clone root, with ~ expanded.
func (c *Config) Root() string { return Expand(c.RootDir) }

// RootFor works out where a project is cloned: the most specific selected
// group that owns it wins, then the instance, then the global default.
func (c *Config) RootFor(inst *Instance, projectPath string) string {
	root := c.Root()
	if inst == nil {
		return root
	}
	root = resolveRoot(root, inst.RootDir)
	best := ""
	override := ""
	for _, g := range inst.Groups {
		if g.RootDir == "" || !ownsOrIs(g, projectPath) {
			continue
		}
		if len(g.FullPath) > len(best) {
			best, override = g.FullPath, g.RootDir
		}
	}
	return resolveRoot(root, override)
}

// GroupRoot is the root a group's own projects land in, for display in the
// settings view.
func (c *Config) GroupRoot(inst *Instance, g Group) string {
	return c.RootFor(inst, g.FullPath+"/project")
}

// ownsOrIs is Owns, but a group with its own root also covers the projects of
// its subgroups when no more specific override exists.
func ownsOrIs(g Group, projectPath string) bool {
	return strings.HasPrefix(projectPath, g.FullPath+"/")
}

// resolveRoot applies an override to a base directory. An absolute or ~ path
// replaces the base, a relative one is taken from it.
func resolveRoot(base, override string) string {
	if strings.TrimSpace(override) == "" {
		return base
	}
	p := Expand(strings.TrimSpace(override))
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// Expand replaces a leading ~ with the user's home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Host is the host part of a URL, for naming things after it.
func Host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	}
	return u.Hostname()
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a host name into something usable as a file name component.
func Slug(s string) string {
	return strings.Trim(notSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// InstancesOfKind lists the configured servers of one kind, in order.
func (c *Config) InstancesOfKind(kind string) []Instance {
	var out []Instance
	for _, inst := range c.Instances {
		if inst.Kind == kind {
			out = append(out, inst)
		}
	}
	return out
}
