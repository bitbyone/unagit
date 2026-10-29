//go:build shots

package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// serve answers as the world's GitLab servers, gitlab.northwind.dev and
// gitlab.com, from server.json: it is the HTTPS proxy unagit is pointed at
// (HTTPS_PROXY), and behind the proxy each server by its name, over TLS
// with a certificate of the world's own authority (SSL_CERT_FILE). It
// keeps what is done to it - a job run again, a comment, an approval, a
// merge request opened - and follows each origin, so a push starts a
// pipeline that runs for a few seconds and passes. Nothing leaves the
// machine.
func serve(dir string) {
	s := &server{world: loadWorld(filepath.Join(dir, "server.json"))}
	s.adopt()
	go func() {
		for range time.Tick(time.Second) {
			s.follow()
		}
	}()

	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "tls", "server.pem"), filepath.Join(dir, "tls", "server-key.pem"))
	check(err)
	inner, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h2", "http/1.1"}})
	check(err)
	go func() { check(http.Serve(inner, s)) }()

	proxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			s.ServeHTTP(w, r)
			return
		}
		to, err := net.Dial("tcp", inner.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hj, _ := w.(http.Hijacker)
		from, _, err := hj.Hijack()
		if err != nil {
			to.Close()
			return
		}
		fmt.Fprint(from, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { io.Copy(to, from); to.Close() }()
		go func() { io.Copy(from, to); from.Close() }()
	})
	fmt.Fprintln(os.Stderr, "mock: the servers answer through 127.0.0.1:"+port)
	check(http.ListenAndServe("127.0.0.1:"+port, proxy))
}

// retrySeconds is how long a job run again runs: long enough that the
// walk is elsewhere when it passes, and hears about it.
const retrySeconds = 18

type server struct {
	mu    sync.Mutex
	world *serverWorld
	ids   int
}

func (s *server) id() int {
	s.ids++
	return 900000 + s.ids
}

// adopt gives each origin's branch the pipeline it was pushed with: the
// one server.json has for it, or one that passed when it was pushed.
func (s *server) adopt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.world.Hosts {
		for _, p := range h.Projects {
			if p.Origin == "" {
				for _, pipe := range p.Pipelines {
					if pipe.SHA == "" {
						pipe.SHA = fakeSHA(p.ID*1000 + pipe.ID)
					}
				}
				continue
			}
			for ref, sha := range heads(p.Origin) {
				found := false
				for _, pipe := range p.Pipelines {
					if pipe.Ref == ref && (pipe.SHA == "" || pipe.SHA == sha) {
						pipe.SHA, found = sha, true
					}
				}
				if !found {
					at := commitTime(p.Origin, sha)
					p.Pipelines = append(p.Pipelines, passed(s.id(), ref, sha, at, s.world.Me))
				}
			}
			for _, pipe := range p.Pipelines {
				if pipe.SHA == "" {
					pipe.SHA = fakeSHA(p.ID*1000 + pipe.ID)
				}
			}
		}
	}
}

// follow starts a pipeline for every head pushed since.
func (s *server) follow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.world.Hosts {
		for _, p := range h.Projects {
			if p.Origin == "" {
				continue
			}
			for ref, sha := range heads(p.Origin) {
				known := false
				for _, pipe := range p.Pipelines {
					known = known || pipe.Ref == ref && pipe.SHA == sha
				}
				if !known {
					p.Pipelines = append(p.Pipelines, s.pushed(ref, sha))
				}
			}
		}
	}
}

// pushed is the pipeline a push starts: a build, then the tests, a few
// seconds each.
func (s *server) pushed(ref, sha string) *wpipeline {
	now := time.Now()
	at := func(sec float64) time.Time { return now.Add(time.Duration(sec * float64(time.Second))) }
	me := s.world.Me
	job := func(name, stage string, start, secs float64) *wjob {
		return &wjob{ID: int64(s.id()), Name: name, Stage: stage, Start: at(start), Seconds: secs, User: me, Log: okLog(name)}
	}
	return &wpipeline{ID: s.id(), Ref: ref, SHA: sha, Source: "push", Created: now, User: me, Jobs: []*wjob{
		job("build", "build", 0.5, 4), job("lint", "test", 4.5, 3), job("test:unit", "test", 4.5, 7), job("test:integration", "test", 4.5, 9)}}
}

