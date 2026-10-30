//go:build shots

// Command mock lays out a made-up world for the website's screenshots and
// walk, and for trying unagit out: two GitLab servers - their groups,
// fifteen repositories, pipelines and merge requests - real clones on disk
// with branches and changes, a vault sealed with the passphrase "demo",
// and a unibox stand-in answering for a few sandboxes. serve answers as
// both servers, so everything unagit asks of them reads and everything it
// does to them holds; nothing leaves the machine. make demo runs the real
// unagit over it; shoot.py photographs it, record.py walks through it.
//
//	go run -tags shots ./website/shots <dir>          # the world, unagit set up in it
//	go run -tags shots ./website/shots -fresh <dir>   # the world, unagit not set up yet
//	go run -tags shots ./website/shots serve <dir>    # answer as its servers
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
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

// port is where serve answers, as the proxy to the world's servers.
const port = "8929"

// The world's servers, by name.
const (
	northwind = "gitlab.northwind.dev"
	gitlabCom = "gitlab.com"
)

func main() {
	args := os.Args[1:]
	fresh := len(args) > 0 && args[0] == "-fresh"
	if fresh {
		args = args[1:]
	}
	switch {
	case len(args) == 2 && args[0] == "serve":
		serve(args[1])
		return
	case len(args) != 1:
		fmt.Fprintln(os.Stderr, "usage: mock [-fresh] <dir> | mock serve <dir>")
		os.Exit(2)
	}
	dir, err := filepath.Abs(args[0])
	check(err)
	check(os.RemoveAll(dir))
	lay(dir, fresh)
	fmt.Println(dir)
}

// The people of the world; me is whose the token is.
var (
	sarah  = forge.User{Username: "sarah", Name: "Sarah Mitchell"}
	james  = forge.User{Username: "james", Name: "James Carter"}
	emily  = forge.User{Username: "emily", Name: "Emily Clarke"}
	daniel = forge.User{Username: "daniel", Name: "Daniel Brooks"}
	olivia = forge.User{Username: "olivia", Name: "Olivia Bennett"}
	me     = forge.User{Username: "alex", Name: "Alex Morgan"}
)

