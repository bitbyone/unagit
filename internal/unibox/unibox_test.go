package unibox_test

import (
	"context"
	"errors"
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

func TestAStartNamesWhatItIsAsked(t *testing.T) {
	t.Parallel()
	s := unibox.Start{Sandbox: "web-1", Profile: "web", Agent: "claude", Dirs: []string{"/w/a", "/w/b"}, Push: true}
	if got := strings.Join(s.Args(), " "); got != "/w/a /w/b -s web-1 -p web --agent claude --push" {
		t.Fatalf("args: %s", got)
	}
}
