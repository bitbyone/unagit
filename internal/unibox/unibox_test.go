package unibox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tobola/unagit/internal/testenv"
	"github.com/tobola/unagit/internal/unibox/api"
)

func TestMain(m *testing.M) {
	testenv.Tools = append(testenv.Tools, "base64", "awk", "bash")
	testenv.Main(m)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
func profileFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude, codex]\ncpus: 2\nmemory: 1g\nstart: start.sh\n")
	write(t, filepath.Join(root, "sandboxes", "node", "Dockerfile"), "FROM example/shell\n")
	write(t, filepath.Join(root, "sandboxes", "node", "start.sh"), "echo started >> \"$HOME/start-count\"\n")
	return root
}

func TestProfilesRejectUnknownFieldsAndEscapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"typo", "dockerfile: Dockerfile\nagents: [claude]\nmemmory: 1g\n"},
		{"both", "dockerfile: Dockerfile\nimage: example/image\nagents: [claude]\n"},
		{"credential", "image: example/image\nagents: [claude]\nenv: [GITHUB_TOKEN]\n"},
		{"script", "image: example/image\nagents: [claude]\nstart: ../../host.sh\n"},
		{"documents", "image: example/image\nagents: [claude]\n---\nimage: another\n"},
		{"seed-undeclared", "image: example/image\nagents: [claude]\nagent_config: {codex: codex.json}\n"},
		{"seed-no-json-config", "image: example/image\nagents: [claude, codex]\nagent_config: {codex: codex.json}\n"},
		{"seed-missing-file", "image: example/image\nagents: [claude]\nagent_config: {claude: absent.json}\n"},
		{"seed-not-an-object", "image: example/image\nagents: [claude]\nagent_config: {claude: start.sh}\n"},
		{"allow-list", "image: example/image\nagents: [claude]\nallow_network: [\"a.example,b.example\"]\n"},
		{"allow-everything-bare", "image: example/image\nagents: [claude]\nallow_network: [\"*\"]\n"},
		{"deny-space", "image: example/image\nagents: [claude]\ndeny_network: [\"a example\"]\n"},
		{"auth-undeclared", "image: example/image\nagents: [claude]\nauth: {codex: oauth}\n"},
		{"auth-unknown", "image: example/image\nagents: [claude]\nauth: {claude: password}\n"},
		{"auth-empty-command", "image: example/image\nagents: [claude]\nauth: {claude: {command: ''}}\n"},
		{"auth-other-key", "image: example/image\nagents: [claude]\nauth: {claude: {script: x}}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := profileFixture(t)
			write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), tc.body)
			if _, err := LoadProfile(root, "node"); err == nil {
				t.Fatal("invalid profile was accepted")
			}
		})
	}
}

func TestContentRevisionsAndApprovalHaveDifferentInputs(t *testing.T) {
	t.Parallel()
	root := profileFixture(t)
	p, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "sandboxes", "node", "Dockerfile"), "FROM example/second\n")
	changed, _ := LoadProfile(root, "node")
	if changed.Revision == p.Revision {
		t.Fatal("uncommitted Dockerfile edit did not change revision")
	}
	if changed.AccessHash() != p.AccessHash() {
		t.Fatal("Dockerfile changed host approval")
	}
	write(t, filepath.Join(root, "sandboxes", "node", "Dockerfile"), "FROM example/third\n")
	third, _ := LoadProfile(root, "node")
	if third.Revision == changed.Revision {
		t.Fatal("two dirty versions shared a revision")
	}
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude, codex]\ncpus: 8\nmemory: 12g\nstart: start.sh\n")
	resource, _ := LoadProfile(root, "node")
	if resource.Revision != third.Revision {
		t.Fatal("resource-only change rebuilt image")
	}
	if resource.AccessHash() != third.AccessHash() {
		t.Fatal("resource-only change changed approval")
	}
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude, codex]\nenv: [BUILD_MODE]\n")
	access, _ := LoadProfile(root, "node")
	if access.AccessHash() == resource.AccessHash() {
		t.Fatal("new host environment access kept approval")
	}
}

