package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

var mergeMR = forge.MergeRequest{IID: 7, ProjectID: 42, ProjectPath: "g/app"}

// mergeServer records every request that is not a GET and answers GETs from
// gets, by path.
func mergeServer(t *testing.T, sent *[]recorded, gets map[string]string, answer func(w http.ResponseWriter, r recorded)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			body, ok := gets[r.URL.Path+"?"+r.URL.RawQuery]
			if !ok {
				body, ok = gets[r.URL.Path]
			}
			if !ok {
				t.Errorf("unexpected GET %s", r.URL)
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, body)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		rec := recorded{method: r.Method, path: r.URL.Path}
		_ = json.Unmarshal(raw, &rec.body)
		*sent = append(*sent, rec)
		if answer != nil {
			answer(w, rec)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMergeSendsTheChoicesAndTheHead(t *testing.T) {
	var sent []recorded
	srv := mergeServer(t, &sent, nil, nil)
	err := New(srv.URL, "t").Merge(context.Background(), mergeMR, forge.MergeOptions{
		Squash: true, RemoveSourceBranch: true, WhenPipelineSucceeds: true, SHA: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].method != http.MethodPut || sent[0].path != "/api/v4/projects/42/merge_requests/7/merge" {
		t.Fatalf("sent = %+v", sent)
	}
	for key, want := range map[string]any{
		"squash": true, "should_remove_source_branch": true, "sha": "abc123",
		"merge_when_pipeline_succeeds": true, "auto_merge": true,
	} {
		if sent[0].body[key] != want {
			t.Errorf("%s = %v, want %v", key, sent[0].body[key], want)
		}
	}
}

func TestMergeSaysWhyGitLabRefused(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   string
		moved  bool
	}{
		{http.StatusConflict, `{"message":"SHA does not match HEAD of source branch"}`, "", true},
		{http.StatusMethodNotAllowed, `{"message":"405 Method Not Allowed"}`, "GitLab will not merge !7: 405 Method Not Allowed", false},
		{http.StatusUnprocessableEntity, `{"message":["Branch cannot be merged"]}`, "Branch cannot be merged", false},
	} {
		var sent []recorded
		srv := mergeServer(t, &sent, nil, func(w http.ResponseWriter, _ recorded) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		})
		err := New(srv.URL, "t").Merge(context.Background(), mergeMR, forge.MergeOptions{SHA: "abc"})
		if c.moved {
			if !errors.Is(err, forge.ErrHeadMoved) {
				t.Errorf("%d: err = %v, want ErrHeadMoved", c.status, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d: err = %v, want it to say %q", c.status, err, c.want)
		}
	}
}

func TestSetDraftRewritesTheTitleItReadsFresh(t *testing.T) {
	for _, c := range []struct {
		title string
		draft bool
		want  string // "" when nothing should be written
	}{
		{"Rate limit", true, "Draft: Rate limit"},
		{"Draft: Rate limit", false, "Rate limit"},
		{"[Draft] Draft: Rate limit", false, "Rate limit"},
		{"(draft) Rate limit", true, "Draft: Rate limit"},
		{"Draft: Rate limit", true, ""},
		{"Rate limit", false, ""},
	} {
		var sent []recorded
		gets := map[string]string{"/api/v4/projects/42/merge_requests/7": fmt.Sprintf(`{"title":%q}`, c.title)}
		srv := mergeServer(t, &sent, gets, nil)
		// The list's title is stale on purpose: the fresh one counts.
		stale := mergeMR
		stale.Title = "something old"
		if err := New(srv.URL, "t").SetDraft(context.Background(), stale, c.draft); err != nil {
			t.Fatal(err)
		}
		if c.want == "" {
			if len(sent) != 0 {
				t.Errorf("%q draft=%v wrote %+v, want nothing", c.title, c.draft, sent)
			}
			continue
		}
		if len(sent) != 1 || sent[0].method != http.MethodPut || sent[0].body["title"] != c.want {
			t.Errorf("%q draft=%v sent %+v, want title %q", c.title, c.draft, sent, c.want)
		}
	}
}

func TestCloseMergeRequestSendsTheStateEvent(t *testing.T) {
	var sent []recorded
	srv := mergeServer(t, &sent, nil, nil)
	if err := New(srv.URL, "t").CloseMergeRequest(context.Background(), mergeMR); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].method != http.MethodPut || sent[0].path != "/api/v4/projects/42/merge_requests/7" ||
		sent[0].body["state_event"] != "close" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestReviewersAreMembersAndAreSetByID(t *testing.T) {
	var sent []recorded
	srv := mergeServer(t, &sent, map[string]string{
		"/api/v4/projects/42/members/all": `[{"id":3,"username":"jane","name":"Jane","state":"active"},
			{"id":4,"username":"gone","name":"Gone","state":"blocked"}]`,
		"/api/v4/users?username=jane":   `[{"id":3,"username":"jane"}]`,
		"/api/v4/users?username=nobody": `[]`,
	}, nil)
	c := New(srv.URL, "t")
	users, err := c.ReviewerCandidates(context.Background(), mergeMR)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Username != "jane" || users[0].Name != "Jane" {
		t.Errorf("candidates = %+v, want jane alone - a blocked member cannot review", users)
	}

	if err := c.SetReviewers(context.Background(), mergeMR, []string{"jane"}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || fmt.Sprint(sent[0].body["reviewer_ids"]) != "[3]" {
		t.Errorf("sent = %+v", sent)
	}
	if err := c.SetReviewers(context.Background(), mergeMR, nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || fmt.Sprint(sent[1].body["reviewer_ids"]) != "[0]" {
		t.Errorf("clearing sent %+v", sent[1:])
	}
	if err := c.SetReviewers(context.Background(), mergeMR, []string{"nobody"}); err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Errorf("an unknown user: err = %v", err)
	}
}

// TestLabelsAreTheProjectsAndTheGroupsAndAreSetByName: the choices include
// the groups' labels, and the labels go back as one comma-separated
// string, an empty one clearing them.
func TestLabelsAreTheProjectsAndTheGroupsAndAreSetByName(t *testing.T) {
	var sent []recorded
	srv := mergeServer(t, &sent, map[string]string{
		"/api/v4/projects/42/labels?include_ancestor_groups=true&page=1&per_page=100": `[{"name":"bug","color":"#d9534f","description":"Something broke"},
			{"name":"group::backend","color":"#428bca"}]`,
	}, nil)
	c := New(srv.URL, "t")
	labels, err := c.LabelChoices(context.Background(), mergeMR)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 || labels[0] != (forge.Label{Name: "bug", Color: "#d9534f", Description: "Something broke"}) || labels[1].Name != "group::backend" {
		t.Errorf("choices = %+v", labels)
	}
	if err := c.SetLabels(context.Background(), mergeMR, []string{"bug", "group::backend"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetLabels(context.Background(), mergeMR, nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0].path != "/api/v4/projects/42/merge_requests/7" ||
		sent[0].body["labels"] != "bug,group::backend" || sent[1].body["labels"] != "" {
		t.Errorf("sent = %+v", sent)
	}
}

// TestALabelIsReadAsAnObjectOrAName: GitLab sends a label's details only
// when asked, its bare name otherwise; a merge request decodes either way.
func TestALabelIsReadAsAnObjectOrAName(t *testing.T) {
	var mr forge.MergeRequest
	if err := json.Unmarshal([]byte(`{"iid":7,"labels":["bug","ux"]}`), &mr); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(forge.LabelNames(mr.Labels)) != "[bug ux]" {
		t.Errorf("names = %+v", mr.Labels)
	}
	if err := json.Unmarshal([]byte(`{"iid":7,"labels":[{"name":"bug","color":"#d9534f"}]}`), &mr); err != nil {
		t.Fatal(err)
	}
	if len(mr.Labels) != 1 || mr.Labels[0].Color != "#d9534f" {
		t.Errorf("details = %+v", mr.Labels)
	}
}

// TestAssigneesAreSetByIDAndClearedWithZero: as the reviewers are, in their
// own field.
func TestAssigneesAreSetByIDAndClearedWithZero(t *testing.T) {
	var sent []recorded
	srv := mergeServer(t, &sent, map[string]string{
		"/api/v4/projects/42/members/all": `[{"id":3,"username":"jane","name":"Jane","state":"active"}]`,
		"/api/v4/users?username=jane":     `[{"id":3,"username":"jane"}]`,
	}, nil)
	c := New(srv.URL, "t")
	users, err := c.AssigneeCandidates(context.Background(), mergeMR)
	if err != nil || len(users) != 1 || users[0].Username != "jane" {
		t.Fatalf("candidates = %+v, %v", users, err)
	}
	if err := c.SetAssignees(context.Background(), mergeMR, []string{"jane"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAssignees(context.Background(), mergeMR, nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || fmt.Sprint(sent[0].body["assignee_ids"]) != "[3]" || fmt.Sprint(sent[1].body["assignee_ids"]) != "[0]" {
		t.Errorf("sent = %+v", sent)
	}
}