func lay(dir string, fresh bool) {
	// unagit runs with HOME at dir, so paths read as ~/src/...
	cfgDir, root := filepath.Join(dir, "config"), filepath.Join(dir, "src")
	check(os.MkdirAll(cfgDir, 0o700))
	authority(filepath.Join(dir, "tls"))
	// Commits made in the world are the world's user's, whoever runs it.
	write(dir, ".gitconfig", "[user]\n\tname = "+me.Name+"\n\temail = alex@northwind.dev\n[init]\n\tdefaultBranch = main\n")

	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	type repo struct {
		host, path, desc string
		at               time.Duration
		cloned           bool
	}
	repos := []repo{
		{northwind, "platform/api-gateway", "The edge: routing, auth and rate limits", 20 * time.Minute, true},
		{northwind, "platform/identity", "Sign-in, sessions and tokens", 2 * time.Hour, true},
		{northwind, "platform/web", "The customer-facing web app", 3 * time.Hour, true},
		{northwind, "platform/notifications", "Email, SMS and push delivery", 7 * time.Hour, true},
		{northwind, "platform/search", "Product search and autocomplete", 11 * time.Hour, false},
		{northwind, "platform/infra/terraform", "Clusters, queues and DNS", 26 * time.Hour, false},
		{northwind, "platform/infra/helm-charts", "Charts every service deploys with", 50 * time.Hour, false},
		{northwind, "payments/billing", "Invoices, credit notes and dunning", 40 * time.Minute, true},
		{northwind, "payments/ledger", "Double-entry bookkeeping service", 5 * time.Hour, false},
		{northwind, "payments/checkout", "Card and wallet checkout flows", 9 * time.Hour, true},
		{northwind, "payments/payouts", "Merchant payouts and settlement", 14 * time.Hour, true},
		{northwind, "payments/fraud-rules", "Scoring rules and their tests", 6 * 24 * time.Hour, false},
		{gitlabCom, "northwind-oss/tracing-kit", "OpenTelemetry helpers for Go services", 30 * time.Hour, true},
		{gitlabCom, "northwind-oss/feature-flags", "A tiny feature-flag client for Go", 3 * 24 * time.Hour, false},
		{gitlabCom, "northwind-oss/design-tokens", "Colours, type and spacing as code", 4 * 24 * time.Hour, false},
	}
	world := &serverWorld{Me: me, Users: []forge.User{sarah, james, emily, daniel, olivia}, Hosts: map[string]*host{
		northwind: {Groups: []forge.Group{
			{ID: 1, FullPath: "payments", Path: "payments", Name: "payments", FullName: "payments", WebURL: "https://" + northwind + "/payments"},
			{ID: 2, FullPath: "platform", Path: "platform", Name: "platform", FullName: "platform", WebURL: "https://" + northwind + "/platform"},
			{ID: 4, ParentID: 2, FullPath: "platform/infra", Path: "infra", Name: "infra", FullName: "platform / infra", WebURL: "https://" + northwind + "/platform/infra"}}},
		gitlabCom: {Groups: []forge.Group{
			{ID: 3, FullPath: "northwind-oss", Path: "northwind-oss", Name: "northwind-oss", FullName: "northwind-oss", WebURL: "https://" + gitlabCom + "/northwind-oss"}}},
	}}
	byPath := map[string]*wproject{}
	for i, r := range repos {
		p := &wproject{Project: forge.Project{ID: i + 1, Name: filepath.Base(r.path), PathWithNamespace: r.path, Description: r.desc,
			DefaultBranch: "main", LastActivityAt: ago(r.at), Visibility: "internal",
			WebURL: "https://" + r.host + "/" + r.path, HTTPURLToRepo: "https://" + r.host + "/" + r.path + ".git",
			SSHURLToRepo: "git@" + r.host + ":" + r.path + ".git"},
			Languages: map[string]float64{"Go": 91.4, "Makefile": 5.1, "Dockerfile": 3.5}}
		if r.cloned {
			p.Origin = filepath.Join(dir, "origins", r.path+".git")
		}
		world.Hosts[r.host].Projects = append(world.Hosts[r.host].Projects, p)
		byPath[r.path] = p
	}

	// Real clones, each origin a bare repository beside them.
	for _, r := range repos {
		if r.cloned {
			seed(byPath[r.path].Origin, filepath.Join(root, r.path), filepath.Base(r.path), r.at+3*time.Minute)
		}
	}
	gw := filepath.Join(root, "platform/api-gateway")
	// The gateway's main last ran its integration tests into a Redis that
	// did not answer in time; run again, they pass.
	pipe := func(id int, ref string, at time.Duration, who forge.User, jobs ...*wjob) *wpipeline {
		return &wpipeline{ID: id, Ref: ref, Source: "push", Created: ago(at), User: who, Jobs: jobs}
	}
	job := func(id int64, name, stage, status string, at time.Duration, secs float64, log string, who forge.User) *wjob {
		return &wjob{ID: id, Name: name, Stage: stage, Status: status, Start: ago(at), Seconds: secs, Log: log, User: who}
	}
	byPath["platform/api-gateway"].Pipelines = append(byPath["platform/api-gateway"].Pipelines,
		pipe(4711, "main", 19*time.Minute, daniel,
			job(47111, "build", "build", "success", 19*time.Minute, 52, okLog("build"), daniel),
			job(47112, "lint", "test", "success", 18*time.Minute, 31, okLog("lint"), daniel),
			job(47113, "test:unit", "test", "success", 18*time.Minute, 84, okLog("test:unit"), daniel),
			job(47114, "test:integration", "test", "failed", 18*time.Minute, 212, failedLog, daniel),
			job(47115, "deploy:staging", "deploy", "skipped", 0, 0, "", daniel)))

	// The gateway's sign-in limiter, open for review on its own branch.
	git(gw, "checkout", "-q", "-b", "feat/login-rate-limit")
	write(gw, "limiter/limiter.go", limiterGo)
	git(gw, "add", "-A")
	commit(gw, sarah, "Add a token bucket per client", 3*time.Hour)
	write(gw, "limiter/limiter_test.go", limiterTest)
	git(gw, "add", "-A")
	commit(gw, sarah, "Test the bucket's refill", 2*time.Hour)
	write(gw, "router/routes.go", routesGo+"\n\tr.Use(limiter.PerClient(limiter.Default))\n}\n")
	git(gw, "add", "-A")
	commit(gw, sarah, "Wire the limiter into the router", 14*time.Minute)
	git(gw, "push", "-q", "-u", "origin", "feat/login-rate-limit")
	if fresh {
		// The walk watches the gateway's main and reads its log.
		git(gw, "checkout", "-q", "main")
	} else {
		// The clone the screenshots open alone: a change not committed and
		// a file not versioned yet.
		write(gw, "limiter/limiter.go", strings.Replace(limiterGo, "burst: 10", "burst: 20", 1))
		write(gw, "docs/rate-limits.md", "# Rate limits\n\nSign-in allows 20 attempts per client, then 1 a second.\n")
	}

	// Identity's OAuth sign-in, half done: a commit not pushed yet, and
	// more on the way.
	id := filepath.Join(root, "platform/identity")
	git(id, "checkout", "-q", "-b", "feat/oauth")
	write(id, "oauth/provider.go", providerGo)
	git(id, "add", "-A")
	commit(id, me, "Describe the OAuth providers we sign in with", 70*time.Minute)
	write(id, "oauth/provider.go", strings.Replace(providerGo, "\tScopes   []string\n", "\tScopes   []string\n\t// PKCE is required by every provider we add from now on.\n\tPKCE bool\n", 1))
	write(id, "oauth/callback.go", callbackGo)
	write(id, "README.md", "# identity\n\nSign-in, sessions and tokens for Northwind.\n\n## OAuth\n\nSign in with GitHub or Google: see `oauth/`.\n")

	billing := filepath.Join(root, "payments/billing")
	git(billing, "checkout", "-q", "-b", "fix/invoice-rounding")
	write(billing, "invoice/round.go", "package invoice\n\n// Round rounds half to even, as the auditors expect.\nfunc Round(cents float64) int64 { return int64(cents + 0.5) }\n")
	git(billing, "add", "-A")
	commit(billing, emily, "Round half to even", 46*time.Minute)
	git(billing, "push", "-q", "-u", "origin", "fix/invoice-rounding")

	label := func(n, c string) forge.Label { return forge.Label{Name: n, Color: c} }
	type mrSpec struct {
		id, iid      int
		path, title  string
		src          string
		at           time.Duration
		author       forge.User
		pipeline     string
		reviewers    []forge.User
		approved     []string
		required     int
		labels       []forge.Label
		draft        bool
		desc         string
		discussions  []*wdiscussion
		commits      []string
		extraComment int
	}
	n := func(id int, who forge.User, body string, at time.Duration, resolvable, resolved bool) *wnote {
		return &wnote{ID: id, Body: body, Created: ago(at), Author: who, Resolvable: resolvable, Resolved: resolved}
	}
	specs := []mrSpec{
		{id: 101, iid: 42, path: "platform/api-gateway", title: "Rate-limit sign-in attempts per client", src: "feat/login-rate-limit", at: 12 * time.Minute,
			author: sarah, pipeline: "success", reviewers: []forge.User{me, james}, approved: []string{"james"}, required: 2,
			labels: []forge.Label{label("security", "#d9534f"), label("backend", "#428bca")},
			desc: "Sign-in gets a token bucket per client: a burst of ten attempts, then one a second.\n\n" +
				"- [x] the limiter and its tests\n- [x] wired into the router\n- [ ] the numbers agreed with the security team",
			discussions: []*wdiscussion{
				{ID: "d1", Notes: []*wnote{
					n(1, james, "Should the burst be per client or per account? A shared office NAT puts everyone behind one client.", 5*time.Hour, true, false),
					n(2, sarah, "Per client for now - per account needs the session first. I'll write it up in the ADR.", 4*time.Hour, true, false)}},
				{ID: "d2", Notes: []*wnote{
					n(3, olivia, "Ten feels low for the mobile app, which retries on its own. Could we start at twenty?", 3*time.Hour, true, false)}},
				{ID: "d3", Notes: []*wnote{
					n(4, james, "The refill rounds down, so 999ms after a sign-in still counts as none. Intended?", 2*time.Hour, true, true),
					n(5, sarah, "Yes - it errs on the strict side. Added a test that says so.", 90*time.Minute, true, true)}}},
		},
		{id: 102, iid: 318, path: "payments/billing", title: "Round invoice totals half to even", src: "fix/invoice-rounding", at: 45 * time.Minute,
			author: emily, pipeline: "success", reviewers: []forge.User{me}, required: 1, labels: []forge.Label{label("bug", "#cc0033")},
			desc: "Totals ending in exactly half a cent were always rounded up; the auditors expect half to even.", commits: []string{"Round half to even"}, extraComment: 3},
		{id: 103, iid: 57, path: "platform/identity", title: "Rotate refresh tokens on every use", src: "feat/token-rotation", at: 3 * time.Hour,
			author: me, pipeline: "failed", reviewers: []forge.User{sarah}, required: 1,
			desc: "A refresh token is good for one refresh; using one twice signs the session out.", commits: []string{"Rotate on refresh", "Revoke the family on reuse"}, extraComment: 1},
		{id: 104, iid: 1204, path: "platform/web", title: "Draw the new order history page", src: "feat/order-history", at: 5 * time.Hour,
			author: daniel, pipeline: "success", draft: true, labels: []forge.Label{label("frontend", "#5cb85c")},
			desc: "The order history as designed: filters on top, a row per order.", commits: []string{"Lay out the order history"}},
		{id: 105, iid: 77, path: "payments/checkout", title: "Support Apple Pay in checkout", src: "feat/apple-pay", at: 8 * time.Hour,
			author: james, pipeline: "success", reviewers: []forge.User{me, emily}, approved: []string{"emily", "alex"}, required: 2,
			desc: "Apple Pay as a wallet beside Google Pay, behind the `apple_pay` flag.", commits: []string{"Add the Apple Pay session endpoint", "Show the button where supported"}, extraComment: 12},
		{id: 106, iid: 319, path: "payments/billing", title: "Send dunning reminders in the customer's locale", src: "feat/dunning-i18n", at: 26 * time.Hour,
			author: sarah, pipeline: "success", desc: "Reminders were always in English.", commits: []string{"Translate the dunning templates"}, extraComment: 2},
		{id: 107, iid: 23, path: "northwind-oss/tracing-kit", title: "Propagate baggage across queue consumers", src: "feat/baggage", at: 2 * 24 * time.Hour,
			author: emily, pipeline: "success", reviewers: []forge.User{me}, desc: "Baggage survives a hop through the queue.", commits: []string{"Carry baggage in message headers"}, extraComment: 4},
		{id: 108, iid: 58, path: "platform/identity", title: "Expire sessions idle for 30 days", src: "feat/idle-sessions", at: 30 * time.Hour,
			author: james, pipeline: "success", approved: []string{"sarah"}, required: 1, desc: "Sessions nobody used for 30 days end.", commits: []string{"Expire idle sessions"}},
		{id: 109, iid: 1207, path: "platform/web", title: "Lazy-load the product images", src: "perf/lazy-images", at: 31 * time.Hour,
			author: olivia, pipeline: "running", labels: []forge.Label{label("frontend", "#5cb85c"), label("performance", "#f0ad4e")},
			desc: "Images below the fold load as they scroll in.", commits: []string{"Load images lazily"}, extraComment: 1},
		{id: 110, iid: 412, path: "platform/infra/terraform", title: "Move the staging cluster to three zones", src: "infra/staging-zones", at: 3 * 24 * time.Hour,
			author: daniel, pipeline: "manual", reviewers: []forge.User{me}, desc: "Staging spreads over three zones, as production does.", commits: []string{"Spread staging over three zones"}, extraComment: 6},
		{id: 111, iid: 88, path: "payments/ledger", title: "Book refunds against the original entry", src: "fix/refund-entries", at: 4 * 24 * time.Hour,
			author: sarah, pipeline: "success", approved: []string{"james", "emily"}, required: 2, labels: []forge.Label{label("bug", "#cc0033")},
			desc: "A refund books against the entry it refunds, not a new one.", commits: []string{"Link refunds to their entry"}, extraComment: 9},
		{id: 112, iid: 79, path: "payments/checkout", title: "Retry declined cards once with 3-D Secure", src: "feat/3ds-retry", at: 5 * 24 * time.Hour,
			author: me, pipeline: "success", reviewers: []forge.User{james}, desc: "A soft decline is tried once more with 3-D Secure.", commits: []string{"Retry soft declines with 3DS"}},
		{id: 113, iid: 31, path: "payments/fraud-rules", title: "Score new devices higher for an hour", src: "rules/new-device", at: 6 * 24 * time.Hour,
			author: emily, pipeline: "failed", draft: true, desc: "A device seen for the first time scores higher for its first hour.", commits: []string{"Score new devices"}},
		{id: 114, iid: 15, path: "platform/notifications", title: "Batch push notifications per device", src: "feat/push-batching", at: 10 * time.Hour,
			author: olivia, pipeline: "success", reviewers: []forge.User{me}, required: 1, labels: []forge.Label{label("backend", "#428bca")},
			desc: "Pushes for one device within a second go out as one.", commits: []string{"Batch pushes per device"}, extraComment: 2},
		{id: 115, iid: 9, path: "payments/payouts", title: "Settle payouts in the merchant's currency", src: "feat/payout-currency", at: 20 * time.Hour,
			author: daniel, pipeline: "running", reviewers: []forge.User{emily}, desc: "Payouts settle in the currency the merchant chose.", commits: []string{"Settle in the merchant's currency"}},
	}
	status := map[int]string{}
	for _, s := range specs {
		status[s.id] = s.pipeline
		p := byPath[s.path]
		h := world.Hosts[hostOf(p)]
		mr := &wmr{MergeRequest: forge.MergeRequest{ID: s.id, IID: s.iid, ProjectID: p.ID, ProjectPath: p.PathWithNamespace,
			Title: s.title, State: "opened", Draft: s.draft, SourceBranch: s.src, TargetBranch: "main", UpdatedAt: ago(s.at),
			Author: s.author, WebURL: fmt.Sprintf("%s/-/merge_requests/%d", p.WebURL, s.iid), SHA: fakeSHA(s.id * 7919),
			Reviewers: s.reviewers, ApprovedBy: s.approved, ApprovalsRequired: s.required, Labels: s.labels, Comments: s.extraComment},
			Description: s.desc, Created: ago(s.at + 52*time.Hour), Discussions: s.discussions, Commits: s.commits, Changes: 4}
		h.MRs = append(h.MRs, mr)
		sha := ""
		if p.Origin == "" || !branchExists(p.Origin, s.src) {
			sha = mr.SHA
		}
		p.Pipelines = append(p.Pipelines, mrPipeline(s.id, s.src, sha, s.pipeline, ago(s.at), s.author))
	}
	world.save(filepath.Join(dir, "server.json"))

	cfg := config.Default()
	cfg.SetDir(cfgDir)
	cfg.FavouriteEditor, cfg.Editor = "custom", "true"
	// As unagit is meant to be seen: with Nerd Font's icons (fonts.py).
	cfg.NerdFont = config.NerdFontOn
	// The website's look: warm gold and teal on charcoal.
	cfg.Theme = "retro-block"
	v, err := secret.NewVault([]byte("demo"))
	check(err)
	if fresh {
		// Not set up yet: the walk adds the servers, their groups and
		// where the clones go.
		check(cfg.Save())
		check(v.Save(cfg.VaultPath()))
		return
	}
	cfg.RootDir = "~/src"
	on := true
	cfg.Integrations.Unibox = &on
	instances := map[string]string{}
	for _, s := range []struct{ host, name string }{{northwind, "Northwind"}, {gitlabCom, "GitLab.com"}} {
		var groups []config.Group
		for _, g := range world.Hosts[s.host].Groups {
			if g.ParentID == 0 {
				groups = append(groups, config.Group{ID: g.ID, FullPath: g.FullPath, Name: g.Name, Scope: config.ScopeSubgroups})
			}
		}
		inst := cfg.AddInstance(config.Instance{Name: s.name, URL: "https://" + s.host, Groups: groups})
		instances[s.host] = inst.ID
		v.Set(inst.ID, "glpat-not-a-real-token")
	}
	check(cfg.Save())
	check(v.Save(cfg.VaultPath()))
	var projects []forge.Project
	var mrs []forge.MergeRequest
	var groups []forge.Group
	for name, h := range world.Hosts {
		for _, p := range h.Projects {
			fp := p.Project
			fp.Instance = instances[name]
			projects = append(projects, fp)
		}
		for _, m := range h.MRs {
			fm := m.MergeRequest
			fm.Instance = instances[name]
			fm.Pipeline = status[m.ID]
			if fm.Comments == 0 {
				for _, d := range m.Discussions {
					fm.Comments += len(d.Notes)
				}
			}
			mrs = append(mrs, fm)
		}
		for _, g := range h.Groups {
			g.Instance = instances[name]
			groups = append(groups, g)
		}
	}
	check(index.Save(cfg.IndexPath("projects"), index.Projects{Version: index.Version, UpdatedAt: now.Add(-4 * time.Minute), Items: projects}))
	check(index.Save(cfg.IndexPath("mrs"), index.MergeRequests{Version: index.Version, UpdatedAt: now.Add(-4 * time.Minute), Items: mrs}))
	check(index.Save(cfg.IndexPath("groups"), index.Groups{Version: index.Version, UpdatedAt: now, Items: groups}))
	writeUnibox(filepath.Join(dir, "bin", "unibox"), root)
}

