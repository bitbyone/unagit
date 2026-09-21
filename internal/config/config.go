// Package config loads and stores the unagit configuration and index files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
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

// Config is the on-disk configuration (~/.config/unagit/config.yaml).
// The GitLab token is NOT stored here; it lives encrypted in token.enc.
type Config struct {
	GitLabURL  string   `yaml:"gitlab_url"`
	RootDir    string   `yaml:"root_dir"`
	Editor     string   `yaml:"editor"`
	EditorArgs []string `yaml:"editor_args"`
	Groups     []Group  `yaml:"groups"`
}

// Default returns a configuration with sane defaults filled in.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		GitLabURL:  "https://gitlab.com",
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

// TokenPath is the location of the encrypted GitLab token.
func TokenPath() string { return filepath.Join(Dir(), "token.enc") }

// IndexPath is the location of a cached index file (projects, mrs, groups).
func IndexPath(name string) string { return filepath.Join(Dir(), "index-"+name+".json") }

// Load reads config.yaml and applies defaults for missing values.
func Load() (*Config, error) {
	b, err := os.ReadFile(Path())
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(), err)
	}
	if cfg.Editor == "" {
		cfg.Editor = "nvim"
	}
	cfg.GitLabURL = strings.TrimRight(cfg.GitLabURL, "/")
	for i := range cfg.Groups {
		if cfg.Groups[i].Scope == "" {
			cfg.Groups[i].Scope = ScopeSubgroups
		}
	}
	return cfg, nil
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

// Root returns the repository root directory with ~ expanded.
func (c *Config) Root() string { return Expand(c.RootDir) }

// HasGroup reports whether the group id is selected.
func (c *Config) HasGroup(id int) bool { return c.GroupScope(id) != "" }

// GroupScope returns the scope a group is selected with, or "" when it is not
// selected at all.
func (c *Config) GroupScope(id int) string {
	for _, g := range c.Groups {
		if g.ID == id {
			if g.Scope == "" {
				return ScopeSubgroups
			}
			return g.Scope
		}
	}
	return ""
}

// CycleGroup steps a group through "not selected" -> "this group only" ->
// "including subgroups" -> "not selected" and returns the new scope.
func (c *Config) CycleGroup(g Group) string {
	switch c.GroupScope(g.ID) {
	case "":
		g.Scope = ScopeGroup
		c.Groups = append(c.Groups, g)
		return ScopeGroup
	case ScopeGroup:
		for i := range c.Groups {
			if c.Groups[i].ID == g.ID {
				c.Groups[i].Scope = ScopeSubgroups
			}
		}
		return ScopeSubgroups
	default:
		for i, existing := range c.Groups {
			if existing.ID == g.ID {
				c.Groups = append(c.Groups[:i], c.Groups[i+1:]...)
				break
			}
		}
		return ""
	}
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
