package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

func TestPaginationAndAuthHeader(t *testing.T) {
	var seenToken, seenSubgroups string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenToken = r.Header.Get("PRIVATE-TOKEN")
		seenSubgroups = r.URL.Query().Get("include_subgroups")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("x-next-page", "2")
			fmt.Fprint(w, `[{"id":1,"path_with_namespace":"g/a"}]`)
		default:
			w.Header().Set("x-next-page", "")
			fmt.Fprint(w, `[{"id":2,"path_with_namespace":"g/b"}]`)
		}
	}))
	defer srv.Close()

	projects, err := New(srv.URL, "secret-token").GroupProjects(context.Background(), forge.Group{ID: 5}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || projects[1].PathWithNamespace != "g/b" {
		t.Fatalf("projects = %+v", projects)
	}
	if seenToken != "secret-token" {
		t.Errorf("token header = %q", seenToken)
	}
	if seenSubgroups != "true" {
		t.Errorf("include_subgroups = %q", seenSubgroups)
	}

	if _, err := New(srv.URL, "t").GroupProjects(context.Background(), forge.Group{ID: 5}, false); err != nil {
		t.Fatal(err)
	}
	if seenSubgroups != "false" {
		t.Errorf("include_subgroups = %q, want false", seenSubgroups)
	}
}

func TestUnauthorizedMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"401 Unauthorized"}`)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "bad").CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeRequestDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"iid":42,"title":"Fix login","source_branch":"feature/login",
			"source_project_id":7,"target_project_id":7,"draft":true,
			"author":{"username":"jane"},"references":{"full":"acme/app!42"},
			"updated_at":"2026-01-02T03:04:05Z"}]`)
	}))
	defer srv.Close()

	mrs, err := New(srv.URL, "t").GroupMergeRequests(context.Background(), forge.Group{ID: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	mr := mrs[0]
	if mr.IID != 42 || mr.Author.Username != "jane" || !mr.Draft {
		t.Fatalf("mr = %+v", mr)
	}
	// The project path is recovered from GitLab's reference.
	if mr.ProjectPath != "acme/app" {
		t.Errorf("project path = %q", mr.ProjectPath)
	}
	if mr.UpdatedAt.Year() != 2026 {
		t.Errorf("updated_at = %v", mr.UpdatedAt)
	}
}

func TestMergeRequestCommitsReportsTheTotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("per_page"); got != "10" {
			t.Errorf("per_page = %q", got)
		}
		w.Header().Set("x-total", "27")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"short_id":"abc1234","title":"Fix it"}]`)
	}))
	defer srv.Close()

	commits, total, err := New(srv.URL, "t").MergeRequestCommits(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 42}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || total != 27 {
		t.Fatalf("got %d commit(s), total %d", len(commits), total)
	}
}

func TestMergeRequestCommitsWithoutATotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	// GitLab omits x-total on very large collections; that is "unknown", not 0.
	if _, total, err := New(srv.URL, "t").MergeRequestCommits(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 42}, 10); err != nil || total != -1 {
		t.Fatalf("total = %d, err = %v", total, err)
	}
}

// TestAllMergeRequestCommitsArePaged: a limit of zero reads every page, and
// keeps GitLab's newest-first order.
func TestAllMergeRequestCommitsArePaged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[{"id":"c1"}]`)
			return
		}
		w.Header().Set("x-next-page", "2")
		fmt.Fprint(w, `[{"id":"c3"},{"id":"c2"}]`)
	}))
	defer srv.Close()

	commits, total, err := New(srv.URL, "t").MergeRequestCommits(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 42}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range commits {
		ids = append(ids, c.ID)
	}
	if got := strings.Join(ids, " "); got != "c3 c2 c1" || total != 3 {
		t.Fatalf("got %s, total %d", got, total)
	}
}