func passed(id int, ref, sha string, at time.Time, who forge.User) *wpipeline {
	p := &wpipeline{ID: id, Ref: ref, SHA: sha, Source: "push", Created: at, User: who}
	for i, n := range [][2]string{{"build", "build"}, {"lint", "test"}, {"test:unit", "test"}, {"test:integration", "test"}} {
		p.Jobs = append(p.Jobs, &wjob{ID: int64(id*10 + i), Name: n[0], Stage: n[1], Start: at.Add(time.Duration(i) * 40 * time.Second),
			Seconds: 38 + float64(i*21), User: who, Log: okLog(n[0])})
	}
	return p
}

func okLog(name string) string {
	return "\x1b[0KRunning with gitlab-runner 17.4.0 on northwind-runner-3\n" +
		"\x1b[0K\x1b[36;1mPreparing the \"docker\" executor\x1b[0;m\n" +
		"Using docker image golang:1.26 ...\n" +
		"\x1b[32;1mGetting source from Git repository\x1b[0;m\n" +
		"$ make " + strings.ReplaceAll(name, ":", "-") + "\n" +
		"ok  \tnorthwind.dev/...\t3.412s\n" +
		"\x1b[32;1mJob succeeded\x1b[0;m\n"
}

// heads is each branch of a bare repository and the commit it is at.
func heads(origin string) map[string]string {
	out, err := exec.Command("git", "-C", origin, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads").Output()
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if ref, sha, ok := strings.Cut(line, " "); ok {
			m[ref] = sha
		}
	}
	return m
}

func commitTime(origin, sha string) time.Time {
	out, err := exec.Command("git", "-C", origin, "show", "-s", "--format=%cI", sha).Output()
	if err != nil {
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(out)))
	if err != nil {
		return time.Now()
	}
	return t
}

func fakeSHA(n int) string { return fmt.Sprintf("%040x", uint64(n)*2654435761) }

var (
	projectRoute = regexp.MustCompile(`^/api/v4/projects/([^/]+)(/.*)?$`)
	groupRoute   = regexp.MustCompile(`^/api/v4/groups/(\d+)/(projects|merge_requests)$`)
	mrRoute      = regexp.MustCompile(`^/merge_requests/(\d+)(/.*)?$`)
)

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	name, _, _ := strings.Cut(r.Host, ":")
	h := s.world.Hosts[name]
	if h == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	path := r.URL.EscapedPath()
	switch {
	case path == "/api/graphql":
		// No GraphQL here: the watches are read over REST, as on a server
		// whose GraphQL fails.
		http.NotFound(w, r)
	case path == "/api/v4/user":
		reply(w, s.world.Me)
	case path == "/api/v4/users":
		var found []forge.User
		for _, u := range append(s.world.Users, s.world.Me) {
			if u.Username == q.Get("username") {
				found = append(found, u)
			}
		}
		reply(w, orEmpty(found))
	case path == "/api/v4/groups":
		reply(w, h.Groups)
	case groupRoute.MatchString(path):
		m := groupRoute.FindStringSubmatch(path)
		id, _ := strconv.Atoi(m[1])
		var g forge.Group
		for _, x := range h.Groups {
			if x.ID == id {
				g = x
			}
		}
		under := func(p string) bool {
			rest, ok := strings.CutPrefix(p, g.FullPath+"/")
			return ok && (q.Get("include_subgroups") == "true" || !strings.Contains(rest, "/"))
		}
		if m[2] == "projects" {
			var out []forge.Project
			for _, p := range h.Projects {
				if under(p.PathWithNamespace) {
					out = append(out, p.Project)
				}
			}
			reply(w, orEmpty(out))
			return
		}
		var out []map[string]any
		for _, mr := range h.MRs {
			if mr.State == "opened" && under(mr.ProjectPath) {
				out = append(out, s.listed(h, mr))
			}
		}
		reply(w, orEmpty(out))
	case path == "/api/v4/projects":
		if r.Method == http.MethodPost {
			http.Error(w, `{"message":"not in the demo"}`, http.StatusForbidden)
			return
		}
		var out []forge.Project
		if text := strings.ToLower(q.Get("search")); text != "" {
			for _, p := range h.Projects {
				if strings.Contains(strings.ToLower(p.PathWithNamespace), text) {
					out = append(out, p.Project)
				}
			}
		}
		reply(w, orEmpty(out))
	case projectRoute.MatchString(path):
		m := projectRoute.FindStringSubmatch(path)
		ref, _ := url.PathUnescape(m[1])
		p := h.project(ref)
		if p == nil {
			http.NotFound(w, r)
			return
		}
		s.project(w, r, h, p, m[2])
	default:
		fmt.Fprint(w, "[]")
	}
}

