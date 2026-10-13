package unibox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type SBX struct {
	Cache string
	// Bindings is sbx's credentials.yaml, where a generated kit's
	// credentials are approved; empty leaves it alone.
	Bindings string
}

func (r *SBX) Name() string               { return "sbx" }
func (r *SBX) Capabilities() Capabilities { return Capabilities{"vm", true, true, true} }
func (r *SBX) Available(ctx context.Context) error {
	if _, err := exec.LookPath("sbx"); err != nil {
		return fmt.Errorf("sbx is not installed; install Docker Sandboxes and run sbx login")
	}
	b, err := commandOutput(hostCommand(ctx, "sbx", "settings", "get", "ssh.agentForwardingEnabled", "--json"))
	if err != nil {
		return err
	}
	var setting struct {
		Value *bool `json:"value"`
	}
	if err = json.Unmarshal(b, &setting); err != nil || setting.Value == nil {
		return fmt.Errorf("cannot determine sbx SSH forwarding; update sbx and retry")
	}
	if *setting.Value {
		return Fail(4, "host_access", "sbx forwards the host SSH agent; run sbx settings set ssh.agentForwardingEnabled false, then sbx daemon restart")
	}
	_, err = r.List(ctx)
	return err
}
func (r *SBX) List(ctx context.Context) ([]Box, error) {
	b, err := commandOutput(hostCommand(ctx, "sbx", "ls", "--json"))
	if err != nil {
		return nil, err
	}
	var response struct {
		Sandboxes []struct{ Name, Status, Memory, ID string } `json:"sandboxes"`
	}
	if err = json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("cannot parse sbx sandbox list; update sbx: %w", err)
	}
	boxes := []Box{}
	for _, v := range response.Sandboxes {
		boxes = append(boxes, Box{v.Name, v.Status, v.Memory, v.ID})
	}
	return boxes, nil
}
func (r *SBX) Images(ctx context.Context) ([]Image, error) {
	b, err := commandOutput(hostCommand(ctx, "sbx", "template", "ls", "--json"))
	if err != nil {
		return nil, err
	}
	var response struct {
		Images []struct{ ID, Repository, Tag string } `json:"images"`
	}
	if err = json.Unmarshal(b, &response); err != nil {
		return nil, err
	}
	images := []Image{}
	for _, v := range response.Images {
		ref := strings.TrimPrefix(v.Repository, "docker.io/") + ":" + v.Tag
		images = append(images, Image{ref, v.ID})
	}
	return images, nil
}
func (r *SBX) Create(ctx context.Context, s Spec, log io.Writer) error {
	args := []string{"create", "--name", s.Name, "--pull", "never", "--skills", "off"}
	if len(s.Agents) <= 1 {
		args = append(args, "--template", s.Image)
	}
	if s.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprint(s.CPUs))
	}
	if s.Memory != "" {
		args = append(args, "--memory", s.Memory)
	}
	for _, h := range s.Access.DenyNetwork {
		args = append(args, "--deny-network", h)
	}
	for _, k := range s.Access.Env {
		args = append(args, "-e", k)
	}
	for _, c := range s.Access.Caches {
		args = append(args, "-e", c.Env+"="+c.Host)
	}
	if len(s.Agents) > 1 {
		// Several agents: a generated v3 workload kit carries their
		// credential declarations; built-in v2 kinds cannot be combined.
		dir, err := r.writeKit(s, oauthFirst(ctx, s))
		if err != nil {
			return err
		}
		if err = writeBindings(r.Bindings, s.Agents, log); err != nil {
			return err
		}
		args = append(args, dir)
	} else {
		kind := s.Agent
		if kind == "" {
			kind = "shell"
		}
		args = append(args, kind)
	}
	for _, c := range s.Access.Caches {
		args = append(args, c.Host+":ro")
	}
	cmd := hostCommand(ctx, "sbx", args...)
	for _, k := range s.Access.Env {
		v, ok := os.LookupEnv(k)
		if !ok {
			return fmt.Errorf("profile requires environment variable %s; set it before creating the sandbox", k)
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if err := runLogged(cmd, log); err != nil {
		return err
	}
	if len(s.Access.AllowNetwork) > 0 {
		// sbx create takes deny rules only; an allow rule is added to the
		// sandbox's own policy, which a deny rule still overrides.
		return runLogged(hostCommand(ctx, "sbx", "policy", "allow", "network", "--sandbox", s.Name, strings.Join(s.Access.AllowNetwork, ",")), log)
	}
	return nil
}
func (r *SBX) Exec(ctx context.Context, name string, c Command) *exec.Cmd {
	args := []string{"exec"}
	if c.Interactive {
		args = append(args, "-it")
	} else if c.Input != nil {
		args = append(args, "-i")
	}
	if c.Directory != "" {
		args = append(args, "-w", c.Directory)
	}
	if c.User != "" {
		args = append(args, "--user", c.User)
	}
	args = append(args, name)
	args = append(args, c.Args...)
	cmd := hostCommand(ctx, "sbx", args...)
	cmd.Stdin = c.Input
	cmd.Stdout = c.Output
	cmd.Stderr = c.Error
	return cmd
}
func (r *SBX) Stop(ctx context.Context, name string) error {
	_, err := commandOutput(hostCommand(ctx, "sbx", "stop", name))
	return err
}
func (r *SBX) Remove(ctx context.Context, name string) error {
	// Confirmation is unibox's: unpushed work and active sessions are
	// checked before any removal, so sbx must not prompt a stdin it lacks.
	_, err := commandOutput(hostCommand(ctx, "sbx", "rm", "--force", name))
	return err
}

func (r *SBX) Build(ctx context.Context, p Profile, tag string, log io.Writer) (string, error) {
	if err := os.MkdirAll(r.Cache, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(r.Cache, "build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if p.Image != "" {
		if err = runLogged(hostCommand(ctx, "docker", "pull", p.Image), log); err != nil {
			return "", err
		}
		if err = runLogged(hostCommand(ctx, "docker", "tag", p.Image, tag), log); err != nil {
			return "", err
		}
	} else {
		for name, file := range p.BuildFiles() {
			dest := filepath.Join(dir, filepath.FromSlash(name))
			if err = AtomicWrite(dest, file.Data, os.FileMode(file.Mode)); err != nil {
				return "", err
			}
		}
		args := []string{"build", "--tag", tag, "--file", filepath.Join(dir, p.Dockerfile)}
		buildArgs, err := p.BuildArgs(r.Name())
		if err != nil {
			return "", err
		}
		for _, arg := range buildArgs {
			args = append(args, "--build-arg", arg)
		}
		args = append(args, dir)
		if err = runLogged(hostCommand(ctx, "docker", args...), log); err != nil {
			return "", err
		}
	}
	for _, id := range p.Agents {
		a, _ := FindAgent(id)
		fmt.Fprintf(log, "checking agent %s\n", id)
		cmd := hostCommand(ctx, "docker", "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--entrypoint", "/bin/sh", tag, "-c", "command -v "+Quote(a.Command))
		if err = runLogged(cmd, log); err != nil {
			return "", fmt.Errorf("profile %s declares %s but its image cannot find it; install it in the Dockerfile", p.Name, id)
		}
	}
	archive := filepath.Join(dir, "image.tar")
	if err = runLogged(hostCommand(ctx, "docker", "image", "save", "--output", archive, tag), log); err != nil {
		return "", err
	}
	if err = runLogged(hostCommand(ctx, "sbx", "template", "load", archive), log); err != nil {
		return "", err
	}
	b, err := commandOutput(hostCommand(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", tag))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