func TestMergeRequestDetailCarriesTheDiffRefs(t *testing.T) {
	var diverged string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		diverged = r.URL.Query().Get("include_diverged_commits_count")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"iid":42,"target_branch":"main","diverged_commits_count":3,
			"diff_refs":{"base_sha":"aaa111","head_sha":"bbb222","start_sha":"ccc333"}}`)
	}))
	defer srv.Close()

	mr, err := New(srv.URL, "t").MergeRequestDetail(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if mr.DiffRefs.BaseSHA != "aaa111" || mr.DiffRefs.HeadSHA != "bbb222" {
		t.Fatalf("diff refs = %+v", mr.DiffRefs)
	}
	if mr.DivergedCommitsCount != 3 {
		t.Errorf("diverged = %d", mr.DivergedCommitsCount)
	}
	if diverged != "true" {
		t.Errorf("include_diverged_commits_count = %q", diverged)
	}
}

func TestApprovePostsToTheApproveEndpoint(t *testing.T) {
	var path, method, token string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method, token = r.URL.Path, r.Method, r.Header.Get("PRIVATE-TOKEN")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	err := New(srv.URL, "secret").Approve(context.Background(),
		forge.MergeRequest{ProjectID: 3, IID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %q", method)
	}
	if path != "/api/v4/projects/3/merge_requests/42/approve" {
		t.Errorf("path = %q", path)
	}
	if token != "secret" {
		t.Errorf("token header = %q", token)
	}
}

func TestCommentPostsTheBody(t *testing.T) {
	var path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	err := New(srv.URL, "t").Comment(context.Background(),
		forge.MergeRequest{ProjectID: 3, IID: 42}, "looks **good**")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v4/projects/3/merge_requests/42/notes" {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"body":"looks **good**"`) {
		t.Errorf("payload = %q", body)
	}
}

// TestApproveReportsWhatWentWrong: approving is a paid feature on some tiers,
// so the error has to be legible.
func TestApproveReportsWhatWentWrong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"403 Forbidden"}`)
	}))
	defer srv.Close()

	err := New(srv.URL, "t").Approve(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 2})
	if err == nil || !strings.Contains(err.Error(), "denied access") {
		t.Fatalf("err = %v", err)
	}
}

// TestNotesComeFromDiscussions: the flat notes endpoint cannot say which
// comment answers which, the discussions one can.
func TestNotesComeFromDiscussions(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"id":"thread-a","notes":[
				{"id":1,"body":"is this right?","created_at":"2026-09-19T10:00:00Z",
				 "author":{"username":"ann"},"resolvable":true,"resolved":false,
				 "position":{"new_path":"rate.go","new_line":42}},
				{"id":2,"body":"yes","created_at":"2026-09-19T11:00:00Z","author":{"username":"bob"},
				 "resolvable":true,"resolved":false}
			]},
			{"id":"thread-b","notes":[
				{"id":3,"body":"LGTM","created_at":"2026-09-21T10:00:00Z","author":{"username":"carol"}}
			]},
			{"id":"thread-c","notes":[
				{"id":4,"body":"changed title","system":true,"created_at":"2026-09-20T10:00:00Z",
				 "author":{"username":"dave"}}
			]}
		]`)
	}))
	defer srv.Close()

	notes, err := New(srv.URL, "t").MergeRequestNotes(context.Background(),
		forge.MergeRequest{ProjectID: 1, IID: 7}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v4/projects/1/merge_requests/7/discussions" {
		t.Fatalf("path = %q", path)
	}
	if len(notes) != 4 {
		t.Fatalf("notes = %d", len(notes))
	}
	// Newest first, as the detail column wants them.
	if notes[0].ID != 3 || notes[len(notes)-1].ID != 1 {
		t.Fatalf("order = %d …%d", notes[0].ID, notes[len(notes)-1].ID)
	}
	// The two halves of the conversation know they belong together.
	byID := map[int]forge.Note{}
	for _, n := range notes {
		byID[n.ID] = n
	}
	if byID[1].Thread == "" || byID[1].Thread != byID[2].Thread {
		t.Fatalf("threads: %q and %q", byID[1].Thread, byID[2].Thread)
	}
	if byID[3].Thread == byID[1].Thread {
		t.Error("a separate comment landed in the same thread")
	}
	if byID[1].Path != "rate.go" || byID[1].Line != 42 || !byID[1].Resolvable {
		t.Errorf("inline details lost: %+v", byID[1])
	}
	if !byID[4].System {
		t.Error("system notes should still be marked, the interface filters them")
	}
}

func TestNotesRespectTheLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"t","notes":[
			{"id":1,"body":"a","created_at":"2026-09-19T10:00:00Z","author":{"username":"ann"}},
			{"id":2,"body":"b","created_at":"2026-09-20T10:00:00Z","author":{"username":"ann"}},
			{"id":3,"body":"c","created_at":"2026-09-21T10:00:00Z","author":{"username":"ann"}}]}]`)
	}))
	defer srv.Close()

	notes, err := New(srv.URL, "t").MergeRequestNotes(context.Background(),
		forge.MergeRequest{ProjectID: 1, IID: 7}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].ID != 3 || notes[1].ID != 2 {
		t.Fatalf("notes = %+v", notes)
	}
}

// TestListingCarriesTheCommentCount: GitLab reports it on the listing, which
// is what lets the table show it without opening anything.
func TestListingCarriesTheCommentCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"iid":42,"title":"Fix login","user_notes_count":7,
			"source_branch":"feat/x","target_branch":"main","project_id":3,
			"author":{"username":"jane"},"references":{"full":"acme/app!42"},
			"updated_at":"2026-01-02T03:04:05Z"},
			{"iid":43,"title":"Quiet one","user_notes_count":0,"project_id":3,
			"author":{"username":"bob"},"references":{"full":"acme/app!43"},
			"updated_at":"2026-01-02T03:04:05Z"}]`)
	}))
	defer srv.Close()

	mrs, err := New(srv.URL, "t").GroupMergeRequests(context.Background(), forge.Group{ID: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 2 {
		t.Fatalf("merge requests = %d", len(mrs))
	}
	if mrs[0].Comments != 7 {
		t.Fatalf("comments = %d, want 7", mrs[0].Comments)
	}
	if mrs[1].Comments != 0 {
		t.Errorf("a quiet merge request reported %d comments", mrs[1].Comments)
	}
}

// TestDetailCarriesTheCommentCountToo keeps the list and the detail agreeing.
func TestDetailCarriesTheCommentCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"iid":42,"title":"Fix login","user_notes_count":7,"project_id":3}`)
	}))
	defer srv.Close()

	det, err := New(srv.URL, "t").MergeRequestDetail(context.Background(),
		forge.MergeRequest{ProjectID: 3, IID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if det.UserNotesCount != 7 || det.Comments != 7 {
		t.Fatalf("detail counts: UserNotesCount=%d Comments=%d", det.UserNotesCount, det.Comments)
	}
}

func TestDeletedLineKeepsItsLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":"deleted","notes":[
   {"id":1,"position":{"old_path":"old.go","new_path":"renamed.go","old_line":7}},
   {"id":2,"position":{"old_path":"gone.go","old_line":12}},
   {"id":3,"position":{"old_path":"same.go","new_path":"same.go","old_line":2,"new_line":3}}
  ]}]`)
	}))
	defer srv.Close()
	notes, err := New(srv.URL, "t").MergeRequestNotes(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("notes: %+v", notes)
	}
	if notes[0].Path != "renamed.go" || notes[0].Line != 7 || !notes[0].Orphaned {
		t.Fatalf("deleted line: %+v", notes[0])
	}
	if notes[1].Path != "gone.go" || notes[1].Line != 12 || !notes[1].Orphaned {
		t.Fatalf("deleted file: %+v", notes[1])
	}
	if notes[2].Line != 3 || notes[2].Orphaned {
		t.Fatalf("current line: %+v", notes[2])
	}
}