func hostOf(p *wproject) string {
	return strings.SplitN(strings.TrimPrefix(p.WebURL, "https://"), "/", 2)[0]
}

// mrPipeline is a merge request's newest pipeline, standing at status.
func mrPipeline(id int, ref, sha, status string, at time.Time, who forge.User) *wpipeline {
	p := &wpipeline{ID: 5000 + id, Ref: ref, SHA: sha, Source: "merge_request_event", Created: at, User: who}
	jobs := [][2]string{{"build", "build"}, {"lint", "test"}, {"test:unit", "test"}, {"deploy:review", "deploy"}}
	for i, j := range jobs {
		job := &wjob{ID: int64((5000+id)*10 + i), Name: j[0], Stage: j[1], Status: "success", Start: at.Add(time.Duration(i) * time.Minute), Seconds: 40 + float64(i*17), Log: okLog(j[0]), User: who}
		switch {
		case status == "failed" && j[0] == "test:unit":
			job.Status, job.Log = "failed", unitFailedLog
		case status == "failed" && j[1] == "deploy":
			job.Status = "skipped"
		case status == "running" && j[1] != "build":
			// Still at it, for as long as anyone watches.
			job.Status, job.Seconds = "", 6*3600
		case status == "manual" && j[1] == "deploy":
			job.Status = "manual"
			p.Status = "manual"
		}
		p.Jobs = append(p.Jobs, job)
	}
	return p
}

