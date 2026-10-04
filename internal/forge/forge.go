// Package forge is the common language unagit speaks about hosted git.
//
// GitLab and GitHub differ in naming and in shape; everything above this
// package sees one vocabulary. Pull requests are merge requests throughout,
// and a GitHub organisation is a group.
package forge

import (
	"context"
	"errors"
	"time"
)

// ErrNotSupported is what a provider returns for something the forge's API
// cannot do, so a caller can tell "not possible here" from a failure.
var ErrNotSupported = errors.New("not supported by this forge")

// ErrHeadMoved is what Merge returns when commits were pushed after the head
// it was told to merge.
var ErrHeadMoved = errors.New("the merge request has new commits - refresh it and look again")

// ErrMergeRequestExists is what CreateMergeRequest returns, wrapped in a message
// that names the request when the forge said which, when an open merge request
// already exists for that source branch.
var ErrMergeRequestExists = errors.New("a merge request is already open for this branch")

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
	// SHA is the head commit; Reviewers and Assignees who it is asked of.
	SHA       string `json:"sha,omitempty"`
	Reviewers []User `json:"reviewers,omitempty"`
	Assignees []User `json:"assignees,omitempty"`
	// Pipeline is the status of the head's latest pipeline, "" for none.
	// The listings do not carry it, nor the approvals and the threads not
	// resolved; unagit asks for them on refresh. Unresolved means something
	// only where UnresolvedKnown says the forge could tell.
	Pipeline          string   `json:"pipeline,omitempty"`
	ApprovedBy        []string `json:"approved_by,omitempty"`
	ApprovalsRequired int      `json:"approvals_required,omitempty"`
	Unresolved        int      `json:"unresolved,omitempty"`
	UnresolvedKnown   bool     `json:"unresolved_known,omitempty"`
	// Comments is how many people have said something. GitLab reports it on
	// the listing; GitHub only on a single merge request, so there it stays
	// zero until the detail is opened.
	Comments int `json:"comments,omitempty"`
	// ProjectPath and Instance are filled in by unagit, not by the server.
	ProjectPath string `json:"project_path,omitempty"`
	Instance    string `json:"instance,omitempty"`
}

// NewMergeRequest is what a merge request is created from.
type NewMergeRequest struct {
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
	// Draft opens it as a draft. RemoveSourceBranch and Squash are GitLab's
	// options; GitHub has no such fields on creation and ignores them.
	Draft              bool
	RemoveSourceBranch bool
	Squash             bool
}

// MergeOptions say how a merge request is merged.
type MergeOptions struct {
	// Squash makes one commit of the merge request's commits.
	Squash bool
	// RemoveSourceBranch deletes the source branch on the server once merged.
	// When the merge is left to the pipeline, GitHub leaves that to the
	// repository's own setting.
	RemoveSourceBranch bool
	// WhenPipelineSucceeds leaves the merge to the head's pipeline: it
	// happens once that passes, rather than now.
	WhenPipelineSucceeds bool
	// SHA is the head the user decided on.
	SHA string
}

// NewProject is what a repository is created from.
type NewProject struct {
	Name        string
	Description string
	// Visibility is VisibilityPrivate, VisibilityInternal or
	// VisibilityPublic; internal is GitLab's, and GitHub takes it as private.
	Visibility string
	// Readme, License and Gitignore make the first commit: a README, a
	// license by its key (mit, apache-2.0, ...) and a .gitignore by its
	// template name (Go, Node, ...). With none of them the repository is
	// created empty.
	Readme    bool
	License   string
	Gitignore string
	// DefaultBranch names the first branch; "" leaves it to the forge.
	DefaultBranch string
}

// Visibilities of a repository.
const (
	VisibilityPrivate  = "private"
	VisibilityInternal = "internal"
	VisibilityPublic   = "public"
)

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
		// StartSHA is GitLab's start of the diff, which a positioned comment
		// has to name; GitHub has no such thing and leaves it empty.
		StartSHA string `json:"start_sha"`
		HeadSHA  string `json:"head_sha"`
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
	// ParentIDs tells a merge commit, which has more than one.
	ParentIDs []string `json:"parent_ids"`
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

// Job is one step of a pipeline: a GitLab job, a GitHub check run. Status
// is in GitLab's words on both: success, failed, running, pending, canceled,
// skipped, manual.
type Job struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Stage    string  `json:"stage"`
	Status   string  `json:"status"`
	WebURL   string  `json:"web_url"`
	Duration float64 `json:"duration"`
}

