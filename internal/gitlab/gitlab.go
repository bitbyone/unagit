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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// CommitURL is a commit's page on the server.
func (c *Client) CommitURL(p forge.Project, sha string) string {
	if p.WebURL == "" {
		return ""
	}
	return strings.TrimRight(p.WebURL, "/") + "/-/commit/" + sha
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
	return c.send(ctx, http.MethodPost, path, payload, out)
}

// send performs a request with a JSON body and decodes the answer into out,
// when there is one to decode into.
func (c *Client) send(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v4"+path, bytes.NewReader(body))
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

// ResolveDiscussion resolves a discussion, or reopens it.
func (c *Client) ResolveDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, resolved bool) error {
	return c.send(ctx, http.MethodPut, mrPath(mr)+"/discussions/"+url.PathEscape(thread),
		map[string]bool{"resolved": resolved}, nil)
}

// UpdateMergeRequestDescription replaces a merge request's description.
func (c *Client) UpdateMergeRequestDescription(ctx context.Context, mr forge.MergeRequest, description string) error {
	return c.send(ctx, http.MethodPut, mrPath(mr), map[string]string{"description": description}, nil)
}

// CreateMergeRequest opens a merge request. GitLab marks a draft by its title,
// so a title that does not say so already gets the "Draft: " prefix. A 409 means
// a request is already open for the source branch, and GitLab says which.
func (c *Client) CreateMergeRequest(ctx context.Context, p forge.Project, req forge.NewMergeRequest) (*forge.MergeRequest, error) {
	title := req.Title
	if req.Draft && !isDraftTitle(title) {
		title = "Draft: " + title
	}
	var created mergeRequest
	err := c.postDecode(ctx, projectPath(p)+"/merge_requests", map[string]any{
		"source_branch":        req.SourceBranch,
		"target_branch":        req.TargetBranch,
		"title":                title,
		"description":          req.Description,
		"remove_source_branch": req.RemoveSourceBranch,
		"squash":               req.Squash,
	}, &created)
	var refused *apiError
	if errors.As(err, &refused) && refused.status == http.StatusConflict {
		return nil, existsError(p, refused.body)
	}
	if err != nil {
		return nil, err
	}
	mr := created.MergeRequest
	mr.Comments = created.UserNotesCount
	if mr.ProjectID == 0 {
		mr.ProjectID = p.ID
	}
	if mr.ProjectPath == "" {
		mr.ProjectPath = p.PathWithNamespace
	}
	if !mr.Draft && isDraftTitle(mr.Title) {
		mr.Draft = true
	}
	return &mr, nil
}

// isDraftTitle reports whether a title already marks the request as a draft,
// in any of the spellings GitLab accepts.
func isDraftTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, prefix := range []string{"draft:", "draft ", "[draft]", "(draft)"} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

var existingMR = regexp.MustCompile(`!(\d+)`)

// existsError is ErrMergeRequestExists with the request GitLab named, if it did.
func existsError(p forge.Project, body string) error {
	m := existingMR.FindStringSubmatch(body)
	if m == nil {
		return fmt.Errorf("%w", forge.ErrMergeRequestExists)
	}
	if p.WebURL != "" {
		return fmt.Errorf("%w: !%s %s/-/merge_requests/%s", forge.ErrMergeRequestExists, m[1], strings.TrimRight(p.WebURL, "/"), m[1])
	}
	return fmt.Errorf("%w: !%s", forge.ErrMergeRequestExists, m[1])
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
	q.Set("page", "1")
	var first []T
	header, err := c.get(ctx, path, q, &first)
	if err != nil {
		return nil, err
	}
	// GitLab says how many pages there are, and the rest are asked for side
	// by side: a group's hundreds of merge requests took a dozen round trips
	// one after another. A listing too long to be counted does not say, and
	// is followed page by page.
	if total, err := strconv.Atoi(header.Get("x-total-pages")); err == nil && total > 1 {
		pages := make([][]T, total+1)
		pages[1] = first
		var wg sync.WaitGroup
		var mu sync.Mutex
		var firstErr error
		sem := make(chan struct{}, pageFanOut)
		for n := 2; n <= total; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				pq := url.Values{}
				for k, v := range q {
					pq[k] = v
				}
				pq.Set("page", strconv.Itoa(n))
				var batch []T
				_, err := c.get(ctx, path, pq, &batch)
				mu.Lock()
				defer mu.Unlock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				pages[n] = batch
			}(n)
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
		var all []T
		for _, page := range pages[1:] {
			all = append(all, page...)
		}
		return all, nil
	}
	all := first
	page := header.Get("x-next-page")
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

