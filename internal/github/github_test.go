package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

// stub is a small github.com that answers the endpoints unagit uses.
type stub struct {
	*httptest.Server
	mux      *http.ServeMux
	requests atomic.Int64
	// inFlight records how many requests overlapped, to show the fan out is
	// real.
	mu       sync.Mutex
	current  int
	maxParal int
	seenAuth string
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{mux: http.NewServeMux()}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.mu.Lock()
		s.current++
		if s.current > s.maxParal {
			s.maxParal = s.current
		}
		s.seenAuth = r.Header.Get("Authorization")
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.current--
			s.mu.Unlock()
		}()
		w.Header().Set("Content-Type", "application/json")
		s.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *stub) handle(path, body string) {
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	})
}

func (s *stub) client() *Client { return newAt(s.URL, "ghp-test") }

const userJSON = `{"login":"toby","name":"Toby"}`

func TestKindAndGitPlumbing(t *testing.T) {
	c := New("t")
	if c.Kind() != forge.KindGitHub {
		t.Errorf("kind = %q", c.Kind())
	}
	if got := c.HeadRef(42); got != "refs/pull/42/head" {
		t.Errorf("head ref = %q", got)
	}
	// GitHub wants this user name with a token as the password.
	if got := c.GitUser(); got != "x-access-token" {
		t.Errorf("git user = %q", got)
	}
}

func TestGroupsAreTheAccountAndItsOrganisations(t *testing.T) {
	s := newStub(t)
	s.handle("/user", userJSON)
	s.handle("/user/orgs", `[{"id":10,"login":"acme"},{"id":11,"login":"widgets"}]`)

	groups, err := s.client().Groups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].FullPath != "toby" || !strings.Contains(groups[0].FullName, "your account") {
		t.Errorf("the account itself should come first: %+v", groups[0])
	}
	if groups[1].FullPath != "acme" || groups[2].FullPath != "widgets" {
		t.Errorf("orgs = %+v", groups[1:])
	}
	if s.seenAuth != "Bearer ghp-test" {
		t.Errorf("auth header = %q", s.seenAuth)
	}
}

