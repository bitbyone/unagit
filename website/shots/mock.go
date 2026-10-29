//go:build shots

// Command mock lays out a made-up world for the website's screenshots and
// for trying unagit out: a configuration of unagit's with two servers,
// their repositories and merge requests as the lists cache them, a vault
// sealed with the passphrase "demo", real clones on disk with branches and
// changes, and a unibox stand-in answering for a few sandboxes. serve
// answers for both servers on the loopback, as a GitLab would, so a merge
// request's detail, commits and threads read; nothing leaves the machine.
// make demo runs the real unagit over it; shoot.py photographs it.
//
//	go run -tags shots ./website/shots <dir>        # lay the world out
//	go run -tags shots ./website/shots serve <dir>  # answer as its servers
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
	"github.com/tobola/unagit/internal/secret"
)

// port is where serve answers for the world's servers.
const port = "8929"

func main() {
	switch {
	case len(os.Args) == 3 && os.Args[1] == "serve":
		serve(os.Args[2])
		return
	case len(os.Args) != 2:
		fmt.Fprintln(os.Stderr, "usage: mock <dir> | mock serve <dir>")
		os.Exit(2)
	}
	dir, err := filepath.Abs(os.Args[1])
	check(err)
	check(os.RemoveAll(dir))
	// The screenshots run with HOME at dir, so paths read as ~/src/...
	cfgDir, root := filepath.Join(dir, "config"), filepath.Join(dir, "src")
	check(os.MkdirAll(cfgDir, 0o700))

	cfg := config.Default()
	cfg.SetDir(cfgDir)
	cfg.RootDir = "~/src"
	cfg.FavouriteEditor, cfg.Editor = "custom", "true"
	cfg.NerdFont = config.NerdFontOff
	on := true
	cfg.Integrations.Unibox = &on
	gl := cfg.AddInstance(config.Instance{Name: "Northwind", URL: "http://127.0.0.1:" + port,
		Groups: []config.Group{{ID: 1, FullPath: "platform", Name: "platform", Scope: config.ScopeSubgroups},
			{ID: 2, FullPath: "payments", Name: "payments", Scope: config.ScopeSubgroups}}})
	glID := gl.ID
	gh := cfg.AddInstance(config.Instance{Name: "GitLab.com", URL: "http://localhost:" + port,
		Groups: []config.Group{{ID: 3, FullPath: "northwind-oss", Name: "northwind-oss", Scope: config.ScopeGroup}}})
	ghID := gh.ID
	check(cfg.Save())

	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	type repo struct {
		inst, path, desc string
		at               time.Duration
		cloned           bool
	}
	repos := []repo{
		{glID, "platform/api-gateway", "The edge: routing, auth and rate limits", 20 * time.Minute, true},
		{glID, "platform/identity", "Sign-in, sessions and tokens", 2 * time.Hour, true},
		{glID, "platform/web", "The customer-facing web app", 3 * time.Hour, true},
		{glID, "platform/infra/terraform", "Clusters, queues and DNS", 26 * time.Hour, false},
		{glID, "platform/infra/helm-charts", "Charts every service deploys with", 50 * time.Hour, false},
		{glID, "payments/billing", "Invoices, credit notes and dunning", 40 * time.Minute, true},
		{glID, "payments/ledger", "Double-entry bookkeeping service", 5 * time.Hour, false},
		{glID, "payments/checkout", "Card and wallet checkout flows", 9 * time.Hour, true},
		{glID, "payments/fraud-rules", "Scoring rules and their tests", 6 * 24 * time.Hour, false},
		{ghID, "northwind-oss/tracing-kit", "OpenTelemetry helpers for Go services", 30 * time.Hour, true},
		{ghID, "northwind-oss/design-tokens", "Colours, type and spacing as code", 4 * 24 * time.Hour, false},
	}
	var projects []forge.Project
	for i, r := range repos {
		name := filepath.Base(r.path)
		host := "gitlab.northwind.dev"
		if r.inst == ghID {
			host = "gitlab.com"
		}
		projects = append(projects, forge.Project{ID: i + 1, Name: name, PathWithNamespace: r.path, Description: r.desc,
			DefaultBranch: "main", LastActivityAt: ago(r.at), Instance: r.inst,
			WebURL: "https://" + host + "/" + r.path, HTTPURLToRepo: "https://" + host + "/" + r.path + ".git"})
	}
	user := func(u, n string) forge.User { return forge.User{Username: u, Name: n} }
	jana, tomas, eva, lukas, me := user("jana", "Jana Svobodová"), user("tomas", "Tomáš Král"), user("eva", "Eva Dvořák"), user("lukas", "Lukáš Beneš"), user("demo", "Demo User")
	label := func(n, c string) forge.Label { return forge.Label{Name: n, Color: c} }
	mr := func(id, iid, pid int, title, src string, at time.Duration, author forge.User) forge.MergeRequest {
		return forge.MergeRequest{ID: id, IID: iid, ProjectID: pid, ProjectPath: repos[pid-1].path, Instance: repos[pid-1].inst,
			Title: title, State: "opened", SourceBranch: src, TargetBranch: "main", UpdatedAt: ago(at), Author: author,
			WebURL: projects[pid-1].WebURL + "/-/merge_requests/" + fmt.Sprint(iid), SHA: fmt.Sprintf("%040x", id*7919)}
	}
	mrs := []forge.MergeRequest{
		with(mr(101, 42, 1, "Rate-limit sign-in attempts per client", "feat/login-rate-limit", 12*time.Minute, jana),
			func(m *forge.MergeRequest) {
				m.Reviewers, m.Pipeline, m.ApprovedBy, m.ApprovalsRequired, m.Comments = []forge.User{me, tomas}, "running", []string{"tomas"}, 2, 7
				m.Unresolved, m.Resolved, m.UnresolvedKnown = 2, 3, true
				m.Labels = []forge.Label{label("security", "#d9534f"), label("backend", "#428bca")}
			}),
		with(mr(102, 318, 6, "Round invoice totals half to even", "fix/invoice-rounding", 45*time.Minute, eva),
			func(m *forge.MergeRequest) {
				m.Reviewers, m.Pipeline, m.ApprovedBy, m.ApprovalsRequired, m.Comments = []forge.User{me}, "success", nil, 1, 3
				m.Labels = []forge.Label{label("bug", "#cc0033")}
			}),
		with(mr(103, 57, 2, "Rotate refresh tokens on every use", "feat/token-rotation", 3*time.Hour, me),
			func(m *forge.MergeRequest) {
				m.Reviewers, m.Pipeline, m.ApprovedBy, m.ApprovalsRequired, m.Comments = []forge.User{jana}, "failed", nil, 1, 1
			}),
		with(mr(104, 1204, 3, "Draw the new order history page", "feat/order-history", 5*time.Hour, lukas),
			func(m *forge.MergeRequest) {
				m.Draft, m.Pipeline, m.Comments = true, "success", 0
				m.Labels = []forge.Label{label("frontend", "#5cb85c")}
			}),
		with(mr(105, 77, 8, "Support Apple Pay in checkout", "feat/apple-pay", 8*time.Hour, tomas),
			func(m *forge.MergeRequest) {
				m.Reviewers, m.Pipeline, m.ApprovedBy, m.ApprovalsRequired, m.Comments = []forge.User{me, eva}, "success", []string{"eva", "demo"}, 2, 12
			}),
		with(mr(106, 319, 6, "Send dunning reminders in the customer's locale", "feat/dunning-i18n", 26*time.Hour, jana),
			func(m *forge.MergeRequest) { m.Pipeline, m.Comments = "success", 2 }),
		with(mr(107, 23, 10, "Propagate baggage across queue consumers", "feat/baggage", 2*24*time.Hour, eva),
			func(m *forge.MergeRequest) { m.Pipeline, m.Comments, m.Reviewers = "success", 4, []forge.User{me} }),
		with(mr(108, 58, 2, "Expire sessions idle for 30 days", "feat/idle-sessions", 30*time.Hour, tomas),
			func(m *forge.MergeRequest) {
				m.Pipeline, m.ApprovedBy, m.ApprovalsRequired = "success", []string{"jana"}, 1
			}),
		with(mr(109, 1207, 3, "Lazy-load the product images", "perf/lazy-images", 31*time.Hour, eva),
			func(m *forge.MergeRequest) {
				m.Pipeline, m.Comments = "running", 1
				m.Labels = []forge.Label{label("frontend", "#5cb85c"), label("performance", "#f0ad4e")}
			}),
		with(mr(110, 412, 4, "Move the staging cluster to three zones", "infra/staging-zones", 3*24*time.Hour, lukas),
			func(m *forge.MergeRequest) { m.Pipeline, m.Reviewers, m.Comments = "manual", []forge.User{me}, 6 }),
		with(mr(111, 88, 7, "Book refunds against the original entry", "fix/refund-entries", 4*24*time.Hour, jana),
			func(m *forge.MergeRequest) {
				m.Pipeline, m.ApprovedBy, m.ApprovalsRequired, m.Comments = "success", []string{"tomas", "eva"}, 2, 9
				m.Labels = []forge.Label{label("bug", "#cc0033")}
			}),
		with(mr(112, 79, 8, "Retry declined cards once with 3-D Secure", "feat/3ds-retry", 5*24*time.Hour, me),
			func(m *forge.MergeRequest) { m.Pipeline, m.Reviewers = "success", []forge.User{tomas} }),
		with(mr(113, 31, 9, "Score new devices higher for an hour", "rules/new-device", 6*24*time.Hour, eva),
			func(m *forge.MergeRequest) { m.Draft, m.Pipeline = true, "failed" }),
	}
	check(index.Save(cfg.IndexPath("projects"), index.Projects{Version: index.Version, UpdatedAt: now.Add(-4 * time.Minute), Items: projects}))
	check(index.Save(cfg.IndexPath("mrs"), index.MergeRequests{Version: index.Version, UpdatedAt: now.Add(-4 * time.Minute), Items: mrs}))
	check(index.Save(cfg.IndexPath("groups"), index.Groups{Version: index.Version, UpdatedAt: now, Items: []forge.Group{
		{ID: 1, FullPath: "platform", Name: "platform", Instance: glID},
		{ID: 2, FullPath: "payments", Name: "payments", Instance: glID},
		{ID: 3, FullPath: "northwind-oss", Name: "northwind-oss", Instance: ghID}}}))

	v, err := secret.NewVault([]byte("demo"))
	check(err)
	v.Set(glID, "glpat-not-a-real-token")
	v.Set(ghID, "glpat-not-a-real-token-either")
	check(v.Save(cfg.VaultPath()))

	// Real clones, origin a bare repository beside them.
	for _, r := range repos {
		if !r.cloned {
			continue
		}
		origin := filepath.Join(dir, "origins", r.path+".git")
		clone := filepath.Join(root, r.path)
		seed(origin, clone, filepath.Base(r.path))
	}
	// The clone the screenshots open alone: a branch ahead of origin, a
	// change not committed and a file not versioned yet.
	gw := filepath.Join(root, "platform/api-gateway")
	git(gw, "checkout", "-q", "-b", "feat/login-rate-limit")
	write(gw, "limiter/limiter.go", limiterGo)
	git(gw, "add", "-A")
	commit(gw, "Jana Svobodová", "Add a token bucket per client")
	write(gw, "limiter/limiter_test.go", limiterTest)
	git(gw, "add", "-A")
	commit(gw, "Jana Svobodová", "Test the bucket's refill")
	git(gw, "push", "-q", "-u", "origin", "feat/login-rate-limit")
	write(gw, "limiter/limiter.go", strings.Replace(limiterGo, "burst: 10", "burst: 20", 1))
	write(gw, "router/routes.go", routesGo+"\n\tr.Use(limiter.PerClient(limiter.Default))\n}\n")
	write(gw, "docs/rate-limits.md", "# Rate limits\n\nSign-in allows 20 attempts per client, then 1 a second.\n")
	billing := filepath.Join(root, "payments/billing")
	git(billing, "checkout", "-q", "-b", "fix/invoice-rounding")
	write(billing, "invoice/round.go", "package invoice\n\n// Round rounds half to even, as the auditors expect.\nfunc Round(cents float64) int64 { return int64(cents + 0.5) }\n")
	git(billing, "add", "-A")
	commit(billing, "Eva Dvořák", "Round half to even")

	writeUnibox(filepath.Join(dir, "bin", "unibox"), root)
	fmt.Println(dir)
}