func TestInstructionsAreSandboxWideAndConfined(t *testing.T) {
	t.Parallel()
	root := profileFixture(t)
	write(t, filepath.Join(root, "instructions", "team.md"), "team instructions")
	write(t, filepath.Join(root, "instructions", "review.md"), "review instructions")
	p, _ := LoadProfile(root, "node")
	text, err := p.InstructionText([]string{"team", "review"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "team instructions") || strings.Index(text, "team instructions") > strings.Index(text, "review instructions") {
		t.Fatal(text)
	}
	if _, err = p.InstructionText([]string{"missing"}); err == nil {
		t.Fatal("missing instructions accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	write(t, outside, "private")
	if err = os.Symlink(outside, filepath.Join(root, "instructions", "escape.md")); err != nil {
		t.Fatal(err)
	}
	if _, err = p.InstructionText([]string{"escape"}); err == nil {
		t.Fatal("instructions escaped repository")
	}
}

func TestProfileInstructionsAreNamesAndAttachOnce(t *testing.T) {
	t.Parallel()
	root := profileFixture(t)
	write(t, filepath.Join(root, "instructions", "team.md"), "team instructions")
	write(t, filepath.Join(root, "instructions", "backend.md"), "backend instructions")
	write(t, filepath.Join(root, "_shared", "notes.md"), "shared notes")
	write(t, filepath.Join(root, "sandboxes", "node", "instructions.md"), "node's own instructions")
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude]\nstart: start.sh\ninstructions: [backend, team, ../../_shared/notes.md]\n")
	p, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.InstructionText([]string{"team"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, "team instructions") != 1 || !strings.HasPrefix(text, "backend instructions") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "shared notes") || !strings.Contains(text, "node's own instructions") {
		t.Fatal(text)
	}
	if strings.Index(text, "node's own instructions") < strings.Index(text, "shared notes") {
		t.Fatal("the profile's own instructions.md must follow its listed texts")
	}
	if _, err = p.InstructionText([]string{"../../_shared/notes.md"}); err == nil {
		t.Fatal("an attached instruction took a path")
	}
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude]\nstart: start.sh\ninstructions: [absent]\n")
	if _, err = LoadProfile(root, "node"); err == nil {
		t.Fatal("missing instruction name accepted")
	}
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude]\nstart: start.sh\ninstructions: [../../_shared/absent.md]\n")
	if _, err = LoadProfile(root, "node"); err == nil {
		t.Fatal("missing instruction path accepted")
	}
}

func TestOnlyImageInputsTurnTheRevision(t *testing.T) {
	t.Parallel()
	root := profileFixture(t)
	write(t, filepath.Join(root, "sandboxes", "node", "_data", "claude.json"), `{"theme": "dark"}`)
	write(t, filepath.Join(root, "sandboxes", "node", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [claude]\nstart: start.sh\nagent_config: {claude: _data/claude.json}\n")
	before, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "sandboxes", "node", "instructions.md"), "own text")
	write(t, filepath.Join(root, "sandboxes", "node", "_data", "claude.json"), `{"theme": "light"}`)
	after, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Fatal("instructions and seeds are not image inputs; they must not outdate it")
	}
	write(t, filepath.Join(root, "sandboxes", "node", ".sbx", "build.args"), "BASE=example/sbx\n")
	changed, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision == after.Revision {
		t.Fatal("build arguments are image inputs; they must turn the revision")
	}
}

func TestTheRepositoryLayoutIsHeldToItsShape(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, content string }{
		{"stray-top-level", "notes.txt", "x"},
		{"stray-dir", "tools/x.sh", "x"},
		{"file-in-sandboxes", "sandboxes/readme.txt", "x"},
		{"non-md-instruction", "instructions/run.sh", "x"},
		{"reserved-instruction-name", "instructions/instructions.md", "x"},
		{"unhidden-runtime-dir", "sandboxes/node/sbx/build.args", "BASE=x"},
		{"stray-profile-file", "sandboxes/node/notes.txt", "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := profileFixture(t)
			write(t, filepath.Join(root, filepath.FromSlash(tc.path)), tc.content)
			if _, err := LoadProfile(root, "node"); err == nil {
				t.Fatalf("%s was accepted", tc.path)
			}
		})
	}
	t.Run("underscore-readme-and-hidden-pass", func(t *testing.T) {
		t.Parallel()
		root := profileFixture(t)
		write(t, filepath.Join(root, "README.md"), "about")
		write(t, filepath.Join(root, ".gitignore"), "x")
		write(t, filepath.Join(root, "_notes", "ideas.md"), "x")
		if _, err := LoadProfile(root, "node"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("profile-without-yaml", func(t *testing.T) {
		t.Parallel()
		root := profileFixture(t)
		write(t, filepath.Join(root, "sandboxes", "empty", "Dockerfile"), "FROM example/shell\n")
		if _, err := LoadProfile(root, "node"); err == nil {
			t.Fatal("profile directory without profile.yaml was accepted")
		}
	})
}

func TestSharedDirectoryJoinsTheBuildContextAndRevision(t *testing.T) {
	t.Parallel()
	root := profileFixture(t)
	before, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "_shared", "installations", "claude.sh"), "npm install -g claude\n")
	p, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Files["_shared/installations/claude.sh"]; !ok {
		t.Fatalf("_shared missing from the build context: %v", p.Files)
	}
	if p.Revision == before.Revision {
		t.Fatal("a _shared change left the image current")
	}
	write(t, filepath.Join(root, "sandboxes", "node", "_shared", "own.sh"), "x")
	if _, err = LoadProfile(root, "node"); err == nil {
		t.Fatal("a profile's own _shared was accepted")
	}
}

