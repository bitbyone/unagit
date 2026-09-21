// Package config loads and stores the unagit configuration and index files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Group is a GitLab group the user selected in the settings view.
type Group struct {
	ID       int    `yaml:"id" json:"id"`
	FullPath string `yaml:"full_path" json:"full_path"`
	Name     string `yaml:"name" json:"name"`
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
func (c *Config) HasGroup(id int) bool {
	for _, g := range c.Groups {
		if g.ID == id {
			return true
		}
	}
	return false
}

// ToggleGroup adds or removes a group from the selection.
func (c *Config) ToggleGroup(g Group) bool {
	for i, existing := range c.Groups {
		if existing.ID == g.ID {
			c.Groups = append(c.Groups[:i], c.Groups[i+1:]...)
			return false
		}
	}
	c.Groups = append(c.Groups, g)
	return true
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
