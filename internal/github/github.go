// Package github talks to github.com and answers in forge's vocabulary.
//
// GitHub has no subgroups and no group wide merge request listing, so a
// group's merge requests are gathered by asking every repository in parallel.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
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

// CommitURL is a commit's page on the server.
func (c *Client) CommitURL(p forge.Project, sha string) string {
	if p.WebURL == "" {
		return ""
	}
	return strings.TrimRight(p.WebURL, "/") + "/commit/" + sha
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
	return c.send(ctx, http.MethodPost, path, payload, out)
}

// send makes a request with a JSON body, decoding the answer when out is set.
func (c *Client) send(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
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

// UpdateMergeRequestDescription replaces a pull request's body.
func (c *Client) UpdateMergeRequestDescription(ctx context.Context, mr forge.MergeRequest, description string) error {
	return c.send(ctx, http.MethodPatch, c.pullPath(mr), map[string]string{"body": description}, nil)
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

// UserName is the name an account has set in its profile.
func (c *Client) UserName(ctx context.Context, login string) (string, error) {
	var u user
	if _, err := c.get(ctx, "/users/"+url.PathEscape(login), nil, &u); err != nil {
		return "", err
	}
	return u.Name, nil
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
	Language      string    `json:"language"`
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
		Stars:             r.Stars,
		Language:          r.Language,
	}
}

// StarredProjects is the repositories the account has starred, the most
// recently starred first.
func (c *Client) StarredProjects(ctx context.Context) ([]forge.Project, error) {
	repos, err := getAll[repo](ctx, c, "/user/starred", nil)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Project, 0, len(repos))
	for _, r := range repos {
		p := r.project()
		p.Starred = true
		out = append(out, p)
	}
	return out, nil
}