func TestRuntimeBuildArgumentsStaySeparate(t *testing.T) {
	t.Parallel()
	root := profileFixture(t)
	write(t, filepath.Join(root, "sandboxes", "node", ".sbx", "build.args"), "BASE=example/sbx\n")
	write(t, filepath.Join(root, "sandboxes", "node", ".podman", "build.args"), "BASE=example/podman\n")
	p, err := LoadProfile(root, "node")
	if err != nil {
		t.Fatal(err)
	}
	args, err := p.BuildArgs("sbx")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "UNIBOX_RUNTIME=sbx") || !strings.Contains(joined, "BASE=example/sbx") || strings.Contains(joined, "example/podman") {
		t.Fatal(args)
	}
	write(t, filepath.Join(root, "sandboxes", "node", ".sbx", "extra"), "bad")
	if _, err = LoadProfile(root, "node"); err == nil {
		t.Fatal("unknown runtime file accepted")
	}
}

func TestCloneURLsLoseCredentialsAndAmbiguousSSHIsRefused(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{"https://user:fake-secret@example.com/team/repo.git?token=fake": "https://example.com/team/repo.git", "git@example.com:team/repo.git": "https://example.com/team/repo.git", "ssh://git@example.com/team/repo.git": "https://example.com/team/repo.git"} {
		got, err := CloneURL(raw)
		if err != nil || got != want {
			t.Fatalf("%s: %s %v", raw, got, err)
		}
	}
	for _, raw := range []string{"ssh://git@example.com:2222/team/repo.git", "file:///tmp/repo", "/tmp/repo", "http://example.com/repo.git"} {
		if _, err := CloneURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestStateWritesDoNotLoseConcurrentTasks(t *testing.T) {
	t.Parallel()
	paths := Paths{Config: t.TempDir(), Cache: t.TempDir()}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- paths.State(func(s *State) error {
				name := fmt.Sprintf("box-%d", i)
				s.Sandboxes[name] = api.Sandbox{Name: name}
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := paths.ReadState()
	if err != nil || len(state.Sandboxes) != 20 {
		t.Fatalf("state count %d: %v", len(state.Sandboxes), err)
	}
	info, _ := os.Stat(filepath.Join(paths.Config, "state.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
}

type fakeRuntime struct {
	mu                              sync.Mutex
	root                            string
	boxes                           []Box
	images                          []Image
	creates, removes, stops, builds int
	lastSpec                        Spec
	failBuild                       bool
	remotes                         map[string]string
	scriptError                     bool
}

func (f *fakeRuntime) Name() string                    { return "fake" }
func (f *fakeRuntime) Capabilities() Capabilities      { return Capabilities{Isolation: "container"} }
func (f *fakeRuntime) Available(context.Context) error { return nil }
func (f *fakeRuntime) List(context.Context) ([]Box, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Box{}, f.boxes...), nil
}
func (f *fakeRuntime) Images(context.Context) ([]Image, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Image{}, f.images...), nil
}
func (f *fakeRuntime) Build(_ context.Context, p Profile, tag string, _ io.Writer) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.builds++
	if f.failBuild {
		return "", fmt.Errorf("build failed")
	}
	id := fmt.Sprintf("image-%d", f.builds)
	f.images = append(f.images, Image{tag, id})
	return id, nil
}
func (f *fakeRuntime) Create(_ context.Context, s Spec, _ io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	f.lastSpec = s
	f.boxes = append(f.boxes, Box{Name: s.Name, State: "running", ID: fmt.Sprint(f.creates)})
	return nil
}
func (f *fakeRuntime) Stop(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}
func (f *fakeRuntime) Remove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes++
	for i, b := range f.boxes {
		if b.Name == name {
			f.boxes = append(f.boxes[:i], f.boxes[i+1:]...)
			break
		}
	}
	return nil
}
func (f *fakeRuntime) Exec(ctx context.Context, name string, c Command) *exec.Cmd {
	boxRoot := filepath.Join(f.root, name)
	_ = os.MkdirAll(filepath.Join(boxRoot, "home"), 0o700)
	replace := func(s string) string {
		s = strings.ReplaceAll(s, "/var/lib/unibox", filepath.Join(boxRoot, "state"))
		s = strings.ReplaceAll(s, "/work", filepath.Join(boxRoot, "work"))
		s = strings.ReplaceAll(s, "/proc/sys/kernel/random/boot_id", filepath.Join(f.root, "boot"))
		s = strings.ReplaceAll(s, "chown -R agent:agent", ":")
		for from, to := range f.remotes {
			s = strings.ReplaceAll(s, from, to)
		}
		return s
	}
	args := append([]string{}, c.Args...)
	for i, a := range args {
		args[i] = replace(a)
	}
	if f.scriptError {
		args = []string{"sh", "-c", "exit 4"}
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(HostEnvironment(), "HOME="+filepath.Join(boxRoot, "home"))
	if c.Directory != "" {
		cmd.Dir = replace(c.Directory)
	}
	if c.Input != nil {
		b, _ := io.ReadAll(c.Input)
		cmd.Stdin = strings.NewReader(replace(string(b)))
	}
	cmd.Stdout = c.Output
	cmd.Stderr = c.Error
	return cmd
}

func managerFixture(t *testing.T) (*Manager, *fakeRuntime) {
	t.Helper()
	root := profileFixture(t)
	paths := Paths{Config: t.TempDir(), Cache: t.TempDir()}
	var log bytes.Buffer
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(log.String())
		}
	})
	m, err := Open(paths, &log)
	if err != nil {
		t.Fatal(err)
	}
	m.Config.Environments = root
	f := &fakeRuntime{root: t.TempDir(), remotes: map[string]string{}}
	write(t, filepath.Join(f.root, "boot"), "first-boot")
	m.Runtimes = map[string]Runtime{"fake": f}
	if err = m.Approve("node"); err != nil {
		t.Fatal(err)
	}
	if err = m.Build(context.Background(), "node", "fake", false); err != nil {
		t.Fatal(err)
	}
	return m, f
}

