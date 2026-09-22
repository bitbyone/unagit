package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