// TestCreateProjectCommitsTheTemplates: GitLab takes the README and the
// branch on creation; the license and .gitignore come from its templates and
// are committed in one go on that branch.
func TestCreateProjectCommitsTheTemplates(t *testing.T) {
	var created, committed string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects":
			b, _ := io.ReadAll(r.Body)
			created = string(b)
			fmt.Fprint(w, `{"id":77,"name":"tool","path_with_namespace":"acme/tool","default_branch":"trunk",
				"http_url_to_repo":"https://gl.test/acme/tool.git"}`)
		case r.URL.Path == "/api/v4/user":
			fmt.Fprint(w, `{"username":"jane","name":"Jane Doe"}`)
		case r.URL.Path == "/api/v4/templates/licenses/mit":
			if r.URL.Query().Get("fullname") != "Jane Doe" {
				t.Errorf("license asked for %q", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"content":"MIT License, Jane Doe"}`)
		case r.URL.Path == "/api/v4/templates/gitignores/Go":
			fmt.Fprint(w, `{"content":"*.test"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/77/repository/commits":
			b, _ := io.ReadAll(r.Body)
			committed = string(b)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p, err := New(srv.URL, "t").CreateProject(context.Background(), forge.Group{ID: 5, FullPath: "acme"}, forge.NewProject{
		Name: "tool", Description: "A tool", Visibility: forge.VisibilityInternal, Readme: true,
		License: "mit", Gitignore: "Go", DefaultBranch: "trunk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.PathWithNamespace != "acme/tool" || p.DefaultBranch != "trunk" {
		t.Errorf("project = %+v", p)
	}
	for _, want := range []string{`"namespace_id":5`, `"visibility":"internal"`, `"initialize_with_readme":true`,
		`"default_branch":"trunk"`, `"description":"A tool"`} {
		if !strings.Contains(created, want) {
			t.Errorf("creation lacks %s: %s", want, created)
		}
	}
	for _, want := range []string{`"branch":"trunk"`, `"file_path":"LICENSE"`, `MIT License, Jane Doe`,
		`"file_path":".gitignore"`, `*.test`} {
		if !strings.Contains(committed, want) {
			t.Errorf("commit lacks %s: %s", want, committed)
		}
	}
}

// TestDeleteBranchEscapesTheName: a branch with a slash is one path segment.
func TestDeleteBranchEscapesTheName(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Method + " " + r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	err := New(srv.URL, "t").DeleteBranch(context.Background(), forge.Project{ID: 7}, "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if seen != "DELETE /api/v4/projects/7/repository/branches/feat%2Fx" {
		t.Errorf("asked %q", seen)
	}
}

// TestPipelineJobsLogsRetryAndThreads: a merge request's newest pipeline and
// its jobs, a job's trace as text, a retry posted to the job, and the
// discussions still to resolve counted once each.
func TestPipelineJobsLogsRetryAndThreads(t *testing.T) {
	var retried, played string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/3/merge_requests/7/pipelines":
			fmt.Fprint(w, `[{"id":90,"status":"failed","web_url":"https://gl/p/90"}]`)
		case "/api/v4/projects/3/pipelines":
			if r.URL.Query().Get("ref") != "feat/x" {
				t.Errorf("branch pipeline asked for ref %q", r.URL.Query().Get("ref"))
			}
			fmt.Fprint(w, `[{"id":90,"status":"failed","ref":"feat/x"}]`)
		case "/api/v4/projects/3/pipelines/90/jobs":
			fmt.Fprint(w, `[{"id":5,"name":"test","stage":"check","status":"failed","web_url":"https://gl/j/5","duration":61.2},
				{"id":6,"name":"lint","stage":"check","status":"success"}]`)
		case "/api/v4/projects/3/pipelines/90/bridges":
			fmt.Fprint(w, `[{"id":8,"name":"child","stage":"check","status":"success",
				"downstream_pipeline":{"id":91,"project_id":4,"status":"success"}}]`)
		case "/api/v4/projects/4/pipelines/91":
			fmt.Fprint(w, `{"id":91,"status":"success"}`)
		case "/api/v4/projects/4/pipelines/91/jobs":
			fmt.Fprint(w, `[{"id":21,"name":"deploy","stage":"deploy","status":"manual"},
				{"id":20,"name":"build","stage":"build","status":"success"}]`)
		case "/api/v4/projects/4/jobs/21/play":
			played = r.Method
		case "/api/v4/projects/3/jobs/5/trace":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "FAIL TestThing\n")
		case "/api/v4/projects/3/jobs/5/retry":
			retried = r.Method
			fmt.Fprint(w, `{}`)
		case "/api/v4/projects/3/merge_requests/7/discussions":
			fmt.Fprint(w, `[{"id":"a","notes":[{"resolvable":true,"resolved":false},{"resolvable":true,"resolved":false}]},
				{"id":"b","notes":[{"resolvable":true,"resolved":true}]},
				{"id":"c","notes":[{"resolvable":false}]}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, ctx := New(srv.URL, "t"), context.Background()
	mr := forge.MergeRequest{IID: 7, ProjectID: 3}

	p, jobs, err := c.PipelineJobs(ctx, mr)
	if err != nil || p == nil || p.Status != "failed" || len(jobs) != 3 || jobs[0].Name != "test" || jobs[0].Duration != 61.2 {
		t.Fatalf("pipeline %+v, jobs %+v, err %v", p, jobs, err)
	}
	child := jobs[2]
	if !child.Trigger || child.Downstream == nil || child.Downstream.ID != 91 || jobs[0].Trigger {
		t.Fatalf("the trigger job is %+v", child)
	}
	dp, djobs, err := c.DownstreamJobs(ctx, child)
	if err != nil || dp == nil || dp.ID != 91 || len(djobs) != 2 {
		t.Fatalf("downstream %+v, jobs %+v, err %v", dp, djobs, err)
	}
	// The stages in the order they were made, whatever order they came in.
	if djobs[0].Name != "build" || djobs[1].Status != "manual" {
		t.Errorf("downstream jobs in the wrong order: %+v", djobs)
	}
	if err := c.PlayJob(ctx, forge.Project{ID: 4}, djobs[1]); err != nil || played != http.MethodPost {
		t.Errorf("play: %v, method %q", err, played)
	}
	repo := forge.Project{ID: 3}
	bp, bjobs, err := c.BranchPipelineJobs(ctx, repo, "feat/x")
	if err != nil || bp == nil || bp.Ref != "feat/x" || len(bjobs) != 3 {
		t.Fatalf("branch pipeline %+v, jobs %+v, err %v", bp, bjobs, err)
	}
	if log, err := c.JobLog(ctx, repo, jobs[0]); err != nil || log != "FAIL TestThing\n" {
		t.Errorf("log = %q, %v", log, err)
	}
	if err := c.RetryJob(ctx, repo, jobs[0]); err != nil || retried != http.MethodPost {
		t.Errorf("retry: %v, method %q", err, retried)
	}
	if n, done, known, err := c.Threads(ctx, mr); err != nil || !known || n != 1 || done != 1 {
		t.Errorf("unresolved = %d, %v, %v; want 1 thread", n, known, err)
	}
}

// TestGitLabsPipelineObjectDoesNotBreakTheDecode: GitLab sends "pipeline" as
// an object on a merge request - its older name for head_pipeline - where
// the shared field of that name is a string. Both the detail and the listing
// have to read past it.
func TestGitLabsPipelineObjectDoesNotBreakTheDecode(t *testing.T) {
	const pipeline = `"pipeline":{"id":90,"sha":"bbb222","status":"running","web_url":"https://gl.example/p/90"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/merge_requests") {
			fmt.Fprint(w, `[{"id":1,"iid":42,"project_id":1,"title":"t",`+pipeline+`}]`)
			return
		}
		fmt.Fprint(w, `{"iid":42,"title":"t",`+pipeline+`,"head_pipeline":{"id":90,"status":"running"}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "t")
	det, err := c.MergeRequestDetail(context.Background(), forge.MergeRequest{ProjectID: 1, IID: 42})
	if err != nil {
		t.Fatalf("the detail: %v", err)
	}
	if det.Pipeline == nil || det.Pipeline.Status != "running" {
		t.Errorf("the head pipeline is lost: %+v", det.Pipeline)
	}
	mrs, err := c.GroupMergeRequests(context.Background(), forge.Group{ID: 1}, true)
	if err != nil {
		t.Fatalf("the listing: %v", err)
	}
	if len(mrs) != 1 || mrs[0].IID != 42 {
		t.Errorf("listed %+v", mrs)
	}
}

func TestUserNameAsksByUsername(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/users" || r.URL.Query().Get("username") != "jane" {
			t.Errorf("asked %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"username":"jane","name":"Jane Doe"}]`)
	}))
	defer srv.Close()
	name, err := New(srv.URL, "t").UserName(context.Background(), "jane")
	if err != nil || name != "Jane Doe" {
		t.Fatalf("name = %q, %v", name, err)
	}
}

