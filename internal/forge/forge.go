// Package forge is the common language unagit speaks about hosted git.
//
// GitLab and GitHub differ in naming and in shape; everything above this
// package sees one vocabulary. Pull requests are merge requests throughout,
// and a GitHub organisation is a group.
package forge

import (
	"context"
	"time"
)

// Kinds of forge unagit can talk to.
const (
	KindGitLab = "gitlab"
	KindGitHub = "github"
)

// Group is a GitLab group or subgroup, or a GitHub organisation or user
// account.
type Group struct {
	ID       int    `json:"id"`
	ParentID int    `json:"parent_id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	FullPath string `json:"full_path"`
	FullName string `json:"full_name"`
	WebURL   string `json:"web_url"`
	// Instance is filled in by unagit: which server this came from.
	Instance string `json:"instance,omitempty"`
}

// Project is a repository.
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
	Instance          string    `json:"instance,omitempty"`
}

// ProjectDetail is everything worth showing about a repository.
type ProjectDetail struct {
	Project
	Visibility      string    `json:"visibility"`
	CreatedAt       time.Time `json:"created_at"`
	StarCount       int       `json:"star_count"`
	ForksCount      int       `json:"forks_count"`
	OpenIssuesCount int       `json:"open_issues_count"`
	Topics          []string  `json:"topics"`
	EmptyRepo       bool      `json:"empty_repo"`
	MergeMethod     string    `json:"merge_method"`
	ForkedFrom      string    `json:"forked_from"`
	License         string    `json:"license"`
	CommitCount     int64     `json:"commit_count"`
	RepositorySize  int64     `json:"repository_size"`
}

// MergeRequest is a merge request or a pull request.
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
	Author          User      `json:"author"`
	// ProjectPath and Instance are filled in by unagit, not by the server.
	ProjectPath string `json:"project_path,omitempty"`
	Instance    string `json:"instance,omitempty"`
}

// MergeRequestDetail is the full payload.
type MergeRequestDetail struct {
	MergeRequest
	Description                 string     `json:"description"`
	CreatedAt                   time.Time  `json:"created_at"`
	MergedAt                    *time.Time `json:"merged_at"`
	Labels                      []string   `json:"labels"`
	MergeStatus                 string     `json:"merge_status"`
	HasConflicts                bool       `json:"has_conflicts"`
	BlockingDiscussionsResolved bool       `json:"blocking_discussions_resolved"`
	ChangesCount                string     `json:"changes_count"`
	UserNotesCount              int        `json:"user_notes_count"`
	Upvotes                     int        `json:"upvotes"`
	Downvotes                   int        `json:"downvotes"`
	Assignees                   []User     `json:"assignees"`
	Reviewers                   []User     `json:"reviewers"`
	Milestone                   string     `json:"milestone"`
	Pipeline                    *Pipeline  `json:"pipeline"`
	TasksDone                   int        `json:"tasks_done"`
	TasksTotal                  int        `json:"tasks_total"`
	// DiffRefs are the commits the change is measured between. BaseSHA is the
	// merge base; when the forge does not hand it over, unagit works it out
	// from the repository instead.
	DiffRefs struct {
		BaseSHA string `json:"base_sha"`
		HeadSHA string `json:"head_sha"`
	} `json:"diff_refs"`
	DivergedCommitsCount int `json:"diverged_commits_count"`
}

// Commit is a repository commit.
type Commit struct {
	ID            string    `json:"id"`
	ShortID       string    `json:"short_id"`
	Title         string    `json:"title"`
	Message       string    `json:"message"`
	AuthorName    string    `json:"author_name"`
	CommittedDate time.Time `json:"committed_date"`
	WebURL        string    `json:"web_url"`
}

// Pipeline is a CI run: a GitLab pipeline or a GitHub combined status.
type Pipeline struct {
	ID        int       `json:"id"`
	Status    string    `json:"status"`
	Ref       string    `json:"ref"`
	SHA       string    `json:"sha"`
	WebURL    string    `json:"web_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Note is a comment. Notes that share a Thread are one conversation, and the
// earliest of them is what it was started with.
type Note struct {
	ID         int       `json:"id"`
	Thread     string    `json:"thread,omitempty"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	System     bool      `json:"system"`
	Resolvable bool      `json:"resolvable"`
	Resolved   bool      `json:"resolved"`
	Author     User      `json:"author"`
	Path       string    `json:"path"`
	Line       int       `json:"line"`
}

// Approvals is the review state.
type Approvals struct {
	Required   int      `json:"required"`
	ApprovedBy []string `json:"approved_by"`
}

// Branch is a repository branch.
type Branch struct {
	Name          string    `json:"name"`
	Default       bool      `json:"default"`
	Protected     bool      `json:"protected"`
	CommitShortID string    `json:"commit_short_id"`
	CommitTitle   string    `json:"commit_title"`
	CommittedDate time.Time `json:"committed_date"`
}

// User is an account on the forge.
type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Provider is one server unagit can ask about projects and merge requests.
// Every call is safe to make concurrently.
type Provider interface {
	// Kind is KindGitLab or KindGitHub.
	Kind() string
	// CurrentUser verifies the token.
	CurrentUser(ctx context.Context) (*User, error)
	// Groups lists everything the token can see: GitLab groups and subgroups,
	// or GitHub organisations plus the account itself.
	Groups(ctx context.Context) ([]Group, error)
	// GroupProjects lists the repositories of a group. includeSubgroups is
	// meaningless where there are no subgroups.
	GroupProjects(ctx context.Context, g Group, includeSubgroups bool) ([]Project, error)
	// GroupMergeRequests lists the open merge requests of a group.
	GroupMergeRequests(ctx context.Context, g Group, includeSubgroups bool) ([]MergeRequest, error)

	ProjectDetail(ctx context.Context, p Project) (*ProjectDetail, error)
	ProjectCommits(ctx context.Context, p Project, ref string, limit int) ([]Commit, error)
	ProjectLanguages(ctx context.Context, p Project) (map[string]float64, error)
	LatestPipeline(ctx context.Context, p Project, ref string) (*Pipeline, error)
	ProjectBranches(ctx context.Context, p Project) ([]Branch, error)

	MergeRequestDetail(ctx context.Context, mr MergeRequest) (*MergeRequestDetail, error)
	MergeRequestNotes(ctx context.Context, mr MergeRequest, limit int) ([]Note, error)
	// MergeRequestCommits returns the newest commits and how many there are.
	MergeRequestCommits(ctx context.Context, mr MergeRequest, limit int) ([]Commit, int, error)
	MergeRequestApprovals(ctx context.Context, mr MergeRequest) (*Approvals, error)

	// Approve records an approval of the merge request as the token's owner.
	Approve(ctx context.Context, mr MergeRequest) error
	// Comment posts a comment on the merge request.
	Comment(ctx context.Context, mr MergeRequest, body string) error

	// HeadRef is where the merge request head can be fetched from: GitLab
	// publishes refs/merge-requests/<iid>/head, GitHub refs/pull/<iid>/head.
	HeadRef(iid int) string
	// GitUser is the user name the HTTPS credential helper hands to git; the
	// token is the password.
	GitUser() string
}
