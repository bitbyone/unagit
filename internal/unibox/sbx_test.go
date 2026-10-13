package unibox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/testenv"
)

func mustWriteExecutable(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := testenv.WriteExecutable(path, content); err != nil {
		t.Fatal(err)
	}
}

func TestSBXUsesOnlyApprovedMountsAndEnvironment(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "argv")
	env := filepath.Join(t.TempDir(), "env")
	mustWriteExecutable(t, filepath.Join(bin, "sbx"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+Quote(log)+"\nenv > "+Quote(env)+"\n"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GITHUB_TOKEN", "fake-secret-for-test")
	t.Setenv("SSH_AUTH_SOCK", "fake-ssh-socket")
	t.Setenv("BUILD_MODE", "test")
	r := &SBX{}
	err := r.Create(context.Background(), Spec{Name: "unibox-dev", Image: "unibox/node:abc", CPUs: 2, Memory: "1g", Access: HostAccess{Env: []string{"BUILD_MODE"}, Caches: []Cache{{Host: "/approved/cache", Env: "CACHE_HOME"}}, DenyNetwork: []string{"private.example"}}}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	argv, _ := os.ReadFile(log)
	environment, _ := os.ReadFile(env)
	for _, bad := range []string{"fake-secret-for-test", "fake-ssh-socket", "GITHUB_TOKEN", "SSH_AUTH_SOCK"} {
		if strings.Contains(string(argv)+string(environment), bad) {
			t.Errorf("leaked %s", bad)
		}
	}
	for _, want := range []string{"--skills\noff\n", "--pull\nnever\n", "shell\n/approved/cache:ro\n", "-e\nBUILD_MODE\n", "--deny-network\nprivate.example\n"} {
		if !strings.Contains(string(argv), want) {
			t.Errorf("missing %q in %s", want, argv)
		}
	}
	if !strings.Contains(string(environment), "BUILD_MODE=test") {
		t.Fatal("approved value missing")
	}
}

func TestSBXChecksSSHForwardingAndParsesRuntimeInventory(t *testing.T) {
	bin := t.TempDir()
	settings := filepath.Join(t.TempDir(), "settings")
	write(t, settings, `{"value":true}`)
	mustWriteExecutable(t, filepath.Join(bin, "sbx"), []byte("#!/bin/sh\ncase \"$1\" in\nsettings) cat "+Quote(settings)+" ;;\nls) printf '%s\\n' '{\"sandboxes\":[{\"name\":\"unibox-dev\",\"id\":\"vm-id\",\"status\":\"stopped\",\"memory\":\"1g\"}]}' ;;\ntemplate) printf '%s\\n' '{\"images\":[{\"repository\":\"docker.io/unibox/node\",\"tag\":\"abc\",\"id\":\"image-id\"}]}' ;;\nesac\n"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &SBX{}
	if err := r.Available(context.Background()); err == nil {
		t.Fatal("forwarding was accepted")
	}
	write(t, settings, `{"value":false}`)
	if err := r.Available(context.Background()); err != nil {
		t.Fatal(err)
	}
	boxes, err := r.List(context.Background())
	if err != nil || len(boxes) != 1 || boxes[0].ID != "vm-id" || boxes[0].State != "stopped" {
		t.Fatalf("%+v %v", boxes, err)
	}
	images, err := r.Images(context.Background())
	if err != nil || len(images) != 1 || images[0].Reference != "unibox/node:abc" {
		t.Fatalf("%+v %v", images, err)
	}
}

func TestMissingAgentFailsBeforeLoadingImage(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "log")
	mustWriteExecutable(t, filepath.Join(bin, "docker"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> "+Quote(log)+"\n[ \"$1\" != run ]\n"))
	mustWriteExecutable(t, filepath.Join(bin, "sbx"), []byte("#!/bin/sh\necho loaded >> "+Quote(log)+"\n"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p, _ := LoadProfile(profileFixture(t), "node")
	r := &SBX{Cache: t.TempDir()}
	if _, err := r.Build(context.Background(), p, "unibox/node:test", io.Discard); err == nil || !strings.Contains(err.Error(), "declares claude") {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "loaded") {
		t.Fatal("loaded invalid image")
	}
}

func TestSavedStateContainsNoEnvironmentValues(t *testing.T) {
	t.Parallel()
	m, _ := managerFixture(t)
	b, err := json.Marshal(m.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "TOKEN") {
		t.Fatal("configuration stores credentials")
	}
}

func TestSeveralAgentsCreateFromAGeneratedKit(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "argv")
	mustWriteExecutable(t, filepath.Join(bin, "sbx"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+Quote(log)+"\n"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &SBX{Cache: t.TempDir()}
	spec := Spec{Name: "unibox-home", Image: "unibox/personal:abc", CPUs: 2, Agents: []string{"claude", "codex", "copilot"}}
	if err := r.Create(context.Background(), spec, io.Discard); err != nil {
		t.Fatal(err)
	}
	argv, _ := os.ReadFile(log)
	dir := r.KitDir("unibox-home")
	if dir == "" {
		t.Fatal("no kit was kept for inspection")
	}
	if !strings.Contains(string(argv), dir) || strings.Contains(string(argv), "--template") {
		t.Fatalf("create did not use the kit alone: %s", argv)
	}
	descriptor, err := os.ReadFile(filepath.Join(dir, "unibox-home.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"service: anthropic", "service: openai", "service: github", "api.anthropic.com:443", "chatgpt.com:443", "api.githubcopilot.com:443", "kind: workload"} {
		if !strings.Contains(string(descriptor), want) {
			t.Fatalf("descriptor lacks %q:\n%s", want, descriptor)
		}
	}
	from, err := os.ReadFile(filepath.Join(dir, "unibox-home.dockerfile"))
	if err != nil || strings.TrimSpace(string(from)) != "FROM unibox/personal:abc" {
		t.Fatalf("%q %v", from, err)
	}
	// One agent keeps the template and its kind; no kit remains.
	single := Spec{Name: "unibox-solo", Image: "unibox/my2n:abc", Agent: "copilot", Agents: []string{"copilot"}}
	if err := r.Create(context.Background(), single, io.Discard); err != nil {
		t.Fatal(err)
	}
	argv, _ = os.ReadFile(log)
	if !strings.Contains(string(argv), "--template\nunibox/my2n:abc") || !strings.Contains(string(argv), "copilot") {
		t.Fatalf("single agent lost its kind: %s", argv)
	}
	if r.KitDir("unibox-solo") != "" {
		t.Fatal("a kit was made for a single agent")
	}
}
