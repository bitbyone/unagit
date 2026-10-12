package atbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/atbox/api"
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
	clone := filepath.Join(f.root, "atbox-dev", "work", "refunds", "payments")
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
	_, _, _, usage, taskLock, err := m.prepare(ctx, Request{Sandbox: "dev", Task: "refunds", Agent: "claude"}, true)
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