// pageFanOut is how many pages of one listing are asked for at once.
const pageFanOut = 4

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

// UserName is the name of the account with that username.
func (c *Client) UserName(ctx context.Context, username string) (string, error) {
	var users []struct {
		Name string `json:"name"`
	}
	q := url.Values{}
	q.Set("username", username)
	if _, err := c.get(ctx, "/users", q, &users); err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "", nil
	}
	return users[0].Name, nil
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
	// Pipeline shadows the shared field of the same JSON name: GitLab sends
	// an object there (its older name for head_pipeline), which would fail
	// the whole decode as a string. The status is read on refresh.
	Pipeline   json.RawMessage `json:"pipeline"`
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
	return listedMRs(raw), nil
}

// StarredProjects is the projects the user has starred.
func (c *Client) StarredProjects(ctx context.Context) ([]forge.Project, error) {
	q := url.Values{}
	q.Set("starred", "true")
	q.Set("archived", "false")
	projects, err := getAll[forge.Project](ctx, c, "/projects", q)
	for i := range projects {
		projects[i].Starred = true
	}
	return projects, err
}

// Readme is the README.md of a project's default branch, "" when it has
// none.
func (c *Client) Readme(ctx context.Context, p forge.Project) (string, error) {
	ref := p.DefaultBranch
	if ref == "" {
		ref = "HEAD"
	}
	text, err := c.getText(ctx, projectPath(p)+"/repository/files/README.md/raw?ref="+url.QueryEscape(ref))
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.status == http.StatusNotFound {
		return "", nil
	}
	return text, err
}

// ProjectMergeRequests returns the open merge requests of one project.
func (c *Client) ProjectMergeRequests(ctx context.Context, p forge.Project) ([]forge.MergeRequest, error) {
	q := url.Values{}
	q.Set("state", "opened")
	q.Set("order_by", "updated_at")
	raw, err := getAll[mergeRequest](ctx, c, projectPath(p)+"/merge_requests", q)
	if err != nil {
		return nil, err
	}
	out := listedMRs(raw)
	for i := range out {
		if out[i].ProjectPath == "" {
			out[i].ProjectPath = p.PathWithNamespace
		}
	}
	return out, nil
}

// listedMRs turns the listing's shape into forge's.
func listedMRs(raw []mergeRequest) []forge.MergeRequest {
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
	return out
}

func groupPath(g forge.Group) string { return "/groups/" + strconv.Itoa(g.ID) }

// A project is addressed by its id, or - for one known only from a link - by
// its escaped path, which the API accepts in the same place.
func projectPath(p forge.Project) string {
	return "/projects/" + projectRef(p.ID, p.PathWithNamespace)
}
func mrPath(mr forge.MergeRequest) string {
	return "/projects/" + projectRef(mr.ProjectID, mr.ProjectPath) + "/merge_requests/" + strconv.Itoa(mr.IID)
}

func projectRef(id int, path string) string {
	if id == 0 && path != "" {
		return url.PathEscape(path)
	}
	return strconv.Itoa(id)
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

// MergeRequestPipeline is the newest pipeline run for the merge request.
func (c *Client) MergeRequestPipeline(ctx context.Context, mr forge.MergeRequest) (*forge.Pipeline, error) {
	var pipelines []forge.Pipeline
	q := url.Values{}
	q.Set("per_page", "1")
	path := "/projects/" + projectRef(mr.ProjectID, mr.ProjectPath) + "/merge_requests/" + strconv.Itoa(mr.IID) + "/pipelines"
	if _, err := c.get(ctx, path, q, &pipelines); err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return nil, nil
	}
	return &pipelines[0], nil
}