func TestSandboxOwnershipAndRuntimeChoice(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	ctx := context.Background()
	b, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node", Runtime: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Runtime != "fake" || b.RuntimeName != "unibox-dev" || b.RuntimeID == "" {
		t.Fatal(b)
	}
	if _, _, _, err = m.Definition(Request{Sandbox: "dev", Runtime: "sbx"}); err == nil {
		t.Fatal("runtime mismatch accepted")
	}
	f.boxes[0].ID = "someone-else"
	if err = m.Remove(ctx, "dev", ""); err == nil {
		t.Fatal("removed externally replaced sandbox")
	}
	if f.removes != 0 {
		t.Fatal("foreign sandbox was removed")
	}
}

func TestStartRunsOncePerBootAndRecreatePreservesFailedBuild(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	ctx := context.Background()
	b, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := m.profile("node")
	if err = m.ensure(ctx, &b, f, p, false, false); err != nil {
		t.Fatal(err)
	}
	countFile := filepath.Join(f.root, b.RuntimeName, "home", "start-count")
	count, _ := os.ReadFile(countFile)
	if string(count) != "started\n" {
		t.Fatalf("start count %q", count)
	}
	write(t, filepath.Join(f.root, "boot"), "second-boot")
	if err = m.ensure(ctx, &b, f, p, false, false); err != nil {
		t.Fatal(err)
	}
	count, _ = os.ReadFile(countFile)
	if string(count) != "started\nstarted\n" {
		t.Fatalf("start count %q", count)
	}
	f.failBuild = true
	if err = m.Recreate(ctx, "dev", true); err == nil {
		t.Fatal("failed build was accepted")
	}
	if f.removes != 0 {
		t.Fatal("old sandbox lost before successful build")
	}
}