// authority makes the world's certificate authority and the servers'
// certificate it signs: unagit trusts the one (SSL_CERT_FILE), serve
// answers with the other.
func authority(dir string) {
	check(os.MkdirAll(dir, 0o755))
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	check(err)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "The demo world's authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(5 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	check(err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	check(err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: northwind},
		DNSNames: []string{northwind, gitlabCom}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(5 * 365 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	caCert, err := x509.ParseCertificate(caDER)
	check(err)
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &key.PublicKey, caKey)
	check(err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	check(err)
	pemFile := func(name, kind string, der []byte) {
		check(os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600))
	}
	pemFile("ca.pem", "CERTIFICATE", caDER)
	pemFile("server.pem", "CERTIFICATE", leafDER)
	pemFile("server-key.pem", "EC PRIVATE KEY", keyDER)
}

// history is each clone's main before the world begins: what was said, by
// whom.
var history = map[string][]struct {
	who     forge.User
	message string
}{
	"api-gateway": {
		{james, "Route the public API through one router"},
		{sarah, "Terminate TLS at the edge"},
		{daniel, "Proxy /v1/orders to the orders service"},
		{olivia, "Add request IDs to every log line"},
		{james, "Cache the JWKS for five minutes"},
		{sarah, "Drop the legacy /v0 routes"},
		{daniel, "Retry upstream calls on 503 with jitter"},
	},
	"identity": {
		{sarah, "Issue sessions as signed cookies"},
		{james, "Hash passwords with Argon2id"},
		{me, "Add TOTP as a second factor"},
		{sarah, "Lock an account after ten failed sign-ins"},
	},
}

