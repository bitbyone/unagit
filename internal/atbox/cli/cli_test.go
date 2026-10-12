package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tobola/unagit/internal/atbox"
	"github.com/tobola/unagit/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func TestBranchOrderSupportsDefaultsAndIndividualOverrides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		args     []string
		global   string
		branches []string
	}{
		{"shared", []string{"--branch", "feat/shared", "--repo", "./api", "--repo", "./web"}, "feat/shared", []string{"", ""}},
		{"override", []string{"--branch=feat/shared", "--repo=./api", "--repo", "./web", "--branch", "feat/web", "--repo", "./worker"}, "feat/shared", []string{"", "feat/web", ""}},
		{"per-repo", []string{"--repo", "https://forge.invalid/api.git", "--branch", "feat/api", "--repo", "https://forge.invalid/web.git", "--branch=feat/web"}, "", []string{"feat/api", "feat/web"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var req atbox.Request
			c := &cobra.Command{Use: "test", Run: func(*cobra.Command, []string) {}}
			taskFlags(c, &req)
			c.SetArgs(tc.args)
			if err := c.Execute(); err != nil {
				t.Fatal(err)
			}
			if req.Branch != tc.global {
				t.Fatal(req.Branch)
			}
			got := []string{}
			for _, r := range req.Repositories {
				got = append(got, r.Branch)
			}
			if !reflect.DeepEqual(got, tc.branches) {
				t.Fatalf("%v != %v", got, tc.branches)
			}
		})
	}
}

func cliFixture(t *testing.T) (atbox.Paths, string) {
	t.Helper()
	paths := atbox.Paths{Config: t.TempDir(), Cache: t.TempDir()}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node", "profile.yaml"), []byte("image: example/image\nagents: [claude]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := paths.SaveConfig(atbox.Config{Environments: root, Runtimes: map[string]atbox.RuntimeConfig{}}); err != nil {
		t.Fatal(err)
	}
	return paths, root
}

func TestJSONListsAreSingleVersionedObjects(t *testing.T) {
	t.Parallel()
	paths, _ := cliFixture(t)
	for _, args := range [][]string{{"profiles", "--json"}, {"config", "get", "--json"}, {"ls", "--json"}, {"instructions", "node", "--json"}} {
		var out, log bytes.Buffer
		c := New(paths, strings.NewReader(""), &out, &log)
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var value map[string]any
		d := json.NewDecoder(&out)
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if value["api_version"] != float64(1) {
			t.Fatal(value)
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			t.Fatalf("extra stdout: %v", err)
		}
	}
}

func TestYesDoesNotApproveHostAccessAndJSONDoesNotPrompt(t *testing.T) {
	t.Parallel()
	paths, _ := cliFixture(t)
	var out, log bytes.Buffer
	c := New(paths, strings.NewReader("yes\n"), &out, &log)
	c.SetArgs([]string{"create", "dev", "--profile", "node", "--yes", "--json"})
	err := c.Execute()
	if err == nil || !strings.Contains(err.Error(), "unapproved") {
		t.Fatalf("%v", err)
	}
	s, err := paths.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Approvals) != 0 {
		t.Fatal("--yes approved access")
	}
	if strings.Contains(log.String(), "[y/N]") {
		t.Fatal("JSON prompted")
	}
}

func TestExplicitApprovalAndStrictConfig(t *testing.T) {
	t.Parallel()
	paths, _ := cliFixture(t)
	for _, args := range [][]string{{"approve", "node", "--json"}, {"config", "set", "runtimes.sbx.stop_idle", "false"}, {"config", "set", "default_runtime", "sbx"}} {
		c := New(paths, strings.NewReader(""), io.Discard, io.Discard)
		c.SetArgs(args)
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _ := paths.LoadConfig()
	if cfg.StopIdle("sbx") || cfg.DefaultRuntime != "sbx" {
		t.Fatal(cfg)
	}
	c := New(paths, strings.NewReader(""), io.Discard, io.Discard)
	c.SetArgs([]string{"config", "set", "runtimes.sbx.stop_idel", "false"})
	if err := c.Execute(); err == nil {
		t.Fatal("unknown setting accepted")
	}
}

func TestHelpListsLifecycleAndMultipleRepositories(t *testing.T) {
	t.Parallel()
	paths, _ := cliFixture(t)
	var out bytes.Buffer
	c := New(paths, strings.NewReader(""), &out, io.Discard)
	c.SetArgs([]string{"run", "--help"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--repo", "repeatable", "--branch", "shared branch", "--runtime", "--task", "--push"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help lacks %s", want)
		}
	}
}
