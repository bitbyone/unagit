package unibox_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/testenv"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/unibox/uniboxtest"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func TestTheOverviewIsReadFromTheCommands(t *testing.T) {
	t.Parallel()
	fake := uniboxtest.Install(t, uniboxtest.Overview())
	o, err := unibox.New(fake.Path).Overview()
	if err != nil {
		t.Fatal(err)
	}
	if o.Version.Version != "0.5" || o.Default != "web" {
		t.Fatalf("version and default env: %+v", o)
	}
	if len(o.Envs) != 2 || o.Envs[0].Name != "api" || o.Envs[0].Sandbox != "api-box" || o.Envs[0].Agent != "codex" ||
		len(o.Envs[0].Missing) != 1 || o.Envs[1].Repo != "/team" || o.Envs[1].Agent != "claude" {
		t.Fatalf("envs: %+v", o.Envs)
	}
	if len(o.Sandboxes) != 1 || len(o.Sandboxes[0].Tasks) != 2 || o.Sandboxes[0].Tasks[0].Name() != "rate" {
		t.Fatalf("sandboxes: %+v", o.Sandboxes)
	}
	s := o.Sandboxes[0]
	if _, ok := s.RunOf("gateway-1a2b3c4d"); !ok {
		t.Fatal("the run of a task is not found")
	}
	if got := strings.Join(s.AgentIDs(), ","); got != "claude,copilot" || !s.Has("copilot") || s.Has("codex") {
		t.Fatalf("agents: %s", got)
	}
	if got := s.Tasks[0].HostPaths(); len(got) != 1 || got[0] != "/src/gateway" {
		t.Fatalf("mounted: %v", got)
	}
	if o.Context.Name() != "web" || o.Context.Env != "web" {
		t.Fatalf("context: %+v", o.Context)
	}
	if o.Config.DefaultEnv != "web" || o.Config.StopIdle == nil || *o.Config.StopIdle {
		t.Fatalf("configuration: %+v", o.Config)
	}
	if len(o.Secrets) != 2 || o.Secrets[1].Service != "github" || len(o.Secrets[1].UsedBy) != 2 || len(o.Unread) != 0 {
		t.Fatalf("secrets: %+v, unread %v", o.Secrets, o.Unread)
	}
}

func TestWhatTheScreenCanDoWithoutStillLeavesTheOverview(t *testing.T) {
	t.Parallel()
	answers := uniboxtest.Overview()
	answers["secret ls --all --json"] = uniboxtest.Failure("error", "sbx is not signed in", 1)
	answers["env show api --json"] = uniboxtest.Failure("error", "api.sbxenv.yaml: no such file", 1)
	o, err := unibox.New(uniboxtest.Install(t, answers).Path).Overview()
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Sandboxes) != 1 || o.Unread["secrets"] != "sbx is not signed in" {
		t.Fatalf("a failed listing of the secrets: %v, %d sandboxes", o.Unread, len(o.Sandboxes))
	}
	if o.Envs[0].Error == "" || o.Envs[1].Error != "" {
		t.Fatalf("an env that does not load: %+v", o.Envs)
	}
}

func TestAnEnvSaysWhichOfItsArgsAreMissing(t *testing.T) {
	t.Parallel()
	args, err := unibox.New(uniboxtest.Install(t, uniboxtest.Overview()).Path).Args("api")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || !args[0].Missing() || args[0].Name != "jira_user" {
		t.Fatalf("args: %+v", args)
	}
}

func TestTheSandboxDetailIsRead(t *testing.T) {
	t.Parallel()
	c := unibox.New(uniboxtest.Install(t, uniboxtest.Overview()).Path)
	ports, err := c.Ports("web")
	if err != nil || len(ports) != 2 || ports[0].Published[0].URL() != "http://127.0.0.1:3000" {
		t.Fatalf("ports: %+v, %v", ports, err)
	}
	network, err := c.Network("web")
	if err != nil || len(network.Rules) != 1 || network.Blocked[0].Count != 3 {
		t.Fatalf("network: %+v, %v", network, err)
	}
	repos, err := c.Repos("web")
	if err != nil || len(repos) != 1 || repos[0].Name != "adrs" || repos[0].Default != "always" {
		t.Fatalf("repos: %+v, %v", repos, err)
	}
	secrets, err := c.Secrets("web")
	if err != nil || len(secrets) != 2 || secrets[1].Stored || secrets[1].Page == "" {
		t.Fatalf("secrets: %+v, %v", secrets, err)
	}
	res, err := c.Resources("web")
	if err != nil || res.Resources.Memory != "8g" {
		t.Fatalf("resources: %+v, %v", res, err)
	}
}

