package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// Merge merges the merge request, or sets it to merge when its pipeline
// succeeds. GitLab renamed that option auto_merge in 17.11 and still reads
// the old name, so both are sent and an older server ignores the new one.
func (c *Client) Merge(ctx context.Context, mr forge.MergeRequest, opts forge.MergeOptions) error {
	payload := map[string]any{
		"squash":                       opts.Squash,
		"should_remove_source_branch":  opts.RemoveSourceBranch,
		"merge_when_pipeline_succeeds": opts.WhenPipelineSucceeds,
		"auto_merge":                   opts.WhenPipelineSucceeds,
	}
	if opts.SHA != "" {
		payload["sha"] = opts.SHA
	}
	err := c.send(ctx, http.MethodPut, mrPath(mr)+"/merge", payload, nil)
	var refused *apiError
	if !errors.As(err, &refused) {
		return err
	}
	switch refused.status {
	case http.StatusConflict:
		return forge.ErrHeadMoved
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusUnprocessableEntity:
		// GitLab's reasons: a draft, conflicts, threads to resolve, a pipeline
		// that must pass first. It says which in the message.
		return fmt.Errorf("GitLab will not merge !%d: %s", mr.IID, apiMessage(refused.body))
	}
	return err
}

// SetDraft marks a merge request as a draft or ready. GitLab keeps that in
// the title, so the title is read fresh - the list's copy may be old - and
// written back with the prefix added or taken away.
func (c *Client) SetDraft(ctx context.Context, mr forge.MergeRequest, draft bool) error {
	var current struct {
		Title string `json:"title"`
	}
	if _, err := c.get(ctx, mrPath(mr), nil, &current); err != nil {
		return err
	}
	title := stripDraft(current.Title)
	if draft {
		title = "Draft: " + title
	}
	if title == current.Title {
		return nil
	}
	return c.send(ctx, http.MethodPut, mrPath(mr), map[string]string{"title": title}, nil)
}

// stripDraft takes every draft prefix GitLab recognises off a title.
func stripDraft(title string) string {
	for {
		t := strings.TrimSpace(title)
		lower := strings.ToLower(t)
		cut := false
		for _, prefix := range []string{"draft:", "[draft]", "(draft)", "draft "} {
			if strings.HasPrefix(lower, prefix) {
				title, cut = t[len(prefix):], true
				break
			}
		}
		if !cut {
			return t
		}
	}
}

// CloseMergeRequest closes a merge request without merging it.
func (c *Client) CloseMergeRequest(ctx context.Context, mr forge.MergeRequest) error {
	return c.send(ctx, http.MethodPut, mrPath(mr), map[string]string{"state_event": "close"}, nil)
}

// ReviewerCandidates lists the project's members, inherited ones included.
func (c *Client) ReviewerCandidates(ctx context.Context, mr forge.MergeRequest) ([]forge.User, error) {
	members, err := getAll[member](ctx, c, "/projects/"+projectRef(mr.ProjectID, mr.ProjectPath)+"/members/all", nil)
	if err != nil {
		return nil, err
	}
	out := make([]forge.User, 0, len(members))
	for _, m := range members {
		if m.State != "" && m.State != "active" {
			continue
		}
		out = append(out, forge.User{Username: m.Username, Name: m.Name})
	}
	return out, nil
}

type member struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	State    string `json:"state"`
}

// SetReviewers replaces the reviewers. GitLab takes user ids, so each name is
// looked up first; an empty list clears them.
func (c *Client) SetReviewers(ctx context.Context, mr forge.MergeRequest, usernames []string) error {
	return c.setPeople(ctx, mr, "reviewer_ids", usernames)
}

// AssigneeCandidates are the project's members, as the reviewers' are.
func (c *Client) AssigneeCandidates(ctx context.Context, mr forge.MergeRequest) ([]forge.User, error) {
	return c.ReviewerCandidates(ctx, mr)
}

// SetAssignees replaces the assignees, as SetReviewers the reviewers.
func (c *Client) SetAssignees(ctx context.Context, mr forge.MergeRequest, usernames []string) error {
	return c.setPeople(ctx, mr, "assignee_ids", usernames)
}

// setPeople puts the users named into one of a merge request's lists of
// people by their ids; a lone 0 is how GitLab documents clearing one.
func (c *Client) setPeople(ctx context.Context, mr forge.MergeRequest, field string, usernames []string) error {
	ids := make([]int, 0, len(usernames))
	for _, name := range usernames {
		q := url.Values{}
		q.Set("username", name)
		var found []member
		if _, err := c.get(ctx, "/users", q, &found); err != nil {
			return err
		}
		if len(found) == 0 {
			return fmt.Errorf("GitLab knows no user %s", name)
		}
		ids = append(ids, found[0].ID)
	}
	if len(ids) == 0 {
		ids = []int{0}
	}
	return c.send(ctx, http.MethodPut, mrPath(mr), map[string]any{field: ids}, nil)
}

// apiMessage pulls the reason out of a GitLab error body, which is JSON with
// a message that is a string, or now and then a list or a map of them.
func apiMessage(body string) string {
	var parsed struct {
		Message any `json:"message"`
		Error   any `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil {
		for _, m := range []any{parsed.Message, parsed.Error} {
			switch v := m.(type) {
			case string:
				if v != "" {
					return v
				}
			case []any:
				var parts []string
				for _, p := range v {
					parts = append(parts, fmt.Sprint(p))
				}
				return strings.Join(parts, "; ")
			case map[string]any:
				var parts []string
				for k, p := range v {
					parts = append(parts, k+" "+fmt.Sprint(p))
				}
				return strings.Join(parts, "; ")
			}
		}
	}
	if s := strings.TrimSpace(body); s != "" {
		return s
	}
	return "no reason given"
}

// LabelChoices lists the labels of the merge request's project and of the
// groups above it, which GitLab lets be put on it as well.
func (c *Client) LabelChoices(ctx context.Context, mr forge.MergeRequest) ([]forge.Label, error) {
	q := url.Values{}
	q.Set("include_ancestor_groups", "true")
	return getAll[forge.Label](ctx, c, "/projects/"+projectRef(mr.ProjectID, mr.ProjectPath)+"/labels", q)
}

// SetLabels replaces the labels; GitLab takes them as one comma-separated
// string, and an empty one clears them.
func (c *Client) SetLabels(ctx context.Context, mr forge.MergeRequest, names []string) error {
	return c.send(ctx, http.MethodPut, mrPath(mr), map[string]any{"labels": strings.Join(names, ",")}, nil)
}
