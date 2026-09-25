package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

var newPRProject = forge.Project{ID: 7, PathWithNamespace: "acme/api"}

const createdPRJSON = `{"id":5001,"number":33,"title":"Rate limit","state":"open","draft":true,
	"html_url":"https://github.com/acme/api/pull/33","user":{"login":"toby","name":"Toby"},
	"head":{"ref":"feat/x","sha":"abc"},"base":{"ref":"main","sha":"def","repo":{"id":7,"full_name":"acme/api"}},
	"updated_at":"2026-09-25T10:00:00Z"}`

func TestCreateMergeRequestOpensAPullRequest(t *testing.T) {
	s := newStub(t)
	var posts []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls", capture(&posts, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, createdPRJSON)
	}))
	mr, err := s.client().CreateMergeRequest(context.Background(), newPRProject, forge.NewMergeRequest{
		Title: "Rate limit", Description: "Adds a bucket.", SourceBranch: "feat/x", TargetBranch: "main",
		Draft: true, RemoveSourceBranch: true, Squash: true, // GitHub has no place for the last two
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.seenAuth != "Bearer ghp-test" {
		t.Errorf("authorization = %q", s.seenAuth)
	}
	if len(posts) != 1 || posts[0]["_method"] != http.MethodPost {
		t.Fatalf("posts = %+v", posts)
	}
	for key, want := range map[string]any{
		"title": "Rate limit", "body": "Adds a bucket.", "head": "feat/x", "base": "main", "draft": true,
	} {
		if posts[0][key] != want {
			t.Errorf("%s = %v, want %v", key, posts[0][key], want)
		}
	}
	if _, sent := posts[0]["squash"]; sent {
		t.Error("squash is GitLab's; it must not be sent")
	}
	if mr.IID != 33 || mr.ID != 5001 || !mr.Draft || mr.State != "open" || mr.SourceBranch != "feat/x" ||
		mr.TargetBranch != "main" || mr.ProjectID != 7 || mr.ProjectPath != "acme/api" ||
		mr.WebURL != "https://github.com/acme/api/pull/33" || mr.Author.Username != "toby" || mr.UpdatedAt.IsZero() {
		t.Errorf("returned = %+v", mr)
	}
}

func TestCreateMergeRequestSaysWhenOneIsAlreadyOpen(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/repos/acme/api/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"message":"Validation Failed","errors":[{"message":"A pull request already exists for acme:feat/x."}]}`)
			return
		}
		if got := r.URL.Query().Get("head"); got != "acme:feat/x" || r.URL.Query().Get("state") != "open" {
			t.Errorf("lookup query = %v", r.URL.Query())
		}
		fmt.Fprint(w, `[{"number":21,"html_url":"https://github.com/acme/api/pull/21"}]`)
	})
	_, err := s.client().CreateMergeRequest(context.Background(), newPRProject,
		forge.NewMergeRequest{Title: "x", SourceBranch: "feat/x", TargetBranch: "main"})
	if !errors.Is(err, forge.ErrMergeRequestExists) {
		t.Fatalf("err = %v, want ErrMergeRequestExists", err)
	}
	if !strings.Contains(err.Error(), "!21") || !strings.Contains(err.Error(), "pull/21") {
		t.Errorf("the message should name the existing pull request: %v", err)
	}
}

func TestCreateMergeRequestDoesNotHideOtherValidationErrors(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/repos/acme/api/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"message":"Validation Failed","errors":[{"message":"No commits between main and feat/x"}]}`)
	})
	_, err := s.client().CreateMergeRequest(context.Background(), newPRProject,
		forge.NewMergeRequest{Title: "x", SourceBranch: "feat/x", TargetBranch: "main"})
	if err == nil || errors.Is(err, forge.ErrMergeRequestExists) || !strings.Contains(err.Error(), "No commits") {
		t.Fatalf("err = %v", err)
	}
}
