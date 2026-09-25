// Package github talks to github.com and answers in forge's vocabulary.
//
// GitHub has no subgroups and no group wide merge request listing, so a
// group's merge requests are gathered by asking every repository in parallel.
package github

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

// APIBase is github.com's API root. GitHub Enterprise is out of scope.
const APIBase = "https://api.github.com"

// WebBase is where repositories are cloned from.
const WebBase = "https://github.com"

// fanOut is how many repositories are asked about at once. GitHub's secondary
// rate limits start to bite well above this.
const fanOut = 8

// Client is a github.com client. The token stays in memory only.
type Client struct {
	base  string
	token string
	http  *http.Client

	once  sync.Once
	login string // the authenticated account, cached
	err   error
}

// New returns a client authenticated with a personal access token.
func New(token string) *Client { return newAt(APIBase, token) }

// newAt points a client at another API root, for tests.
func newAt(base, token string) *Client {
	return &Client{base: base, token: token, http: &http.Client{Timeout: 60 * time.Second}}
}

// Kind identifies the forge.
func (c *Client) Kind() string { return forge.KindGitHub }

// HeadRef is where GitHub publishes a pull request head.
func (c *Client) HeadRef(iid int) string { return fmt.Sprintf("refs/pull/%d/head", iid) }

// GitUser is the user name git authenticates with; the token is the password.
func (c *Client) GitUser() string { return "x-access-token" }

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
		return "GitHub rejected the token (401) - check that it is valid"
	case http.StatusForbidden:
		return fmt.Sprintf("GitHub denied access to %s (403): %s", e.path, msg)
	case http.StatusNotFound:
		return fmt.Sprintf("GitHub has no %s (404) - the token may lack the repo scope", e.path)
	}
	return fmt.Sprintf("GitHub %s returned %d: %s", e.path, e.status, msg)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return resp.Header, nil
}

// post sends a JSON body and discards the answer.
func (c *Client) post(ctx context.Context, path string, payload any) error {
	return c.postDecode(ctx, path, payload, nil)
}

// postDecode is post for the callers that need what was created - its id.
func (c *Client) postDecode(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
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

// Approve submits an approving review.
func (c *Client) Approve(ctx context.Context, mr forge.MergeRequest) error {
	return c.post(ctx, c.pullPath(mr)+"/reviews", map[string]string{"event": "APPROVE"})
}

// Comment posts a comment on the pull request's conversation.
func (c *Client) Comment(ctx context.Context, mr forge.MergeRequest, body string) error {
	return c.post(ctx, c.issuePath(mr)+"/comments", map[string]string{"body": body})
}

// CommentNote posts a comment on the pull request's conversation and returns
// it. Nothing can be replied to as a thread there, so it has no Thread.
func (c *Client) CommentNote(ctx context.Context, mr forge.MergeRequest, body string) (*forge.Note, error) {
	var created ghComment
	if err := c.postDecode(ctx, c.issuePath(mr)+"/comments", map[string]string{"body": body}, &created); err != nil {
		return nil, err
	}
	n := created.note()
	n.Thread = ""
	return &n, nil
}

// ResolveDiscussion is not something GitHub's REST API can do: resolving a
// review thread exists only in its GraphQL API.
func (c *Client) ResolveDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, resolved bool) error {
	return forge.ErrNotSupported
}

// CreateMergeRequest opens a pull request from a branch of the repository into
// another. RemoveSourceBranch and Squash have no counterpart when a pull
// request is created, so they are ignored. A 422 that says a pull request
// already exists for the branch comes back as ErrMergeRequestExists, naming it
// when it can be found.
func (c *Client) CreateMergeRequest(ctx context.Context, p forge.Project, req forge.NewMergeRequest) (*forge.MergeRequest, error) {
	path := "/repos/" + p.PathWithNamespace + "/pulls"
	var created pull
	err := c.postDecode(ctx, path, map[string]any{
		"title": req.Title, "body": req.Description,
		"head": req.SourceBranch, "base": req.TargetBranch, "draft": req.Draft,
	}, &created)
	var refused *apiError
	if errors.As(err, &refused) && refused.status == http.StatusUnprocessableEntity &&
		strings.Contains(strings.ToLower(refused.body), "already exists") {
		return nil, c.existsError(ctx, p, req.SourceBranch)
	}
	if err != nil {
		return nil, err
	}
	mr := created.mergeRequest(p.PathWithNamespace)
	if mr.ProjectID == 0 {
		mr.ProjectID = p.ID
	}
	return &mr, nil
}