func with(m forge.MergeRequest, f func(*forge.MergeRequest)) forge.MergeRequest {
	f(&m)
	return m
}

func seed(origin, clone, name string) {
	check(os.MkdirAll(filepath.Dir(origin), 0o755))
	git(filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	check(os.MkdirAll(filepath.Dir(clone), 0o755))
	git(filepath.Dir(clone), "clone", "-q", origin, clone)
	git(clone, "checkout", "-q", "-B", "main")
	write(clone, "README.md", "# "+name+"\n\nPart of Northwind's platform.\n")
	write(clone, "go.mod", "module northwind.dev/"+name+"\n\ngo 1.26\n")
	git(clone, "add", "-A")
	commit(clone, "Tomáš Král", "Start "+name)
	if name == "api-gateway" {
		write(clone, "router/routes.go", routesGo+"}\n")
		git(clone, "add", "-A")
		commit(clone, "Lukáš Beneš", "Route the public API through one router")
	}
	git(clone, "push", "-q", "-u", "origin", "main")
}

func commit(dir, author, message string) {
	email := strings.ToLower(strings.Fields(author)[0]) + "@northwind.dev"
	cmd := exec.Command("git", "-c", "user.name="+author, "-c", "user.email="+email, "commit", "-q", "-m", message)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		check(fmt.Errorf("git commit in %s: %v: %s", dir, err, out))
	}
}

