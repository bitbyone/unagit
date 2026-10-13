package unibox

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/unibox/api"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s (%v)", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func remoteFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	host := filepath.Join(root, "host")
	gitTest(t, root, "init", "--bare", "--initial-branch=main", remote)
	gitTest(t, root, "clone", remote, host)
	write(t, filepath.Join(host, "code.txt"), "initial\n")
	gitTest(t, host, "add", ".")
	gitTest(t, host, "commit", "-m", "Initial")
	gitTest(t, host, "push", "origin", "main")
	gitTest(t, host, "switch", "-c", "feat/refunds")
	write(t, filepath.Join(host, "feature.txt"), "feature\n")
	gitTest(t, host, "add", ".")
	gitTest(t, host, "commit", "-m", "Feature")
	gitTest(t, host, "push", "-u", "origin", "HEAD")
	gitTest(t, host, "switch", "main")
	return remote, host
}

func TestLocalPathWithBranchDoesNotSelectOrChangeMain(t *testing.T) {
	t.Parallel()
	remote, host := remoteFixture(t)
	url := "https://forge.invalid/team/payments.git"
	gitTest(t, host, "remote", "set-url", "origin", url)
	gitTest(t, host, "config", "url.file://"+remote+".insteadOf", url)
	write(t, filepath.Join(host, "dirty.txt"), "local main edit\n")
	repos, needs, err := ResolveRepositories(context.Background(), Request{Repositories: []RepoInput{{Source: host}}, Branch: "feat/refunds"})
	if err != nil {
		t.Fatal(err)
	}
	if len(needs) != 0 {
		t.Fatalf("main edits affected selected feature: %+v", needs)
	}
	if len(repos) != 1 || repos[0].Branch != "feat/refunds" {
		t.Fatal(repos)
	}
	if got := gitTest(t, host, "branch", "--show-current"); got != "main" {
		t.Fatalf("host branch changed to %s", got)
	}
	_, needs, err = ResolveRepositories(context.Background(), Request{Repositories: []RepoInput{{Source: host}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range needs {
		if n.Code == "default_branch" {
			found = true
		}
	}
	if !found {
		t.Fatal("default branch was not refused")
	}
}

func TestTaskClonesFromForgeAndKeepsOwnEdits(t *testing.T) {
	t.Parallel()
	remote, host := remoteFixture(t)
	m, f := managerFixture(t)
	url := "https://forge.invalid/team/payments.git"
	f.remotes[url] = "file://" + remote
	ctx := context.Background()
	_, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node"})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Sandbox: "dev", Task: "refunds", Agent: "claude", Author: "Person <person@example.com>", Repositories: []RepoInput{{Source: url, Branch: "feat/refunds"}}}
	prepared, err := m.Prepare(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Directory != "/work/refunds" {
		t.Fatal(prepared)
	}
	clone := filepath.Join(f.root, "unibox-dev", "work", "refunds", "payments")
	if got := gitTest(t, clone, "branch", "--show-current"); got != "feat/refunds" {
		t.Fatal(got)
	}
	if got := gitTest(t, host, "branch", "--show-current"); got != "main" {
		t.Fatal("host changed")
	}
	write(t, filepath.Join(clone, "agent.txt"), "uncommitted agent work\n")
	if err = m.Recreate(ctx, "dev", false); err == nil {
		t.Fatal("recreated sandbox with uncommitted files")
	}
	if f.removes != 0 {
		t.Fatal("discarded guest work")
	}
	if _, err = m.Prepare(ctx, req); err == nil {
		t.Fatal("prepare overwrote guest edits")
	}
	// The failed explicit preparation marks the task incomplete; restore readiness
	// as a successfully started task would have before the agent made its edits.
	b, _ := m.sandbox("dev")
	b.Tasks[0].State = "ready"
	if err = m.saveBox(b); err != nil {
		t.Fatal(err)
	}
	_, _, _, usage, taskLock, err := m.prepare(ctx, Request{Sandbox: "dev", Task: "refunds"}, true)
	if err != nil {
		t.Fatalf("cannot resume own edits: %v", err)
	}
	usage.Close()
	taskLock.Close()
	if content, _ := os.ReadFile(filepath.Join(clone, "agent.txt")); string(content) != "uncommitted agent work\n" {
		t.Fatal("own edits changed")
	}
	gitTest(t, clone, "add", "agent.txt")
	gitTest(t, clone, "commit", "-m", "Agent work")
	message := gitTest(t, clone, "log", "-1", "--format=%B")
	if strings.Count(message, "Co-Authored-By: Claude <noreply@anthropic.com>") != 1 {
		t.Fatal(message)
	}
	if err = m.Remove(ctx, "dev", ""); err == nil {
		t.Fatal("removed sandbox with unpushed commit")
	}
	gitTest(t, clone, "push", "origin", "HEAD")
	if err = m.Remove(ctx, "dev", ""); err != nil {
		t.Fatal(err)
	}
	if f.removes != 1 {
		t.Fatal("safe sandbox was not removed")
	}
}

func TestTaskDefaultBranchAndMissingBranchAreRefusedInsideGuest(t *testing.T) {
	t.Parallel()
	for _, branch := range []string{"main", "missing"} {
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			remote, _ := remoteFixture(t)
			m, f := managerFixture(t)
			url := "https://forge.invalid/team/payments.git"
			f.remotes[url] = "file://" + remote
			ctx := context.Background()
			if _, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node"}); err != nil {
				t.Fatal(err)
			}
			_, err := m.Prepare(ctx, Request{Sandbox: "dev", Task: "refused", Agent: "claude", Author: "Person <person@example.com>", Repositories: []RepoInput{{Source: url, Branch: branch}}})
			if err == nil {
				t.Fatal("unsafe branch accepted")
			}
		})
	}
}

func TestPushCreatesABranchNobodyHasFromTheHostHead(t *testing.T) {
	t.Parallel()
	remote, host := remoteFixture(t)
	url := "https://forge.invalid/team/payments.git"
	gitTest(t, host, "remote", "set-url", "origin", url)
	gitTest(t, host, "config", "url.file://"+remote+".insteadOf", url)
	ctx := context.Background()
	req := Request{Repositories: []RepoInput{{Source: host}}, Branch: "feat/new", Push: true}
	repos, needs, err := ResolveRepositories(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	missing := false
	for _, n := range needs {
		if n.Code == "branch_missing" {
			missing = true
		}
	}
	if !missing {
		t.Fatalf("a branch nobody has was not reported: %+v", needs)
	}
	if err = PushRepositories(ctx, repos); err != nil {
		t.Fatal(err)
	}
	head := gitTest(t, host, "rev-parse", "HEAD")
	if tip := gitTest(t, host, "ls-remote", "origin", "refs/heads/feat/new"); !strings.HasPrefix(tip, head) {
		t.Fatalf("forge branch is not the host HEAD: %s", tip)
	}
	if got := gitTest(t, host, "branch", "--show-current"); got != "main" {
		t.Fatalf("host branch changed to %s", got)
	}
	if locals := gitTest(t, host, "branch", "--list", "feat/new"); locals != "" {
		t.Fatalf("a local branch was made: %s", locals)
	}
	_, needs, err = ResolveRepositories(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(needs) != 0 {
		t.Fatalf("created branch still blocked: %+v", needs)
	}
}

func TestAgentConfigSeedMergesAndKeepsAgentState(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	root := m.Config.Environments
	write(t, filepath.Join(root, "sandboxes", "node", "_data", "claude.json"), `{"theme": "dark", "hasCompletedOnboarding": true, "nested": {"a": 1}}`)
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude, codex]\ncpus: 2\nmemory: 1g\nstart: start.sh\nagent_config: {claude: _data/claude.json}\n")
	ctx := context.Background()
	if err := m.Build(ctx, "node", "fake", false); err != nil {
		t.Fatal(err)
	}
	// The agent's own state, written before the sandbox is ensured.
	home := filepath.Join(f.root, "unibox-dev", "home")
	write(t, filepath.Join(home, ".claude.json"), `{"theme": "light", "projects": {"/work/x": {"trusted": true}}, "nested": {"a": 2, "keep": "yes"}}`)
	if _, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node"}); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err = json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := read()
	if got["theme"] != "dark" || got["hasCompletedOnboarding"] != true {
		t.Fatalf("seed keys were not enforced: %v", got)
	}
	if _, ok := got["projects"].(map[string]any)["/work/x"]; !ok {
		t.Fatalf("agent state was lost: %v", got)
	}
	nested := got["nested"].(map[string]any)
	if nested["a"] != float64(1) || nested["keep"] != "yes" {
		t.Fatalf("nested merge went wrong: %v", nested)
	}
	// The agent works on: its own additions survive the next initialization.
	got["projects"].(map[string]any)["/work/y"] = map[string]any{"trusted": true}
	b, _ := json.Marshal(got)
	write(t, filepath.Join(home, ".claude.json"), string(b))
	box, err := m.sandbox("dev")
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ensure(ctx, &box, f, p, false, false); err != nil {
		t.Fatal(err)
	}
	got = read()
	projects := got["projects"].(map[string]any)
	if _, ok := projects["/work/y"]; !ok {
		t.Fatalf("agent state added later was lost: %v", got)
	}
	if got["theme"] != "dark" {
		t.Fatalf("seed key was not kept enforced: %v", got)
	}
}

func TestPushCreatesABranchOnTheForgeFromAURL(t *testing.T) {
	t.Parallel()
	remote, _ := remoteFixture(t)
	url := "https://forge-url-push.invalid/team/payments.git"
	gitTest(t, ".", "config", "--global", "url.file://"+remote+".insteadOf", url)
	ctx := context.Background()
	req := Request{Repositories: []RepoInput{{Source: url, Branch: "feat/from-url"}}, Push: true, Author: "Person <person@example.com>"}
	repos, needs, err := ResolveRepositories(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(needs) != 0 {
		t.Fatalf("url repository raised host needs: %+v", needs)
	}
	if err = PushRepositories(ctx, repos); err != nil {
		t.Fatal(err)
	}
	main := gitTest(t, remote, "rev-parse", "refs/heads/main")
	if got := gitTest(t, remote, "rev-parse", "refs/heads/feat/from-url"); got != main {
		t.Fatalf("forge branch is not the default tip: %s != %s", got, main)
	}
	// A second push finds the branch and leaves it alone.
	if err = PushRepositories(ctx, repos); err != nil {
		t.Fatal(err)
	}
}

func TestIdleStoppingCountsEverySessionAndOwnsOnlyItsSandbox(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	b, err := m.Create(context.Background(), Request{NewSandbox: "dev", Profile: "node"})
	if err != nil {
		t.Fatal(err)
	}
	u1, _ := m.usage("dev", true)
	u2, _ := m.usage("dev", true)
	r1, l1, err := m.recordRun(b, api.Task{ID: "first"}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	r2, l2, err := m.recordRun(b, api.Task{ID: "second"}, "codex")
	if err != nil {
		t.Fatal(err)
	}
	m.finishRun(b, f, r1, l1, u1, nil)
	if f.stops != 0 {
		t.Fatal("stopped while another session was alive")
	}
	m.finishRun(b, f, r2, l2, u2, nil)
	if f.stops != 1 {
		t.Fatal("did not stop last session's sandbox")
	}
}

func TestATaskNamedAloneFindsItsSandbox(t *testing.T) {
	t.Parallel()
	m, _ := managerFixture(t)
	// Two sandboxes holding one name is state from before names were unique.
	ctx := context.Background()
	for _, name := range []string{"dev", "ci"} {
		if _, err := m.Create(ctx, Request{NewSandbox: name, Profile: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	for name, ids := range map[string][]string{"dev": {"refunds", "shared"}, "ci": {"shared"}} {
		b, _ := m.sandbox(name)
		for _, id := range ids {
			b.Tasks = append(b.Tasks, api.Task{ID: id, Sandbox: name, Directory: "/work/" + id, Agent: "claude", State: "ready"})
		}
		if err := m.saveBox(b); err != nil {
			t.Fatal(err)
		}
	}
	if b, _, _, err := m.Definition(Request{Task: "refunds"}); err != nil || b.Name != "dev" {
		t.Fatalf("%s %v", b.Name, err)
	}
	if _, _, _, err := m.Definition(Request{Task: "shared"}); err == nil || !strings.Contains(err.Error(), "ci, dev; remove the one not wanted") {
		t.Fatalf("%v", err)
	}
	if _, _, _, err := m.Definition(Request{Task: "missing"}); err == nil || !strings.Contains(err.Error(), "unibox tasks lists them") {
		t.Fatalf("%v", err)
	}
	// A new task named by the user still needs to be told where to go.
	if _, _, _, err := m.Definition(Request{Task: "missing", Repositories: []RepoInput{{Source: "."}}}); err == nil || !strings.Contains(err.Error(), "no sandbox chosen") {
		t.Fatalf("%v", err)
	}
}

func TestTaskNamesAreUniqueAcrossSandboxes(t *testing.T) {
	t.Parallel()
	remote, _ := remoteFixture(t)
	m, f := managerFixture(t)
	url := "https://forge.invalid/team/payments.git"
	f.remotes[url] = "file://" + remote
	ctx := context.Background()
	for _, name := range []string{"dev", "ci"} {
		if _, err := m.Create(ctx, Request{NewSandbox: name, Profile: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	req := func(sandbox, task string) Request {
		return Request{Sandbox: sandbox, Task: task, Agent: "claude", Author: "Person <person@example.com>", Repositories: []RepoInput{{Source: url, Branch: "feat/refunds"}}}
	}
	if _, err := m.Prepare(ctx, req("dev", "refunds")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Prepare(ctx, req("ci", "refunds")); err == nil || !strings.Contains(err.Error(), "already exists in sandbox dev") {
		t.Fatalf("%v", err)
	}
	ids := map[string]string{}
	for _, name := range []string{"dev", "ci", "ci"} {
		p, err := m.Prepare(ctx, req(name, ""))
		if err != nil {
			t.Fatal(err)
		}
		if old, ok := ids[name]; ok && old != p.Task {
			t.Fatalf("the same repositories in %s became %s and %s", name, old, p.Task)
		}
		ids[name] = p.Task
	}
	if ids["dev"] == ids["ci"] || !strings.HasPrefix(ids["ci"], "payments-") {
		t.Fatal(ids)
	}
}