// existsError is ErrMergeRequestExists, with the pull request that is open for
// the branch when GitHub can be asked which.
func (c *Client) existsError(ctx context.Context, p forge.Project, branch string) error {
	owner, _, _ := strings.Cut(p.PathWithNamespace, "/")
	q := url.Values{}
	q.Set("head", owner+":"+branch)
	q.Set("state", "open")
	var open []pull
	if _, err := c.get(ctx, "/repos/"+p.PathWithNamespace+"/pulls", q, &open); err == nil && len(open) > 0 {
		return fmt.Errorf("%w: !%d %s", forge.ErrMergeRequestExists, open[0].Number, open[0].HTMLURL)
	}
	return fmt.Errorf("%w", forge.ErrMergeRequestExists)
}

// ghComment is GitHub's comment shape, for review comments and for the
// conversation alike.
type ghComment struct {
	ID           int       `json:"id"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	User         user      `json:"user"`
	Path         string    `json:"path"`
	Line         int       `json:"line"`
	OriginalLine int       `json:"original_line"`
	Side         string    `json:"side"`
	HTMLURL      string    `json:"html_url"`
	// Review comments hang off each other; the conversation is named
	// after the one that started it.
	InReplyTo int `json:"in_reply_to_id"`
}

// note converts a comment. A thread is named by the id of the comment that
// started it, which for a comment that stands on its own is its own.
func (cm ghComment) note() forge.Note {
	thread := strconv.Itoa(cm.ID)
	if cm.InReplyTo != 0 {
		thread = strconv.Itoa(cm.InReplyTo)
	}
	line := cm.Line
	orphaned := cm.Side == "LEFT"
	if line == 0 && cm.OriginalLine > 0 {
		line, orphaned = cm.OriginalLine, true
	}
	return forge.Note{
		ID: cm.ID, Thread: thread, Body: cm.Body, CreatedAt: cm.CreatedAt,
		Author: forge.User{Username: cm.User.Login, Name: cm.User.Name},
		Path:   cm.Path, Line: line, Orphaned: orphaned, URL: cm.HTMLURL,
	}
}

// CreateDiscussion starts a review thread on line of path, on the pull
// request's new side. GitHub answers a line that is not part of the diff with
// a 422; the comment then goes to the conversation, with a "path:line"
// reference opening the body, and comes back without a Path and Line. Such a
// comment cannot be replied to.
func (c *Client) CreateDiscussion(ctx context.Context, mr forge.MergeRequest, path string, line int, body string) (*forge.Note, error) {
	var p pull
	if _, err := c.get(ctx, c.pullPath(mr), nil, &p); err != nil {
		return nil, err
	}
	var created ghComment
	err := c.postDecode(ctx, c.pullPath(mr)+"/comments", map[string]any{
		"body": body, "commit_id": p.Head.SHA, "path": path, "line": line, "side": "RIGHT",
	}, &created)
	var refused *apiError
	onConversation := false
	if errors.As(err, &refused) && refused.status == http.StatusUnprocessableEntity {
		fallback := fmt.Sprintf("`%s:%d`\n\n%s", path, line, body)
		created = ghComment{}
		if err := c.postDecode(ctx, c.issuePath(mr)+"/comments", map[string]string{"body": fallback}, &created); err != nil {
			return nil, err
		}
		onConversation = true
	} else if err != nil {
		return nil, err
	}
	n := created.note()
	if onConversation {
		// A conversation comment is not a thread: there is nothing to reply to.
		n.Thread = ""
	}
	return &n, nil
}

// ReplyToDiscussion answers a review thread. thread is the id of the comment
// that started it, which is what MergeRequestNotes reports as its Thread.
func (c *Client) ReplyToDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, body string) (*forge.Note, error) {
	var created ghComment
	if err := c.postDecode(ctx, c.pullPath(mr)+"/comments/"+url.PathEscape(thread)+"/replies", map[string]string{"body": body}, &created); err != nil {
		return nil, err
	}
	n := created.note()
	n.Thread = thread
	return &n, nil
}

// nextLink pulls the "next" URL out of a Link header.
var nextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func nextPage(h http.Header) string {
	m := nextRe.FindStringSubmatch(h.Get("Link"))
	if len(m) != 2 {
		return ""
	}
	if u, err := url.Parse(m[1]); err == nil {
		return u.Query().Get("page")
	}
	return ""
}

// getAll pages through a collection endpoint.
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
		page = nextPage(header)
	}
	return all, nil
}

type user struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// org is a GitHub organisation.
type org struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
}

// CurrentUser verifies the token and returns the authenticated account.
func (c *Client) CurrentUser(ctx context.Context) (*forge.User, error) {
	var u user
	if _, err := c.get(ctx, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &forge.User{Username: u.Login, Name: u.Name}, nil
}

// whoami caches the login, which addresses the account's own repositories.
func (c *Client) whoami(ctx context.Context) (string, error) {
	c.once.Do(func() {
		u, err := c.CurrentUser(ctx)
		if err != nil {
			c.err = err
			return
		}
		c.login = u.Username
	})
	return c.login, c.err
}

// Scopes returns what a classic personal access token is allowed to do, as
// GitHub reports it on every authenticated response. A fine grained token
// carries no such header, so the answer is then empty and unknown.
func (c *Client) Scopes(ctx context.Context) ([]string, bool) {
	header, err := c.get(ctx, "/user", nil, nil)
	if err != nil {
		return nil, false
	}
	raw, ok := header["X-Oauth-Scopes"]
	if !ok || len(raw) == 0 {
		return nil, false
	}
	var scopes []string
	for _, scope := range strings.Split(raw[0], ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes, true
}

// ExplainMissingOrgs says why the organisation list came back empty, which
// GitHub reports by simply answering with nothing at all.
func (c *Client) ExplainMissingOrgs(ctx context.Context) string {
	scopes, classic := c.Scopes(ctx)
	if !classic {
		return "no organisations came back. A fine grained token only sees an " +
			"organisation that has approved it - check the token's Resource owner."
	}
	for _, scope := range scopes {
		if scope == "read:org" || scope == "admin:org" || scope == "write:org" {
			return "no organisations came back, and the token may read them - " +
				"so you are probably not a member of any."
		}
	}
	return "no organisations came back: the token has no read:org scope (it has " +
		strings.Join(scopes, ", ") + "), so GitHub hides them. Re-issue it with read:org."
}

// Groups returns the organisations the token can see, plus the account itself
// so personal repositories are reachable too.
func (c *Client) Groups(ctx context.Context) ([]forge.Group, error) {
	login, err := c.whoami(ctx)
	if err != nil {
		return nil, err
	}
	groups := []forge.Group{{
		ID: -1, Name: login, Path: login, FullPath: login,
		FullName: login + " (your account)", WebURL: WebBase + "/" + login,
	}}
	orgs, err := getAll[org](ctx, c, "/user/orgs", nil)
	if err != nil {
		return nil, err
	}
	for _, o := range orgs {
		groups = append(groups, forge.Group{
			ID: o.ID, Name: o.Login, Path: o.Login, FullPath: o.Login,
			FullName: o.Login, WebURL: WebBase + "/" + o.Login,
		})
	}
	return groups, nil
}

// repo is GitHub's repository shape.
type repo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	CloneURL      string    `json:"clone_url"`
	SSHURL        string    `json:"ssh_url"`
	HTMLURL       string    `json:"html_url"`
	Archived      bool      `json:"archived"`
	Fork          bool      `json:"fork"`
	PushedAt      time.Time `json:"pushed_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Private       bool      `json:"private"`
	Stars         int       `json:"stargazers_count"`
	Forks         int       `json:"forks_count"`
	OpenIssues    int       `json:"open_issues_count"`
	Topics        []string  `json:"topics"`
	Size          int64     `json:"size"`
	CreatedAt     time.Time `json:"created_at"`
	License       *struct {
		Name string `json:"name"`
	} `json:"license"`
	Parent *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
}