// PipelineJobs is the newest pipeline of the merge request and its jobs.
func (c *Client) PipelineJobs(ctx context.Context, mr forge.MergeRequest) (*forge.Pipeline, []forge.Job, error) {
	p, err := c.MergeRequestPipeline(ctx, mr)
	if err != nil || p == nil {
		return p, nil, err
	}
	jobs, err := c.pipelineJobs(ctx, "/projects/"+projectRef(mr.ProjectID, mr.ProjectPath), p.ID)
	return p, jobs, err
}

// BranchPipelineJobs is the newest pipeline of a branch and its jobs.
func (c *Client) BranchPipelineJobs(ctx context.Context, p forge.Project, branch string) (*forge.Pipeline, []forge.Job, error) {
	pipe, err := c.LatestPipeline(ctx, p, branch)
	if err != nil || pipe == nil {
		return pipe, nil, err
	}
	jobs, err := c.pipelineJobs(ctx, projectPath(p), pipe.ID)
	return pipe, jobs, err
}

// pipelineJobs is every job of a pipeline - those still to run, manual and
// delayed ones included - with its trigger jobs, in the order of its
// stages: a stage comes where its first job was made, which is how GitLab
// made them, and a job run again stays in its stage.
func (c *Client) pipelineJobs(ctx context.Context, project string, pipeline int) ([]forge.Job, error) {
	base := project + "/pipelines/" + strconv.Itoa(pipeline)
	// The attempts of a job run again are listed too, so what an earlier
	// one failed on can still be read.
	all := url.Values{}
	all.Set("include_retried", "true")
	jobs, err := getAll[forge.Job](ctx, c, base+"/jobs", all)
	if err != nil {
		return nil, err
	}
	bridges, err := getAll[forge.Job](ctx, c, base+"/bridges", nil)
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.status == http.StatusNotFound {
		// A server older than the bridges has no trigger jobs to list.
		bridges, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	for i := range bridges {
		bridges[i].Trigger = true
	}
	jobs = append(jobs, bridges...)
	forge.MarkRetried(jobs)
	first := map[string]int64{}
	for _, j := range jobs {
		if at, ok := first[j.Stage]; !ok || j.ID < at {
			first[j.Stage] = j.ID
		}
	}
	// In a stage, a job's attempts stand together, the newest last.
	firstOf := map[string]int64{}
	for _, j := range jobs {
		if at, ok := firstOf[j.Name]; !ok || j.ID < at {
			firstOf[j.Name] = j.ID
		}
	}
	sort.SliceStable(jobs, func(i, k int) bool {
		if a, b := first[jobs[i].Stage], first[jobs[k].Stage]; a != b {
			return a < b
		}
		if a, b := firstOf[jobs[i].Name], firstOf[jobs[k].Name]; a != b {
			return a < b
		}
		return jobs[i].ID < jobs[k].ID
	})
	return jobs, nil
}

// Pipelines lists a merge request's, a branch's or a commit's pipelines,
// newest first.
func (c *Client) Pipelines(ctx context.Context, p forge.Project, q forge.PipelineQuery) ([]forge.Pipeline, error) {
	v := url.Values{}
	v.Set("per_page", "50")
	var pipes []forge.Pipeline
	var err error
	if q.MR != nil {
		if _, err = c.get(ctx, mrPath(*q.MR)+"/pipelines", v, &pipes); err != nil {
			return nil, err
		}
		c.pipelineDetails(ctx, "/projects/"+strconv.Itoa(q.MR.ProjectID), pipes)
		return pipes, nil
	}
	if q.Ref != "" {
		v.Set("ref", q.Ref)
	}
	if q.SHA != "" {
		v.Set("sha", q.SHA)
	}
	if _, err = c.get(ctx, projectPath(p)+"/pipelines", v, &pipes); err != nil {
		return nil, err
	}
	c.pipelineDetails(ctx, projectPath(p), pipes)
	return pipes, nil
}

// pipelineDetails fills in whom each pipeline was started by and when it
// began, which GitLab's list leaves out and each pipeline's own page has. A
// page that cannot be read leaves its pipeline as the list had it: these
// are details of a list already read.
func (c *Client) pipelineDetails(ctx context.Context, project string, pipes []forge.Pipeline) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i := range pipes {
		wg.Add(1)
		go func(p *forge.Pipeline) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			// A merge request's pipeline may be its source project's.
			at := project
			if p.ProjectID != 0 {
				at = "/projects/" + strconv.Itoa(p.ProjectID)
			}
			var detail forge.Pipeline
			if _, err := c.get(ctx, at+"/pipelines/"+strconv.Itoa(p.ID), nil, &detail); err == nil {
				p.User, p.StartedAt = detail.User, detail.StartedAt
			}
		}(&pipes[i])
	}
	wg.Wait()
}

