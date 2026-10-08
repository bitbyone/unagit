package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

var mergePR = forge.MergeRequest{IID: 7, ProjectPath: "acme/api", Author: forge.User{Username: "toby"}}

const mergePRJSON = `{"number":7,"node_id":"PR_node7","draft":false,
	"head":{"ref":"feat/x","sha":"abc","repo":{"full_name":"acme/api"}},
	"requested_reviewers":[{"login":"jane"},{"login":"old"}]}`

func TestMergeNowAndDeleteTheBranch(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", mergePRJSON)
	var merges, deletes []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7/merge", capture(&merges, func(w http.ResponseWriter) {
		fmt.Fprint(w, `{"merged":true}`)
	}))
	s.mux.HandleFunc("/repos/acme/api/git/refs/heads/feat/x", capture(&deletes, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNoContent)
	}))
	err := s.client().Merge(context.Background(), mergePR, forge.MergeOptions{Squash: true, RemoveSourceBranch: true, SHA: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(merges) != 1 || merges[0]["_method"] != http.MethodPut || merges[0]["merge_method"] != "squash" || merges[0]["sha"] != "abc" {
		t.Errorf("merge = %+v", merges)
	}
	if len(deletes) != 1 || deletes[0]["_method"] != http.MethodDelete {
		t.Errorf("the branch was not deleted: %+v", deletes)
	}
}

func TestMergeLeavesAForksBranchAlone(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"number":7,"head":{"ref":"feat/x","repo":{"full_name":"someone/api"}}}`)
	s.handle("/repos/acme/api/pulls/7/merge", `{"merged":true}`)
	s.mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	})
	if err := s.client().Merge(context.Background(), mergePR, forge.MergeOptions{RemoveSourceBranch: true}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeSaysWhyGitHubRefused(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusConflict:         "",
		http.StatusMethodNotAllowed: "GitHub will not merge #7: Pull Request is not mergeable",
	} {
		s := newStub(t)
		s.mux.HandleFunc("/repos/acme/api/pulls/7/merge", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"message":"Pull Request is not mergeable"}`)
		})
		err := s.client().Merge(context.Background(), mergePR, forge.MergeOptions{SHA: "abc"})
		if want == "" {
			if !errors.Is(err, forge.ErrHeadMoved) {
				t.Errorf("%d: err = %v, want ErrHeadMoved", status, err)
			}
			continue
		}
		if err == nil || err.Error() != want {
			t.Errorf("%d: err = %v, want %q", status, err, want)
		}
	}
}