func TestGroupProjectsSkipsArchivedAndForeignRepos(t *testing.T) {
	s := newStub(t)
	s.handle("/user", userJSON)
	s.handle("/orgs/acme/repos", `[
		{"id":1,"name":"api","full_name":"acme/api","owner":{"login":"acme"},
		 "default_branch":"main","clone_url":"https://github.com/acme/api.git",
		 "html_url":"https://github.com/acme/api","pushed_at":"2026-09-20T10:00:00Z"},
		{"id":2,"name":"old","full_name":"acme/old","owner":{"login":"acme"},"archived":true},
		{"id":3,"name":"other","full_name":"someone/other","owner":{"login":"someone"}}
	]`)

	projects, err := s.client().GroupProjects(context.Background(), forge.Group{FullPath: "acme"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %+v", projects)
	}
	p := projects[0]
	if p.PathWithNamespace != "acme/api" || p.DefaultBranch != "main" {
		t.Errorf("project = %+v", p)
	}
	if p.HTTPURLToRepo != "https://github.com/acme/api.git" {
		t.Errorf("clone url = %q", p.HTTPURLToRepo)
	}
	if p.LastActivityAt.Year() != 2026 {
		t.Errorf("activity = %v", p.LastActivityAt)
	}
}

// TestPersonalReposComeFromTheUserEndpoint: /orgs/<me>/repos would 404 and
// would miss private repositories.
func TestPersonalReposComeFromTheUserEndpoint(t *testing.T) {
	s := newStub(t)
	s.handle("/user", userJSON)
	s.handle("/user/repos", `[{"id":9,"name":"dotfiles","full_name":"toby/dotfiles","owner":{"login":"toby"}}]`)

	projects, err := s.client().GroupProjects(context.Background(), forge.Group{FullPath: "toby"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].PathWithNamespace != "toby/dotfiles" {
		t.Fatalf("projects = %+v", projects)
	}
}

// TestGroupMergeRequestsFansOutOverRepositories: GitHub has no group wide
// listing, so every repository is asked, several at a time.
func TestGroupMergeRequestsFansOutOverRepositories(t *testing.T) {
	s := newStub(t)
	s.handle("/user", userJSON)
	var repos []string
	for i := 1; i <= 12; i++ {
		repos = append(repos, fmt.Sprintf(`{"id":%d,"name":"r%d","full_name":"acme/r%d","owner":{"login":"acme"}}`, i, i, i))
	}
	s.handle("/orgs/acme/repos", "["+strings.Join(repos, ",")+"]")
	for i := 1; i <= 12; i++ {
		i := i
		s.mux.HandleFunc(fmt.Sprintf("/repos/acme/r%d/pulls", i), func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Errorf("state = %q", got)
			}
			if i > 2 {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprintf(w, `[{"id":%d00,"number":%d,"title":"PR in r%d","draft":false,
				"html_url":"https://github.com/acme/r%d/pull/%d",
				"user":{"login":"jane","name":"Jane"},
				"head":{"ref":"feat/x","sha":"abc","repo":{"id":%d,"full_name":"acme/r%d"}},
				"base":{"ref":"main","sha":"def","repo":{"id":%d,"full_name":"acme/r%d"}},
				"updated_at":"2026-09-21T10:00:00Z"}]`, i, i, i, i, i, i, i, i, i)
		})
	}

	mrs, err := s.client().GroupMergeRequests(context.Background(), forge.Group{FullPath: "acme"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 2 {
		t.Fatalf("merge requests = %d: %+v", len(mrs), mrs)
	}
	byPath := map[string]forge.MergeRequest{}
	for _, mr := range mrs {
		byPath[mr.ProjectPath] = mr
	}
	mr, ok := byPath["acme/r1"]
	if !ok {
		t.Fatalf("acme/r1 missing: %+v", mrs)
	}
	if mr.IID != 1 || mr.SourceBranch != "feat/x" || mr.TargetBranch != "main" {
		t.Errorf("mr = %+v", mr)
	}
	if mr.Author.Username != "jane" || mr.ProjectID != 1 {
		t.Errorf("mr = %+v", mr)
	}
	// The repositories really were asked in parallel.
	if s.maxParal < 2 {
		t.Errorf("requests never overlapped (max %d in flight)", s.maxParal)
	}
	if s.maxParal > fanOut {
		t.Errorf("%d requests in flight, the limit is %d", s.maxParal, fanOut)
	}
}

func TestPaginationFollowsTheLinkHeader(t *testing.T) {
	s := newStub(t)
	s.handle("/user", userJSON)
	s.mux.HandleFunc("/orgs/acme/repos", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", `<https://api.github.com/orgs/acme/repos?page=2>; rel="next", `+
				`<https://api.github.com/orgs/acme/repos?page=2>; rel="last"`)
			fmt.Fprint(w, `[{"id":1,"full_name":"acme/one","owner":{"login":"acme"}}]`)
		default:
			fmt.Fprint(w, `[{"id":2,"full_name":"acme/two","owner":{"login":"acme"}}]`)
		}
	})

	projects, err := s.client().GroupProjects(context.Background(), forge.Group{FullPath: "acme"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %+v", projects)
	}
}

func TestLanguagesBecomePercentages(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/languages", `{"Go":750,"Shell":250}`)

	langs, err := s.client().ProjectLanguages(context.Background(), forge.Project{PathWithNamespace: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	if langs["Go"] != 75 || langs["Shell"] != 25 {
		t.Fatalf("languages = %+v", langs)
	}
}

func TestCombinedStatusBecomesAPipeline(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/commits/main/status",
		`{"state":"failure","sha":"abc123","total_count":2,
		  "statuses":[{"updated_at":"2026-09-21T10:00:00Z","target_url":"https://ci"}]}`)

	pipe, err := s.client().LatestPipeline(context.Background(),
		forge.Project{PathWithNamespace: "acme/api", DefaultBranch: "main"}, "main")
	if err != nil {
		t.Fatal(err)
	}
	// GitHub says failure, the interface colours GitLab's vocabulary.
	if pipe == nil || pipe.Status != "failed" {
		t.Fatalf("pipeline = %+v", pipe)
	}
	if pipe.WebURL != "https://ci" {
		t.Errorf("web url = %q", pipe.WebURL)
	}
}

func TestNoStatusesMeansNoPipeline(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/commits/main/status", `{"state":"pending","total_count":0}`)
	pipe, err := s.client().LatestPipeline(context.Background(),
		forge.Project{PathWithNamespace: "acme/api"}, "main")
	if err != nil || pipe != nil {
		t.Fatalf("pipeline = %+v, err = %v", pipe, err)
	}
}

func TestMergeRequestDetail(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7", `{"id":700,"number":7,"title":"Add rate limiting",
		"body":"Adds a token bucket.","draft":true,"state":"open",
		"html_url":"https://github.com/acme/api/pull/7",
		"user":{"login":"jane","name":"Jane Doe"},
		"head":{"ref":"feat/rate","sha":"headsha","repo":{"id":1,"full_name":"acme/api"}},
		"base":{"ref":"main","sha":"basesha","repo":{"id":1,"full_name":"acme/api"}},
		"created_at":"2026-09-18T10:00:00Z","updated_at":"2026-09-21T07:00:00Z",
		"labels":[{"name":"backend"}],"assignees":[{"login":"jane"}],
		"requested_reviewers":[{"login":"john"}],"milestone":{"title":"Q4"},
		"mergeable":false,"mergeable_state":"dirty",
		"comments":3,"review_comments":2,"commits":4,"changed_files":12}`)
	s.handle("/repos/acme/api/commits/headsha/status", `{"state":"success","total_count":1,"statuses":[]}`)

	mr := forge.MergeRequest{ProjectPath: "acme/api", IID: 7, Instance: "gh"}
	det, err := s.client().MergeRequestDetail(context.Background(), mr)
	if err != nil {
		t.Fatal(err)
	}
	if det.Title != "Add rate limiting" || !det.Draft || det.Description != "Adds a token bucket." {
		t.Fatalf("detail = %+v", det)
	}
	if det.Instance != "gh" {
		t.Errorf("instance was lost: %q", det.Instance)
	}
	if det.ChangesCount != "12" || det.UserNotesCount != 5 {
		t.Errorf("counts: changes %q notes %d", det.ChangesCount, det.UserNotesCount)
	}
	if !det.HasConflicts {
		t.Error("a dirty mergeable_state means conflicts")
	}
	if len(det.Reviewers) != 1 || det.Reviewers[0].Username != "john" {
		t.Errorf("reviewers = %+v", det.Reviewers)
	}
	if det.Milestone != "Q4" || len(det.Labels) != 1 {
		t.Errorf("detail = %+v", det)
	}
	if det.Pipeline == nil || det.Pipeline.Status != "success" {
		t.Errorf("pipeline = %+v", det.Pipeline)
	}
	// GitHub's base.sha follows the target branch, so it must not be passed
	// off as a merge base.
	if det.DiffRefs.BaseSHA != "" {
		t.Errorf("base sha should be left for git to work out, got %q", det.DiffRefs.BaseSHA)
	}
	if det.DiffRefs.HeadSHA != "headsha" {
		t.Errorf("head sha = %q", det.DiffRefs.HeadSHA)
	}
}

func TestNotesMergeBothConversations(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/issues/7/comments",
		`[{"id":1,"body":"looks good","created_at":"2026-09-20T10:00:00Z","user":{"login":"john"}}]`)
	s.handle("/repos/acme/api/pulls/7/comments",
		`[{"id":2,"body":"this retry loop","created_at":"2026-09-21T10:00:00Z","user":{"login":"ann"},
		   "path":"rate.go","line":42}]`)

	notes, err := s.client().MergeRequestNotes(context.Background(),
		forge.MergeRequest{ProjectPath: "acme/api", IID: 7}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %+v", notes)
	}
	// Newest first, and the inline one keeps its place in the file.
	if notes[0].Author.Username != "ann" || notes[0].Path != "rate.go" || notes[0].Line != 42 {
		t.Errorf("first note = %+v", notes[0])
	}
	if notes[1].Author.Username != "john" {
		t.Errorf("second note = %+v", notes[1])
	}
}

func TestCommitsAreNewestFirst(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7/commits", `[
		{"sha":"1111111111","commit":{"message":"first\n\nbody","author":{"name":"jane","date":"2026-09-19T10:00:00Z"},"committer":{"date":"2026-09-19T10:00:00Z"}}},
		{"sha":"2222222222","commit":{"message":"second","author":{"name":"jane","date":"2026-09-20T10:00:00Z"},"committer":{"date":"2026-09-20T10:00:00Z"}}}
	]`)

	commits, count, err := s.client().MergeRequestCommits(context.Background(),
		forge.MergeRequest{ProjectPath: "acme/api", IID: 7}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d", count)
	}
	if len(commits) != 2 || commits[0].ShortID != "22222222" {
		t.Fatalf("commits = %+v", commits)
	}
	if commits[1].Title != "first" {
		t.Errorf("title should be the first line only: %q", commits[1].Title)
	}
}

// TestCommitTotalComesFromPagination: with more commits than asked for,
// GitHub's last page number is the count.
func TestCommitTotalComesFromPagination(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/repos/acme/api/pulls/7/commits", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") == "1" {
			w.Header().Set("Link", `<https://api.github.com/x?page=23>; rel="last"`)
			fmt.Fprint(w, `[{"sha":"aaa"}]`)
			return
		}
		w.Header().Set("Link", `<https://api.github.com/x?page=3>; rel="next", <https://api.github.com/x?page=3>; rel="last"`)
		fmt.Fprint(w, `[{"sha":"aaa"},{"sha":"bbb"}]`)
	})

	_, count, err := s.client().MergeRequestCommits(context.Background(),
		forge.MergeRequest{ProjectPath: "acme/api", IID: 7}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if count != 23 {
		t.Errorf("count = %d, want 23", count)
	}
}

