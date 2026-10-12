// Package atbox owns sandbox environments independently of unagit.
package atbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tobola/unagit/internal/atbox/api"
	"gopkg.in/yaml.v3"
)

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func ValidName(s string) error {
	if !identifier.MatchString(s) {
		return fmt.Errorf("invalid name %q; use lowercase letters, digits and hyphens (up to 63 characters)", s)
	}
	return nil
}

type RuntimeConfig struct {
	On       *bool `yaml:"on,omitempty" json:"on,omitempty"`
	StopIdle *bool `yaml:"stop_idle,omitempty" json:"stop_idle,omitempty"`
}
type Config struct {
	Environments   string                   `yaml:"environments" json:"environments"`
	DefaultRuntime string                   `yaml:"default_runtime,omitempty" json:"default_runtime,omitempty"`
	Runtimes       map[string]RuntimeConfig `yaml:"runtimes,omitempty" json:"runtimes,omitempty"`
}
type Paths struct{ Config, Cache string }

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	return Paths{filepath.Join(config, "atbox"), filepath.Join(cache, "atbox")}, nil
}

func ExpandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		if strings.HasSuffix(path, "/~") {
			path = home
		}
	}
	return filepath.Abs(path)
}

func DecodeYAML(data []byte, v any) error {
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("multiple yaml documents are not supported; use one document")
	}
	return nil
}

func (p Paths) LoadConfig() (Config, error) {
	c := Config{Runtimes: map[string]RuntimeConfig{}}
	b, err := os.ReadFile(filepath.Join(p.Config, "config.yaml"))
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = DecodeYAML(b, &c); err != nil {
		return c, fmt.Errorf("invalid atbox configuration: %w", err)
	}
	if c.Runtimes == nil {
		c.Runtimes = map[string]RuntimeConfig{}
	}
	return c, nil
}

func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".atbox-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func (p Paths) SaveConfig(c Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(p.Config, "config.yaml"), b, 0o600)
}

type BuildRecord struct {
	Revision string                  `json:"revision"`
	Image    string                  `json:"image"`
	Snapshot map[string]SnapshotFile `json:"snapshot"`
}
type State struct {
	Version        int                    `json:"version"`
	Approvals      map[string]string      `json:"approvals"`
	ApprovedFields map[string]HostAccess  `json:"approved_fields"`
	Builds         map[string]BuildRecord `json:"builds"`
	Sandboxes      map[string]api.Sandbox `json:"sandboxes"`
}

func freshState() State {
	return State{Version: 1, Approvals: map[string]string{}, ApprovedFields: map[string]HostAccess{}, Builds: map[string]BuildRecord{}, Sandboxes: map[string]api.Sandbox{}}
}

func (p Paths) State(fn func(*State) error) error {
	l, err := LockFile(filepath.Join(p.Config, "state.lock"), false, true)
	if err != nil {
		return err
	}
	defer l.Close()
	s := freshState()
	path := filepath.Join(p.Config, "state.json")
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read atbox state; preserve %s and repair it: %w", path, err)
	}
	if s.Version != 1 {
		return fmt.Errorf("unsupported atbox state version %d; upgrade atbox", s.Version)
	}
	if err = fn(&s); err != nil {
		return err
	}
	b, err = json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(path, append(b, '\n'), 0o600)
}

func (p Paths) ReadState() (State, error) {
	var out State
	err := p.State(func(s *State) error { out = *s; return nil })
	return out, err
}

func (c Config) Enabled(name string) bool {
	r, ok := c.Runtimes[name]
	return !ok || r.On == nil || *r.On
}
func (c Config) StopIdle(name string) bool {
	r, ok := c.Runtimes[name]
	return !ok || r.StopIdle == nil || *r.StopIdle
}

type Failure struct {
	Status        int
	Code, Message string
}

func (e *Failure) Error() string { return e.Message }
func Fail(status int, code, format string, args ...any) error {
	return &Failure{status, code, fmt.Sprintf(format, args...)}
}