func (r repo) project() forge.Project {
	activity := r.PushedAt
	if activity.IsZero() {
		activity = r.UpdatedAt
	}
	return forge.Project{
		ID:                r.ID,
		Name:              r.Name,
		PathWithNamespace: r.FullName,
		Description:       r.Description,
		DefaultBranch:     r.DefaultBranch,
		HTTPURLToRepo:     r.CloneURL,
		SSHURLToRepo:      r.SSHURL,
		WebURL:            r.HTMLURL,
		Archived:          r.Archived,
		LastActivityAt:    activity,
	}
}

// groupReposPath is where a group's repositories live: an organisation has
// its own endpoint, the account itself is asked about differently so that
// private repositories come back too.
func (c *Client) groupReposPath(ctx context.Context, g forge.Group) (string, url.Values, error) {
	login, err := c.whoami(ctx)
	if err != nil {
		return "", nil, err
	}
	q := url.Values{}
	q.Set("sort", "pushed")
	if g.FullPath == login {
		q.Set("affiliation", "owner")
		return "/user/repos", q, nil
	}
	return "/orgs/" + g.FullPath + "/repos", q, nil
}

// GroupProjects lists a group's repositories. GitHub has no subgroups, so the
// flag is ignored.
func (c *Client) GroupProjects(ctx context.Context, g forge.Group, _ bool) ([]forge.Project, error) {
	path, q, err := c.groupReposPath(ctx, g)
	if err != nil {
		return nil, err
	}
	repos, err := getAll[repo](ctx, c, path, q)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Project, 0, len(repos))
	for _, r := range repos {
		if r.Archived {
			continue
		}
		// /user/repos also returns organisation repositories; keep the ones
		// that really belong to this group.
		if !strings.EqualFold(r.Owner.Login, g.FullPath) {
			continue
		}
		out = append(out, r.project())
	}
	return out, nil
}