func TestApprovalsCountTheLatestReviewPerPerson(t *testing.T) {
	s := newStub(t)
	s.handle("/repos/acme/api/pulls/7/reviews", `[
		{"user":{"login":"john"},"state":"CHANGES_REQUESTED"},
		{"user":{"login":"john"},"state":"APPROVED"},
		{"user":{"login":"ann"},"state":"APPROVED"},
		{"user":{"login":"ann"},"state":"CHANGES_REQUESTED"},
		{"user":{"login":"bob"},"state":"COMMENTED"}
	]`)

	approvals, err := s.client().MergeRequestApprovals(context.Background(),
		forge.MergeRequest{ProjectPath: "acme/api", IID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals.ApprovedBy) != 1 || approvals.ApprovedBy[0] != "john" {
		t.Fatalf("approved by = %v (ann changed her mind, bob only commented)", approvals.ApprovedBy)
	}
}

func TestErrorsMentionTheScope(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	})
	_, err := s.client().CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("err = %v", err)
	}
}

func TestApproveSubmitsAReview(t *testing.T) {
	s := newStub(t)
	var path, method, body string
	s.mux.HandleFunc("/repos/acme/api/pulls/7/reviews", func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		fmt.Fprint(w, `{}`)
	})

	err := s.client().Approve(context.Background(), forge.MergeRequest{ProjectPath: "acme/api", IID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/repos/acme/api/pulls/7/reviews" {
		t.Errorf("%s %s", method, path)
	}
	if !strings.Contains(body, `"event":"APPROVE"`) {
		t.Errorf("payload = %q", body)
	}
}

