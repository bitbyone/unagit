package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

var discussionMR = forge.MergeRequest{ProjectID: 42, IID: 7, WebURL: "https://gl.example/g/app/-/merge_requests/7"}

const diffRefsJSON = `{"diff_refs":{"base_sha":"base111","start_sha":"start222","head_sha":"head333"}}`

type recorded struct {
	method, path, token string
	body                map[string]any
}

// discussionServer answers the merge request itself with its diff refs and
// hands every POST to post.
func discussionServer(t *testing.T, posts *[]recorded, post func(w http.ResponseWriter, n int)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, diffRefsJSON)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{method: r.Method, path: r.URL.Path, token: r.Header.Get("PRIVATE-TOKEN")}
		_ = json.Unmarshal(raw, &rec.body)
		*posts = append(*posts, rec)
		post(w, len(*posts))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateDiscussionPositionsTheComment(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		fmt.Fprint(w, `{"id":"abc123","notes":[{"id":501,"body":"please check","created_at":"2026-09-20T10:00:00Z",
			"author":{"username":"toby","name":"Toby"},"resolvable":true,
			"position":{"new_path":"src/app/main.go","new_line":12}}]}`)
	})

	n, err := New(srv.URL, "secret-token").CreateDiscussion(context.Background(), discussionMR, "src/app/main.go", 12, "please check")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].path != "/api/v4/projects/42/merge_requests/7/discussions" {
		t.Fatalf("posts = %+v", posts)
	}
	if posts[0].token != "secret-token" {
		t.Errorf("token header = %q", posts[0].token)
	}
	if posts[0].body["body"] != "please check" {
		t.Errorf("body = %v", posts[0].body["body"])
	}
	pos, _ := posts[0].body["position"].(map[string]any)
	for key, want := range map[string]any{
		"position_type": "text", "base_sha": "base111", "start_sha": "start222", "head_sha": "head333",
		"new_path": "src/app/main.go", "old_path": "src/app/main.go", "new_line": float64(12),
	} {
		if pos[key] != want {
			t.Errorf("position.%s = %v, want %v", key, pos[key], want)
		}
	}
	if n.ID != 501 || n.Thread != "abc123" || n.URL != discussionMR.WebURL+"#note_501" {
		t.Errorf("note = %+v", n)
	}
	if n.Path != "src/app/main.go" || n.Line != 12 {
		t.Errorf("a positioned note keeps its location: %+v", n)
	}
	if n.Author.Username != "toby" {
		t.Errorf("author = %+v", n.Author)
	}
}

func TestCreateDiscussionFallsBackToTheConversation(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, n int) {
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"message":"400 Bad request - Note {:line_code=>[\"can't be blank\"]}"}`)
			return
		}
		fmt.Fprint(w, `{"id":"def456","notes":[{"id":502,"body":"x","created_at":"2026-09-20T10:00:00Z"}]}`)
	})

	n, err := New(srv.URL, "t").CreateDiscussion(context.Background(), discussionMR, "src/app/main.go", 99, "not in the diff")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Fatalf("posts = %+v", posts)
	}
	if _, has := posts[1].body["position"]; has {
		t.Error("the fallback must not carry a position")
	}
	if want := "`src/app/main.go:99`\n\nnot in the diff"; posts[1].body["body"] != want {
		t.Errorf("fallback body = %q, want %q", posts[1].body["body"], want)
	}
	if n.ID != 502 || n.Thread != "def456" || n.URL != discussionMR.WebURL+"#note_502" {
		t.Errorf("note = %+v", n)
	}
	if n.Path != "" || n.Line != 0 {
		t.Errorf("the fallback is signalled by an empty location: %+v", n)
	}
}

func TestCreateDiscussionDoesNotHideOtherErrors(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"403 Forbidden"}`)
	})
	if _, err := New(srv.URL, "t").CreateDiscussion(context.Background(), discussionMR, "a.go", 1, "x"); err == nil {
		t.Fatal("a 403 is not a position problem and must be reported")
	}
	if len(posts) != 1 {
		t.Errorf("only the positioned attempt should have been made: %+v", posts)
	}
}

func TestReplyToDiscussionPostsIntoTheThread(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		fmt.Fprint(w, `{"id":503,"body":"thanks","created_at":"2026-09-20T11:00:00Z","author":{"username":"toby"}}`)
	})

	n, err := New(srv.URL, "secret-token").ReplyToDiscussion(context.Background(), discussionMR, "abc123", "thanks")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].path != "/api/v4/projects/42/merge_requests/7/discussions/abc123/notes" {
		t.Fatalf("posts = %+v", posts)
	}
	if posts[0].token != "secret-token" || posts[0].body["body"] != "thanks" {
		t.Errorf("request = %+v", posts[0])
	}
	if n.ID != 503 || n.Thread != "abc123" || n.URL != discussionMR.WebURL+"#note_503" {
		t.Errorf("note = %+v", n)
	}
}

func TestNotesCarryTheirLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"d1","notes":[{"id":9,"body":"hi","created_at":"2026-09-20T10:00:00Z"}]}]`)
	}))
	defer srv.Close()
	notes, err := New(srv.URL, "t").MergeRequestNotes(context.Background(), discussionMR, 0)
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
	if notes[0].URL != discussionMR.WebURL+"#note_9" {
		t.Errorf("url = %q", notes[0].URL)
	}
}

func TestDetailCarriesTheStartSHA(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(http.ResponseWriter, int) {})
	d, err := New(srv.URL, "t").MergeRequestDetail(context.Background(), discussionMR)
	if err != nil {
		t.Fatal(err)
	}
	if d.DiffRefs.StartSHA != "start222" {
		t.Errorf("start sha = %q", d.DiffRefs.StartSHA)
	}
}

func TestCommentNoteReturnsWhatItCreated(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		fmt.Fprint(w, `{"id":901,"body":"hello","created_at":"2026-09-20T10:00:00Z","author":{"username":"toby"}}`)
	})
	n, err := New(srv.URL, "secret-token").CommentNote(context.Background(), discussionMR, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0].path != "/api/v4/projects/42/merge_requests/7/notes" || posts[0].body["body"] != "hello" {
		t.Fatalf("posts = %+v", posts)
	}
	if n.ID != 901 || n.Thread != "" || n.URL != discussionMR.WebURL+"#note_901" {
		t.Errorf("note = %+v", n)
	}
}