// Jobs is the jobs of one pipeline.
func (c *Client) Jobs(ctx context.Context, p forge.Project, pipe forge.Pipeline) ([]forge.Job, error) {
	return c.pipelineJobs(ctx, projectPath(p), pipe.ID)
}

// CommitFiles counts the lines a commit added and deleted in each file,
// from its diff.
func (c *Client) CommitFiles(ctx context.Context, p forge.Project, sha string) ([]forge.FileChange, error) {
	diffs, err := getAll[struct {
		NewPath string `json:"new_path"`
		Diff    string `json:"diff"`
	}](ctx, c, projectPath(p)+"/repository/commits/"+url.PathEscape(sha)+"/diff", nil)
	if err != nil {
		return nil, err
	}
	files := make([]forge.FileChange, 0, len(diffs))
	for _, d := range diffs {
		f := forge.FileChange{Path: d.NewPath, Binary: strings.HasPrefix(d.Diff, "Binary files")}
		for _, line := range strings.Split(d.Diff, "\n") {
			switch {
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				f.Added++
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				f.Deleted++
			}
		}
		files = append(files, f)
	}
	return files, nil
}

// DownstreamJobs is the pipeline a trigger job started and its jobs.
func (c *Client) DownstreamJobs(ctx context.Context, job forge.Job) (*forge.Pipeline, []forge.Job, error) {
	d := job.Downstream
	if d == nil {
		return nil, nil, fmt.Errorf("%s started no pipeline", job.Name)
	}
	project := "/projects/" + strconv.Itoa(d.ProjectID)
	var pipe forge.Pipeline
	if _, err := c.get(ctx, project+"/pipelines/"+strconv.Itoa(d.ID), nil, &pipe); err != nil {
		return nil, nil, err
	}
	jobs, err := c.pipelineJobs(ctx, project, d.ID)
	return &pipe, jobs, err
}

// JobLog is a job's trace.
func (c *Client) JobLog(ctx context.Context, p forge.Project, job forge.Job) (string, error) {
	return c.getText(ctx, projectPath(p)+"/jobs/"+strconv.FormatInt(job.ID, 10)+"/trace")
}

// RetryJob runs a job again.
func (c *Client) RetryJob(ctx context.Context, p forge.Project, job forge.Job) error {
	return c.post(ctx, projectPath(p)+"/jobs/"+strconv.FormatInt(job.ID, 10)+"/retry", struct{}{})
}

// PlayJob starts a manual or delayed job.
func (c *Client) PlayJob(ctx context.Context, p forge.Project, job forge.Job) error {
	return c.post(ctx, projectPath(p)+"/jobs/"+strconv.FormatInt(job.ID, 10)+"/play", struct{}{})
}

// Threads counts the discussions to resolve: those with a note still to
// resolve, and those resolved.
func (c *Client) Threads(ctx context.Context, mr forge.MergeRequest) (int, int, bool, error) {
	discussions, err := getAll[discussion](ctx, c, mrPath(mr)+"/discussions", nil)
	if err != nil {
		return 0, 0, false, err
	}
	unresolved, resolved := 0, 0
	for _, d := range discussions {
		resolvable, open := false, false
		for _, note := range d.Notes {
			if note.Resolvable {
				resolvable = true
				open = open || !note.Resolved
			}
		}
		switch {
		case open:
			unresolved++
		case resolvable:
			resolved++
		}
	}
	return unresolved, resolved, true, nil
}