func TestAnotherAPIVersionIsRefused(t *testing.T) {
	t.Parallel()
	answers := uniboxtest.Overview()
	answers["version --json"] = uniboxtest.JSON(`{"api_version":3,"version":"9.0"}`)
	fake := uniboxtest.Install(t, answers)
	_, err := unibox.New(fake.Path).Overview()
	var version *unibox.VersionError
	if !errors.As(err, &version) || !strings.Contains(err.Error(), "update unagit") {
		t.Fatalf("a newer contract: %v", err)
	}
	answers["version --json"] = uniboxtest.JSON(`{"api_version":1,"version":"0.3","profile_format":6}`)
	_, err = unibox.New(uniboxtest.Install(t, answers).Path).Overview()
	if !errors.As(err, &version) || !strings.Contains(err.Error(), "update unibox") {
		t.Fatalf("an older contract: %v", err)
	}
}

func TestAFailureCarriesItsCodeAndStatus(t *testing.T) {
	t.Parallel()
	fake := uniboxtest.Install(t, map[string]uniboxtest.Answer{
		"sandbox rm web --json":   uniboxtest.Failure("unpushed_work", "task rate has unpushed commits - push them first", 4),
		"sandbox stop web --json": uniboxtest.OK("stopped web"),
	})
	c := unibox.New(fake.Path)
	_, err := c.Do(context.Background(), "sandbox", "rm", "web")
	var failure *unibox.Error
	if !errors.As(err, &failure) || failure.Code != "unpushed_work" || !failure.Refused() || !strings.Contains(err.Error(), "push them first") {
		t.Fatalf("a refusal: %#v", err)
	}
	said, err := c.Do(context.Background(), "sandbox", "stop", "web")
	if err != nil || said != "stopped web" {
		t.Fatalf("a change: %q, %v", said, err)
	}
}

func TestAMissingBinarySaysToInstallIt(t *testing.T) {
	t.Parallel()
	_, err := unibox.New("/nonexistent/unibox").Sandboxes()
	if err == nil || !strings.Contains(err.Error(), "install it") {
		t.Fatalf("missing binary: %v", err)
	}
}

func TestAStreamHandsOverEveryLine(t *testing.T) {
	t.Parallel()
	fake := uniboxtest.Install(t, map[string]uniboxtest.Answer{
		"env update web --yes": {Out: "updating /team\nweb: up to date\n"},
		"env update bad --yes": {Out: "updating /bad\nthe clone has local changes - fix line 3\n", Status: 1},
	})
	c := unibox.New(fake.Path)
	var lines []string
	if err := c.Stream(context.Background(), func(s string) { lines = append(lines, s) }, "env", "update", "web"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "updating /team|web: up to date" {
		t.Fatalf("lines: %q", lines)
	}
	err := c.Stream(context.Background(), func(string) {}, "env", "update", "bad")
	if err == nil || !strings.Contains(err.Error(), "fix line 3") {
		t.Fatalf("a failed stream says its last line: %v", err)
	}
}

// TestAStreamKeepsItsColours: a stream's output goes to a terminal, so
// what colours only a terminal's output - sbx, docker - colours it; the
// line that says why it failed is read without the colours.
func TestAStreamKeepsItsColours(t *testing.T) {
	t.Parallel()
	bin := filepath.Join(t.TempDir(), "unibox")
	script := "#!/bin/sh\nif [ -t 1 ] && ! [ -t 0 ]; then printf '\\033[32mbuilt\\033[0m\\n'; else echo plain; fi\nprintf '\\033[31mno room left\\033[0m\\n' >&2\nexit 1\n"
	if err := testenv.WriteExecutable(bin, []byte(script)); err != nil {
		t.Fatal(err)
	}
	var lines []string
	err := unibox.New(bin).Stream(context.Background(), func(s string) { lines = append(lines, s) }, "env", "update", "web")
	if len(lines) != 2 || lines[0] != "\x1b[32mbuilt\x1b[0m" {
		t.Fatalf("lines: %q", lines)
	}
	if err == nil || !strings.HasSuffix(err.Error(), ": no room left") {
		t.Fatalf("the failure is not its last line, plain: %v", err)
	}
	if unibox.StripANSI("a\x1b]0;title\x07b \x1b[1;38;5;208m[x]\x1b[m") != "ab [x]" {
		t.Fatal("escape sequences are left in")
	}
}

func TestAStartNamesWhatItIsAsked(t *testing.T) {
	t.Parallel()
	s := unibox.Start{Sandbox: "web", Env: "web", Agent: "claude", Dirs: []string{"/w/a", "/w/b"}, Clone: true, Push: true}
	if got := strings.Join(s.Args(), " "); got != "--repo /w/a --repo /w/b -s web -e web --clone --push" {
		t.Fatalf("args: %s", got)
	}
	if got := strings.Join(s.RunArgs(), " "); got != "run --repo /w/a --repo /w/b -s web -e web --clone --push --agent claude" {
		t.Fatalf("run: %s", got)
	}
}
