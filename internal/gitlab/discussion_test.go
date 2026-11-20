package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestResolveDiscussionSendsAPutWithTheState(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		fmt.Fprint(w, `{"id":"abc123","notes":[]}`)
	})
	c := New(srv.URL, "secret-token")
	if err := c.ResolveDiscussion(context.Background(), discussionMR, "abc123", true); err != nil {
		t.Fatal(err)
	}
	if err := c.ResolveDiscussion(context.Background(), discussionMR, "abc123", false); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 {
		t.Fatalf("requests = %+v", posts)
	}
	for i, want := range []bool{true, false} {
		r := posts[i]
		if r.method != http.MethodPut || r.path != "/api/v4/projects/42/merge_requests/7/discussions/abc123" {
			t.Errorf("request %d = %s %s", i, r.method, r.path)
		}
		if r.token != "secret-token" {
			t.Errorf("token header = %q", r.token)
		}
		if got, ok := r.body["resolved"].(bool); !ok || got != want {
			t.Errorf("request %d body = %v, want resolved=%v", i, r.body, want)
		}
	}
}

func TestResolveDiscussionReportsARefusal(t *testing.T) {
	var posts []recorded
	srv := discussionServer(t, &posts, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"403 Forbidden"}`)
	})
	if err := New(srv.URL, "t").ResolveDiscussion(context.Background(), discussionMR, "abc", true); err == nil {
		t.Fatal("a refusal must be an error")
	}
}

// The diffs come from /diffs, or from /changes on a server older than it.
func TestMergeRequestDiffsFallBackOnChanges(t *testing.T) {
	for _, old := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			file := `{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1,2 @@\n a\n+b\n"}`
			switch {
			case r.URL.Path == "/api/v4/projects/42/merge_requests/7/diffs" && !old:
				fmt.Fprint(w, "["+file+"]")
			case r.URL.Path == "/api/v4/projects/42/merge_requests/7/changes" && old:
				fmt.Fprint(w, `{"changes":[`+file+`]}`)
			default:
				http.NotFound(w, r)
			}
		}))
		files, err := New(srv.URL, "t").MergeRequestDiffs(context.Background(), discussionMR)
		srv.Close()
		if err != nil || len(files) != 1 || files[0].Path != "a.go" || !strings.Contains(files[0].Diff, "+b") {
			t.Errorf("old server %v: files = %+v, %v", old, files, err)
		}
	}
}

// A comment on a range says the diff it was written on and its first and
// last line; GitLab's note that a later version changed the line says the
// commits of that change, the version's head read from the versions.
func TestNotesSayTheirDiffTheirRangeAndWhatChangedThem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/42/merge_requests/7/discussions":
			fmt.Fprint(w, `[{"id":"d1","notes":[
				{"id":1,"body":"use mapToSet","created_at":"2026-09-20T10:00:00Z",
				 "position":{"base_sha":"b1","head_sha":"h1","new_path":"K.kt","old_path":"K.kt","new_line":117,
				  "line_range":{"start":{"old_line":119,"new_line":null},"end":{"old_line":null,"new_line":117}}}},
				{"id":2,"system":true,"created_at":"2026-09-21T10:00:00Z",
				 "body":"changed this line in [version 11 of the diff](/g/p/-/merge_requests/7/diffs?diff_id=5511&start_sha=aaa111#abc_117_118)"}]}]`)
		case "/api/v4/projects/42/merge_requests/7/versions":
			fmt.Fprint(w, `[{"id":5511,"head_commit_sha":"h11"},{"id":5500,"head_commit_sha":"h10"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	notes, err := New(srv.URL, "t").MergeRequestNotes(context.Background(), discussionMR, 0)
	if err != nil || len(notes) != 2 {
		t.Fatalf("notes = %+v, %v", notes, err)
	}
	byID := map[int]forge.Note{notes[0].ID: notes[0], notes[1].ID: notes[1]}
	c := byID[1]
	if c.Base != "b1" || c.Head != "h1" || c.From != (forge.LineRef{Old: 119}) || c.To != (forge.LineRef{New: 117}) {
		t.Errorf("comment = %+v", c)
	}
	ch := byID[2].Changed
	if ch == nil || ch.From != "aaa111" || ch.To != "h11" || ch.Line != (forge.LineRef{Old: 117, New: 118}) {
		t.Errorf("change = %+v", ch)
	}
}

func TestFilesAreReadAtTheirCommits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.EscapedPath() == "/api/v4/projects/42/repository/files/src%2Fx.go/raw" && r.URL.Query().Get("ref") == "b":
			fmt.Fprint(w, "package x\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "t")
	text, err := c.FileAt(context.Background(), discussionMR, "src/x.go", "b")
	if err != nil || text != "package x\n" {
		t.Errorf("file = %q, %v", text, err)
	}
}

func TestANoteIsEditedAndDeletedInItsDiscussion(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "t")
	n := forge.Note{ID: 9, Thread: "d1"}
	if err := c.EditNote(context.Background(), discussionMR, n, "better"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteNote(context.Background(), discussionMR, n); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`PUT /api/v4/projects/42/merge_requests/7/discussions/d1/notes/9 {"body":"better"}`,
		`DELETE /api/v4/projects/42/merge_requests/7/discussions/d1/notes/9 `,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}
