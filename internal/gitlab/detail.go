package gitlab

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// ProjectDetail is everything GET /projects/:id hands out that is worth
// showing, including the optional statistics and license blocks.
type ProjectDetail struct {
	Project
	Visibility      string    `json:"visibility"`
	CreatedAt       time.Time `json:"created_at"`
	StarCount       int       `json:"star_count"`
	ForksCount      int       `json:"forks_count"`
	OpenIssuesCount int       `json:"open_issues_count"`
	Topics          []string  `json:"topics"`
	ReadmeURL       string    `json:"readme_url"`
	EmptyRepo       bool      `json:"empty_repo"`
	IssuesEnabled   bool      `json:"issues_enabled"`
	WikiEnabled     bool      `json:"wiki_enabled"`
	AvatarURL       string    `json:"avatar_url"`
	MergeMethod     string    `json:"merge_method"`
	SSHURL          string    `json:"ssh_url_to_repo"`
	ForkedFromLink  *Project  `json:"forked_from_project"`
	Namespace       struct {
		FullPath string `json:"full_path"`
		Kind     string `json:"kind"`
	} `json:"namespace"`
	License *struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"license"`
	Statistics *struct {
		CommitCount    int64 `json:"commit_count"`
		RepositorySize int64 `json:"repository_size"`
		StorageSize    int64 `json:"storage_size"`
		JobArtifacts   int64 `json:"job_artifacts_size"`
	} `json:"statistics"`
	Permissions *struct {
		ProjectAccess *struct {
			AccessLevel int `json:"access_level"`
		} `json:"project_access"`
		GroupAccess *struct {
			AccessLevel int `json:"access_level"`
		} `json:"group_access"`
	} `json:"permissions"`
}

// Commit is a repository commit.
type Commit struct {
	ID            string    `json:"id"`
	ShortID       string    `json:"short_id"`
	Title         string    `json:"title"`
	Message       string    `json:"message"`
	AuthorName    string    `json:"author_name"`
	AuthoredDate  time.Time `json:"authored_date"`
	CommittedDate time.Time `json:"committed_date"`
	WebURL        string    `json:"web_url"`
}

// Pipeline is a CI pipeline run.
type Pipeline struct {
	ID        int       `json:"id"`
	Status    string    `json:"status"`
	Ref       string    `json:"ref"`
	SHA       string    `json:"sha"`
	Source    string    `json:"source"`
	WebURL    string    `json:"web_url"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// MergeRequestDetail is the full merge request payload.
type MergeRequestDetail struct {
	MergeRequest
	Description                 string     `json:"description"`
	CreatedAt                   time.Time  `json:"created_at"`
	MergedAt                    *time.Time `json:"merged_at"`
	ClosedAt                    *time.Time `json:"closed_at"`
	Labels                      []string   `json:"labels"`
	MergeStatus                 string     `json:"merge_status"`
	DetailedMergeStatus         string     `json:"detailed_merge_status"`
	HasConflicts                bool       `json:"has_conflicts"`
	BlockingDiscussionsResolved bool       `json:"blocking_discussions_resolved"`
	ChangesCount                string     `json:"changes_count"`
	UserNotesCount              int        `json:"user_notes_count"`
	Upvotes                     int        `json:"upvotes"`
	Downvotes                   int        `json:"downvotes"`
	ShouldRemoveSourceBranch    bool       `json:"should_remove_source_branch"`
	ForceRemoveSourceBranch     bool       `json:"force_remove_source_branch"`
	Assignees                   []User     `json:"assignees"`
	Reviewers                   []User     `json:"reviewers"`
	MergedBy                    *User      `json:"merged_by"`
	Milestone                   *struct {
		Title   string `json:"title"`
		DueDate string `json:"due_date"`
	} `json:"milestone"`
	HeadPipeline *Pipeline `json:"head_pipeline"`
	// DiffRefs are the commits GitLab itself renders the "Changes" tab from:
	// BaseSHA is the merge base, not the tip of the target branch.
	DiffRefs struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
	DivergedCommitsCount int `json:"diverged_commits_count"`
	TaskCompletionStatus *struct {
		Count          int `json:"count"`
		CompletedCount int `json:"completed_count"`
	} `json:"task_completion_status"`
}

// Note is a comment on a merge request. System notes are the automatic
// "changed title", "added label" entries GitLab writes itself.
type Note struct {
	ID         int       `json:"id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	System     bool      `json:"system"`
	Resolvable bool      `json:"resolvable"`
	Resolved   bool      `json:"resolved"`
	Type       string    `json:"type"`
	Author     User      `json:"author"`
	Position   *struct {
		NewPath string `json:"new_path"`
		OldPath string `json:"old_path"`
		NewLine int    `json:"new_line"`
		OldLine int    `json:"old_line"`
	} `json:"position"`
}

// Approvals is the approval state. The endpoint is not available on every
// GitLab tier, so callers should treat an error as "unknown".
type Approvals struct {
	ApprovalsRequired int `json:"approvals_required"`
	ApprovalsLeft     int `json:"approvals_left"`
	ApprovedBy        []struct {
		User User `json:"user"`
	} `json:"approved_by"`
}

func projectPath(id int) string { return "/projects/" + strconv.Itoa(id) }

// Project returns the full project payload including statistics.
func (c *Client) Project(ctx context.Context, id int) (*ProjectDetail, error) {
	q := url.Values{}
	q.Set("statistics", "true")
	q.Set("license", "true")
	var p ProjectDetail
	if _, err := c.get(ctx, projectPath(id), q, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ProjectCommits returns the newest commits of a ref.
func (c *Client) ProjectCommits(ctx context.Context, id int, ref string, limit int) ([]Commit, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	if ref != "" {
		q.Set("ref_name", ref)
	}
	var commits []Commit
	if _, err := c.get(ctx, projectPath(id)+"/repository/commits", q, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

// ProjectLanguages returns the language breakdown in percent.
func (c *Client) ProjectLanguages(ctx context.Context, id int) (map[string]float64, error) {
	var langs map[string]float64
	if _, err := c.get(ctx, projectPath(id)+"/languages", nil, &langs); err != nil {
		return nil, err
	}
	return langs, nil
}

// LatestPipeline returns the most recent pipeline of a ref, or nil when the
// project has none.
func (c *Client) LatestPipeline(ctx context.Context, id int, ref string) (*Pipeline, error) {
	q := url.Values{}
	q.Set("per_page", "1")
	if ref != "" {
		q.Set("ref", ref)
	}
	var pipelines []Pipeline
	if _, err := c.get(ctx, projectPath(id)+"/pipelines", q, &pipelines); err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return nil, nil
	}
	return &pipelines[0], nil
}

func mrPath(projectID, iid int) string {
	return projectPath(projectID) + "/merge_requests/" + strconv.Itoa(iid)
}

// MergeRequest returns the full merge request payload, including how far the
// source branch has fallen behind the target.
func (c *Client) MergeRequest(ctx context.Context, projectID, iid int) (*MergeRequestDetail, error) {
	q := url.Values{}
	q.Set("include_diverged_commits_count", "true")
	var mr MergeRequestDetail
	if _, err := c.get(ctx, mrPath(projectID, iid), q, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// MergeRequestNotes returns the newest comments, most recent first.
func (c *Client) MergeRequestNotes(ctx context.Context, projectID, iid, limit int) ([]Note, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("order_by", "created_at")
	q.Set("sort", "desc")
	var notes []Note
	if _, err := c.get(ctx, mrPath(projectID, iid)+"/notes", q, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

// MergeRequestCommits returns the newest commits of a merge request together
// with how many there are in total - that is, how many commits the merge
// request adds on top of its target branch.
func (c *Client) MergeRequestCommits(ctx context.Context, projectID, iid, limit int) ([]Commit, int, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	var commits []Commit
	header, err := c.get(ctx, mrPath(projectID, iid)+"/commits", q, &commits)
	if err != nil {
		return nil, 0, err
	}
	return commits, total(header), nil
}

// MergeRequestApprovals returns the approval state. Not every GitLab tier
// exposes it.
func (c *Client) MergeRequestApprovals(ctx context.Context, projectID, iid int) (*Approvals, error) {
	var a Approvals
	if _, err := c.get(ctx, mrPath(projectID, iid)+"/approvals", nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