// pull is GitHub's pull request shape.
type pull struct {
	ID      int    `json:"id"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	HTMLURL string `json:"html_url"`
	User    user   `json:"user"`
	Head    struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *repo  `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *repo  `json:"repo"`
	} `json:"base"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees          []user `json:"assignees"`
	RequestedReviewers []user `json:"requested_reviewers"`
	Milestone          *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
	Comments       int    `json:"comments"`
	ReviewComments int    `json:"review_comments"`
	Commits        int    `json:"commits"`
	ChangedFiles   int    `json:"changed_files"`
}

func (p pull) mergeRequest(projectPath string) forge.MergeRequest {
	mr := forge.MergeRequest{
		ID:           p.ID,
		IID:          p.Number,
		Title:        p.Title,
		State:        p.State,
		Draft:        p.Draft,
		SourceBranch: p.Head.Ref,
		TargetBranch: p.Base.Ref,
		WebURL:       p.HTMLURL,
		UpdatedAt:    p.UpdatedAt,
		Author:       forge.User{Username: p.User.Login, Name: p.User.Name},
		ProjectPath:  projectPath,
	}
	if p.Base.Repo != nil {
		mr.ProjectID = p.Base.Repo.ID
		mr.TargetProjectID = p.Base.Repo.ID
		if mr.ProjectPath == "" {
			mr.ProjectPath = p.Base.Repo.FullName
		}
	}
	if p.Head.Repo != nil {
		mr.SourceProjectID = p.Head.Repo.ID
	}
	// The listing carries no counts; a single pull request does.
	mr.Comments = p.Comments + p.ReviewComments
	return mr
}

// GroupMergeRequests asks every repository of a group for its open pull
// requests, several at a time.
func (c *Client) GroupMergeRequests(ctx context.Context, g forge.Group, includeSubgroups bool) ([]forge.MergeRequest, error) {
	projects, err := c.GroupProjects(ctx, g, includeSubgroups)
	if err != nil {
		return nil, err
	}

	var (
		mu       sync.Mutex
		all      []forge.MergeRequest
		firstErr error
		wg       sync.WaitGroup
	)
	sem := make(chan struct{}, fanOut)
	for _, p := range projects {
		wg.Add(1)
		go func(p forge.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pulls, err := c.projectPulls(ctx, p)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			all = append(all, pulls...)
		}(p)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func (c *Client) projectPulls(ctx context.Context, p forge.Project) ([]forge.MergeRequest, error) {
	q := url.Values{}
	q.Set("state", "open")
	q.Set("sort", "updated")
	q.Set("direction", "desc")
	pulls, err := getAll[pull](ctx, c, "/repos/"+p.PathWithNamespace+"/pulls", q)
	if err != nil {
		return nil, err
	}
	out := make([]forge.MergeRequest, 0, len(pulls))
	for _, pr := range pulls {
		out = append(out, pr.mergeRequest(p.PathWithNamespace))
	}
	return out, nil
}

// ProjectDetail returns the repository with its statistics.
func (c *Client) ProjectDetail(ctx context.Context, p forge.Project) (*forge.ProjectDetail, error) {
	var r repo
	if _, err := c.get(ctx, "/repos/"+p.PathWithNamespace, nil, &r); err != nil {
		return nil, err
	}
	visibility := "public"
	if r.Private {
		visibility = "private"
	}
	d := &forge.ProjectDetail{
		Project:         r.project(),
		Visibility:      visibility,
		CreatedAt:       r.CreatedAt,
		StarCount:       r.Stars,
		ForksCount:      r.Forks,
		OpenIssuesCount: r.OpenIssues,
		Topics:          r.Topics,
		RepositorySize:  r.Size * 1024, // GitHub reports kilobytes
	}
	if r.License != nil {
		d.License = r.License.Name
	}
	if r.Parent != nil {
		d.ForkedFrom = r.Parent.FullName
	}
	return d, nil
}

// ProjectCommits returns the newest commits of a ref.
func (c *Client) ProjectCommits(ctx context.Context, p forge.Project, ref string, limit int) ([]forge.Commit, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	if ref != "" {
		q.Set("sha", ref)
	}
	var raw []commit
	if _, err := c.get(ctx, "/repos/"+p.PathWithNamespace+"/commits", q, &raw); err != nil {
		return nil, err
	}
	return commits(raw), nil
}

type commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
	HTMLURL string `json:"html_url"`
}

func commits(raw []commit) []forge.Commit {
	out := make([]forge.Commit, 0, len(raw))
	for _, c := range raw {
		short := c.SHA
		if len(short) > 8 {
			short = short[:8]
		}
		date := c.Commit.Committer.Date
		if date.IsZero() {
			date = c.Commit.Author.Date
		}
		out = append(out, forge.Commit{
			ID:            c.SHA,
			ShortID:       short,
			Title:         strings.SplitN(c.Commit.Message, "\n", 2)[0],
			Message:       c.Commit.Message,
			AuthorName:    c.Commit.Author.Name,
			CommittedDate: date,
			WebURL:        c.HTMLURL,
		})
	}
	return out
}

// ProjectLanguages returns the language breakdown in percent. GitHub reports
// bytes, so they are converted here.
func (c *Client) ProjectLanguages(ctx context.Context, p forge.Project) (map[string]float64, error) {
	var bytes map[string]float64
	if _, err := c.get(ctx, "/repos/"+p.PathWithNamespace+"/languages", nil, &bytes); err != nil {
		return nil, err
	}
	total := 0.0
	for _, n := range bytes {
		total += n
	}
	if total == 0 {
		return map[string]float64{}, nil
	}
	out := make(map[string]float64, len(bytes))
	for name, n := range bytes {
		out[name] = n / total * 100
	}
	return out, nil
}

// LatestPipeline maps the combined commit status of a ref onto a pipeline.
func (c *Client) LatestPipeline(ctx context.Context, p forge.Project, ref string) (*forge.Pipeline, error) {
	if ref == "" {
		ref = p.DefaultBranch
	}
	if ref == "" {
		return nil, nil
	}
	return c.combinedStatus(ctx, p.PathWithNamespace, ref)
}

// combinedStatus rolls GitHub's per commit statuses into one pipeline.
func (c *Client) combinedStatus(ctx context.Context, projectPath, ref string) (*forge.Pipeline, error) {
	var raw struct {
		State      string `json:"state"`
		SHA        string `json:"sha"`
		TotalCount int    `json:"total_count"`
		Statuses   []struct {
			UpdatedAt time.Time `json:"updated_at"`
			TargetURL string    `json:"target_url"`
		} `json:"statuses"`
	}
	if _, err := c.get(ctx, "/repos/"+projectPath+"/commits/"+url.PathEscape(ref)+"/status", nil, &raw); err != nil {
		return nil, err
	}
	if raw.TotalCount == 0 {
		return nil, nil
	}
	pipe := &forge.Pipeline{Status: mapStatus(raw.State), Ref: ref, SHA: raw.SHA}
	if len(raw.Statuses) > 0 {
		pipe.UpdatedAt = raw.Statuses[0].UpdatedAt
		pipe.WebURL = raw.Statuses[0].TargetURL
	}
	return pipe, nil
}

// mapStatus translates GitHub's status vocabulary into GitLab's, which is
// what the interface colours by.
func mapStatus(state string) string {
	switch state {
	case "success":
		return "success"
	case "failure", "error":
		return "failed"
	case "pending":
		return "running"
	}
	return state
}

// branch is GitHub's branch shape.
type branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Commit    struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// ProjectBranches returns every branch of a repository.
func (c *Client) ProjectBranches(ctx context.Context, p forge.Project) ([]forge.Branch, error) {
	raw, err := getAll[branch](ctx, c, "/repos/"+p.PathWithNamespace+"/branches", nil)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Branch, 0, len(raw))
	for _, b := range raw {
		short := b.Commit.SHA
		if len(short) > 8 {
			short = short[:8]
		}
		out = append(out, forge.Branch{
			Name:          b.Name,
			Default:       b.Name == p.DefaultBranch,
			Protected:     b.Protected,
			CommitShortID: short,
		})
	}
	return out, nil
}

// MergeRequestDetail returns the full pull request.
func (c *Client) MergeRequestDetail(ctx context.Context, mr forge.MergeRequest) (*forge.MergeRequestDetail, error) {
	var p pull
	if _, err := c.get(ctx, c.pullPath(mr), nil, &p); err != nil {
		return nil, err
	}
	d := &forge.MergeRequestDetail{
		MergeRequest:   p.mergeRequest(mr.ProjectPath),
		Description:    p.Body,
		CreatedAt:      p.CreatedAt,
		MergeStatus:    p.MergeableState,
		UserNotesCount: p.Comments + p.ReviewComments,
		ChangesCount:   strconv.Itoa(p.ChangedFiles),
		// GitHub resolves review threads, but not in a way that blocks the
		// merge, so nothing is reported as blocking here.
		BlockingDiscussionsResolved: true,
	}
	d.Instance = mr.Instance
	if p.Mergeable != nil && !*p.Mergeable {
		d.HasConflicts = p.MergeableState == "dirty"
	}
	for _, l := range p.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	for _, u := range p.Assignees {
		d.Assignees = append(d.Assignees, forge.User{Username: u.Login, Name: u.Name})
	}
	for _, u := range p.RequestedReviewers {
		d.Reviewers = append(d.Reviewers, forge.User{Username: u.Login, Name: u.Name})
	}
	if p.Milestone != nil {
		d.Milestone = p.Milestone.Title
	}
	d.DiffRefs.HeadSHA = p.Head.SHA
	// GitHub's base.sha follows the target branch, so it is not a merge base;
	// unagit works that out from the repository instead.

	if status, err := c.combinedStatus(ctx, mr.ProjectPath, p.Head.SHA); err == nil {
		d.Pipeline = status
	}
	return d, nil
}

func (c *Client) pullPath(mr forge.MergeRequest) string {
	return "/repos/" + mr.ProjectPath + "/pulls/" + strconv.Itoa(mr.IID)
}

func (c *Client) issuePath(mr forge.MergeRequest) string {
	return "/repos/" + mr.ProjectPath + "/issues/" + strconv.Itoa(mr.IID)
}

// MergeRequestNotes returns the conversation and the inline review comments,
// newest first.
func (c *Client) MergeRequestNotes(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Note, error) {
	var (
		mu       sync.Mutex
		notes    []forge.Note
		firstErr error
		wg       sync.WaitGroup
		paths    = []string{c.issuePath(mr) + "/comments", c.pullPath(mr) + "/comments"}
	)
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			raw, err := getAll[ghComment](ctx, c, path, url.Values{"sort": {"created"}, "direction": {"desc"}})
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, cm := range raw {
				notes = append(notes, cm.note())
			}
		}(path)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].CreatedAt.After(notes[j].CreatedAt) })
	if limit > 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	return notes, nil
}

// MergeRequestCommits returns the newest commits of a pull request and how
// many there are.
func (c *Client) MergeRequestCommits(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Commit, int, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	var raw []commit
	header, err := c.get(ctx, c.pullPath(mr)+"/commits", q, &raw)
	if err != nil {
		return nil, 0, err
	}
	out := commits(raw)
	// GitHub returns them oldest first; the interface shows the newest.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	count := len(out)
	if lastPage(header) > 0 {
		// There are more than we asked for. One page of one is the cheapest
		// way GitHub will tell us how many there are.
		count = c.countPages(ctx, c.pullPath(mr)+"/commits")
	}
	return out, count, nil

	// countPages asks for a single item and reads the last page number, which for
	// a per_page of one is the number of items.
}

func (c *Client) countPages(ctx context.Context, path string) int {
	q := url.Values{}
	q.Set("per_page", "1")
	header, err := c.get(ctx, path, q, &[]json.RawMessage{})
	if err != nil {
		return -1
	}
	if n := lastPage(header); n > 0 {
		return n
	}
	return -1
}

// lastPage returns the last page number from a Link header, or 0.
var lastRe = regexp.MustCompile(`<([^>]+)>;\s*rel="last"`)

func lastPage(h http.Header) int {
	m := lastRe.FindStringSubmatch(h.Get("Link"))
	if len(m) != 2 {
		return 0
	}
	u, err := url.Parse(m[1])
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(u.Query().Get("page"))
	return n
}

// review is one submitted review of a pull request.
type review struct {
	User  user   `json:"user"`
	State string `json:"state"`
}

// MergeRequestApprovals counts the approving reviews.
func (c *Client) MergeRequestApprovals(ctx context.Context, mr forge.MergeRequest) (*forge.Approvals, error) {
	raw, err := getAll[review](ctx, c, c.pullPath(mr)+"/reviews", nil)
	if err != nil {
		return nil, err
	}
	// Only the latest review of each person counts.
	latest := map[string]string{}
	var order []string
	for _, r := range raw {
		if _, seen := latest[r.User.Login]; !seen {
			order = append(order, r.User.Login)
		}
		if r.State == "APPROVED" || r.State == "CHANGES_REQUESTED" || r.State == "DISMISSED" {
			latest[r.User.Login] = r.State
		}
	}
	a := &forge.Approvals{}
	for _, login := range order {
		if latest[login] == "APPROVED" {
			a.ApprovedBy = append(a.ApprovedBy, login)
		}
	}
	return a, nil
}