// Note is a comment. Notes that share a Thread are one conversation, and the
// earliest of them is what it was started with.
type Note struct {
	// Orphaned means the location belongs to an old or deleted line.
	Orphaned   bool      `json:"orphaned,omitempty"`
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
	// URL links to the comment itself, when the forge says where it lives.
	URL string `json:"url,omitempty"`
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
	// CommitURL is a commit's page on the server.
	CommitURL(p Project, sha string) string

	MergeRequestDetail(ctx context.Context, mr MergeRequest) (*MergeRequestDetail, error)
	// MergeRequestNotes returns all comments when limit is zero.
	MergeRequestNotes(ctx context.Context, mr MergeRequest, limit int) ([]Note, error)
	// MergeRequestCommits returns the newest commits, newest first, and how
	// many there are; a limit of zero returns all of them.
	MergeRequestCommits(ctx context.Context, mr MergeRequest, limit int) ([]Commit, int, error)
	MergeRequestApprovals(ctx context.Context, mr MergeRequest) (*Approvals, error)
	// MergeRequestPipeline is the latest pipeline of the merge request's
	// head, nil when there is none.
	MergeRequestPipeline(ctx context.Context, mr MergeRequest) (*Pipeline, error)
	// PipelineJobs is that pipeline and its jobs - GitHub's check runs.
	PipelineJobs(ctx context.Context, mr MergeRequest) (*Pipeline, []Job, error)
	// JobLog is a job's output as plain text.
	JobLog(ctx context.Context, mr MergeRequest, job Job) (string, error)
	// RetryJob runs a job again.
	RetryJob(ctx context.Context, mr MergeRequest, job Job) error
	// UnresolvedThreads counts the threads not resolved yet; known is false
	// where the forge cannot say.
	UnresolvedThreads(ctx context.Context, mr MergeRequest) (n int, known bool, err error)

	// Approve records an approval of the merge request as the token's owner.
	Approve(ctx context.Context, mr MergeRequest) error
	// Comment posts a comment on the merge request.
	Comment(ctx context.Context, mr MergeRequest, body string) error
	// CommentNote is Comment for the caller that needs to know what it created:
	// the note comes back with its ID and URL. It has no Thread, because a
	// comment on the conversation cannot be replied to as a thread.
	CommentNote(ctx context.Context, mr MergeRequest, body string) (*Note, error)
	// CreateDiscussion starts a thread on line of path, on the new side of the
	// merge request, and returns the comment it created (ID, URL and Thread).
	//
	// When the forge refuses the position - the line is not part of the diff -
	// the comment is posted on the conversation instead, its body opening with
	// a "path:line" reference. That is signalled by the returned note's Path
	// and Line being empty; a note that was positioned carries both.
	CreateDiscussion(ctx context.Context, mr MergeRequest, path string, line int, body string) (*Note, error)
	// ReplyToDiscussion adds a comment to an existing thread. thread is a
	// Note.Thread as MergeRequestNotes or CreateDiscussion report it: GitLab's
	// discussion id, or the id of the review comment that started a GitHub
	// thread. The returned note carries thread unchanged. A GitHub comment on
	// the conversation cannot be replied to, and the call fails.
	ReplyToDiscussion(ctx context.Context, mr MergeRequest, thread string, body string) (*Note, error)

	// CreateMergeRequest opens a merge request (a pull request on GitHub) from
	// one branch of p into another. It returns the request as the list would
	// hold it, without Instance, which is unagit's own. When one is already
	// open for the source branch the error wraps ErrMergeRequestExists and says
	// which, when the forge did. RemoveSourceBranch and Squash are GitLab
	// options and GitHub ignores them.
	CreateMergeRequest(ctx context.Context, p Project, req NewMergeRequest) (*MergeRequest, error)
	// CreateProject makes a repository in a group - an organisation or the
	// account itself on GitHub - and returns it as the list would hold it,
	// without Instance. What the forge cannot do on creation (a GitLab
	// license or .gitignore, a GitHub default branch) it does right after.
	CreateProject(ctx context.Context, g Group, req NewProject) (*Project, error)
	// DeleteBranch deletes a branch on the server. The forge refuses the
	// default branch and a protected one.
	DeleteBranch(ctx context.Context, p Project, branch string) error
	// Merge merges the merge request, or with WhenPipelineSucceeds sets it to
	// merge itself once the head's pipeline passes. With SHA set the forge
	// refuses when the head has moved since, and the error wraps ErrHeadMoved.
	Merge(ctx context.Context, mr MergeRequest, opts MergeOptions) error
	// SetDraft marks the merge request as a draft, or as ready for review.
	SetDraft(ctx context.Context, mr MergeRequest, draft bool) error
	// CloseMergeRequest closes it without merging; its branch stays.
	CloseMergeRequest(ctx context.Context, mr MergeRequest) error
	// ReviewerCandidates lists who can be asked to review: the repository's
	// members on GitLab, those who can be assigned on GitHub.
	ReviewerCandidates(ctx context.Context, mr MergeRequest) ([]User, error)
	// SetReviewers makes these user names the reviewers asked, adding and
	// removing as needed.
	SetReviewers(ctx context.Context, mr MergeRequest, usernames []string) error
	// UpdateMergeRequestDescription replaces the description of a merge
	// request - to point merge requests made together at one another, once
	// each one's address is known.
	UpdateMergeRequestDescription(ctx context.Context, mr MergeRequest, description string) error

	// ResolveDiscussion marks a thread resolved, or reopens it. It returns
	// ErrNotSupported where the forge's API cannot do that.
	ResolveDiscussion(ctx context.Context, mr MergeRequest, thread string, resolved bool) error

	// HeadRef is where the merge request head can be fetched from: GitLab
	// publishes refs/merge-requests/<iid>/head, GitHub refs/pull/<iid>/head.
	HeadRef(iid int) string
	// GitUser is the user name the HTTPS credential helper hands to git; the
	// token is the password.
	GitUser() string
}