func TestActiveSessionsBlockRecreateButNotAnotherTask(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	ctx := context.Background()
	b, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node"})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := m.usage("dev", true)
	if err != nil {
		t.Fatal(err)
	}
	defer usage.Close()
	p, _ := m.profile("node")
	if err = m.ensure(ctx, &b, f, p, false, false); err != nil {
		t.Fatalf("unchanged shared environment blocked: %v", err)
	}
	if err = m.Recreate(ctx, "dev", false); err == nil {
		t.Fatal("recreated active environment")
	}
	if f.removes != 0 {
		t.Fatal("removed active environment")
	}
	if err = m.Stop(ctx, "dev"); err == nil {
		t.Fatal("stopped active environment")
	}
}

func TestInstructionChangesWaitWithoutRebuildingOrRecreating(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	ctx := context.Background()
	b, err := m.Create(ctx, Request{NewSandbox: "dev", Profile: "node"})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(m.Config.Environments, "instructions", "team.md"), "new team rules")
	if err = m.AttachInstructions("dev", []string{"team"}); err != nil {
		t.Fatal(err)
	}
	b, _ = m.sandbox("dev")
	p, _ := m.profile("node")
	usage, _ := m.usage("dev", true)
	if err = m.ensure(ctx, &b, f, p, false, false); err == nil {
		t.Fatal("instructions changed under running agent")
	}
	usage.Close()
	if err = m.ensure(ctx, &b, f, p, false, false); err != nil {
		t.Fatal(err)
	}
	if f.creates != 1 || f.builds != 1 {
		t.Fatalf("creates=%d builds=%d", f.creates, f.builds)
	}
	data, _ := os.ReadFile(filepath.Join(f.root, b.RuntimeName, "home", ".claude", "CLAUDE.md"))
	if !strings.Contains(string(data), "new team rules") {
		t.Fatal("instructions not updated")
	}
}

func TestBuildBoundary(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "list", "-deps", "-json", "../../cmd/unibox")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var p struct{ ImportPath string }
		err = d.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(p.ImportPath, "github.com/tobola/unagit/") && !strings.HasPrefix(p.ImportPath, "github.com/tobola/unagit/internal/unibox") && p.ImportPath != "github.com/tobola/unagit/cmd/unibox" {
			t.Errorf("unibox imports unagit package %s", p.ImportPath)
		}
	}
}

func TestAProfileAskingNothingOfTheHostNeedsNoApproval(t *testing.T) {
	t.Parallel()
	m, _ := managerFixture(t)
	root := m.Config.Environments
	write(t, filepath.Join(root, "sandboxes", "plain", "sandbox.yaml"), "image: example/image\nagents: [claude]\n")
	p, err := LoadProfile(root, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.approved(p); err != nil {
		t.Fatalf("a profile with no host access demanded approval: %v", err)
	}
	write(t, filepath.Join(root, "sandboxes", "plain", "sandbox.yaml"), "image: example/image\nagents: [claude]\nenv: [BUILD_MODE]\n")
	asking, err := LoadProfile(root, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.approved(asking); err == nil {
		t.Fatal("host access ran unapproved")
	}
}

func TestASingleAgentProfileCreatesItsKindOfSandbox(t *testing.T) {
	t.Parallel()
	m, f := managerFixture(t)
	root := m.Config.Environments
	write(t, filepath.Join(root, "sandboxes", "solo", "sandbox.yaml"), "dockerfile: Dockerfile\nagents: [copilot]\n")
	write(t, filepath.Join(root, "sandboxes", "solo", "Dockerfile"), "FROM example/shell\n")
	ctx := context.Background()
	if err := m.Build(ctx, "solo", "fake", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, Request{NewSandbox: "one", Profile: "solo"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	kind := f.lastSpec.Agent
	f.mu.Unlock()
	if kind != "copilot" {
		t.Fatalf("single-agent profile made a %q sandbox; its agent's kind binds the runtime's credentials", kind)
	}
	if _, err := m.Create(ctx, Request{NewSandbox: "two", Profile: "node"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	kind = f.lastSpec.Agent
	f.mu.Unlock()
	if kind != "" {
		t.Fatalf("a profile of several agents must make a plain sandbox, got %q", kind)
	}
}
