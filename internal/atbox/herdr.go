package atbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tobola/unagit/internal/atbox/api"
)

const pluginID = "atbox"

func PluginManifest(binary string) string {
	command := "[" + strconv.Quote(binary) + ", \"herdr\", \"ensure\"]"
	return `id = "atbox"
name = "Atbox"
version = "0.1.0"
min_herdr_version = "0.9.1"
description = "Report coding agents running in atbox sandboxes"
platforms = ["macos", "linux"]

[[startup]]
command = ` + command + `

[[events]]
on = "pane.created"
command = ` + command + `

[[events]]
on = "pane.focused"
command = ` + command + "\n"
}

func (m *Manager) pluginPath() string {
	return filepath.Join(m.Paths.Config, "herdr-plugin", "herdr-plugin.toml")
}
func (m *Manager) RefreshPlugin() error {
	path := m.pluginPath()
	old, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	next := PluginManifest(binary)
	if string(old) == next {
		return nil
	}
	return AtomicWrite(path, []byte(next), 0o600)
}

func herdrCommand(ctx context.Context, socket string, args ...string) *exec.Cmd {
	binary := "herdr"
	if p := os.Getenv("HERDR_BIN_PATH"); p != "" {
		binary = p
	}
	cmd := hostCommand(ctx, binary, args...)
	if socket != "" {
		cmd.Env = append(cmd.Env, "HERDR_SOCKET_PATH="+socket)
	}
	return cmd
}

type PluginStatus struct {
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
	Path      string `json:"path"`
}

func (m *Manager) PluginStatus(ctx context.Context) PluginStatus {
	s := PluginStatus{Path: m.pluginPath()}
	_, err := os.Stat(s.Path)
	s.Installed = err == nil
	b, err := commandOutput(herdrCommand(ctx, "", "--version"))
	if err != nil {
		s.Error = "herdr is not available; install herdr 0.9.1 or newer"
		return s
	}
	s.Version = strings.TrimSpace(string(b))
	var major, minor, patch int
	if _, err = fmt.Sscanf(strings.TrimPrefix(s.Version, "herdr "), "%d.%d.%d", &major, &minor, &patch); err != nil || (major == 0 && (minor < 9 || (minor == 9 && patch < 1))) {
		s.Error = "herdr 0.9.1 or newer is required"
		return s
	}
	s.Available = true
	return s
}
func (m *Manager) InstallPlugin(ctx context.Context) error {
	status := m.PluginStatus(ctx)
	if !status.Available {
		return fmt.Errorf("%s", status.Error)
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if err = AtomicWrite(m.pluginPath(), []byte(PluginManifest(binary)), 0o600); err != nil {
		return err
	}
	return runLogged(herdrCommand(ctx, os.Getenv("HERDR_SOCKET_PATH"), "plugin", "link", "--enabled", filepath.Dir(m.pluginPath())), m.Log)
}
func (m *Manager) UninstallPlugin(ctx context.Context) error {
	if err := runLogged(herdrCommand(ctx, os.Getenv("HERDR_SOCKET_PATH"), "plugin", "unlink", pluginID), m.Log); err != nil {
		return err
	}
	if err := os.Remove(m.pluginPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) EnsureObserver() error {
	socket := os.Getenv("HERDR_SOCKET_PATH")
	state := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if socket == "" || state == "" {
		return fmt.Errorf("herdr observer needs HERDR_SOCKET_PATH and HERDR_PLUGIN_STATE_DIR; install the plugin in herdr")
	}
	lock, err := LockFile(filepath.Join(state, "atbox-"+digest(socket)[:16]+".lock"), false, false)
	if err != nil {
		return nil
	}
	lock.Close()
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(state, "atbox.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(binary, "herdr", "observe")
	cmd.Env = HostEnvironment()
	for _, key := range []string{"HERDR_SOCKET_PATH", "HERDR_PLUGIN_STATE_DIR", "HERDR_BIN_PATH"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func decodeHerdr(b []byte, v any) error {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &envelope) == nil {
		if envelope.Error != nil {
			return fmt.Errorf("herdr: %s", envelope.Error.Message)
		}
		if len(envelope.Result) > 0 {
			b = envelope.Result
		}
	}
	return json.Unmarshal(b, v)
}

type observed struct {
	Run   api.Run
	State string
}

func (m *Manager) observeOnce(ctx context.Context, socket, stateDir string, previous map[string]observed) error {
	s, err := m.Paths.ReadState()
	if err != nil {
		return err
	}
	current := map[string]api.Run{}
	for _, box := range s.Sandboxes {
		for _, run := range box.Runs {
			if run.Socket == socket && run.Pane != "" && run.Agent != "" && m.runActive(run) {
				current[run.Pane] = run
			}
		}
	}
	for pane, old := range previous {
		run, ok := current[pane]
		if !ok || run.ID != old.Run.ID {
			if _, err = commandOutput(herdrCommand(ctx, socket, "pane", "release-agent", pane, "--source", "atbox", "--agent", old.Run.Agent)); err != nil {
				return err
			}
			_, _ = commandOutput(herdrCommand(ctx, socket, "pane", "report-metadata", pane, "--source", "atbox", "--clear-title"))
			delete(previous, pane)
		}
	}
	for pane, run := range current {
		b, err := commandOutput(herdrCommand(ctx, socket, "pane", "read", pane, "--source", "detection", "--raw"))
		if err != nil {
			continue
		}
		file := filepath.Join(stateDir, "screen-"+digest(pane)[:16]+".txt")
		if err = AtomicWrite(file, b, 0o600); err != nil {
			return err
		}
		result, err := commandOutput(herdrCommand(ctx, socket, "agent", "explain", "--file", file, "--agent", run.Agent, "--json"))
		os.Remove(file)
		if err != nil {
			continue
		}
		var status struct {
			State string `json:"state"`
			Skip  bool   `json:"skip_state_update"`
		}
		if err = decodeHerdr(result, &status); err != nil || status.Skip {
			continue
		}
		switch status.State {
		case "idle", "working", "blocked", "unknown":
		default:
			continue
		}
		if old, ok := previous[pane]; ok && old.Run.ID == run.ID && old.State == status.State {
			continue
		}
		if _, err = commandOutput(herdrCommand(ctx, socket, "pane", "report-agent", pane, "--source", "atbox", "--agent", run.Agent, "--state", status.State)); err != nil {
			continue
		}
		if _, err = commandOutput(herdrCommand(ctx, socket, "pane", "report-metadata", pane, "--source", "atbox", "--applies-to-source", "atbox", "--title", run.Sandbox)); err != nil {
			continue
		}
		previous[pane] = observed{run, status.State}
	}
	return nil
}

func (m *Manager) Observe(ctx context.Context) error {
	socket, state := os.Getenv("HERDR_SOCKET_PATH"), os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if socket == "" || state == "" {
		return fmt.Errorf("observer runs only inside herdr")
	}
	lock, err := LockFile(filepath.Join(state, "atbox-"+digest(socket)[:16]+".lock"), false, false)
	if err != nil {
		return nil
	}
	defer lock.Close()
	previous := map[string]observed{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err = os.Stat(m.pluginPath()); os.IsNotExist(err) {
			return nil
		}
		if _, err = os.Stat(socket); os.IsNotExist(err) {
			return nil
		}
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = m.observeOnce(step, socket, state, previous)
		cancel()
		if err != nil {
			fmt.Fprintln(m.Log, "observer:", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