func (h *host) project(ref string) *wproject {
	for _, p := range h.Projects {
		if strconv.Itoa(p.ID) == ref || p.PathWithNamespace == ref {
			return p
		}
	}
	return nil
}

func (s *server) project(w http.ResponseWriter, r *http.Request, h *host, p *wproject, rest string) {
	q := r.URL.Query()
	now := time.Now()
	switch {
	case rest == "":
		reply(w, map[string]any{
			"id": p.ID, "name": p.Name, "path_with_namespace": p.PathWithNamespace, "description": p.Description,
			"default_branch": p.DefaultBranch, "web_url": p.WebURL, "http_url_to_repo": p.HTTPURLToRepo,
			"last_activity_at": p.LastActivityAt, "created_at": p.LastActivityAt.Add(-400 * 24 * time.Hour).Format(time.RFC3339),
			"star_count": 4, "forks_count": 1, "topics": []string{"go"}, "merge_method": "merge",
			"statistics": map[string]int64{"commit_count": int64(len(s.commits(p, p.DefaultBranch, 100))), "repository_size": 412000}})
	case rest == "/languages":
		reply(w, p.Languages)
	case strings.HasPrefix(rest, "/repository/files/README.md/raw"):
		w.Header().Set("Content-Type", "text/plain")
		if p.Origin != "" {
			if out, err := exec.Command("git", "-C", p.Origin, "show", p.DefaultBranch+":README.md").Output(); err == nil {
				w.Write(out)
				return
			}
		}
		fmt.Fprintf(w, "# %s\n\n%s.\n", p.Name, p.Description)
	case rest == "/repository/commits":
		n, _ := strconv.Atoi(q.Get("per_page"))
		ref := q.Get("ref_name")
		if ref == "" {
			ref = p.DefaultBranch
		}
		reply(w, orEmpty(s.commits(p, ref, max(n, 20))))
	case strings.HasPrefix(rest, "/repository/commits/") && strings.HasSuffix(rest, "/diff"):
		sha := strings.TrimSuffix(strings.TrimPrefix(rest, "/repository/commits/"), "/diff")
		reply(w, orEmpty(diffOf(p, sha)))
	case rest == "/repository/branches":
		reply(w, orEmpty(s.branches(p)))
	case strings.HasPrefix(rest, "/repository/branches/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(rest, "/repository/branches/"))
		for _, b := range s.branches(p) {
			if b["name"] == name {
				reply(w, b)
				return
			}
		}
		http.Error(w, `{"message":"404 Branch Not Found"}`, http.StatusNotFound)
	case rest == "/pipelines":
		var out []map[string]any
		for _, pipe := range newestFirst(p.Pipelines) {
			if (q.Get("ref") == "" || pipe.Ref == q.Get("ref")) && (q.Get("sha") == "" || pipe.SHA == q.Get("sha")) {
				out = append(out, pipelineJSON(p, pipe, now))
			}
		}
		if n, _ := strconv.Atoi(q.Get("per_page")); n > 0 && len(out) > n {
			out = out[:n]
		}
		reply(w, orEmpty(out))
	case strings.HasPrefix(rest, "/pipelines/"):
		parts := strings.Split(strings.TrimPrefix(rest, "/pipelines/"), "/")
		id, _ := strconv.Atoi(parts[0])
		pipe := p.pipeline(id)
		if pipe == nil {
			http.NotFound(w, r)
			return
		}
		switch {
		case len(parts) == 1:
			reply(w, pipelineJSON(p, pipe, now))
		case parts[1] == "jobs":
			var out []map[string]any
			for _, j := range pipe.Jobs {
				out = append(out, jobJSON(p, j, now))
			}
			reply(w, orEmpty(out))
		default:
			fmt.Fprint(w, "[]")
		}
	case strings.HasPrefix(rest, "/jobs/"):
		parts := strings.Split(strings.TrimPrefix(rest, "/jobs/"), "/")
		id, _ := strconv.ParseInt(parts[0], 10, 64)
		pipe, j := p.job(id)
		if j == nil || len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		switch parts[1] {
		case "trace":
			w.Header().Set("Content-Type", "text/plain")
			if j.status(now) == "running" {
				fmt.Fprint(w, strings.SplitAfterN(j.Log, "\n", 4)[0])
				return
			}
			fmt.Fprint(w, j.Log)
		case "retry", "play":
			again := &wjob{ID: int64(s.id()), Name: j.Name, Stage: j.Stage, Start: now.Add(time.Second), Seconds: retrySeconds, User: s.world.Me, Log: okLog(j.Name)}
			pipe.Jobs = append(pipe.Jobs, again)
			// What the failure skipped runs once it passes.
			for _, later := range pipe.Jobs {
				if later.Status == "skipped" {
					later.Status, later.Start, later.Seconds, later.Log, later.User = "", again.Start.Add(time.Duration(retrySeconds*float64(time.Second))), 4, okLog(later.Name), s.world.Me
				}
			}
			pipe.Status = ""
			w.WriteHeader(http.StatusCreated)
			reply(w, jobJSON(p, again, now))
		}
	case rest == "/merge_requests" && r.Method == http.MethodPost:
		var req struct {
			Source string `json:"source_branch"`
			Target string `json:"target_branch"`
			Title  string `json:"title"`
			Desc   string `json:"description"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		for _, mr := range h.MRs {
			if mr.ProjectID == p.ID && mr.SourceBranch == req.Source && mr.State == "opened" {
				http.Error(w, fmt.Sprintf(`{"message":["Another open merge request already exists for this source branch: !%d"]}`, mr.IID), http.StatusConflict)
				return
			}
		}
		iid := 1
		for _, mr := range h.MRs {
			if mr.ProjectID == p.ID {
				iid = max(iid, mr.IID+1)
			}
		}
		mr := &wmr{MergeRequest: forge.MergeRequest{ID: s.id(), IID: iid, ProjectID: p.ID, ProjectPath: p.PathWithNamespace,
			Title: req.Title, State: "opened", Draft: strings.HasPrefix(strings.ToLower(req.Title), "draft"), SourceBranch: req.Source,
			TargetBranch: req.Target, UpdatedAt: now, Author: s.world.Me, WebURL: fmt.Sprintf("%s/-/merge_requests/%d", p.WebURL, iid)},
			Description: req.Desc, Created: now, Changes: 3}
		h.MRs = append(h.MRs, mr)
		w.WriteHeader(http.StatusCreated)
		reply(w, s.listed(h, mr))
	case rest == "/merge_requests":
		var out []map[string]any
		for _, mr := range h.MRs {
			if mr.ProjectID == p.ID && mr.State == "opened" {
				out = append(out, s.listed(h, mr))
			}
		}
		reply(w, orEmpty(out))
	case mrRoute.MatchString(rest):
		m := mrRoute.FindStringSubmatch(rest)
		iid, _ := strconv.Atoi(m[1])
		for _, mr := range h.MRs {
			if mr.ProjectID == p.ID && mr.IID == iid {
				s.mergeRequest(w, r, h, p, mr, m[2])
				return
			}
		}
		http.NotFound(w, r)
	default:
		fmt.Fprint(w, "[]")
	}
}

func (s *server) mergeRequest(w http.ResponseWriter, r *http.Request, h *host, p *wproject, mr *wmr, rest string) {
	now := time.Now()
	body := map[string]any{}
	if r.Method != http.MethodGet {
		json.NewDecoder(r.Body).Decode(&body)
	}
	note := func(text string) *wnote {
		return &wnote{ID: s.id(), Body: text, Created: now, Author: s.world.Me}
	}
	switch {
	case rest == "" && r.Method == http.MethodPut:
		if t, ok := body["title"].(string); ok {
			mr.Title = t
		}
		if d, ok := body["description"].(string); ok {
			mr.Description = d
		}
		if e, _ := body["state_event"].(string); e == "close" {
			mr.State = "closed"
		}
		mr.UpdatedAt = now
		reply(w, s.detail(h, p, mr))
	case rest == "":
		reply(w, s.detail(h, p, mr))
	case rest == "/commits":
		var out []map[string]any
		if p.Origin != "" && branchExists(p.Origin, mr.SourceBranch) {
			out = s.commitsBetween(p, mr.TargetBranch, mr.SourceBranch)
		} else {
			for i, t := range mr.Commits {
				at := mr.UpdatedAt.Add(-time.Duration(i*3) * time.Hour)
				out = append(out, map[string]any{"id": fakeSHA(mr.ID*100 + i), "short_id": fakeSHA(mr.ID*100 + i)[:8],
					"title": t, "message": t, "author_name": mr.Author.Name, "authored_date": at, "committed_date": at, "created_at": at})
			}
		}
		reply(w, orEmpty(out))
	case rest == "/discussions" && r.Method == http.MethodPost:
		text, _ := body["body"].(string)
		d := &wdiscussion{ID: fmt.Sprintf("d%d", s.id()), Notes: []*wnote{note(text)}}
		mr.Discussions = append(mr.Discussions, d)
		mr.UpdatedAt = now
		w.WriteHeader(http.StatusCreated)
		reply(w, discussionJSON(d))
	case rest == "/notes" && r.Method == http.MethodPost:
		text, _ := body["body"].(string)
		n := note(text)
		mr.Discussions = append(mr.Discussions, &wdiscussion{ID: fmt.Sprintf("d%d", s.id()), Notes: []*wnote{n}})
		mr.UpdatedAt = now
		w.WriteHeader(http.StatusCreated)
		reply(w, noteJSON(n))
	case strings.HasPrefix(rest, "/discussions/"):
		parts := strings.Split(strings.TrimPrefix(rest, "/discussions/"), "/")
		for _, d := range mr.Discussions {
			if d.ID != parts[0] {
				continue
			}
			if len(parts) > 1 && parts[1] == "notes" {
				text, _ := body["body"].(string)
				n := note(text)
				d.Notes = append(d.Notes, n)
				mr.UpdatedAt = now
				w.WriteHeader(http.StatusCreated)
				reply(w, noteJSON(n))
				return
			}
			if resolved, ok := body["resolved"].(bool); ok {
				for _, n := range d.Notes {
					if n.Resolvable {
						n.Resolved = resolved
					}
				}
			}
			reply(w, discussionJSON(d))
			return
		}
		http.NotFound(w, r)
	case rest == "/discussions":
		var out []map[string]any
		for _, d := range mr.Discussions {
			out = append(out, discussionJSON(d))
		}
		reply(w, orEmpty(out))
	case rest == "/approvals":
		var by []map[string]any
		for _, u := range mr.ApprovedBy {
			by = append(by, map[string]any{"user": map[string]string{"username": u, "name": s.nameOf(u)}})
		}
		reply(w, map[string]any{"approved_by": orEmpty(by), "approvals_required": mr.ApprovalsRequired,
			"approvals_left": max(0, mr.ApprovalsRequired-len(mr.ApprovedBy))})
	case rest == "/approve":
		if !contains(mr.ApprovedBy, s.world.Me.Username) {
			mr.ApprovedBy = append(mr.ApprovedBy, s.world.Me.Username)
		}
		mr.UpdatedAt = now
		w.WriteHeader(http.StatusCreated)
		reply(w, map[string]any{"id": mr.ID})
	case rest == "/pipelines":
		var out []map[string]any
		for _, pipe := range newestFirst(p.Pipelines) {
			if pipe.Ref == mr.SourceBranch {
				out = append(out, pipelineJSON(p, pipe, now))
			}
		}
		if n, _ := strconv.Atoi(r.URL.Query().Get("per_page")); n > 0 && len(out) > n {
			out = out[:n]
		}
		reply(w, orEmpty(out))
	default:
		fmt.Fprint(w, "[]")
	}
}

// listed is a merge request as the listings give it.
func (s *server) listed(h *host, mr *wmr) map[string]any {
	p := h.project(strconv.Itoa(mr.ProjectID))
	sha := mr.SHA
	if p != nil && p.Origin != "" {
		if head := heads(p.Origin)[mr.SourceBranch]; head != "" {
			sha = head
		}
	}
	labels := []map[string]string{}
	for _, l := range mr.Labels {
		labels = append(labels, map[string]string{"name": l.Name, "color": l.Color})
	}
	return map[string]any{
		"id": mr.ID, "iid": mr.IID, "project_id": mr.ProjectID, "source_project_id": mr.ProjectID, "target_project_id": mr.ProjectID,
		"title": mr.Title, "state": mr.State, "draft": mr.Draft, "source_branch": mr.SourceBranch, "target_branch": mr.TargetBranch,
		"web_url": mr.WebURL, "updated_at": mr.UpdatedAt, "created_at": mr.Created, "author": mr.Author,
		"reviewers": orEmpty(mr.Reviewers), "assignees": []forge.User{mr.Author}, "labels": labels, "sha": sha,
		"user_notes_count": s.notes(mr), "references": map[string]string{"full": mr.ProjectPath + "!" + strconv.Itoa(mr.IID)},
	}
}

func (s *server) detail(h *host, p *wproject, mr *wmr) map[string]any {
	d := s.listed(h, mr)
	now := time.Now()
	var head any
	for _, pipe := range newestFirst(p.Pipelines) {
		if pipe.Ref == mr.SourceBranch {
			head = pipelineJSON(p, pipe, now)
			break
		}
	}
	done, count := strings.Count(mr.Description, "- [x]"), strings.Count(mr.Description, "- [")
	d["description"], d["detailed_merge_status"], d["changes_count"] = mr.Description, "mergeable", strconv.Itoa(mr.Changes)
	d["head_pipeline"], d["upvotes"], d["diverged_commits_count"] = head, len(mr.ApprovedBy), 0
	d["task_completion_status"] = map[string]int{"count": count, "completed_count": done}
	d["diff_refs"] = map[string]string{"base_sha": fakeSHA(mr.ID), "start_sha": fakeSHA(mr.ID), "head_sha": d["sha"].(string)}
	d["blocking_discussions_resolved"] = true
	return d
}

func (s *server) notes(mr *wmr) int {
	n := 0
	for _, d := range mr.Discussions {
		n += len(d.Notes)
	}
	return max(n, mr.Comments)
}

func (s *server) nameOf(username string) string {
	for _, u := range append(s.world.Users, s.world.Me) {
		if u.Username == username {
			return u.Name
		}
	}
	return username
}

func (p *wproject) pipeline(id int) *wpipeline {
	for _, pipe := range p.Pipelines {
		if pipe.ID == id {
			return pipe
		}
	}
	return nil
}

func (p *wproject) job(id int64) (*wpipeline, *wjob) {
	for _, pipe := range p.Pipelines {
		for _, j := range pipe.Jobs {
			if j.ID == id {
				return pipe, j
			}
		}
	}
	return nil, nil
}

func newestFirst(ps []*wpipeline) []*wpipeline {
	out := append([]*wpipeline(nil), ps...)
	sort.SliceStable(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out
}

func pipelineJSON(p *wproject, pipe *wpipeline, now time.Time) map[string]any {
	return map[string]any{"id": pipe.ID, "project_id": p.ID, "status": pipe.status(now), "ref": pipe.Ref, "sha": pipe.SHA,
		"web_url": fmt.Sprintf("%s/-/pipelines/%d", p.WebURL, pipe.ID), "source": pipe.Source,
		"created_at": pipe.Created, "updated_at": pipe.updated(now), "started_at": pipe.started(), "user": pipe.User}
}

func jobJSON(p *wproject, j *wjob, now time.Time) map[string]any {
	st := j.status(now)
	var dur float64
	switch st {
	case "running":
		dur = now.Sub(j.Start).Seconds()
	case "pending", "manual", "created", "skipped":
	default:
		dur = j.Seconds
	}
	out := map[string]any{"id": j.ID, "name": j.Name, "stage": j.Stage, "status": st, "duration": dur,
		"web_url": fmt.Sprintf("%s/-/jobs/%d", p.WebURL, j.ID), "user": j.User}
	if st != "pending" && st != "manual" && st != "created" && st != "skipped" {
		out["started_at"] = j.Start
	}
	return out
}

func discussionJSON(d *wdiscussion) map[string]any {
	var notes []map[string]any
	for _, n := range d.Notes {
		notes = append(notes, noteJSON(n))
	}
	return map[string]any{"id": d.ID, "notes": notes}
}

func noteJSON(n *wnote) map[string]any {
	return map[string]any{"id": n.ID, "body": n.Body, "created_at": n.Created, "author": n.Author,
		"resolvable": n.Resolvable, "resolved": n.Resolved, "system": false}
}

// commits is a ref's newest commits: the origin's, or made up for a
// project never cloned.
func (s *server) commits(p *wproject, ref string, n int) []map[string]any {
	if p.Origin == "" {
		var out []map[string]any
		for i, t := range []string{"Bump the Go toolchain to 1.26", "Document the release steps", "Start " + p.Name} {
			at := p.LastActivityAt.Add(-time.Duration(i*31) * time.Hour)
			out = append(out, map[string]any{"id": fakeSHA(p.ID*10 + i), "short_id": fakeSHA(p.ID*10 + i)[:8], "title": t, "message": t,
				"author_name": "Sarah Mitchell", "committed_date": at, "web_url": p.WebURL + "/-/commit/" + fakeSHA(p.ID*10+i), "parent_ids": []string{}})
		}
		return out
	}
	return gitCommits(p, "-n", strconv.Itoa(n), ref)
}

func (s *server) commitsBetween(p *wproject, base, head string) []map[string]any {
	return gitCommits(p, base+".."+head)
}

func gitCommits(p *wproject, args ...string) []map[string]any {
	cmd := append([]string{"-C", p.Origin, "log", "--format=%H%x1f%h%x1f%s%x1f%B%x1f%an%x1f%cI%x1f%P%x1e"}, args...)
	out, err := exec.Command("git", cmd...).Output()
	if err != nil {
		return nil
	}
	var list []map[string]any
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) < 7 {
			continue
		}
		at, _ := time.Parse(time.RFC3339, f[5])
		list = append(list, map[string]any{"id": f[0], "short_id": f[1], "title": f[2], "message": strings.TrimSpace(f[3]),
			"author_name": f[4], "committed_date": at, "authored_date": at, "created_at": at,
			"web_url": p.WebURL + "/-/commit/" + f[0], "parent_ids": orEmpty(strings.Fields(f[6]))})
	}
	return list
}

func (s *server) branches(p *wproject) []map[string]any {
	refs := map[string]string{}
	if p.Origin != "" {
		refs = heads(p.Origin)
	} else {
		refs[p.DefaultBranch] = fakeSHA(p.ID * 10)
	}
	var out []map[string]any
	for name, sha := range refs {
		at := p.LastActivityAt
		title := "Bump the Go toolchain to 1.26"
		if p.Origin != "" {
			at = commitTime(p.Origin, sha)
			if b, err := exec.Command("git", "-C", p.Origin, "show", "-s", "--format=%s", sha).Output(); err == nil {
				title = strings.TrimSpace(string(b))
			}
		}
		out = append(out, map[string]any{"name": name, "default": name == p.DefaultBranch, "protected": name == p.DefaultBranch,
			"commit": map[string]any{"id": sha, "short_id": sha[:8], "title": title, "committed_date": at}})
	}
	sort.Slice(out, func(i, k int) bool { return out[i]["name"].(string) < out[k]["name"].(string) })
	return out
}

func branchExists(origin, branch string) bool { _, ok := heads(origin)[branch]; return ok }

func diffOf(p *wproject, sha string) []map[string]string {
	if p.Origin == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", p.Origin, "diff-tree", "-p", "--no-commit-id", "-r", "--root", sha).Output()
	if err != nil {
		return nil
	}
	var list []map[string]string
	for _, part := range strings.Split(string(out), "diff --git ")[1:] {
		head, diff, _ := strings.Cut(part, "\n@@")
		fields := strings.Fields(strings.SplitN(head, "\n", 2)[0])
		name := strings.TrimPrefix(fields[len(fields)-1], "b/")
		list = append(list, map[string]string{"new_path": name, "old_path": name, "diff": "@@" + diff})
	}
	return list
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// orEmpty keeps an empty list a list: GitLab never answers null.
func orEmpty[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}

func reply(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
