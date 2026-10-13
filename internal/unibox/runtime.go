package unibox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Box struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Memory string `json:"memory,omitempty"`
	ID     string `json:"id"`
}
type Image struct {
	Reference string
	ID        string
}
type Spec struct {
	Name, Image string
	CPUs        int
	Memory      string
	Access      HostAccess
	// Agent is the runtime's own kind for the sandbox. sbx binds its stored
	// service credentials by this kind, so a profile with one agent gets the
	// proxy-managed login; empty means a plain shell sandbox. Agents names
	// every declared agent: several mean a generated v3 workload kit.
	Agent  string
	Agents []string
}
type Command struct {
	Args            []string
	Directory, User string
	Interactive     bool
	Input           io.Reader
	Output, Error   io.Writer
}
type Capabilities struct {
	Isolation       string `json:"isolation"`
	ReadOnlyMounts  bool   `json:"read_only_mounts"`
	NetworkRules    bool   `json:"network_rules"`
	CredentialProxy bool   `json:"credential_proxy"`
}

type Runtime interface {
	Name() string
	Capabilities() Capabilities
	Available(context.Context) error
	List(context.Context) ([]Box, error)
	Images(context.Context) ([]Image, error)
	Build(context.Context, Profile, string, io.Writer) (string, error)
	Create(context.Context, Spec, io.Writer) error
	Exec(context.Context, string, Command) *exec.Cmd
	Stop(context.Context, string) error
	Remove(context.Context, string) error
}

// Child processes receive only the host context they need. In particular, an
// installed runtime must not discover an unrelated token or SSH agent by accident.
func HostEnvironment() []string {
	keys := []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TMP", "TEMP", "TERM", "COLORTERM", "LANG", "LC_ALL", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM"}
	env := []string{}
	for _, key := range keys {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}
func hostCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = HostEnvironment()
	return c
}
func commandOutput(c *exec.Cmd) ([]byte, error) {
	var errout bytes.Buffer
	c.Stderr = &errout
	b, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("%s failed; check its configuration and retry: %s", c.Path, strings.TrimSpace(errout.String()))
	}
	return b, nil
}
func runLogged(c *exec.Cmd, log io.Writer) error {
	c.Stdout = log
	c.Stderr = log
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s failed; see the operation log: %w", c.Path, err)
	}
	return nil
}
func runtimeOutput(ctx context.Context, r Runtime, name string, args ...string) (string, error) {
	b, err := commandOutput(r.Exec(ctx, name, Command{Args: args}))
	return strings.TrimSpace(string(b)), err
}
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