func git(dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		check(fmt.Errorf("git %v in %s: %v: %s", args, dir, err, out))
	}
}

func write(dir, name, content string) {
	path := filepath.Join(dir, name)
	check(os.MkdirAll(filepath.Dir(path), 0o755))
	check(os.WriteFile(path, []byte(content), 0o644))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mock:", err)
		os.Exit(1)
	}
}

const routesGo = `package router

import "northwind.dev/api-gateway/limiter"

// Routes wires the public API.
func Routes(r *Router) {
	r.Post("/v1/sessions", signIn)
	r.Delete("/v1/sessions/{id}", signOut)
	r.Get("/v1/me", me)
`

const limiterGo = `package limiter

import (
	"sync"
	"time"
)

// Default allows a burst of sign-ins, then one a second.
var Default = Config{burst: 10, every: time.Second}

type Config struct {
	burst int
	every time.Duration
}

// Bucket is one client's allowance.
type Bucket struct {
	mu     sync.Mutex
	tokens int
	last   time.Time
	cfg    Config
}

// Allow takes a token, refilling first for the time gone by.
func (b *Bucket) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.cfg.burst, b.tokens+int(now.Sub(b.last)/b.cfg.every))
	b.last = now
	if b.tokens == 0 {
		return false
	}
	b.tokens--
	return true
}
`

