package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	_, err := c.get(ctx, "/repos/"+p.PathWithNamespace+"/branches/"+url.PathEscape(branch), nil, &b)
	var api *apiError
	if errors.As(err, &api) && api.status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

// Fingerprints asks GraphQL about every watch in one query: for a pull
// request its state, head, conversation, approvals and its head's checks,
// for a branch its head and checks - null once the branch is gone. Each
// watch's part of the answer is its fingerprint.
func (c *Client) Fingerprints(ctx context.Context, refs []forge.WatchRef) ([]string, error) {
	out := make([]string, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	var q strings.Builder
	q.WriteString("query {")
	for i, r := range refs {
		owner, name, ok := strings.Cut(r.Project.PathWithNamespace, "/")
		if !ok {
			continue
		}
		o, _ := json.Marshal(owner)
		n, _ := json.Marshal(name)
		if r.IID > 0 {
			fmt.Fprintf(&q, ` w%d: repository(owner: %s, name: %s) { pullRequest(number: %d) { state title isDraft headRefOid `+
				`comments { totalCount } reviewThreads { totalCount } reviews(states: APPROVED) { totalCount } `+
				`assignees(first: 20) { nodes { login } } reviewRequests(first: 20) { nodes { requestedReviewer { ... on User { login } } } } `+
				`labels(first: 30) { nodes { name } } `+
				`commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } } }`, i, o, n, r.IID)
			continue
		}
		ref, _ := json.Marshal("refs/heads/" + r.Branch)
		fmt.Fprintf(&q, ` w%d: repository(owner: %s, name: %s) { ref(qualifiedName: %s) { target { oid `+
			`... on Commit { statusCheckRollup { state } } } } }`, i, o, n, ref)
	}
	q.WriteString(" }")
	var answer struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.postDecode(ctx, "/graphql", map[string]any{"query": q.String()}, &answer); err != nil {
		return nil, err
	}
	if answer.Data == nil && len(answer.Errors) > 0 {
		return nil, fmt.Errorf("GitHub GraphQL: %s", answer.Errors[0].Message)
	}
	for i := range refs {
		if raw := answer.Data["w"+strconv.Itoa(i)]; len(raw) > 0 && string(raw) != "null" {
			out[i] = string(raw)
		}
	}
	return out, nil
}