// getText is a GET whose answer is plain text, a job's trace.
func (c *Client) getText(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v4"+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
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
	return string(body), nil
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
		// See mergeRequest: GitLab's pipeline is an object.
		Pipeline                    json.RawMessage `json:"pipeline"`
		Description                 string          `json:"description"`
		CreatedAt                   string          `json:"created_at"`
		Labels                      []string        `json:"labels"`
		MergeStatus                 string          `json:"merge_status"`
		DetailedMergeStatus         string          `json:"detailed_merge_status"`
		HasConflicts                bool            `json:"has_conflicts"`
		BlockingDiscussionsResolved bool            `json:"blocking_discussions_resolved"`
		ChangesCount                string          `json:"changes_count"`
		UserNotesCount              int             `json:"user_notes_count"`
		Upvotes                     int             `json:"upvotes"`
		Downvotes                   int             `json:"downvotes"`
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

// MergeRequestCommits returns the newest commits of a merge request, newest
// first, together with how many there are in total; a limit of zero returns
// all of them.
func (c *Client) MergeRequestCommits(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Commit, int, error) {
	// GitLab lists them newest first, so the newest are one page.
	if limit <= 0 {
		all, err := getAll[forge.Commit](ctx, c, mrPath(mr)+"/commits", nil)
		return all, len(all), err
	}
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

// CreateProject makes a project in a group. GitLab takes a README and the
// first branch's name on creation, but neither a license nor a .gitignore:
// those come from its templates and are committed right after, in one
// commit, on the branch the README is on or will be.
func (c *Client) CreateProject(ctx context.Context, g forge.Group, req forge.NewProject) (*forge.Project, error) {
	visibility := req.Visibility
	if visibility == "" {
		visibility = forge.VisibilityPrivate
	}
	payload := map[string]any{
		"name":                   req.Name,
		"namespace_id":           g.ID,
		"description":            req.Description,
		"visibility":             visibility,
		"initialize_with_readme": req.Readme,
	}
	if req.DefaultBranch != "" {
		payload["default_branch"] = req.DefaultBranch
	}
	var p forge.Project
	if err := c.postDecode(ctx, "/projects", payload, &p); err != nil {
		return nil, err
	}
	var files []map[string]string
	if req.License != "" {
		who, err := c.CurrentUser(ctx)
		if err != nil {
			return nil, err
		}
		q := url.Values{}
		q.Set("project", req.Name)
		q.Set("fullname", who.Name)
		var t struct {
			Content string `json:"content"`
		}
		if _, err := c.get(ctx, "/templates/licenses/"+url.PathEscape(req.License), q, &t); err != nil {
			return &p, fmt.Errorf("%s was created, but its license was not: %w", p.PathWithNamespace, err)
		}
		files = append(files, map[string]string{"action": "create", "file_path": "LICENSE", "content": t.Content})
	}
	if req.Gitignore != "" {
		var t struct {
			Content string `json:"content"`
		}
		if _, err := c.get(ctx, "/templates/gitignores/"+url.PathEscape(req.Gitignore), nil, &t); err != nil {
			return &p, fmt.Errorf("%s was created, but its .gitignore was not: %w", p.PathWithNamespace, err)
		}
		files = append(files, map[string]string{"action": "create", "file_path": ".gitignore", "content": t.Content})
	}
	if len(files) > 0 {
		branch := req.DefaultBranch
		if branch == "" {
			branch = p.DefaultBranch
		}
		if branch == "" {
			branch = "main"
		}
		err := c.post(ctx, projectPath(p)+"/repository/commits", map[string]any{
			"branch":         branch,
			"commit_message": "Add " + strings.Join(fileNames(files), " and "),
			"actions":        files,
		})
		if err != nil {
			return &p, fmt.Errorf("%s was created, but its first files were not: %w", p.PathWithNamespace, err)
		}
		p.DefaultBranch = branch
	}
	return &p, nil
}

func fileNames(files []map[string]string) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f["file_path"]
	}
	return names
}

// DeleteBranch deletes a branch on the server.
func (c *Client) DeleteBranch(ctx context.Context, p forge.Project, branch string) error {
	return c.send(ctx, http.MethodDelete, projectPath(p)+"/repository/branches/"+url.PathEscape(branch), nil, nil)
}