func TestCommentGoesToTheConversation(t *testing.T) {
	s := newStub(t)
	var path, body string
	s.mux.HandleFunc("/repos/acme/api/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		fmt.Fprint(w, `{}`)
	})

	err := s.client().Comment(context.Background(),
		forge.MergeRequest{ProjectPath: "acme/api", IID: 7}, "nice work")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/repos/acme/api/issues/7/comments" {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"body":"nice work"`) {
		t.Errorf("payload = %q", body)
	}
}

// TestExplainMissingOrgsNamesTheScope: GitHub answers an org listing the
// token may not read with an empty array, not an error, so the interface has
// to say what happened.
func TestExplainMissingOrgsNamesTheScope(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, gist")
		fmt.Fprint(w, userJSON)
	})

	got := s.client().ExplainMissingOrgs(context.Background())
	if !strings.Contains(got, "read:org") {
		t.Fatalf("the missing scope is not named: %q", got)
	}
	if !strings.Contains(got, "repo, gist") {
		t.Errorf("what the token does have is not shown: %q", got)
	}
}

func TestExplainMissingOrgsWhenTheScopeIsThere(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, read:org")
		fmt.Fprint(w, userJSON)
	})

	got := s.client().ExplainMissingOrgs(context.Background())
	if strings.Contains(got, "no read:org") {
		t.Fatalf("blamed the scope although it is granted: %q", got)
	}
	if !strings.Contains(got, "not a member") {
		t.Errorf("got %q", got)
	}
}

// TestExplainMissingOrgsForFineGrainedTokens: those carry no scope header.
func TestExplainMissingOrgsForFineGrained(t *testing.T) {
	s := newStub(t)
	s.handle("/user", userJSON)

	got := s.client().ExplainMissingOrgs(context.Background())
	if !strings.Contains(got, "fine grained") {
		t.Fatalf("got %q", got)
	}
	if scopes, classic := s.client().Scopes(context.Background()); classic || scopes != nil {
		t.Errorf("scopes = %v, classic = %v", scopes, classic)
	}
}

func TestScopesAreParsed(t *testing.T) {
	s := newStub(t)
	s.mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, read:org,  gist ")
		fmt.Fprint(w, userJSON)
	})

	scopes, classic := s.client().Scopes(context.Background())
	if !classic {
		t.Fatal("a scope header means a classic token")
	}
	if len(scopes) != 3 || scopes[0] != "repo" || scopes[1] != "read:org" || scopes[2] != "gist" {
		t.Fatalf("scopes = %#v", scopes)
	}
}
