package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// Merge merges the pull request now, or turns on auto-merge, which waits for
// the required checks. Auto-merge exists only in the GraphQL API and only
// where the repository allows it; GitHub says so when it does not.
func (c *Client) Merge(ctx context.Context, mr forge.MergeRequest, opts forge.MergeOptions) error {
	method := "merge"
	if opts.Squash {
		method = "squash"
	}
	if opts.WhenPipelineSucceeds {
		pr, err := c.pull(ctx, mr)
		if err != nil {
			return err
		}
		input := map[string]any{"pullRequestId": pr.NodeID, "mergeMethod": strings.ToUpper(method)}
		if opts.SHA != "" {
			input["expectedHeadOid"] = opts.SHA
		}
		return c.graphql(ctx, `mutation($input: EnablePullRequestAutoMergeInput!) {
			enablePullRequestAutoMerge(input: $input) { clientMutationId } }`, map[string]any{"input": input})
	}

	payload := map[string]any{"merge_method": method}
	if opts.SHA != "" {
		payload["sha"] = opts.SHA
	}
	err := c.send(ctx, http.MethodPut, c.pullPath(mr)+"/merge", payload, nil)
	var refused *apiError
	if errors.As(err, &refused) {
		switch refused.status {
		case http.StatusConflict:
			return forge.ErrHeadMoved
		case http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
			return fmt.Errorf("GitHub will not merge #%d: %s", mr.IID, ghMessage(refused.body))
		}
	}
	if err != nil || !opts.RemoveSourceBranch {
		return err
	}
	return c.deleteHead(ctx, mr)
}

// deleteHead deletes a merged pull request's branch - GitHub has no option
// for it on the merge itself. A branch in a fork is left alone, and one the
// repository's own setting already deleted is not an error.
func (c *Client) deleteHead(ctx context.Context, mr forge.MergeRequest) error {
	pr, err := c.pull(ctx, mr)
	if err != nil {
		return err
	}
	if pr.Head.Repo == nil || pr.Head.Repo.FullName != mr.ProjectPath || pr.Head.Ref == "" {
		return nil
	}
	err = c.send(ctx, http.MethodDelete, "/repos/"+mr.ProjectPath+"/git/refs/heads/"+pr.Head.Ref, nil, nil)
	var refused *apiError
	if errors.As(err, &refused) && refused.status == http.StatusUnprocessableEntity {
		return nil
	}
	if err != nil {
		return fmt.Errorf("merged, but the branch stays: %w", err)
	}
	return nil
}

// SetDraft converts a pull request to a draft or marks it ready, which the
// REST API cannot do.
func (c *Client) SetDraft(ctx context.Context, mr forge.MergeRequest, draft bool) error {
	pr, err := c.pull(ctx, mr)
	if err != nil {
		return err
	}
	if pr.Draft == draft {
		return nil
	}
	mutation := `mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { clientMutationId } }`
	if draft {
		mutation = `mutation($id: ID!) { convertPullRequestToDraft(input: {pullRequestId: $id}) { clientMutationId } }`
	}
	return c.graphql(ctx, mutation, map[string]any{"id": pr.NodeID})
}

// CloseMergeRequest closes the pull request without merging it.
func (c *Client) CloseMergeRequest(ctx context.Context, mr forge.MergeRequest) error {
	return c.send(ctx, http.MethodPatch, c.pullPath(mr), map[string]string{"state": "closed"}, nil)
}

// ReviewerCandidates lists who can be assigned in the repository - those
// with push access, which is who GitHub lets review. The author is left out:
// GitHub refuses to ask them.
func (c *Client) ReviewerCandidates(ctx context.Context, mr forge.MergeRequest) ([]forge.User, error) {
	users, err := getAll[user](ctx, c, "/repos/"+mr.ProjectPath+"/assignees", nil)
	if err != nil {
		return nil, err
	}
	out := make([]forge.User, 0, len(users))
	for _, u := range users {
		if u.Login != mr.Author.Username {
			out = append(out, forge.User{Username: u.Login, Name: u.Name})
		}
	}
	return out, nil
}

// SetReviewers asks the reviewers that are new and withdraws the request
// from those no longer wanted. GitHub forgets a reviewer once they have
// reviewed, so they are asked again only if wanted again.
func (c *Client) SetReviewers(ctx context.Context, mr forge.MergeRequest, usernames []string) error {
	pr, err := c.pull(ctx, mr)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, u := range usernames {
		want[u] = true
	}
	var add, remove []string
	asked := map[string]bool{}
	for _, u := range pr.RequestedReviewers {
		asked[u.Login] = true
		if !want[u.Login] {
			remove = append(remove, u.Login)
		}
	}
	for _, u := range usernames {
		if !asked[u] {
			add = append(add, u)
		}
	}
	if len(remove) > 0 {
		if err := c.send(ctx, http.MethodDelete, c.pullPath(mr)+"/requested_reviewers", map[string]any{"reviewers": remove}, nil); err != nil {
			return err
		}
	}
	if len(add) > 0 {
		return c.post(ctx, c.pullPath(mr)+"/requested_reviewers", map[string]any{"reviewers": add})
	}
	return nil
}

// pull reads the pull request itself.
func (c *Client) pull(ctx context.Context, mr forge.MergeRequest) (*pull, error) {
	var pr pull
	if _, err := c.get(ctx, c.pullPath(mr), nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// graphql runs a mutation. GitHub answers 200 with the failure in errors.
func (c *Client) graphql(ctx context.Context, query string, variables map[string]any) error {
	var answer struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.postDecode(ctx, "/graphql", map[string]any{"query": query, "variables": variables}, &answer); err != nil {
		return err
	}
	if len(answer.Errors) > 0 {
		var msgs []string
		for _, e := range answer.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("GitHub refused: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// ghMessage is the message of a GitHub error body, or the body itself.
func ghMessage(body string) string {
	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Message != "" {
		return parsed.Message
	}
	if s := strings.TrimSpace(body); s != "" {
		return s
	}
	return "no reason given"
}

// LabelChoices lists the repository's labels.
func (c *Client) LabelChoices(ctx context.Context, mr forge.MergeRequest) ([]forge.Label, error) {
	raw, err := getAll[label](ctx, c, "/repos/"+mr.ProjectPath+"/labels", nil)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Label, len(raw))
	for i, l := range raw {
		out[i] = l.label()
	}
	return out, nil
}

// SetLabels replaces the pull request's labels; GitHub keeps them on the
// issue every pull request also is.
func (c *Client) SetLabels(ctx context.Context, mr forge.MergeRequest, names []string) error {
	if names == nil {
		names = []string{}
	}
	return c.send(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/issues/%d/labels", mr.ProjectPath, mr.IID), map[string]any{"labels": names}, nil)
}
