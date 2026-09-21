package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	projects, err := New(srv.URL, "secret-token").GroupProjects(context.Background(), 5, true)
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

	if _, err := New(srv.URL, "t").GroupProjects(context.Background(), 5, false); err != nil {
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

	mrs, err := New(srv.URL, "t").GroupMergeRequests(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	mr := mrs[0]
	if mr.IID != 42 || mr.Author.Username != "jane" || !mr.Draft || mr.References.Full != "acme/app!42" {
		t.Fatalf("mr = %+v", mr)
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

	commits, total, err := New(srv.URL, "t").MergeRequestCommits(context.Background(), 1, 42, 10)
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
	if _, total, err := New(srv.URL, "t").MergeRequestCommits(context.Background(), 1, 42, 10); err != nil || total != -1 {
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

	mr, err := New(srv.URL, "t").MergeRequest(context.Background(), 1, 42)
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
