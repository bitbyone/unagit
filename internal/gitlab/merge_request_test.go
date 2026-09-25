package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

var newMRProject = forge.Project{ID: 42, PathWithNamespace: "g/app", WebURL: "https://gl.example/g/app"}

const createdJSON = `{"id":900,"iid":12,"project_id":42,"title":"Draft: Rate limit","state":"opened","draft":true,
	"source_branch":"feat/x","target_branch":"main","web_url":"https://gl.example/g/app/-/merge_requests/12",
	"updated_at":"2026-09-25T10:00:00Z","author":{"username":"toby","name":"Toby"},"user_notes_count":0}`

func TestCreateMergeRequestPostsTheFieldsAndReturnsWhatWasCreated(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, createdJSON)
	})
	mr, err := New(srv.URL, "secret-token").CreateMergeRequest(context.Background(), newMRProject, forge.NewMergeRequest{
		Title: "Rate limit", Description: "Adds a bucket.", SourceBranch: "feat/x", TargetBranch: "main",
		Draft: true, RemoveSourceBranch: true, Squash: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].method != http.MethodPost || posts[0].path != "/api/v4/projects/42/merge_requests" {
		t.Fatalf("posts = %+v", posts)
	}
	if posts[0].token != "secret-token" {
		t.Errorf("token header = %q", posts[0].token)
	}
	for key, want := range map[string]any{
		"source_branch": "feat/x", "target_branch": "main", "title": "Draft: Rate limit",
		"description": "Adds a bucket.", "remove_source_branch": true, "squash": true,
	} {
		if posts[0].body[key] != want {
			t.Errorf("%s = %v, want %v", key, posts[0].body[key], want)
		}
	}
	if mr.IID != 12 || mr.ID != 900 || !mr.Draft || mr.State != "opened" || mr.SourceBranch != "feat/x" ||
		mr.TargetBranch != "main" || mr.ProjectID != 42 || mr.Author.Username != "toby" ||
		mr.WebURL != "https://gl.example/g/app/-/merge_requests/12" || mr.UpdatedAt.IsZero() {
		t.Errorf("returned = %+v", mr)
	}
	if mr.ProjectPath != "g/app" {
		t.Errorf("project path = %q, want it filled from the project", mr.ProjectPath)
	}
}

func TestCreateMergeRequestDoesNotPrefixATitleThatIsAlreadyADraft(t *testing.T) {
	for _, title := range []string{"Draft: x", "draft: x", "[Draft] x", "(draft) x", "Draft x"} {
		var posts []recorded
		srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) { fmt.Fprint(w, createdJSON) })
		_, err := New(srv.URL, "t").CreateMergeRequest(context.Background(), newMRProject,
			forge.NewMergeRequest{Title: title, SourceBranch: "a", TargetBranch: "main", Draft: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := posts[0].body["title"]; got != title {
			t.Errorf("title %q was sent as %q", title, got)
		}
	}
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) { fmt.Fprint(w, createdJSON) })
	if _, err := New(srv.URL, "t").CreateMergeRequest(context.Background(), newMRProject,
		forge.NewMergeRequest{Title: "Plain", SourceBranch: "a", TargetBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if got := posts[0].body["title"]; got != "Plain" {
		t.Errorf("a non-draft title changed: %q", got)
	}
}

func TestCreateMergeRequestSaysWhenOneIsAlreadyOpen(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"message":["Another open merge request already exists for this source branch: !9"]}`)
	})
	_, err := New(srv.URL, "t").CreateMergeRequest(context.Background(), newMRProject,
		forge.NewMergeRequest{Title: "x", SourceBranch: "feat/x", TargetBranch: "main"})
	if !errors.Is(err, forge.ErrMergeRequestExists) {
		t.Fatalf("err = %v, want ErrMergeRequestExists", err)
	}
	if !strings.Contains(err.Error(), "!9") || !strings.Contains(err.Error(), "https://gl.example/g/app/-/merge_requests/9") {
		t.Errorf("the message should name the existing request and link it: %v", err)
	}
}

func TestCreateMergeRequestDoesNotHideOtherErrors(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"403 Forbidden"}`)
	})
	_, err := New(srv.URL, "t").CreateMergeRequest(context.Background(), newMRProject,
		forge.NewMergeRequest{Title: "x", SourceBranch: "a", TargetBranch: "main"})
	if err == nil || errors.Is(err, forge.ErrMergeRequestExists) || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}