// seed makes a clone and its origin, main's newest commit last ago.
func seed(origin, clone, name string, last time.Duration) {
	check(os.MkdirAll(filepath.Dir(origin), 0o755))
	git(filepath.Dir(origin), "init", "-q", "--bare", "-b", "main", origin)
	check(os.MkdirAll(filepath.Dir(clone), 0o755))
	git(filepath.Dir(clone), "clone", "-q", origin, clone)
	git(clone, "checkout", "-q", "-B", "main")
	write(clone, "README.md", "# "+name+"\n\nPart of Northwind's platform.\n")
	write(clone, "go.mod", "module northwind.dev/"+name+"\n\ngo 1.26\n")
	git(clone, "add", "-A")
	entries := history[name]
	commit(clone, james, "Start "+name, last+time.Duration(len(entries)+3)*31*time.Hour)
	for i, c := range entries {
		if name == "api-gateway" && i == 0 {
			write(clone, "router/routes.go", routesGo+"}\n")
		} else {
			write(clone, "CHANGELOG.md", changelog(history[name][:i+1]))
		}
		git(clone, "add", "-A")
		commit(clone, c.who, c.message, last+time.Duration(len(entries)-1-i)*17*time.Hour)
	}
	git(clone, "push", "-q", "-u", "origin", "main")
}

