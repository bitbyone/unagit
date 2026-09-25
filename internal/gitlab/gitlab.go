// Package gitlab talks to a GitLab instance and answers in forge's vocabulary.
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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// Client is a GitLab REST v4 client. The token stays in memory only.
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

// Kind identifies the forge.
func (c *Client) Kind() string { return forge.KindGitLab }

// HeadRef is where GitLab publishes a merge request head.
func (c *Client) HeadRef(iid int) string {
	return fmt.Sprintf("refs/merge-requests/%d/head", iid)
}

// GitUser is the user name git authenticates with; the token is the password.
func (c *Client) GitUser() string { return "oauth2" }

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

// get performs a single GET and decodes the JSON body into out. It returns the
// response headers, which carry GitLab's pagination totals.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	u := c.baseURL + "/api/v4" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, &apiError{status: resp.StatusCode, body: string(body), path: path}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return resp.Header, nil
}

// post sends a JSON body and discards the answer, which is only ever an echo
// of what was just created.
func (c *Client) post(ctx context.Context, path string, payload any) error {
	return c.postDecode(ctx, path, payload, nil)
}

// postDecode is post for the callers that need what was created - its id.
func (c *Client) postDecode(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v4"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return &apiError{status: resp.StatusCode, body: string(answer), path: path}
	}
	if out != nil {
		if err := json.Unmarshal(answer, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

// Approve approves the merge request as the token's owner.
func (c *Client) Approve(ctx context.Context, mr forge.MergeRequest) error {
	return c.post(ctx, mrPath(mr)+"/approve", struct{}{})
}

// Comment posts a comment on the merge request.
func (c *Client) Comment(ctx context.Context, mr forge.MergeRequest, body string) error {
	return c.post(ctx, mrPath(mr)+"/notes", map[string]string{"body": body})
}

// CommentNote posts a comment on the merge request and returns it.
func (c *Client) CommentNote(ctx context.Context, mr forge.MergeRequest, body string) (*forge.Note, error) {
	var created note
	if err := c.postDecode(ctx, mrPath(mr)+"/notes", map[string]string{"body": body}, &created); err != nil {
		return nil, err
	}
	return convertNote(mr, "", created), nil
}

// CreateDiscussion starts a diff thread on line of path. GitLab refuses a
// position that is not part of the merge request's diff with a 400; the
// discussion is then started without a position, on the conversation, with a
// "path:line" reference opening the body. That fallback comes back without a
// Path and Line.
func (c *Client) CreateDiscussion(ctx context.Context, mr forge.MergeRequest, path string, line int, body string) (*forge.Note, error) {
	var refs struct {
		DiffRefs struct {
			BaseSHA  string `json:"base_sha"`
			StartSHA string `json:"start_sha"`
			HeadSHA  string `json:"head_sha"`
		} `json:"diff_refs"`
	}
	if _, err := c.get(ctx, mrPath(mr), nil, &refs); err != nil {
		return nil, err
	}
	position := map[string]any{
		"position_type": "text",
		"base_sha":      refs.DiffRefs.BaseSHA,
		"start_sha":     refs.DiffRefs.StartSHA,
		"head_sha":      refs.DiffRefs.HeadSHA,
		"old_path":      path,
		"new_path":      path,
		"new_line":      line,
	}
	var created discussion
	err := c.postDecode(ctx, mrPath(mr)+"/discussions", map[string]any{"body": body, "position": position}, &created)
	var refused *apiError
	if errors.As(err, &refused) && refused.status == http.StatusBadRequest {
		fallback := fmt.Sprintf("`%s:%d`\n\n%s", path, line, body)
		created = discussion{}
		if err := c.postDecode(ctx, mrPath(mr)+"/discussions", map[string]any{"body": fallback}, &created); err != nil {
			return nil, err
		}
		return firstNote(mr, created)
	}
	if err != nil {
		return nil, err
	}
	return firstNote(mr, created)
}

// ReplyToDiscussion adds a note to an existing discussion. thread is the
// discussion id MergeRequestNotes reports.
func (c *Client) ReplyToDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, body string) (*forge.Note, error) {
	var created note
	if err := c.postDecode(ctx, mrPath(mr)+"/discussions/"+url.PathEscape(thread)+"/notes", map[string]string{"body": body}, &created); err != nil {
		return nil, err
	}
	return convertNote(mr, thread, created), nil
}

// firstNote is the note a new discussion was started with.
func firstNote(mr forge.MergeRequest, d discussion) (*forge.Note, error) {
	if len(d.Notes) == 0 {
		return nil, fmt.Errorf("GitLab created the discussion %q without a note", d.ID)
	}
	return convertNote(mr, d.ID, d.Notes[0]), nil
}

// convertNote turns GitLab's note into forge's, with the link GitLab does not
// send: the merge request page anchored on the note.
func convertNote(mr forge.MergeRequest, thread string, n note) *forge.Note {
	converted := forge.Note{
		ID: n.ID, Thread: thread, Body: n.Body, CreatedAt: n.CreatedAt,
		System: n.System, Resolvable: n.Resolvable, Resolved: n.Resolved,
		Author: forge.User{Username: n.Author.Username, Name: n.Author.Name},
	}
	if mr.WebURL != "" {
		converted.URL = fmt.Sprintf("%s#note_%d", mr.WebURL, n.ID)
	}
	if n.Position != nil {
		converted.Path, converted.Line = n.Position.NewPath, n.Position.NewLine
		if converted.Line == 0 && n.Position.OldLine > 0 {
			converted.Line = n.Position.OldLine
			converted.Orphaned = true
			if converted.Path == "" {
				converted.Path = n.Position.OldPath
			}
		}
	}
	return &converted
}

// total reads GitLab's x-total header, which is absent on very large
// collections; -1 then means "unknown".
func total(h http.Header) int {
	n, err := strconv.Atoi(h.Get("x-total"))
	if err != nil {
		return -1
	}
	return n
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
		header, err := c.get(ctx, path, q, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		page = header.Get("x-next-page")
	}
	return all, nil
}

// CurrentUser verifies the token and returns the authenticated user.
func (c *Client) CurrentUser(ctx context.Context) (*forge.User, error) {
	var u struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	if _, err := c.get(ctx, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &forge.User{Username: u.Username, Name: u.Name}, nil
}

// Groups returns every group and subgroup the user can see.
func (c *Client) Groups(ctx context.Context) ([]forge.Group, error) {
	q := url.Values{}
	q.Set("all_available", "false")
	q.Set("order_by", "path")
	q.Set("sort", "asc")
	return getAll[forge.Group](ctx, c, "/groups", q)
}

// GroupProjects returns the projects of a group, optionally descending into
// its subgroups.
func (c *Client) GroupProjects(ctx context.Context, g forge.Group, includeSubgroups bool) ([]forge.Project, error) {
	q := url.Values{}
	q.Set("include_subgroups", strconv.FormatBool(includeSubgroups))
	q.Set("archived", "false")
	q.Set("order_by", "last_activity_at")
	q.Set("with_shared", "false")
	return getAll[forge.Project](ctx, c, groupPath(g)+"/projects", q)
}

// mergeRequest is the list shape; the fields unagit needs line up with
// forge.MergeRequest except for the reference, which is only used to recover
// the project path.
type mergeRequest struct {
	forge.MergeRequest
	References struct {
		Full string `json:"full"`
	} `json:"references"`
	// GitLab spells the comment count differently on the listing than the
	// shared field does.
	UserNotesCount int `json:"user_notes_count"`
}

// GroupMergeRequests returns open merge requests targeting projects in a group.
func (c *Client) GroupMergeRequests(ctx context.Context, g forge.Group, includeSubgroups bool) ([]forge.MergeRequest, error) {
	q := url.Values{}
	q.Set("state", "opened")
	q.Set("include_subgroups", "true")
	q.Set("order_by", "updated_at")
	q.Set("scope", "all")
	raw, err := getAll[mergeRequest](ctx, c, groupPath(g)+"/merge_requests", q)
	if err != nil {
		return nil, err
	}
	out := make([]forge.MergeRequest, 0, len(raw))
	for _, mr := range raw {
		m := mr.MergeRequest
		m.Comments = mr.UserNotesCount
		if m.ProjectPath == "" {
			if i := strings.Index(mr.References.Full, "!"); i > 0 {
				m.ProjectPath = mr.References.Full[:i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func groupPath(g forge.Group) string { return "/groups/" + strconv.Itoa(g.ID) }
func projectPath(p forge.Project) string {
	return "/projects/" + strconv.Itoa(p.ID)
}
func mrPath(mr forge.MergeRequest) string {
	return "/projects/" + strconv.Itoa(mr.ProjectID) + "/merge_requests/" + strconv.Itoa(mr.IID)
}

// ProjectDetail returns the full project payload including statistics.
func (c *Client) ProjectDetail(ctx context.Context, p forge.Project) (*forge.ProjectDetail, error) {
	q := url.Values{}
	q.Set("statistics", "true")
	q.Set("license", "true")
	var raw struct {
		forge.Project
		Visibility      string   `json:"visibility"`
		CreatedAt       string   `json:"created_at"`
		StarCount       int      `json:"star_count"`
		ForksCount      int      `json:"forks_count"`
		OpenIssuesCount int      `json:"open_issues_count"`
		Topics          []string `json:"topics"`
		EmptyRepo       bool     `json:"empty_repo"`
		MergeMethod     string   `json:"merge_method"`
		ForkedFrom      *struct {
			PathWithNamespace string `json:"path_with_namespace"`
		} `json:"forked_from_project"`
		License *struct {
			Name string `json:"name"`
		} `json:"license"`
		Statistics *struct {
			CommitCount    int64 `json:"commit_count"`
			RepositorySize int64 `json:"repository_size"`
		} `json:"statistics"`
	}
	if _, err := c.get(ctx, projectPath(p), q, &raw); err != nil {
		return nil, err
	}
	d := &forge.ProjectDetail{
		Project:         raw.Project,
		Visibility:      raw.Visibility,
		StarCount:       raw.StarCount,
		ForksCount:      raw.ForksCount,
		OpenIssuesCount: raw.OpenIssuesCount,
		Topics:          raw.Topics,
		EmptyRepo:       raw.EmptyRepo,
		MergeMethod:     raw.MergeMethod,
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, raw.CreatedAt)
	if raw.ForkedFrom != nil {
		d.ForkedFrom = raw.ForkedFrom.PathWithNamespace
	}
	if raw.License != nil {
		d.License = raw.License.Name
	}
	if raw.Statistics != nil {
		d.CommitCount = raw.Statistics.CommitCount
		d.RepositorySize = raw.Statistics.RepositorySize
	}
	return d, nil
}

// ProjectCommits returns the newest commits of a ref.
func (c *Client) ProjectCommits(ctx context.Context, p forge.Project, ref string, limit int) ([]forge.Commit, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	if ref != "" {
		q.Set("ref_name", ref)
	}
	var commits []forge.Commit
	if _, err := c.get(ctx, projectPath(p)+"/repository/commits", q, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

// ProjectLanguages returns the language breakdown in percent.
func (c *Client) ProjectLanguages(ctx context.Context, p forge.Project) (map[string]float64, error) {
	var langs map[string]float64
	if _, err := c.get(ctx, projectPath(p)+"/languages", nil, &langs); err != nil {
		return nil, err
	}
	return langs, nil
}

// LatestPipeline returns the most recent pipeline of a ref, or nil.
func (c *Client) LatestPipeline(ctx context.Context, p forge.Project, ref string) (*forge.Pipeline, error) {
	q := url.Values{}
	q.Set("per_page", "1")
	if ref != "" {
		q.Set("ref", ref)
	}
	var pipelines []forge.Pipeline
	if _, err := c.get(ctx, projectPath(p)+"/pipelines", q, &pipelines); err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return nil, nil
	}
	return &pipelines[0], nil
}

// branch is GitLab's branch shape.
type branch struct {
	Name      string `json:"name"`
	Default   bool   `json:"default"`
	Protected bool   `json:"protected"`
	Commit    struct {
		ShortID       string    `json:"short_id"`
		Title         string    `json:"title"`
		CommittedDate time.Time `json:"committed_date"`
	} `json:"commit"`
}

// ProjectBranches returns every branch of a project.
func (c *Client) ProjectBranches(ctx context.Context, p forge.Project) ([]forge.Branch, error) {
	raw, err := getAll[branch](ctx, c, projectPath(p)+"/repository/branches", nil)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Branch, 0, len(raw))
	for _, b := range raw {
		out = append(out, forge.Branch{
			Name: b.Name, Default: b.Default, Protected: b.Protected,
			CommitShortID: b.Commit.ShortID, CommitTitle: b.Commit.Title,
			CommittedDate: b.Commit.CommittedDate,
		})
	}
	return out, nil
}

// MergeRequestDetail returns the full merge request payload, including how far
// the source branch has fallen behind the target.
func (c *Client) MergeRequestDetail(ctx context.Context, mr forge.MergeRequest) (*forge.MergeRequestDetail, error) {
	q := url.Values{}
	q.Set("include_diverged_commits_count", "true")
	var raw struct {
		forge.MergeRequest
		Description                 string   `json:"description"`
		CreatedAt                   string   `json:"created_at"`
		Labels                      []string `json:"labels"`
		MergeStatus                 string   `json:"merge_status"`
		DetailedMergeStatus         string   `json:"detailed_merge_status"`
		HasConflicts                bool     `json:"has_conflicts"`
		BlockingDiscussionsResolved bool     `json:"blocking_discussions_resolved"`
		ChangesCount                string   `json:"changes_count"`
		UserNotesCount              int      `json:"user_notes_count"`
		Upvotes                     int      `json:"upvotes"`
		Downvotes                   int      `json:"downvotes"`
		Assignees                   []struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"assignees"`
		Reviewers []struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"reviewers"`
		Milestone *struct {
			Title string `json:"title"`
		} `json:"milestone"`
		HeadPipeline         *forge.Pipeline `json:"head_pipeline"`
		TaskCompletionStatus *struct {
			Count          int `json:"count"`
			CompletedCount int `json:"completed_count"`
		} `json:"task_completion_status"`
		DiffRefs struct {
			BaseSHA  string `json:"base_sha"`
			StartSHA string `json:"start_sha"`
			HeadSHA  string `json:"head_sha"`
		} `json:"diff_refs"`
		DivergedCommitsCount int `json:"diverged_commits_count"`
	}
	if _, err := c.get(ctx, mrPath(mr), q, &raw); err != nil {
		return nil, err
	}
	raw.MergeRequest.Comments = raw.UserNotesCount
	d := &forge.MergeRequestDetail{
		MergeRequest:                raw.MergeRequest,
		Description:                 raw.Description,
		Labels:                      raw.Labels,
		MergeStatus:                 raw.DetailedMergeStatus,
		HasConflicts:                raw.HasConflicts,
		BlockingDiscussionsResolved: raw.BlockingDiscussionsResolved,
		ChangesCount:                raw.ChangesCount,
		UserNotesCount:              raw.UserNotesCount,
		Upvotes:                     raw.Upvotes,
		Downvotes:                   raw.Downvotes,
		Pipeline:                    raw.HeadPipeline,
		DivergedCommitsCount:        raw.DivergedCommitsCount,
	}
	if d.MergeStatus == "" {
		d.MergeStatus = raw.MergeStatus
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339, raw.CreatedAt)
	for _, u := range raw.Assignees {
		d.Assignees = append(d.Assignees, forge.User{Username: u.Username, Name: u.Name})
	}
	for _, u := range raw.Reviewers {
		d.Reviewers = append(d.Reviewers, forge.User{Username: u.Username, Name: u.Name})
	}
	if raw.Milestone != nil {
		d.Milestone = raw.Milestone.Title
	}
	if raw.TaskCompletionStatus != nil {
		d.TasksDone = raw.TaskCompletionStatus.CompletedCount
		d.TasksTotal = raw.TaskCompletionStatus.Count
	}
	d.DiffRefs.BaseSHA = raw.DiffRefs.BaseSHA
	d.DiffRefs.StartSHA = raw.DiffRefs.StartSHA
	d.DiffRefs.HeadSHA = raw.DiffRefs.HeadSHA
	return d, nil
}

// note is GitLab's comment shape.
type note struct {
	ID         int       `json:"id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	System     bool      `json:"system"`
	Resolvable bool      `json:"resolvable"`
	Resolved   bool      `json:"resolved"`
	Author     struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"author"`
	Position *struct {
		OldPath string `json:"old_path"`
		OldLine int    `json:"old_line"`
		NewPath string `json:"new_path"`
		NewLine int    `json:"new_line"`
	} `json:"position"`
}

// discussion is a conversation: one comment, or a thread of them.
type discussion struct {
	ID    string `json:"id"`
	Notes []note `json:"notes"`
}

// MergeRequestNotes returns the newest comments, most recent first. They come
// from the discussions endpoint rather than the flat one, because that is
// where GitLab says which comments are replies to which.
func (c *Client) MergeRequestNotes(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Note, error) {
	discussions, err := getAll[discussion](ctx, c, mrPath(mr)+"/discussions", nil)
	if err != nil {
		return nil, err
	}
	var out []forge.Note
	for _, d := range discussions {
		for _, n := range d.Notes {
			out = append(out, *convertNote(mr, d.ID, n))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MergeRequestCommits returns the newest commits of a merge request together
// with how many there are in total.
func (c *Client) MergeRequestCommits(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Commit, int, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	var commits []forge.Commit
	header, err := c.get(ctx, mrPath(mr)+"/commits", q, &commits)
	if err != nil {
		return nil, 0, err
	}
	return commits, total(header), nil
}

// MergeRequestApprovals returns the approval state. Not every GitLab tier
// exposes it.
func (c *Client) MergeRequestApprovals(ctx context.Context, mr forge.MergeRequest) (*forge.Approvals, error) {
	var raw struct {
		ApprovalsRequired int `json:"approvals_required"`
		ApprovedBy        []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	if _, err := c.get(ctx, mrPath(mr)+"/approvals", nil, &raw); err != nil {
		return nil, err
	}
	a := &forge.Approvals{Required: raw.ApprovalsRequired}
	for _, by := range raw.ApprovedBy {
		a.ApprovedBy = append(a.ApprovedBy, by.User.Username)
	}
	return a, nil
}
