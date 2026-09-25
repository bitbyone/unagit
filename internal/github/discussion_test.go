package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

var reviewMR = forge.MergeRequest{ProjectPath: "acme/api", IID: 7}

// capture records the JSON body and the method of the request a handler sees.
func capture(dst *[]map[string]any, then func(w http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{"_method": r.Method}
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		for k, v := range parsed {
			body[k] = v
		}
		*dst = append(*dst, body)
		then(w)
	}
}

func TestCreateDiscussionStartsAReviewThread(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"number":7,"head":{"sha":"headsha999"}}`)
	var posts []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7/comments", capture(&posts, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":800,"body":"please check","created_at":"2026-09-20T10:00:00Z","user":{"login":"toby"},
			"path":"src/app/main.go","line":12,"side":"RIGHT","html_url":"https://github.com/acme/api/pull/7#discussion_r800"}`)
	}))

	n, err := s.client().CreateDiscussion(context.Background(), reviewMR, "src/app/main.go", 12, "please check")
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
		"body": "please check", "commit_id": "headsha999", "path": "src/app/main.go", "line": float64(12), "side": "RIGHT",
	} {
		if posts[0][key] != want {
			t.Errorf("%s = %v, want %v", key, posts[0][key], want)
		}
	}
	if n.ID != 800 || n.Thread != "800" || n.URL != "https://github.com/acme/api/pull/7#discussion_r800" {
		t.Errorf("note = %+v", n)
	}
	if n.Path != "src/app/main.go" || n.Line != 12 {
		t.Errorf("a positioned note keeps its location: %+v", n)
	}
}

func TestCreateDiscussionFallsBackToTheConversation(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"number":7,"head":{"sha":"headsha999"}}`)
	var review, general []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7/comments", capture(&review, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"message":"Validation Failed","errors":[{"message":"pull_request_review_thread.line must be part of the diff"}]}`)
	}))
	s.mux.HandleFunc("/repos/acme/api/issues/7/comments", capture(&general, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":801,"body":"x","created_at":"2026-09-20T10:00:00Z","user":{"login":"toby"},
			"html_url":"https://github.com/acme/api/pull/7#issuecomment-801"}`)
	}))

	n, err := s.client().CreateDiscussion(context.Background(), reviewMR, "src/app/main.go", 99, "not in the diff")
	if err != nil {
		t.Fatal(err)
	}
	if len(review) != 1 || len(general) != 1 {
		t.Fatalf("review=%d general=%d", len(review), len(general))
	}
	if want := "`src/app/main.go:99`\n\nnot in the diff"; general[0]["body"] != want {
		t.Errorf("fallback body = %q, want %q", general[0]["body"], want)
	}
	if n.ID != 801 || n.Thread != "" || n.URL != "https://github.com/acme/api/pull/7#issuecomment-801" {
		t.Errorf("note = %+v", n)
	}
	if n.Path != "" || n.Line != 0 {
		t.Errorf("the fallback is signalled by an empty location: %+v", n)
	}
}

func TestCreateDiscussionDoesNotHideOtherErrors(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"number":7,"head":{"sha":"h"}}`)
	var general []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7/comments", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"Resource not accessible"}`)
	})
	s.mux.HandleFunc("/repos/acme/api/issues/7/comments", capture(&general, func(w http.ResponseWriter) {}))
	if _, err := s.client().CreateDiscussion(context.Background(), reviewMR, "a.go", 1, "x"); err == nil {
		t.Fatal("a 403 is not a position problem and must be reported")
	}
	if len(general) != 0 {
		t.Error("nothing should have been posted to the conversation")
	}
}

func TestReplyToDiscussionAnswersTheThread(t *testing.T) {
	s := newStub(t)
	var posts []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7/comments/800/replies", capture(&posts, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":802,"body":"thanks","created_at":"2026-09-20T11:00:00Z","user":{"login":"toby"},
			"in_reply_to_id":800,"html_url":"https://github.com/acme/api/pull/7#discussion_r802"}`)
	}))

	n, err := s.client().ReplyToDiscussion(context.Background(), reviewMR, "800", "thanks")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0]["body"] != "thanks" || posts[0]["_method"] != http.MethodPost {
		t.Fatalf("posts = %+v", posts)
	}
	if s.seenAuth != "Bearer ghp-test" {
		t.Errorf("authorization = %q", s.seenAuth)
	}
	if n.ID != 802 || n.Thread != "800" || n.URL != "https://github.com/acme/api/pull/7#discussion_r802" {
		t.Errorf("note = %+v", n)
	}
}

// A Thread from CreateDiscussion goes straight into ReplyToDiscussion, and is
// the same one MergeRequestNotes reports for that comment.
func TestAThreadRoundTripsBetweenCreateReplyAndNotes(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"number":7,"head":{"sha":"h"}}`)
	s.handle("/repos/acme/api/pulls/7/comments/800/replies", `{"id":802,"body":"r","in_reply_to_id":800}`)
	s.mux.HandleFunc("/repos/acme/api/pulls/7/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"id":800,"body":"b","path":"a.go","line":3}`)
			return
		}
		fmt.Fprint(w, `[{"id":800,"body":"b","path":"a.go","line":3,"created_at":"2026-09-20T10:00:00Z"}]`)
	})
	s.handle("/repos/acme/api/issues/7/comments", `[]`)

	created, err := s.client().CreateDiscussion(context.Background(), reviewMR, "a.go", 3, "b")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.client().ReplyToDiscussion(context.Background(), reviewMR, created.Thread, "r")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := s.client().MergeRequestNotes(context.Background(), reviewMR, 0)
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
	if created.Thread != notes[0].Thread || reply.Thread != notes[0].Thread {
		t.Errorf("threads differ: created %q, reply %q, listed %q", created.Thread, reply.Thread, notes[0].Thread)
	}
}

func TestNotesCarryTheirLink(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/issues/7/comments",
		`[{"id":10,"body":"x","created_at":"2026-09-20T10:00:00Z","user":{"login":"j"},"html_url":"https://github.com/acme/api/pull/7#issuecomment-10"}]`)
	s.handle("/repos/acme/api/pulls/7/comments", `[]`)
	notes, err := s.client().MergeRequestNotes(context.Background(), reviewMR, 0)
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
	if notes[0].URL != "https://github.com/acme/api/pull/7#issuecomment-10" {
		t.Errorf("url = %q", notes[0].URL)
	}
}

func TestCommentNoteReturnsWhatItCreatedWithoutAThread(t *testing.T) {
	s := newStub(t)
	var general []map[string]any
	s.mux.HandleFunc("/repos/acme/api/issues/7/comments", capture(&general, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":802,"body":"hello","created_at":"2026-09-20T10:00:00Z","user":{"login":"toby"},
			"html_url":"https://github.com/acme/api/pull/7#issuecomment-802"}`)
	}))
	n, err := s.client().CommentNote(context.Background(), reviewMR, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(general) != 1 || general[0]["body"] != "hello" {
		t.Fatalf("posted: %v", general)
	}
	if n.ID != 802 || n.Thread != "" || n.URL != "https://github.com/acme/api/pull/7#issuecomment-802" {
		t.Errorf("note = %+v", n)
	}
}

func TestResolveDiscussionIsNotSupported(t *testing.T) {
	err := New("token").ResolveDiscussion(context.Background(), forge.MergeRequest{}, "1", true)
	if !errors.Is(err, forge.ErrNotSupported) {
		t.Fatalf("err = %v, want forge.ErrNotSupported", err)
	}
}