func changelog(entries []struct {
	who     forge.User
	message string
}) string {
	var b strings.Builder
	b.WriteString("# Changelog\n\n")
	for i := len(entries) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "- %s\n", entries[i].message)
	}
	return b.String()
}

// commit commits as who, ago before the world was laid out: its history
// spreads over the days before, not the second it took.
func commit(dir string, who forge.User, message string, ago time.Duration) {
	email := who.Username + "@northwind.dev"
	when := time.Now().Add(-ago).Format(time.RFC3339)
	cmd := exec.Command("git", "-c", "user.name="+who.Name, "-c", "user.email="+email, "commit", "-q", "-m", message)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
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

const failedLog = "\x1b[0KRunning with gitlab-runner 17.4.0 on northwind-runner-2\n" +
	"\x1b[0K\x1b[36;1mPreparing the \"docker\" executor\x1b[0;m\n" +
	"Using docker image golang:1.26 ...\n" +
	"Starting service redis:7.4 ...\n" +
	"\x1b[32;1mGetting source from Git repository\x1b[0;m\n" +
	"$ go test -tags integration ./...\n" +
	"ok  \tnorthwind.dev/api-gateway/router\t0.412s\n" +
	"ok  \tnorthwind.dev/api-gateway/auth\t1.087s\n" +
	"--- FAIL: TestUpstreamRetries (5.02s)\n" +
	"    retry_test.go:48: upstream did not answer within 5s:\n" +
	"        dial tcp 10.0.4.12:6379: i/o timeout\n" +
	"--- FAIL: TestUpstreamRetries/503_then_200 (5.01s)\n" +
	"FAIL\n" +
	"FAIL\tnorthwind.dev/api-gateway/proxy\t5.118s\n" +
	"\x1b[31;1mERROR: Job failed: exit code 1\x1b[0;m\n"

const unitFailedLog = "\x1b[0KRunning with gitlab-runner 17.4.0 on northwind-runner-1\n" +
	"$ go test ./...\n" +
	"--- FAIL: TestRotationRevokesTheFamily (0.03s)\n" +
	"    rotate_test.go:71: the old token still refreshes\n" +
	"FAIL\n" +
	"\x1b[31;1mERROR: Job failed: exit code 1\x1b[0;m\n"

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

const providerGo = `package oauth

// Provider is a service users sign in with.
type Provider struct {
	Name     string
	AuthURL  string
	TokenURL string
	Scopes   []string
}

// Providers are the ones offered on the sign-in page, in its order.
var Providers = []Provider{
	{Name: "GitHub", AuthURL: "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token", Scopes: []string{"read:user", "user:email"}},
	{Name: "Google", AuthURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token", Scopes: []string{"openid", "email"}},
}
`

const callbackGo = `package oauth

import (
	"errors"
	"net/http"
)

// ErrState is a callback whose state is not the one we sent.
var ErrState = errors.New("oauth: state does not match")

// Callback finishes a sign-in: it checks the state, trades the code for
// a token and starts the session.
func Callback(w http.ResponseWriter, r *http.Request) error {
	state, err := r.Cookie("oauth_state")
	if err != nil || state.Value != r.URL.Query().Get("state") {
		return ErrState
	}
	token, err := exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		return err
	}
	return startSession(w, token)
}
`

// writeUnibox writes a unibox stand-in: the JSON of unibox 0.5 for two
// envs, the sandbox of one running with three tasks.
func writeUnibox(path, root string) {
	user := me.Name + " <alex@northwind.dev>"
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
		"var ls -e platform --json":         `{"api_version":2,"args":[{"name":"registryUser","value":"alex","from":"yours","description":"your login to the package registry"}]}`,
		"repo ls -e platform --json":        `{"api_version":2,"repos":{"adrs":{"url":"https://gitlab.northwind.dev/platform/adrs.git","default":"always","from":"repo","description":"architecture decisions"}}}`,
		"port ls -s platform --json":        `{"api_version":2,"ports":[{"name":"gateway","port":"8080:8080","team":true,"default":true,"named":true,"on":true,"published":[{"host_ip":"127.0.0.1","host_port":8080,"sandbox_port":8080,"protocol":"tcp"}]}]}`,
		"network ls -s platform --json":     `{"api_version":2,"rules":[{"decision":"allow","host":"proxy.golang.org","from":"env","applied":true}],"blocked":[]}`,
		"sandbox resources platform --json": `{"api_version":2,"own":{},"resources":{"cpus":"6","memory":"12g"}}`,
		"sandbox ls --json": `{"api_version":2,"sandboxes":[
{"name":"platform","env":"platform","agent":"claude","agents":{"claude":["claude"],"codex":["codex"]},"state":"running",
 "tasks":[
  {"id":"api-gateway-3f9a1c2e","alias":"rate-limit","sandbox":"platform","directory":"/work/api-gateway-3f9a1c2e","agent":"claude","state":"ready",
   "repositories":[{"name":"api-gateway","url":"https://gitlab.northwind.dev/platform/api-gateway.git","branch":"feat/login-rate-limit","host_path":"` + root + `/platform/api-gateway","mounted":true,"author":"` + user + `"},
                   {"name":"adrs","url":"https://gitlab.northwind.dev/platform/adrs.git","branch":"main","context":true,"author":"` + user + `"}]},
  {"id":"identity-8b21d0aa","alias":"token-rotation","sandbox":"platform","directory":"/work/identity-8b21d0aa","agent":"codex","state":"ready",
   "repositories":[{"name":"identity","url":"https://gitlab.northwind.dev/platform/identity.git","branch":"feat/token-rotation","author":"` + user + `"}]},
  {"id":"web-51c7e004","sandbox":"platform","directory":"/work/web-51c7e004","agent":"claude","state":"ready",
   "repositories":[{"name":"web","url":"https://gitlab.northwind.dev/platform/web.git","branch":"feat/order-history","author":"` + user + `"},
                   {"name":"api-gateway","url":"https://gitlab.northwind.dev/platform/api-gateway.git","branch":"feat/order-history","author":"` + user + `"}]}],
 "runs":[{"id":"r1","sandbox":"platform","task":"api-gateway-3f9a1c2e","agent":"claude","pid":1,"since":"` + time.Now().Add(-18*time.Minute).UTC().Format(time.RFC3339) + `"},
         {"id":"r2","sandbox":"platform","task":"identity-8b21d0aa","agent":"codex","pid":2,"since":"` + time.Now().Add(-6*time.Minute).UTC().Format(time.RFC3339) + `"}]},
{"name":"payments","env":"payments","agent":"codex","agents":{"codex":["codex"]},"state":"stopped",
 "tasks":[{"id":"billing-0c4e7d19","alias":"invoice-rounding","sandbox":"payments","directory":"/work/billing-0c4e7d19","agent":"codex","state":"ready",
   "repositories":[{"name":"billing","url":"https://gitlab.northwind.dev/payments/billing.git","branch":"fix/invoice-rounding","author":"` + user + `"}]}],
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
