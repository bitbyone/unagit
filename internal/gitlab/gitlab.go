// Package gitlab is a small REST v4 client covering the endpoints unagit needs.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Group is a GitLab group or subgroup.
type Group struct {
	ID       int    `json:"id"`
	ParentID int    `json:"parent_id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	FullPath string `json:"full_path"`
	FullName string `json:"full_name"`
	WebURL   string `json:"web_url"`
}

// Project is a GitLab project.
type Project struct {
	ID                int       `json:"id"`
	Name              string    `json:"name"`
	PathWithNamespace string    `json:"path_with_namespace"`
	Description       string    `json:"description"`
	DefaultBranch     string    `json:"default_branch"`
	HTTPURLToRepo     string    `json:"http_url_to_repo"`
	SSHURLToRepo      string    `json:"ssh_url_to_repo"`
	WebURL            string    `json:"web_url"`
	Archived          bool      `json:"archived"`
	LastActivityAt    time.Time `json:"last_activity_at"`
	GroupID           int       `json:"-"`
}

// MergeRequest is an open merge request.
type MergeRequest struct {
	ID              int       `json:"id"`
	IID             int       `json:"iid"`
	Title           string    `json:"title"`
	State           string    `json:"state"`
	Draft           bool      `json:"draft"`
	SourceBranch    string    `json:"source_branch"`
	TargetBranch    string    `json:"target_branch"`
	WebURL          string    `json:"web_url"`
	ProjectID       int       `json:"project_id"`
	SourceProjectID int       `json:"source_project_id"`
	TargetProjectID int       `json:"target_project_id"`
	UpdatedAt       time.Time `json:"updated_at"`
	Author          struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"author"`
	References struct {
		Full string `json:"full"`
	} `json:"references"`
	// ProjectPath is filled in by unagit from the project index.
	ProjectPath string `json:"project_path,omitempty"`
}

// Branch is a repository branch.
type Branch struct {
	Name      string `json:"name"`
	Default   bool   `json:"default"`
	Merged    bool   `json:"merged"`
	Protected bool   `json:"protected"`
	Commit    struct {
		ShortID       string    `json:"short_id"`
		Title         string    `json:"title"`
		CommittedDate time.Time `json:"committed_date"`
		AuthorName    string    `json:"author_name"`
	} `json:"commit"`
}

// User is the authenticated user.
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Client talks to a GitLab instance. The token stays in memory only.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for baseURL authenticated with a personal access token.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

type apiError struct {
	status int
	body   string
	path   string
}

func (e *apiError) Error() string {
	msg := strings.TrimSpace(e.body)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	switch e.status {
	case http.StatusUnauthorized:
		return "GitLab rejected the token (401) - check that it is valid and has api scope"
	case http.StatusForbidden:
		return fmt.Sprintf("GitLab denied access to %s (403): %s", e.path, msg)
	}
	return fmt.Sprintf("GitLab %s returned %d: %s", e.path, e.status, msg)
}

// get performs a single GET and decodes the JSON body into out.
// It returns the value of the x-next-page header.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (string, error) {
	u := c.baseURL + "/api/v4" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", &apiError{status: resp.StatusCode, body: string(body), path: path}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return "", fmt.Errorf("decode %s: %w", path, err)
	}
	return resp.Header.Get("x-next-page"), nil
}

// getAll pages through a collection endpoint until GitLab stops handing out
// a next page.
func getAll[T any](ctx context.Context, c *Client, path string, q url.Values) ([]T, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("per_page", "100")
	var all []T
	page := "1"
	for page != "" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		q.Set("page", page)
		var batch []T
		next, err := c.get(ctx, path, q, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		page = next
	}
	return all, nil
}

// CurrentUser verifies the token and returns the authenticated user.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	var u User
	if _, err := c.get(ctx, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Groups returns every group and subgroup the user can see.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	q := url.Values{}
	q.Set("all_available", "false")
	q.Set("order_by", "path")
	q.Set("sort", "asc")
	return getAll[Group](ctx, c, "/groups", q)
}

// GroupProjects returns the projects of a group, optionally descending into
// its subgroups.
func (c *Client) GroupProjects(ctx context.Context, groupID int, includeSubgroups bool) ([]Project, error) {
	q := url.Values{}
	q.Set("include_subgroups", strconv.FormatBool(includeSubgroups))
	q.Set("archived", "false")
	q.Set("order_by", "last_activity_at")
	q.Set("with_shared", "false")
	return getAll[Project](ctx, c, "/groups/"+strconv.Itoa(groupID)+"/projects", q)
}

// GroupMergeRequests returns open merge requests targeting projects in a group.
func (c *Client) GroupMergeRequests(ctx context.Context, groupID int) ([]MergeRequest, error) {
	q := url.Values{}
	q.Set("state", "opened")
	q.Set("include_subgroups", "true")
	q.Set("order_by", "updated_at")
	q.Set("scope", "all")
	return getAll[MergeRequest](ctx, c, "/groups/"+strconv.Itoa(groupID)+"/merge_requests", q)
}

// ProjectBranches returns every branch of a project.
func (c *Client) ProjectBranches(ctx context.Context, projectID int) ([]Branch, error) {
	return getAll[Branch](ctx, c, "/projects/"+strconv.Itoa(projectID)+"/repository/branches", nil)
}