// TestPipelinesAndTheirAttempts: the pipelines of a merge request, a
// branch and a commit come from their own places; a pipeline's jobs come
// with the attempts run again since, marked, and a commit's files are
// counted from its diff.
func TestPipelinesAndTheirAttempts(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		asked = append(asked, r.URL.Path+"?"+r.URL.Query().Get("ref")+r.URL.Query().Get("sha"))
		switch r.URL.Path {
		case "/api/v4/projects/3/merge_requests/7/pipelines", "/api/v4/projects/3/pipelines":
			fmt.Fprint(w, `[{"id":90,"status":"failed"},{"id":80,"status":"success"}]`)
		case "/api/v4/projects/3/pipelines/90/jobs":
			if r.URL.Query().Get("include_retried") != "true" {
				t.Error("the attempts run again were not asked for")
			}
			fmt.Fprint(w, `[{"id":5,"name":"test","stage":"check","status":"success"},
				{"id":3,"name":"test","stage":"check","status":"failed"}]`)
		case "/api/v4/projects/3/repository/commits/abc/diff":
			fmt.Fprint(w, `[{"new_path":"a.go","diff":"@@ -1 +1,2 @@\n+x\n+y\n-z\n"}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	c, ctx, repo := New(srv.URL, "t"), context.Background(), forge.Project{ID: 3}
	mr := forge.MergeRequest{IID: 7, ProjectID: 3}
	for _, q := range []forge.PipelineQuery{{MR: &mr}, {Ref: "main"}, {SHA: "abc"}} {
		if pipes, err := c.Pipelines(ctx, repo, q); err != nil || len(pipes) != 2 {
			t.Fatalf("%+v: %v, %v", q, pipes, err)
		}
	}
	if want := []string{"/api/v4/projects/3/merge_requests/7/pipelines?", "/api/v4/projects/3/pipelines?main", "/api/v4/projects/3/pipelines?abc"}; strings.Join(asked, " ") != strings.Join(want, " ") {
		t.Errorf("asked %v", asked)
	}
	jobs, err := c.Jobs(ctx, repo, forge.Pipeline{ID: 90})
	if err != nil || len(jobs) != 2 || jobs[0].ID != 3 || !jobs[0].Retried || jobs[1].Retried {
		t.Fatalf("jobs %+v, %v; want the attempt of 3 first, marked", jobs, err)
	}
	files, err := c.CommitFiles(ctx, repo, "abc")
	if err != nil || len(files) != 1 || files[0].Added != 2 || files[0].Deleted != 1 {
		t.Errorf("files %+v, %v", files, err)
	}
}

// TestPagesAreReadSideBySide: when GitLab says how many pages a listing has,
// the rest are asked for at once, and come back in their order.
func TestPagesAreReadSideBySide(t *testing.T) {
	var inFlight, most atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page > 1 {
			n := inFlight.Add(1)
			for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
			}
			if n >= 3 {
				close(release)
			}
			select {
			case <-release:
			case <-time.After(2 * time.Second):
			}
			defer inFlight.Add(-1)
		}
		w.Header().Set("x-total-pages", "4")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":%d,"iid":%d,"state":"opened"}]`, page, page)
	}))
	defer srv.Close()
	mrs, err := New(srv.URL, "t").ProjectMergeRequests(context.Background(), forge.Project{ID: 1, PathWithNamespace: "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 4 || mrs[0].IID != 1 || mrs[3].IID != 4 {
		t.Fatalf("got %+v", mrs)
	}
	if most.Load() < 3 {
		t.Errorf("at most %d pages were asked for at once", most.Load())
	}
	if mrs[0].ProjectPath != "a/b" {
		t.Errorf("a project's merge request lost its path: %q", mrs[0].ProjectPath)
	}
}