func TestAutoMergeAndDraftGoThroughGraphQL(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", mergePRJSON)
	var calls []map[string]any
	s.mux.HandleFunc("/graphql", capture(&calls, func(w http.ResponseWriter) {
		fmt.Fprint(w, `{"data":{}}`)
	}))
	c := s.client()
	if err := c.Merge(context.Background(), mergePR, forge.MergeOptions{WhenPipelineSucceeds: true, SHA: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetDraft(context.Background(), mergePR, true); err != nil {
		t.Fatal(err)
	}
	// Already ready: nothing to do.
	if err := c.SetDraft(context.Background(), mergePR, false); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("graphql calls = %+v", calls)
	}
	auto := calls[0]
	if q, _ := auto["query"].(string); !strings.Contains(q, "enablePullRequestAutoMerge") {
		t.Errorf("auto-merge query = %v", auto["query"])
	}
	input := auto["variables"].(map[string]any)["input"].(map[string]any)
	if input["pullRequestId"] != "PR_node7" || input["mergeMethod"] != "MERGE" || input["expectedHeadOid"] != "abc" {
		t.Errorf("auto-merge input = %+v", input)
	}
	if q, _ := calls[1]["query"].(string); !strings.Contains(q, "convertPullRequestToDraft") {
		t.Errorf("draft query = %v", calls[1]["query"])
	}
	if id := calls[1]["variables"].(map[string]any)["id"]; id != "PR_node7" {
		t.Errorf("draft id = %v", id)
	}
}

func TestGraphQLErrorsAreErrors(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"number":7,"node_id":"PR_node7","draft":true}`)
	s.handle("/graphql", `{"errors":[{"message":"Pull request Auto merge is not allowed for this repository"}]}`)
	err := s.client().SetDraft(context.Background(), mergePR, false)
	if err == nil || !strings.Contains(err.Error(), "Auto merge is not allowed") {
		t.Errorf("err = %v", err)
	}
}

func TestClosePatchesTheState(t *testing.T) {
	s := newStub(t)
	var sent []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7", capture(&sent, func(w http.ResponseWriter) { fmt.Fprint(w, `{}`) }))
	if err := s.client().CloseMergeRequest(context.Background(), mergePR); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0]["_method"] != http.MethodPatch || sent[0]["state"] != "closed" {
		t.Errorf("sent = %+v", sent)
	}
}

func TestReviewersAddAndWithdrawOnlyTheDifference(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/assignees", `[{"login":"toby"},{"login":"jane"},{"login":"new"}]`)
	var sent []map[string]any
	s.mux.HandleFunc("/repos/acme/api/pulls/7", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, mergePRJSON) })
	s.mux.HandleFunc("/repos/acme/api/pulls/7/requested_reviewers", capture(&sent, func(w http.ResponseWriter) { fmt.Fprint(w, `{}`) }))
	c := s.client()

	users, err := c.ReviewerCandidates(context.Background(), mergePR)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(users) != "[{jane } {new }]" {
		t.Errorf("candidates = %v, want the author left out", users)
	}

	if err := c.SetReviewers(context.Background(), mergePR, []string{"jane", "new"}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatalf("sent = %+v", sent)
	}
	if sent[0]["_method"] != http.MethodDelete || fmt.Sprint(sent[0]["reviewers"]) != "[old]" {
		t.Errorf("withdrawn = %+v", sent[0])
	}
	if sent[1]["_method"] != http.MethodPost || fmt.Sprint(sent[1]["reviewers"]) != "[new]" {
		t.Errorf("asked = %+v", sent[1])
	}
}

// TestLabelsComeWithTheirColourAndAreSetOnTheIssue: a pull request's labels
// carry their colour as "#rrggbb", the choices are the repository's, and
// setting them replaces those of the issue the pull request also is - an
// empty list included.
func TestLabelsComeWithTheirColourAndAreSetOnTheIssue(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/labels", `[{"name":"bug","color":"d73a4a","description":"Something broke"},{"name":"docs","color":"0075ca"}]`)
	var sent []map[string]any
	s.mux.HandleFunc("/repos/acme/api/issues/7/labels", capture(&sent, func(w http.ResponseWriter) { fmt.Fprint(w, `[]`) }))
	c := s.client()

	labels, err := c.LabelChoices(context.Background(), mergePR)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 || labels[0] != (forge.Label{Name: "bug", Color: "#d73a4a", Description: "Something broke"}) {
		t.Errorf("choices = %+v", labels)
	}
	var p pull
	if err := json.Unmarshal([]byte(`{"number":7,"labels":[{"name":"bug","color":"d73a4a"}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if got := p.mergeRequest("acme/api").Labels; len(got) != 1 || got[0].Color != "#d73a4a" {
		t.Errorf("a pull request's labels = %+v", got)
	}

	if err := c.SetLabels(context.Background(), mergePR, []string{"bug"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetLabels(context.Background(), mergePR, nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0]["_method"] != http.MethodPut || fmt.Sprint(sent[0]["labels"]) != "[bug]" || fmt.Sprint(sent[1]["labels"]) != "[]" {
		t.Errorf("sent = %+v", sent)
	}
}

// TestAssigneesTakeTheAuthorAndReplaceTheIssues: an author may be assigned
// their own pull request, and the list replaces the issue's, an empty one
// clearing it.
func TestAssigneesTakeTheAuthorAndReplaceTheIssues(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/assignees", `[{"login":"toby"},{"login":"jane"}]`)
	var sent []map[string]any
	s.mux.HandleFunc("/repos/acme/api/issues/7", capture(&sent, func(w http.ResponseWriter) { fmt.Fprint(w, `{}`) }))
	c := s.client()
	users, err := c.AssigneeCandidates(context.Background(), mergePR)
	if err != nil || fmt.Sprint(users) != "[{toby } {jane }]" {
		t.Fatalf("candidates = %v, %v - the author belongs among them", users, err)
	}
	if err := c.SetAssignees(context.Background(), mergePR, []string{"toby"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAssignees(context.Background(), mergePR, nil); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0]["_method"] != http.MethodPatch || fmt.Sprint(sent[0]["assignees"]) != "[toby]" || fmt.Sprint(sent[1]["assignees"]) != "[]" {
		t.Errorf("sent = %+v", sent)
	}
}