const limiterTest = `package limiter

import (
	"testing"
	"time"
)

func TestTheBucketRefills(t *testing.T) {
	b := &Bucket{cfg: Default}
	now := time.Now()
	for range Default.burst {
		b.Allow(now)
	}
	if b.Allow(now) || !b.Allow(now.Add(time.Second)) {
		t.Fatal("the bucket does not refill once a second")
	}
}
`

// writeUnibox writes a unibox stand-in: the JSON of unibox 0.5 for two
// envs, the sandbox of one running with three tasks.
func writeUnibox(path, root string) {
	answers := map[string]string{
		"version --json":                    `{"api_version":2,"version":"0.5"}`,
		"env ls --json":                     `{"api_version":2,"default":"platform","envs":{"platform":{"files":["~/.local/share/unibox/repos/gitlab.northwind.dev/platform/agent-sandboxes/sandboxes/platform.sbxenv.yaml"],"repo":"~/.local/share/unibox/repos/gitlab.northwind.dev/platform/agent-sandboxes"},"payments":{"files":["~/.local/share/unibox/repos/gitlab.northwind.dev/platform/agent-sandboxes/sandboxes/payments.sbxenv.yaml"],"repo":"~/.local/share/unibox/repos/gitlab.northwind.dev/platform/agent-sandboxes"},"data":{"files":["~/.local/share/unibox/repos/gitlab.northwind.dev/platform/agent-sandboxes/sandboxes/data.sbxenv.yaml"],"repo":"~/.local/share/unibox/repos/gitlab.northwind.dev/platform/agent-sandboxes"}}}`,
		"env show platform --json":          `{"api_version":2,"env":"platform","sandbox":"platform","sbxenv":{"agent":"claude","kits":[{"source":"docker/claude"},{"source":"./kits/go"}]},"missing":[]}`,
		"env show payments --json":          `{"api_version":2,"env":"payments","sandbox":"payments","sbxenv":{"agent":"codex","kits":[{"source":"docker/codex"},{"source":"./kits/java"}]},"missing":[]}`,
		"env show data --json":              `{"api_version":2,"env":"data","sandbox":"data","sbxenv":{"agent":"claude","args":{"warehouseUser":{"required":true}}},"missing":["warehouseUser"]}`,
		"context --json":                    `{"api_version":2,"context":{"sandbox":"platform"},"sandbox":"platform","env":"platform"}`,
		"config get --json":                 `{"api_version":2,"value":{"default_env":"platform","stop_idle":false,"ssh_agent":false,"editor_ssh":true}}`,
		"secret ls --all --json":            `{"api_version":2,"secrets":[{"service":"anthropic","scope":"global","kept":"stored","used_by":["platform"]},{"service":"openai","scope":"global","kept":"stored","used_by":["payments"]},{"service":"gitlab","scope":"global","kept":"stored","used_by":["platform","payments"]}]}`,
		"secret ls -e platform --json":      `{"api_version":2,"secrets":[{"service":"anthropic","stored":true,"kits":["docker/claude"]},{"service":"gitlab","stored":true,"description":"clones and pushes on gitlab.northwind.dev"}]}`,
		"var ls -e platform --json":         `{"api_version":2,"args":[{"name":"registryUser","value":"demo","from":"yours","description":"your login to the package registry"}]}`,
		"repo ls -e platform --json":        `{"api_version":2,"repos":{"adrs":{"url":"https://gitlab.northwind.dev/platform/adrs.git","default":"always","from":"repo","description":"architecture decisions"}}}`,
		"port ls -s platform --json":        `{"api_version":2,"ports":[{"name":"gateway","port":"8080:8080","team":true,"default":true,"named":true,"on":true,"published":[{"host_ip":"127.0.0.1","host_port":8080,"sandbox_port":8080,"protocol":"tcp"}]}]}`,
		"network ls -s platform --json":     `{"api_version":2,"rules":[{"decision":"allow","host":"proxy.golang.org","from":"env","applied":true}],"blocked":[]}`,
		"sandbox resources platform --json": `{"api_version":2,"own":{},"resources":{"cpus":"6","memory":"12g"}}`,
		"sandbox ls --json": `{"api_version":2,"sandboxes":[
{"name":"platform","env":"platform","agent":"claude","agents":{"claude":["claude"],"codex":["codex"]},"state":"running",
 "tasks":[
  {"id":"api-gateway-3f9a1c2e","alias":"rate-limit","sandbox":"platform","directory":"/work/api-gateway-3f9a1c2e","agent":"claude","state":"ready",
   "repositories":[{"name":"api-gateway","url":"https://gitlab.northwind.dev/platform/api-gateway.git","branch":"feat/login-rate-limit","host_path":"` + root + `/platform/api-gateway","mounted":true,"author":"Demo User <demo@northwind.dev>"},
                   {"name":"adrs","url":"https://gitlab.northwind.dev/platform/adrs.git","branch":"main","context":true,"author":"Demo User <demo@northwind.dev>"}]},
  {"id":"identity-8b21d0aa","alias":"token-rotation","sandbox":"platform","directory":"/work/identity-8b21d0aa","agent":"codex","state":"ready",
   "repositories":[{"name":"identity","url":"https://gitlab.northwind.dev/platform/identity.git","branch":"feat/token-rotation","author":"Demo User <demo@northwind.dev>"}]},
  {"id":"web-51c7e004","sandbox":"platform","directory":"/work/web-51c7e004","agent":"claude","state":"ready",
   "repositories":[{"name":"web","url":"https://gitlab.northwind.dev/platform/web.git","branch":"feat/order-history","author":"Demo User <demo@northwind.dev>"},
                   {"name":"api-gateway","url":"https://gitlab.northwind.dev/platform/api-gateway.git","branch":"feat/order-history","author":"Demo User <demo@northwind.dev>"}]}],
 "runs":[{"id":"r1","sandbox":"platform","task":"api-gateway-3f9a1c2e","agent":"claude","pid":1,"since":"` + time.Now().Add(-18*time.Minute).UTC().Format(time.RFC3339) + `"},
         {"id":"r2","sandbox":"platform","task":"identity-8b21d0aa","agent":"codex","pid":2,"since":"` + time.Now().Add(-6*time.Minute).UTC().Format(time.RFC3339) + `"}]},
{"name":"payments","env":"payments","agent":"codex","agents":{"codex":["codex"]},"state":"stopped",
 "tasks":[{"id":"billing-0c4e7d19","alias":"invoice-rounding","sandbox":"payments","directory":"/work/billing-0c4e7d19","agent":"codex","state":"ready",
   "repositories":[{"name":"billing","url":"https://gitlab.northwind.dev/payments/billing.git","branch":"fix/invoice-rounding","author":"Demo User <demo@northwind.dev>"}]}],
 "runs":[]}]}`,
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n# A stand-in for unibox 0.5, for the website's screenshots.\ncase \"$*\" in\n")
	for k, v := range answers {
		fmt.Fprintf(&b, "  '%s') cat <<'JSON'\n%s\nJSON\n;;\n", k, strings.ReplaceAll(v, "\n", " "))
	}
	b.WriteString("  *) echo '{\"api_version\":2,\"ok\":true,\"message\":\"done\"}';;\nesac\n")
	check(os.MkdirAll(filepath.Dir(path), 0o755))
	check(os.WriteFile(path, []byte(b.String()), 0o755))
}
