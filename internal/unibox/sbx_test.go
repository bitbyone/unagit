package unibox

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/testenv"
	"gopkg.in/yaml.v3"
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

func TestAGeneratedKitsCredentialsAreApprovedAndOnlyAdded(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "sbx", "credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("bindings:\n  openai:\n    apiKey:\n      domains: [api.openai.com, mine.example]\n  other:\n    apiKey:\n      domains: [x.example]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := writeBindings(file, []string{"claude", "codex"}, &log); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Bindings map[string]map[string]struct{ Domains []string }
	}
	data, _ := os.ReadFile(file)
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	b := doc.Bindings
	if !slices.Equal(b["openai"]["apiKey"].Domains, []string{"api.openai.com", "mine.example"}) ||
		!slices.Equal(b["openai"]["oauth"].Domains, []string{"auth.openai.com", "chatgpt.com"}) ||
		!slices.Equal(b["anthropic"]["oauth"].Domains, []string{"platform.claude.com"}) ||
		!slices.Equal(b["other"]["apiKey"].Domains, []string{"x.example"}) || b["github"] != nil {
		t.Fatalf("%s", data)
	}
	if !strings.Contains(log.String(), "openai oauth chatgpt.com") || strings.Contains(log.String(), "apiKey api.openai.com") {
		t.Fatal(log.String())
	}
	log.Reset()
	if err := writeBindings(file, []string{"claude", "codex"}, &log); err != nil || log.Len() != 0 {
		t.Fatalf("approved twice: %v %s", err, log.String())
	}
	// Every approved domain is one the kit declares, and nothing beyond.
	for id, want := range sbxKitBindings {
		for _, d := range append(slices.Clone(want.APIKey), want.OAuth...) {
			if !strings.Contains(sbxKitCredentials[id], d) {
				t.Fatalf("%s approves %s, which its declaration does not name", id, d)
			}
		}
	}
}

func TestCodexIsSignedInByTheSandboxProxy(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codex, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(codex, "config.toml")
	user := "model = \"gpt-5\"\nmodel_provider = \"mine\"\n\n[tui]\nmodel_provider = \"kept\"\n"
	if err := os.WriteFile(cfg, []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(mode string) (string, string) {
		cmd := exec.Command("sh", "-c", codexSetup)
		cmd.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME=", "SBX_CRED_OPENAI_MODE="+mode)
		if out, err := cmd.CombinedOutput(); err != nil || len(out) > 0 {
			t.Fatalf("%s: %v %s", mode, err, out)
		}
		c, _ := os.ReadFile(cfg)
		a, _ := os.ReadFile(filepath.Join(codex, "auth.json"))
		return string(c), string(a)
	}
	c, a := run("oauth")
	if again, _ := run("oauth"); again != c {
		t.Fatalf("not idempotent:\n%s\n---\n%s", c, again)
	}
	table := strings.Index(c, "[tui]")
	for _, want := range []string{"forced_login_method = \"api\"", "model_provider = \"sandboxd\""} {
		if i := strings.Index(c, want); i < 0 || i > table {
			t.Fatalf("%q not a top-level key:\n%s", want, c)
		}
	}
	if strings.Contains(c, "\"mine\"") || !strings.Contains(c, "model_provider = \"kept\"") || !strings.Contains(c, "[model_providers.sandboxd]") || strings.Index(c, "[model_providers.sandboxd]") < table {
		t.Fatalf("%s", c)
	}
	if !strings.Contains(a, "\"proxy-managed\"") {
		t.Fatal(a)
	}
	c, a = run("apikey")
	if strings.Contains(c, "sandboxd") || !strings.Contains(c, "forced_login_method") || a == "" {
		t.Fatalf("%s %s", c, a)
	}
	c, a = run("none")
	if strings.Contains(c, "unibox") || strings.Contains(c, "forced_login_method") || a != "" {
		t.Fatalf("none kept the proxy's login:\n%s %s", c, a)
	}
	// A login the user made in the sandbox is not the proxy's to take away.
	if err := os.WriteFile(filepath.Join(codex, "auth.json"), []byte(`{"tokens":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, a = run("none"); a == "" {
		t.Fatal("removed the user's own login")
	}
}

func TestTheStoredOAuthLoginIsDeclaredFirst(t *testing.T) {
	t.Parallel()
	agents := []string{"claude", "codex", "copilot"}
	if got := credentialOrder(agents, nil); !slices.Equal(got, agents) {
		t.Fatal(got)
	}
	if got := credentialOrder(agents, map[string]bool{"openai": true}); !slices.Equal(got, []string{"codex", "claude", "copilot"}) {
		t.Fatal(got)
	}
	r := &SBX{Cache: t.TempDir()}
	dir, err := r.writeKit(Spec{Name: "unibox-home", Image: "x", Agents: agents}, map[string]bool{"openai": true})
	if err != nil {
		t.Fatal(err)
	}
	kit, _ := os.ReadFile(filepath.Join(dir, "unibox-home.yaml"))
	if o, a := strings.Index(string(kit), "service: openai"), strings.Index(string(kit), "service: anthropic"); o < 0 || a < 0 || o > a {
		t.Fatalf("sbx would wire Anthropic's OAuth, not the stored one:\n%s", kit)
	}
}