// Readme is a repository's README as markdown, "" when it has none.
func (c *Client) Readme(ctx context.Context, p forge.Project) (string, error) {
	var raw struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if _, err := c.get(ctx, "/repos/"+p.PathWithNamespace+"/readme", nil, &raw); err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.status == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	if raw.Encoding != "base64" {
		return raw.Content, nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("read the README of %s: %w", p.PathWithNamespace, err)
	}
	return string(data), nil
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
	ID int `json:"id"`
	// NodeID is the pull request's id in the GraphQL API, which alone can
	// change a draft or turn on auto-merge.
	NodeID  string `json:"node_id"`
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
		SHA:          p.Head.SHA,
	}
	for _, u := range p.RequestedReviewers {
		mr.Reviewers = append(mr.Reviewers, forge.User{Username: u.Login, Name: u.Name})
	}
	for _, u := range p.Assignees {
		mr.Assignees = append(mr.Assignees, forge.User{Username: u.Login, Name: u.Name})
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

// ProjectMergeRequests returns the open pull requests of one repository.
func (c *Client) ProjectMergeRequests(ctx context.Context, p forge.Project) ([]forge.MergeRequest, error) {
	return c.projectPulls(ctx, p)
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
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
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
		var parents []string
		for _, p := range c.Parents {
			parents = append(parents, p.SHA)
		}
		out = append(out, forge.Commit{
			ParentIDs:     parents,
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

// LatestPipeline is what a ref's check runs add up to, the way
// BranchPipelineJobs reads them, or its combined status when it has none: a
// repository that builds with GitHub Actions reports through check runs
// alone.
func (c *Client) LatestPipeline(ctx context.Context, p forge.Project, ref string) (*forge.Pipeline, error) {
	pipe, _, err := c.BranchPipelineJobs(ctx, p, ref)
	return pipe, err
}

// MergeRequestPipeline is the combined status of the pull request's head,
// or, for a repository that reports through GitHub Actions - check runs,
// which the combined status leaves out - what its check runs add up to.
func (c *Client) MergeRequestPipeline(ctx context.Context, mr forge.MergeRequest) (*forge.Pipeline, error) {
	p, _, err := c.PipelineJobs(ctx, mr)
	return p, err
}

// PipelineJobs is the pull request's head: its check runs as jobs, and as
// a pipeline the worst of them, or the combined status when there are none.
func (c *Client) PipelineJobs(ctx context.Context, mr forge.MergeRequest) (*forge.Pipeline, []forge.Job, error) {
	if mr.SHA == "" || mr.ProjectPath == "" {
		return nil, nil, nil
	}
	return c.checkRuns(ctx, mr.ProjectPath, mr.SHA, mr.SourceBranch)
}

// BranchPipelineJobs is the check runs of a branch's head, the way
// PipelineJobs reads a pull request's.
func (c *Client) BranchPipelineJobs(ctx context.Context, p forge.Project, branch string) (*forge.Pipeline, []forge.Job, error) {
	if branch == "" {
		branch = p.DefaultBranch
	}
	if branch == "" || p.PathWithNamespace == "" {
		return nil, nil, nil
	}
	return c.checkRuns(ctx, p.PathWithNamespace, branch, branch)
}

// checkRuns reads the check runs of a commit - a SHA or a branch - as jobs,
// and as a pipeline the worst of them, or the combined status when there
// are none.
func (c *Client) checkRuns(ctx context.Context, path, ref, branch string) (*forge.Pipeline, []forge.Job, error) {
	var raw struct {
		CheckRuns []struct {
			ID         int64      `json:"id"`
			Name       string     `json:"name"`
			Status     string     `json:"status"`
			Conclusion string     `json:"conclusion"`
			HTMLURL    string     `json:"html_url"`
			StartedAt  *time.Time `json:"started_at"`
			Completed  *time.Time `json:"completed_at"`
			App        struct {
				Slug string `json:"slug"`
			} `json:"app"`
		} `json:"check_runs"`
	}
	q := url.Values{}
	q.Set("per_page", "100")
	// Every attempt, not only the latest, so a check run again can be read
	// as it failed before.
	q.Set("filter", "all")
	if _, err := c.get(ctx, "/repos/"+path+"/commits/"+url.PathEscape(ref)+"/check-runs", q, &raw); err != nil {
		return nil, nil, err
	}
	if len(raw.CheckRuns) == 0 {
		p, err := c.combinedStatus(ctx, path, ref)
		return p, nil, err
	}
	jobs := make([]forge.Job, 0, len(raw.CheckRuns))
	for _, r := range raw.CheckRuns {
		job := forge.Job{ID: r.ID, Name: r.Name, Stage: r.App.Slug, Status: checkStatus(r.Status, r.Conclusion), WebURL: r.HTMLURL}
		if r.StartedAt != nil && r.Completed != nil {
			job.Duration = r.Completed.Sub(*r.StartedAt).Seconds()
		}
		jobs = append(jobs, job)
	}
	forge.MarkRetried(jobs)
	// In the order GitHub gives them, a run's attempts together, the
	// newest last.
	first := map[string]int{}
	for i, j := range jobs {
		if _, ok := first[j.Stage+"\x00"+j.Name]; !ok {
			first[j.Stage+"\x00"+j.Name] = i
		}
	}
	sort.SliceStable(jobs, func(i, k int) bool {
		if a, b := first[jobs[i].Stage+"\x00"+jobs[i].Name], first[jobs[k].Stage+"\x00"+jobs[k].Name]; a != b {
			return a < b
		}
		return jobs[i].ID < jobs[k].ID
	})
	// What the commit stands at is its latest attempts; one that failed
	// and passed when run again has passed.
	worst := "success"
	rank := map[string]int{"success": 0, "skipped": 0, "manual": 1, "canceled": 2, "pending": 3, "running": 4, "failed": 5}
	for _, job := range jobs {
		if !job.Retried && rank[job.Status] > rank[worst] {
			worst = job.Status
		}
	}
	sha := ref
	if ref == branch {
		sha = ""
	}
	return &forge.Pipeline{Status: worst, SHA: sha, Ref: branch}, jobs, nil
}

// checkStatus puts a check run's state into GitLab's words.
func checkStatus(status, conclusion string) string {
	switch status {
	case "queued", "waiting", "requested", "pending":
		return "pending"
	case "in_progress":
		return "running"
	}
	switch conclusion {
	case "success":
		return "success"
	case "failure", "timed_out", "startup_failure":
		return "failed"
	case "cancelled", "stale":
		return "canceled"
	case "action_required":
		return "manual"
	}
	return "skipped"
}

// JobLog is a GitHub Actions job's log; other apps' check runs have none.
func (c *Client) JobLog(ctx context.Context, p forge.Project, job forge.Job) (string, error) {
	if job.Stage != "" && job.Stage != "github-actions" {
		return "", fmt.Errorf("%s reports through %s, whose log is on its own page - w opens it", job.Name, job.Stage)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/repos/"+p.PathWithNamespace+"/actions/jobs/"+strconv.FormatInt(job.ID, 10)+"/logs", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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
		return "", &apiError{status: resp.StatusCode, body: string(body), path: "job log"}
	}
	return string(body), nil
}

// RetryJob runs a GitHub Actions job again.
func (c *Client) RetryJob(ctx context.Context, p forge.Project, job forge.Job) error {
	return c.post(ctx, "/repos/"+p.PathWithNamespace+"/actions/jobs/"+strconv.FormatInt(job.ID, 10)+"/rerun", struct{}{})
}

// Pipelines is the one "pipeline" a commit has on GitHub: its check runs.
// A pull request's and a branch's are those of its head.
func (c *Client) Pipelines(ctx context.Context, p forge.Project, q forge.PipelineQuery) ([]forge.Pipeline, error) {
	path, ref, branch := p.PathWithNamespace, q.SHA, q.Ref
	switch {
	case q.MR != nil:
		path, ref, branch = q.MR.ProjectPath, q.MR.SHA, q.MR.SourceBranch
	case ref == "":
		ref = q.Ref
	}
	if path == "" || ref == "" {
		return nil, nil
	}
	pipe, _, err := c.checkRuns(ctx, path, ref, branch)
	if err != nil || pipe == nil {
		return nil, err
	}
	if pipe.SHA == "" && ref != branch {
		pipe.SHA = ref
	}
	return []forge.Pipeline{*pipe}, nil
}

// Jobs is the check runs of the commit a pipeline stands for.
func (c *Client) Jobs(ctx context.Context, p forge.Project, pipe forge.Pipeline) ([]forge.Job, error) {
	ref := pipe.SHA
	if ref == "" {
		ref = pipe.Ref
	}
	_, jobs, err := c.checkRuns(ctx, p.PathWithNamespace, ref, pipe.Ref)
	return jobs, err
}

// CommitFiles is what GitHub counts a commit changed, file by file.
func (c *Client) CommitFiles(ctx context.Context, p forge.Project, sha string) ([]forge.FileChange, error) {
	var raw struct {
		Files []struct {
			Filename  string `json:"filename"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
			Patch     string `json:"patch"`
		} `json:"files"`
	}
	if _, err := c.get(ctx, "/repos/"+p.PathWithNamespace+"/commits/"+url.PathEscape(sha), nil, &raw); err != nil {
		return nil, err
	}
	files := make([]forge.FileChange, 0, len(raw.Files))
	for _, f := range raw.Files {
		files = append(files, forge.FileChange{Path: f.Filename, Added: f.Additions, Deleted: f.Deletions,
			Binary: f.Patch == "" && f.Additions == 0 && f.Deletions == 0})
	}
	return files, nil
}

// PlayJob has nothing to start: GitHub has no manual jobs. A workflow run
// by hand is started from its page.
func (c *Client) PlayJob(ctx context.Context, p forge.Project, job forge.Job) error {
	return errors.New("GitHub has no manual jobs - a workflow run by hand starts from its page (w)")
}

// DownstreamJobs has nothing to read: GitHub's check runs start no
// pipelines of their own.
func (c *Client) DownstreamJobs(ctx context.Context, job forge.Job) (*forge.Pipeline, []forge.Job, error) {
	return nil, nil, errors.New("GitHub's checks start no pipelines of their own")
}

// Threads cannot be told through GitHub's REST API, which does not
// say whether a review thread is resolved.
func (c *Client) Threads(ctx context.Context, mr forge.MergeRequest) (int, int, bool, error) {
	return 0, 0, false, nil
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

// MergeRequestCommits returns the newest commits of a pull request, newest
// first, and how many there are; a limit of zero returns all of them.
//
// GitHub lists them oldest first, so the newest are on the last page, and
// there is no telling which page that is without reading them all - the list
// is read whole and cut here.
func (c *Client) MergeRequestCommits(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Commit, int, error) {
	raw, err := getAll[commit](ctx, c, c.pullPath(mr)+"/commits", nil)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) >= pullCommitsCap {
		// The list stops at the cap without saying so; the comparison of the
		// two ends has every commit.
		if raw, err = c.comparedCommits(ctx, mr); err != nil {
			return nil, 0, err
		}
	}
	out := commits(raw)
	slices.Reverse(out)
	count := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, count, nil
}

// pullCommitsCap is as many commits as GitHub lists for a pull request.
const pullCommitsCap = 250

// comparedCommits lists a pull request's commits, oldest first, from a
// comparison of its base with its head, which pages through all of them. A
// three-dot comparison starts at the merge base, as the pull request does.
func (c *Client) comparedCommits(ctx context.Context, mr forge.MergeRequest) ([]commit, error) {
	var p pull
	if _, err := c.get(ctx, c.pullPath(mr), nil, &p); err != nil {
		return nil, err
	}
	path := "/repos/" + mr.ProjectPath + "/compare/" + p.Base.SHA + "..." + p.Head.SHA
	q := url.Values{}
	q.Set("per_page", "100")
	var all []commit
	for page := 1; ; page++ {
		q.Set("page", strconv.Itoa(page))
		var batch struct {
			TotalCommits int      `json:"total_commits"`
			Commits      []commit `json:"commits"`
		}
		if _, err := c.get(ctx, path, q, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch.Commits...)
		if len(batch.Commits) == 0 || len(all) >= batch.TotalCommits {
			return all, nil
		}
	}
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

// CreateProject makes a repository in an organisation, or in the account
// itself. GitHub writes a license and a .gitignore from its templates only
// into a repository it initialises, so either of them brings a README along;
// the first branch is renamed afterwards, as creation cannot name it.
func (c *Client) CreateProject(ctx context.Context, g forge.Group, req forge.NewProject) (*forge.Project, error) {
	login, err := c.whoami(ctx)
	if err != nil {
		return nil, err
	}
	path := "/orgs/" + g.FullPath + "/repos"
	if g.FullPath == login {
		path = "/user/repos"
	}
	initialise := req.Readme || req.License != "" || req.Gitignore != ""
	payload := map[string]any{
		"name":        req.Name,
		"description": req.Description,
		"private":     req.Visibility != forge.VisibilityPublic,
		"auto_init":   initialise,
	}
	if req.License != "" {
		payload["license_template"] = req.License
	}
	if req.Gitignore != "" {
		payload["gitignore_template"] = req.Gitignore
	}
	var created repo
	if err := c.postDecode(ctx, path, payload, &created); err != nil {
		return nil, err
	}
	p := created.project()
	if initialise && req.DefaultBranch != "" && created.DefaultBranch != "" && created.DefaultBranch != req.DefaultBranch {
		rename := "/repos/" + created.FullName + "/branches/" + url.PathEscape(created.DefaultBranch) + "/rename"
		if err := c.post(ctx, rename, map[string]string{"new_name": req.DefaultBranch}); err != nil {
			return &p, fmt.Errorf("%s was created, but its branch is still %s: %w", p.PathWithNamespace, created.DefaultBranch, err)
		}
		p.DefaultBranch = req.DefaultBranch
	}
	return &p, nil
}

// DeleteBranch deletes a branch on the server.
func (c *Client) DeleteBranch(ctx context.Context, p forge.Project, branch string) error {
	return c.send(ctx, http.MethodDelete, "/repos/"+p.PathWithNamespace+"/git/refs/heads/"+branch, nil, nil)
}
