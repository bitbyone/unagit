package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/session"
)

func TestShellCommandsRecordZoxideVisits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "unagit")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cfg := config.Default()
	cfg.SetDir(filepath.Join(dir, "config"))
	cfg.RootDir = filepath.Join(dir, "root with spaces")
	off := false
	cfg.Integrations.Chezmoi = &off
	inst := cfg.AddInstance(config.Instance{Name: "test", URL: "https://unused.test"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(cfg.Root(), "acme", "gateway")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := index.Save(cfg.IndexPath("projects"), index.Projects{Version: index.Version, Items: []forge.Project{{ID: 1, Instance: inst.ID, PathWithNamespace: "acme/gateway"}}}); err != nil {
		t.Fatal(err)
	}
	close := session.New(cfg.Dir()).Open(session.Record{Dir: clone, Project: "acme/gateway", Mode: session.ModeRepository})
	defer close()
	log := filepath.Join(dir, "calls")
	tool := filepath.Join(dir, "zoxide")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nif [ \"$1\" = add ]; then printf '%s\\n' \"$3\" >> \"$ZO_TEST_LOG\"; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "UNAGIT_CONFIG_DIR="+cfg.Dir(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "ZO_TEST_LOG="+log, "_ZO_DATA_DIR="+filepath.Join(dir, "data"), "SHELL=/bin/sh")
		cmd.Stdin = strings.NewReader("pwd\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return string(out)
	}
	for _, command := range []string{"go", "cd"} {
		if got := run(command, "--print", "gateway"); got != clone+"\n" {
			t.Fatalf("%s stdout = %q", command, got)
		}
		if got := run(command, "gateway"); got != clone+"\n" {
			t.Fatalf("%s shell = %q", command, got)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strings.Repeat(clone+"\n", 4) {
		t.Fatalf("visits = %q", data)
	}
	cfg.Integrations.Zoxide = &off
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	run("go", "--print", "gateway")
	run("cd", "--print", "gateway")
	after, err := os.ReadFile(log)
	if err != nil || string(after) != string(data) {
		t.Fatalf("disabled commands recorded visits: %q, %v", after, err)
	}
	// A broken integration never breaks the path printed for shell substitution.
	on := true
	cfg.Integrations.Zoxide = &on
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho failure >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := run("go", "--print", "gateway"); got != clone+"\n" {
		t.Fatalf("failed integration stdout = %q", got)
	}
	// Nor does a configuration that cannot be read keep cd from an open
	// editor's directory; zoxide, whose switch it holds, is left alone.
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nif [ \"$1\" = add ]; then printf '%s\\n' \"$3\" >> \"$ZO_TEST_LOG\"; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Path(), []byte("instances: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(log)
	if got := run("cd", "--print", "gateway"); got != clone+"\n" {
		t.Fatalf("cd with an unreadable configuration = %q", got)
	}
	if now, _ := os.ReadFile(log); string(now) != string(before) {
		t.Fatalf("an unreadable configuration recorded a visit: %q", now)
	}
}
