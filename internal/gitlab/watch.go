package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// PausedUntil is when the server may be asked again in the background.
func (c *Client) PausedUntil() time.Time { return c.pace.PausedUntil() }

// BranchExists asks for the branch itself; 404 is no.
func (c *Client) BranchExists(ctx context.Context, p forge.Project, branch string) (bool, error) {
	var b struct {
		Name string `json:"name"`
	}
	_, err := c.get(ctx, projectPath(p)+"/repository/branches/"+url.PathEscape(branch), nil, &b)
	var api *apiError
	if errors.As(err, &api) && api.status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

// BranchProtected reports whether the server protects a branch.
func (c *Client) BranchProtected(ctx context.Context, p forge.Project, branch string) (bool, error) {
	var b struct {
		Protected bool `json:"protected"`
	}
	_, err := c.get(ctx, projectPath(p)+"/repository/branches/"+url.PathEscape(branch), nil, &b)
	var api *apiError
	if errors.As(err, &api) && api.status == http.StatusNotFound {
		return false, nil
	}
	return b.Protected, err
}

// Fingerprints asks GraphQL about every watch in one query: for a merge
// request its state, title, draft, head, comments, approvals, assignees,
// reviewers, labels and head pipeline, for a
// branch its newest pipeline. Each watch's part of the answer, as it came,
// is its fingerprint.
func (c *Client) Fingerprints(ctx context.Context, refs []forge.WatchRef) ([]string, error) {
	out := make([]string, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	var q strings.Builder
	q.WriteString("query {")
	for i, r := range refs {
		path, _ := json.Marshal(r.Project.PathWithNamespace)
		if r.IID > 0 {
			fmt.Fprintf(&q, ` w%d: project(fullPath: %s) { mergeRequest(iid: "%d") { state title draft diffHeadSha userNotesCount `+
				`approvedBy { nodes { username } } assignees { nodes { username } } reviewers { nodes { username } } `+
				`labels { nodes { title } } headPipeline { id status } } }`, i, path, r.IID)
			continue
		}
		branch, _ := json.Marshal(r.Branch)
		fmt.Fprintf(&q, ` w%d: project(fullPath: %s) { pipelines(ref: %s, first: 1) { nodes { id status sha } } }`, i, path, branch)
	}
	q.WriteString(" }")
	var answer struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.graphql(ctx, q.String(), &answer); err != nil {
		return nil, err
	}
	if answer.Data == nil && len(answer.Errors) > 0 {
		return nil, fmt.Errorf("GitLab GraphQL: %s", answer.Errors[0].Message)
	}
	for i := range refs {
		if raw := answer.Data["w"+strconv.Itoa(i)]; len(raw) > 0 && string(raw) != "null" {
			out[i] = string(raw)
		}
	}
	return out, nil
}

// graphql posts a query to GitLab's GraphQL endpoint, beside /api/v4.
func (c *Client) graphql(ctx context.Context, query string, out any) error {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &apiError{status: resp.StatusCode, body: string(answer), path: "/api/graphql"}
	}
	return json.Unmarshal(answer, out)
}
