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
	if o.Version.Version != "0.3" || o.Version.ProfileFormat != 6 || o.Environments != "/envs" {
		t.Fatalf("version and repository: %+v", o)
	}
	if len(o.Profiles) != 2 || o.Profiles[0].Images["sbx"] != "built" || o.Profiles[1].Error == "" {
		t.Fatalf("profiles: %+v", o.Profiles)
	}
	if len(o.Sandboxes) != 2 || len(o.Sandboxes[0].Tasks) != 2 || o.Sandboxes[0].Tasks[0].Name() != "rate" {
		t.Fatalf("sandboxes: %+v", o.Sandboxes)
	}
	if _, ok := o.Sandboxes[0].RunOf("gateway-1a2b3c4d"); !ok {
		t.Fatal("the run of a task is not found")
	}
	if o.Context.Set.Sandbox != "web-1" || o.Context.Profile != "web" {
		t.Fatalf("context: %+v", o.Context)
	}
	if len(o.Runtimes) != 2 || !o.Runtimes[0].Default || !o.Runtimes[1].Experimental {
		t.Fatalf("runtimes: %+v", o.Runtimes)
	}
	if o.Config.DefaultRuntime != "sbx" || o.Config.Runtimes["sbx"].StopIdle == nil || *o.Config.Runtimes["sbx"].StopIdle {
		t.Fatalf("configuration: %+v", o.Config)
	}
	if len(o.Secrets.Names) != 3 || o.Secrets.Store != "login keychain" {
		t.Fatalf("secrets: %+v", o.Secrets)
	}
	if o.Values.Shared["git_name"] != "Dev" || o.Values.Profiles["web"]["npm_scope"] != "acme" || len(o.Unread) != 0 {
		t.Fatalf("values: %+v, unread %v", o.Values, o.Unread)
	}
}

func TestWhatTheScreenCanDoWithoutStillLeavesTheOverview(t *testing.T) {
	t.Parallel()
	answers := uniboxtest.Overview()
	answers["secret ls --json"] = uniboxtest.Failure("store_locked", "the login keychain is locked", 1)
	o, err := unibox.New(uniboxtest.Install(t, answers).Path).Overview()
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Sandboxes) != 2 || o.Unread["secrets"] != "the login keychain is locked" {
		t.Fatalf("a failed listing of the secrets: %v, %d sandboxes", o.Unread, len(o.Sandboxes))
	}
}

func TestAProfileSaysWhichOfItsValuesAreMissing(t *testing.T) {
	t.Parallel()
	asked, err := unibox.New(uniboxtest.Install(t, uniboxtest.Overview()).Path).Asked("web")
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 || asked[0].Missing() || !asked[2].Missing() || asked[2].Name != "jira_user" {
		t.Fatalf("asked: %+v", asked)
	}
}

func TestAnotherAPIVersionIsRefused(t *testing.T) {
	t.Parallel()
	answers := uniboxtest.Overview()
	answers["version --json"] = uniboxtest.JSON(`{"api_version":2,"version":"9.0"}`)
	fake := uniboxtest.Install(t, answers)
	_, err := unibox.New(fake.Path).Overview()
	var version *unibox.VersionError
	if !errors.As(err, &version) || !strings.Contains(err.Error(), "update unagit") {
		t.Fatalf("a newer contract: %v", err)
	}
}

func TestAFailureCarriesItsCodeAndStatus(t *testing.T) {
	t.Parallel()
	fake := uniboxtest.Install(t, map[string]uniboxtest.Answer{
		"sandbox rm web-1 --json":   uniboxtest.Failure("unpushed_work", "task rate has unpushed commits - push them first", 4),
		"sandbox stop web-1 --json": uniboxtest.OK("stopped web-1"),
	})
	c := unibox.New(fake.Path)
	_, err := c.Do(context.Background(), "sandbox", "rm", "web-1")
	var failure *unibox.Error
	if !errors.As(err, &failure) || failure.Code != "unpushed_work" || !failure.Refused() || !strings.Contains(err.Error(), "push them first") {
		t.Fatalf("a refusal: %#v", err)
	}
	said, err := c.Do(context.Background(), "sandbox", "stop", "web-1")
	if err != nil || said != "stopped web-1" {
		t.Fatalf("a change: %q, %v", said, err)
	}
}

func TestAMissingBinarySaysToInstallIt(t *testing.T) {
	t.Parallel()
	_, err := unibox.New("/nonexistent/unibox").Profiles()
	if err == nil || !strings.Contains(err.Error(), "install it") {
		t.Fatalf("missing binary: %v", err)
	}
}

func TestAStreamHandsOverEveryLine(t *testing.T) {
	t.Parallel()
	fake := uniboxtest.Install(t, map[string]uniboxtest.Answer{
		"profile build web --yes": {Out: "building web\nloaded unibox/web:1\n"},
		"profile build bad --yes": {Out: "building bad\nthe Dockerfile does not parse - fix line 3\n", Status: 1},
	})
	c := unibox.New(fake.Path)
	var lines []string
	if err := c.Stream(context.Background(), func(s string) { lines = append(lines, s) }, "profile", "build", "web"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "building web|loaded unibox/web:1" {
		t.Fatalf("lines: %q", lines)
	}
	err := c.Stream(context.Background(), func(string) {}, "profile", "build", "bad")
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
	err := unibox.New(bin).Stream(context.Background(), func(s string) { lines = append(lines, s) }, "profile", "build", "web")
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
	s := unibox.Start{Sandbox: "web-1", Profile: "web", Agent: "claude", Dirs: []string{"/w/a", "/w/b"}, Push: true}
	if got := strings.Join(s.Args(), " "); got != "/w/a /w/b -s web-1 -p web --agent claude --push" {
		t.Fatalf("args: %s", got)
	}
}
