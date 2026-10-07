package ui

import (
	encjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/chezmoi"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// fakeServer counts what the interface asks for, so tests can check that a
// closed detail column stays quiet and that the debounce coalesces movement.
type fakeServer struct {
	*httptest.Server
	requests  atomic.Int64
	mrDetail  atomic.Int64
	approvals atomic.Int64
	// retried counts the jobs run again, played the manual ones started.
	retried atomic.Int64
	played  atomic.Int64
	// retryGate, when it holds a channel, keeps a retry waiting until the
	// channel is closed.
	retryGate atomic.Value
	// postedComment holds the body of the last comment posted.
	postedComment atomic.Value
	// postedMR holds the JSON body of the last merge request created.
	postedMR atomic.Value
	// postedMR2 is postedMR for acme/billing, and described the descriptions
	// put on merge requests, by path.
	postedMR2 atomic.Value
	described sync.Map
	// closed42 makes the forge answer that !42 of acme/gateway is closed.
	closed42 atomic.Bool
	// mrCommits, when set, is the JSON !7 lists as its commits, for a test
	// whose merge request is real git history.
	mrCommits atomic.Value
	// postedProject is the body of the last repository created, and
	// newRepoURL where the answer says it can be cloned from.
	postedProject atomic.Value
	newRepoURL    atomic.Value
	// deleteBranch, when set, is what deleting a branch on the server does:
	// a test points it at the origin it made.
	deleteBranch atomic.Value // func(project int, branch string)
	// liveBranches, when set, lists a project's branches instead of the
	// fixture: a test points it at the origin it made.
	liveBranches atomic.Value // func(project int) []string
	// pipelineRefs is the ref acme/gateway's pipelines were last asked for.
	pipelineRefs atomic.Value
	// mr7Pipelines counts the questions about !7's pipeline; holdMRList,
	// when set, keeps the group's merge request listing waiting until it is
	// closed.
	mr7Pipelines atomic.Int64
	holdMRList   atomic.Value // chan struct{}
	// groupListed counts the listings of a whole group's merge requests.
	groupListed atomic.Int64
	// namesAsked counts the questions about an account's name.
	namesAsked atomic.Int64
	// mainRunning keeps acme/gateway's branch pipeline and its build job
	// running, and mainLog is what the job has written after its first line.
	mainRunning atomic.Bool
	mainLog     atomic.Value
	// e2eDone ends the child pipeline's job, and e2eLog is what it has
	// written after its first line.
	e2eDone atomic.Bool
	e2eLog  atomic.Value
	// writes are the changes !7 was sent - a merge, a title, a state, its
	// reviewers - each as "METHOD path body".
	writesMu sync.Mutex
	writes   []string
}

// record keeps a change sent to the fake forge.
func (f *fakeServer) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.writesMu.Lock()
	defer f.writesMu.Unlock()
	f.writes = append(f.writes, r.Method+" "+r.URL.Path+" "+strings.TrimSpace(string(body)))
}

// written is what record kept so far.
func (f *fakeServer) written() []string {
	f.writesMu.Lock()
	defer f.writesMu.Unlock()
	return append([]string(nil), f.writes...)
}

// fakeGitLab serves the handful of endpoints the detail column needs.
func fakeGitLab(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	mux := http.NewServeMux()
	json := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
	// Deleting a branch, handed to the test's origin.
	for _, id := range []int{1, 2} {
		prefix := fmt.Sprintf("/api/v4/projects/%d/repository/branches/", id)
		mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if del, ok := f.deleteBranch.Load().(func(int, string)); ok {
				del(id, strings.TrimPrefix(r.URL.Path, prefix))
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	// A new repository, to be cloned from wherever the test put one.
	mux.HandleFunc("/api/v4/projects", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.postedProject.Store(string(b))
		url, _ := f.newRepoURL.Load().(string)
		json(w, `{"id":3,"name":"tool","path_with_namespace":"acme/tool","default_branch":"main",
			"web_url":"https://gl.test/acme/tool","http_url_to_repo":"`+url+`"}`)
	})
	// The group listings, so a refresh has something to read.
	mux.HandleFunc("/api/v4/groups/1/projects", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":1,"name":"gateway","path_with_namespace":"acme/gateway",
			"default_branch":"main","last_activity_at":"2026-09-22T10:00:00Z"},
			{"id":2,"name":"billing","path_with_namespace":"acme/billing",
			"default_branch":"main","last_activity_at":"2026-09-22T09:00:00Z"}]`)
	})
	// Distinct ids: the index dedupes by them, as GitLab always sends them.
	gatewayMRs := `{"id":107,"iid":7,"title":"Rate limiting","source_branch":"feat/rate","target_branch":"main",
			"project_id":1,"user_notes_count":4,"author":{"username":"jane"},
			"references":{"full":"acme/gateway!7"},"updated_at":"2026-09-22T10:00:00Z"},
			{"id":108,"iid":8,"title":"Drop the old client","source_branch":"chore/drop","target_branch":"main",
			"project_id":1,"user_notes_count":1,"author":{"username":"jane"},
			"references":{"full":"acme/gateway!8"},"updated_at":"2026-09-22T08:00:00Z"}`
	billingMRs := `{"id":109,"iid":9,"title":"Invoice rounding","source_branch":"fix/round","target_branch":"main",
			"project_id":2,"user_notes_count":0,"author":{"username":"bob"},
			"references":{"full":"acme/billing!9"},"updated_at":"2026-09-22T09:00:00Z"}`
	hold := func() {
		if hold, ok := f.holdMRList.Load().(chan struct{}); ok {
			<-hold
		}
	}
	mux.HandleFunc("/api/v4/groups/1/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		f.groupListed.Add(1)
		hold()
		json(w, "["+gatewayMRs+","+billingMRs+"]")
	})
	mux.HandleFunc("/api/v4/projects/1", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"id":1,"name":"gateway","path_with_namespace":"acme/gateway",
			"description":"Edge router","visibility":"private","default_branch":"main",
			"star_count":3,"forks_count":1,"open_issues_count":4,"merge_method":"merge",
			"topics":["go","edge"],"created_at":"2024-02-01T10:00:00Z",
			"last_activity_at":"2026-09-20T10:00:00Z","web_url":"https://gl.test/acme/gateway",
			"license":{"name":"MIT"},"statistics":{"commit_count":1823,"repository_size":13107200}}`)
	})
	mux.HandleFunc("/api/v4/projects/1/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":"a1b2c3d000000000000000000000000000000000","short_id":"a1b2c3d","title":"Add rate limiting","author_name":"jane",
			"committed_date":"2026-09-21T08:00:00Z","web_url":"https://gl.test/acme/gateway/-/commit/a1b2c3d"}]`)
	})
	// A README, for the list of stars.
	mux.HandleFunc("/api/v4/projects/1/repository/files/README.md/raw", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "# Gateway\n\nThe **edge** router.\n")
	})
	// What a commit changed, as GitLab's diff of it says.
	mux.HandleFunc("/api/v4/projects/1/repository/commits/{sha}/diff", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"new_path":"limit.go","diff":"@@ -1,2 +1,3 @@\n+bucket\n+refill\n-old\n context\n"},
			{"new_path":"logo.png","diff":"Binary files a/logo.png and b/logo.png differ\n"}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/languages", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"Go":87.3,"Shell":12.7}`)
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines", func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Query().Get("ref")
		if ref == "" {
			ref = "main"
		}
		f.pipelineRefs.Store(ref)
		status := "success"
		if f.mainRunning.Load() {
			status = "running"
		}
		json(w, fmt.Sprintf(`[{"id":9,"status":%q,"ref":%q,"updated_at":"2026-09-21T09:00:00Z"}]`, status, ref))
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines/9/jobs", func(w http.ResponseWriter, r *http.Request) {
		if f.mainRunning.Load() {
			json(w, `[{"id":7,"name":"build","stage":"build","status":"running","duration":30}]`)
			return
		}
		json(w, `[{"id":7,"name":"build","stage":"build","status":"success","duration":30}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/jobs/7/trace", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		log, _ := f.mainLog.Load().(string)
		fmt.Fprint(w, "compiling\n"+log)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			hold()
			json(w, "["+gatewayMRs+"]")
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.postedMR.Store(string(b))
		w.WriteHeader(http.StatusCreated)
		// The branch the request was for, as GitLab answers.
		json(w, `{"id":142,"iid":42,"project_id":1,"title":"created","state":"opened",
			"source_branch":"`+sourceOf(b, "feat/new-thing")+`","target_branch":"main","author":{"username":"jane"},
			"web_url":"https://gl.test/acme/gateway/-/merge_requests/42","updated_at":"2026-09-25T10:00:00Z"}`)
	})
	mux.HandleFunc("/api/v4/projects/1/repository/branches", func(w http.ResponseWriter, r *http.Request) {
		if live, ok := f.liveBranches.Load().(func(int) []string); ok {
			var out []string
			for _, b := range live(1) {
				out = append(out, `{"name":"`+b+`","default":`+fmt.Sprint(b == "main")+`,"commit":{"short_id":"0"}}`)
			}
			json(w, "["+strings.Join(out, ",")+"]")
			return
		}
		json(w, `[{"name":"main","default":true,"commit":{"short_id":"a1b2c3d","title":"Add rate limiting",
			"committed_date":"2026-09-21T08:00:00Z"}},
			{"name":"feat/rate","commit":{"short_id":"beef123","title":"Token bucket",
			"committed_date":"2026-09-20T10:00:00Z"}}]`)
	})
	mux.HandleFunc("/api/v4/projects/2", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"id":2,"name":"billing","path_with_namespace":"acme/billing",
			"description":"Invoicing service","visibility":"private","default_branch":"main"}`)
	})
	mux.HandleFunc("/api/v4/projects/2/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			hold()
			json(w, "["+billingMRs+"]")
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.postedMR2.Store(string(b))
		w.WriteHeader(http.StatusCreated)
		json(w, `{"id":243,"iid":43,"project_id":2,"title":"created","state":"opened",
			"source_branch":"`+sourceOf(b, "feat/both")+`","target_branch":"main","author":{"username":"jane"},
			"web_url":"https://gl.test/acme/billing/-/merge_requests/43","updated_at":"2026-09-25T10:00:00Z"}`)
	})
	for _, p := range []string{"/api/v4/projects/1/merge_requests/42", "/api/v4/projects/2/merge_requests/43"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				b, _ := io.ReadAll(r.Body)
				f.described.Store(r.URL.Path, string(b))
			}
			state := "opened"
			if strings.HasSuffix(r.URL.Path, "/42") && f.closed42.Load() {
				state = "closed"
			}
			json(w, `{"state":"`+state+`"}`)
		})
	}
	mux.HandleFunc("/api/v4/projects/2/repository/branches", func(w http.ResponseWriter, r *http.Request) {
		if live, ok := f.liveBranches.Load().(func(int) []string); ok {
			var out []string
			for _, b := range live(2) {
				out = append(out, `{"name":"`+b+`","default":`+fmt.Sprint(b == "main")+`,"commit":{"short_id":"0"}}`)
			}
			json(w, "["+strings.Join(out, ",")+"]")
			return
		}
		json(w, `[{"name":"main","default":true,"commit":{"short_id":"c0ffee1","title":"Round half even",
			"committed_date":"2026-09-21T07:00:00Z"}}]`)
	})
	mux.HandleFunc("/api/v4/projects/2/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[]`)
	})
	mux.HandleFunc("/api/v4/projects/2/languages", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{}`)
	})
	mux.HandleFunc("/api/v4/projects/2/pipelines", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[]`)
	})
	mux.HandleFunc("/api/v4/projects/2/merge_requests/9", func(w http.ResponseWriter, r *http.Request) {
		f.mrDetail.Add(1)
		json(w, `{"iid":9,"title":"Invoice rounding","description":"Bankers rounding everywhere.",
			"source_branch":"fix/round","target_branch":"main","project_id":2,
			"author":{"username":"bob","name":"Bob Ross"},"detailed_merge_status":"mergeable",
			"created_at":"2026-09-19T10:00:00Z","updated_at":"2026-09-21T07:00:00Z"}`)
	})
	mux.HandleFunc("/api/v4/projects/2/merge_requests/9/pipelines", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":95,"status":"success"}]`)
	})
	mux.HandleFunc("/api/v4/projects/2/merge_requests/9/notes", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[]`)
	})
	mux.HandleFunc("/api/v4/projects/2/merge_requests/9/commits", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[]`)
	})
	mux.HandleFunc("/api/v4/projects/2/merge_requests/9/approvals", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"approvals_required":0,"approved_by":[]}`)
	})
	// A merge request outside the index, as a pasted link names it: by the
	// project's path, which GitLab takes escaped in place of an id.
	mux.HandleFunc("/api/v4/projects/{project}/merge_requests/5", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("project") != "acme/other" {
			http.NotFound(w, r)
			return
		}
		json(w, `{"id":305,"iid":5,"title":"Elsewhere","source_branch":"feat/other","target_branch":"main",
			"project_id":3,"web_url":"https://gl.example/acme/other/-/merge_requests/5"}`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			f.record(r)
			json(w, `{}`)
			return
		}
		f.mrDetail.Add(1)
		json(w, `{"iid":7,"title":"Rate limiting","description":"Adds a token bucket.",
			"source_branch":"feat/rate","target_branch":"main","project_id":1,
			"author":{"username":"jane","name":"Jane Doe"},
			"reviewers":[{"username":"john"}],"assignees":[{"username":"jane"}],
			"labels":["backend"],"detailed_merge_status":"mergeable",
			"blocking_discussions_resolved":true,"changes_count":"12","user_notes_count":2,
			"created_at":"2026-09-18T10:00:00Z","updated_at":"2026-09-21T07:00:00Z",
			"diverged_commits_count":3,
			"diff_refs":{"base_sha":"base000","head_sha":"head000","start_sha":"base000"},
			"pipeline":{"id":90,"status":"running"},
			"head_pipeline":{"status":"running"},"web_url":"https://gl.test/acme/gateway/-/merge_requests/7"}`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/notes", func(w http.ResponseWriter, r *http.Request) {
		// Only writing goes here; reading goes through the discussions, which
		// is where GitLab says what answers what.
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			f.postedComment.Store(string(b))
			json(w, `{"id":99}`)
			return
		}
		json(w, `[]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/discussions", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[
			{"id":"thread-old","notes":[
				{"id":4,"body":"oldest comment","created_at":"2026-09-18T12:00:00Z",
				 "author":{"username":"carol"}}]},
			{"id":"thread-mid","notes":[
				{"id":3,"body":"third comment","created_at":"2026-09-19T12:00:00Z",
				 "author":{"username":"bob"}}]},
			{"id":"thread-live","notes":[
				{"id":2,"body":"- first thing\n- second thing","created_at":"2026-09-20T12:00:00Z",
				 "author":{"username":"ann"},"resolvable":true,"resolved":false},
				{"id":1,"body":"Looks good apart from the **retry loop**",
				 "created_at":"2026-09-21T06:00:00Z","author":{"username":"john"},
				 "resolvable":true,"resolved":false}]},
			{"id":"thread-sys","notes":[
				{"id":5,"body":"changed title","system":true,
				 "created_at":"2026-09-20T06:00:00Z","author":{"username":"jane"}}]}
		]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/merge", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		json(w, `{"state":"merged"}`)
	})
	mux.HandleFunc("/api/v4/projects/1/members/all", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":11,"username":"jane","name":"Jane Doe","state":"active"},
			{"id":12,"username":"john","name":"John Roe","state":"active"},
			{"id":13,"username":"mike","name":"Mike Moe","state":"active"}]`)
	})
	// The merge request list names authors by username alone, as GitHub's
	// does; bob has a name to give, jane none.
	mux.HandleFunc("/api/v4/users", func(w http.ResponseWriter, r *http.Request) {
		f.namesAsked.Add(1)
		ids := map[string]int{"jane": 11, "john": 12, "mike": 13, "bob": 14}
		name := r.URL.Query().Get("username")
		if id, ok := ids[name]; ok {
			full := map[string]string{"bob": "Bob Ross"}[name]
			json(w, fmt.Sprintf(`[{"id":%d,"username":%q,"name":%q}]`, id, name, full))
			return
		}
		json(w, `[]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/approve", func(w http.ResponseWriter, r *http.Request) {
		f.approvals.Add(1)
		json(w, `{"approvals_left":0}`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/commits", func(w http.ResponseWriter, r *http.Request) {
		if listed, ok := f.mrCommits.Load().(string); ok {
			json(w, listed)
			return
		}
		w.Header().Set("x-total", "12")
		json(w, `[{"id":"beef123000000000000000000000000000000000","short_id":"beef123",
			"title":"Token bucket","author_name":"jane","committed_date":"2026-09-20T10:00:00Z",
			"parent_ids":["cafe456000000000000000000000000000000000"]}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/approvals", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"approvals_required":2,"approvals_left":1,"approved_by":[{"user":{"username":"john"}}]}`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/pipelines", func(w http.ResponseWriter, r *http.Request) {
		f.mr7Pipelines.Add(1)
		json(w, `[{"id":90,"status":"failed","ref":"feat/rate","web_url":"https://gl.test/acme/gateway/-/pipelines/90"},
			{"id":80,"status":"success","ref":"feat/rate","source":"push","web_url":"https://gl.test/acme/gateway/-/pipelines/80"}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines/90", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"id":90,"status":"failed","ref":"feat/rate","started_at":"2020-01-02T03:00:00Z","user":{"username":"jane"}}`)
	})
	// An earlier pipeline of !7, which passed.
	mux.HandleFunc("/api/v4/projects/1/pipelines/80/jobs", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":2,"name":"unit tests","stage":"test","status":"success","duration":70}]`)
	})
	// An earlier attempt of unit tests, run again since, and what it said.
	mux.HandleFunc("/api/v4/projects/1/jobs/3/trace", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "--- FAIL: TestFlaky\n")
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines/90/jobs", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":10,"name":"deploy","stage":"deploy","status":"manual"},
			{"id":1,"name":"lint","stage":"check","status":"success","duration":12,"web_url":"https://gl.test/j/1"},
			{"id":5,"name":"unit tests","stage":"test","status":"failed","duration":75,"web_url":"https://gl.test/j/5",
				"started_at":"2020-01-02T03:04:05Z","user":{"username":"jane"}},
			{"id":3,"name":"unit tests","stage":"test","status":"failed","duration":40}]`)
	})
	// A trigger job, and the child pipeline it started.
	mux.HandleFunc("/api/v4/projects/1/pipelines/90/bridges", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":11,"name":"e2e","stage":"deploy","status":"running",
			"downstream_pipeline":{"id":92,"project_id":1,"status":"running"}}]`)
	})
	// The child pipeline runs until a test says it is done, and its job
	// writes the lines a test gives it.
	e2eStatus := func() string {
		if f.e2eDone.Load() {
			return "success"
		}
		return "running"
	}
	mux.HandleFunc("/api/v4/projects/1/pipelines/92", func(w http.ResponseWriter, r *http.Request) {
		json(w, fmt.Sprintf(`{"id":92,"status":%q,"web_url":"https://gl.test/acme/gateway/-/pipelines/92"}`, e2eStatus()))
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines/92/jobs", func(w http.ResponseWriter, r *http.Request) {
		json(w, fmt.Sprintf(`[{"id":30,"name":"browser tests","stage":"e2e","status":%q}]`, e2eStatus()))
	})
	mux.HandleFunc("/api/v4/projects/1/jobs/30/trace", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		log, _ := f.e2eLog.Load().(string)
		fmt.Fprint(w, "step one\n"+log)
	})
	mux.HandleFunc("/api/v4/projects/1/jobs/10/play", func(w http.ResponseWriter, r *http.Request) {
		f.played.Add(1)
		json(w, `{}`)
	})
	mux.HandleFunc("/api/v4/projects/1/jobs/5/trace", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "section_start:1:step_script\r\x1b[0K\x1b[32;1mRunning tests\x1b[0;m\n--- FAIL: TestBucket\nprogress 10%\rprogress 100%\n")
	})
	mux.HandleFunc("/api/v4/projects/1/jobs/5/retry", func(w http.ResponseWriter, r *http.Request) {
		if gate, ok := f.retryGate.Load().(chan struct{}); ok {
			<-gate
		}
		f.retried.Add(1)
		json(w, `{}`)
	})
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// newTestApp writes index files into a temporary config directory and starts
// the TUI on a simulation screen.
func newTestApp(t *testing.T) (*App, tcell.SimulationScreen) {
	t.Helper()
	a, sc, _ := newTestAppSrv(t)
	return a, sc
}

// newTestAppSrv also hands back the fake GitLab so a test can count requests.
func newTestAppSrv(t *testing.T, prepare ...func(*App)) (*App, tcell.SimulationScreen, *fakeServer) {
	t.Helper()
	srv := fakeGitLab(t)
	cfg := writeTestConfig(t, srv.URL)
	app := newApp(cfg, testVault(t, cfg))
	for _, setup := range prepare {
		setup(app)
	}
	a, sc := startApp(t, app)
	return a, sc, srv
}

// testInstanceID is the id the fixture's server gets, derived from its URL.
func writeTestConfig(t *testing.T, gitlabURL string) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.SetDir(t.TempDir())
	cfg.RootDir = t.TempDir()
	// An editor every machine has, chosen, so opening does not stop to ask.
	cfg.FavouriteEditor, cfg.Editor = "custom", "true"
	inst := cfg.AddInstance(config.Instance{
		Name:   "acme",
		URL:    gitlabURL,
		Groups: []config.Group{{ID: 1, FullPath: "acme", Scope: config.ScopeSubgroups}},
	})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Distinct timestamps: the lists are sorted by activity by default, so
	// equal ones would leave the order to chance.
	id := inst.ID
	now := time.Now()
	projects := []forge.Project{
		{ID: 1, Name: "gateway", PathWithNamespace: "acme/gateway", DefaultBranch: "main", LastActivityAt: now, Instance: id,
			WebURL: "https://gl.test/acme/gateway"},
		{ID: 2, Name: "billing", PathWithNamespace: "acme/billing", DefaultBranch: "main", LastActivityAt: now.Add(-time.Hour), Instance: id,
			WebURL: "https://gl.test/acme/billing"},
	}
	mrs := []forge.MergeRequest{
		{ID: 107, IID: 7, ProjectID: 1, ProjectPath: "acme/gateway", Title: "Rate limiting", SourceBranch: "feat/rate", TargetBranch: "main", UpdatedAt: now, Comments: 4, Instance: id},
		{ID: 109, IID: 9, ProjectID: 2, ProjectPath: "acme/billing", Title: "Invoice rounding", SourceBranch: "fix/round", TargetBranch: "main", UpdatedAt: now.Add(-time.Hour), Instance: id},
		{ID: 108, IID: 8, ProjectID: 1, ProjectPath: "acme/gateway", Title: "Drop the old client", SourceBranch: "chore/drop", TargetBranch: "main", UpdatedAt: now.Add(-2 * time.Hour), Comments: 1, Instance: id},
	}
	must(t, index.Save(cfg.IndexPath("projects"),
		index.Projects{Version: index.Version, UpdatedAt: time.Now(), Items: projects}))
	must(t, index.Save(cfg.IndexPath("mrs"),
		index.MergeRequests{Version: index.Version, UpdatedAt: time.Now(), Items: mrs}))
	must(t, index.Save(cfg.IndexPath("groups"), index.Groups{Version: index.Version, UpdatedAt: time.Now(),
		Items: []forge.Group{{ID: 1, FullPath: "acme", Name: "acme", Instance: id}}}))
	return cfg
}

// testVault is an open vault holding a token for every configured server.
func testVault(t *testing.T, cfg *config.Config) tokenVault {
	t.Helper()
	v := &memoryVault{tokens: map[string]string{}}
	for _, inst := range cfg.Instances {
		v.Set(inst.ID, "test-token")
	}
	return v
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func startApp(t *testing.T, a *App) (*App, tcell.SimulationScreen) {
	t.Helper()
	a, sc, _ := startAppWithStop(t, a)
	return a, sc
}

func startAppWithStop(t *testing.T, a *App) (*App, tcell.SimulationScreen, chan struct{}) {
	t.Helper()
	// Workflow assertions observe progress and completion, not the passing
	// of spinner frames. Animation tests choose real or controlled ticks.
	if a.newAnimationTicker == nil {
		ticks := make(chan time.Time)
		a.newAnimationTicker = func(time.Duration) (<-chan time.Time, func()) { return ticks, func() {} }
		t.Cleanup(func() { close(ticks) })
	}
	// Attach the simulation screen from this goroutine: SetScreen initialises
	// it, and the test reads its contents from here too.
	sc := newObservedScreen(t)
	a.SetScreen(sc)
	// Workflow tests need room for the dialogs, not a large terminal's empty
	// cells on every key. Layout tests choose their sizes explicitly.
	sc.SetSize(120, 34)
	// Each app uses its own fake tool or no zoxide, never the machine's
	// directory history. Tests exercising it supply findExecutable.
	lookup := a.findExecutable
	a.findExecutable = func(name string) (string, error) {
		if lookup != nil {
			return lookup(name)
		}
		if name == "zoxide" {
			return "", exec.ErrNotFound
		}
		return exec.LookPath(name)
	}
	// The machine's own chezmoi is not the fixture's.
	if a.findChezmoi == nil {
		a.findChezmoi = func() (chezmoi.Checkout, error) { return chezmoi.Checkout{}, errors.New("no chezmoi in tests") }
	}

	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = a.Run() }()
	t.Cleanup(func() {
		a.tv.Stop()
		select {
		case <-stopped:
		case <-time.After(patience):
			t.Error("application did not stop")
		}
	})
	sc.observeKeys(a)
	return a, sc, stopped
}

// onLoop reads a value from the interface's own goroutine. Everything the
// application touches belongs to the event loop, so a test that reads it
// directly is racing it.
func onLoop[T any](a *App, read func() T) T {
	out := make(chan T, 1)
	a.tv.QueueUpdate(func() { out <- read() })
	select {
	case v := <-out:
		return v
	case <-time.After(patience):
		var zero T
		return zero
	}
}

// screenText renders the simulation screen into a string. The read is queued
// onto the tview event loop because tcell's simulation screen does not
// synchronise GetContents against its own drawing.
func (a *App) screenText(sc tcell.SimulationScreen) string {
	done := make(chan string, 1)
	a.tv.QueueUpdate(func() {
		if s, ok := sc.(*observedScreen); ok {
			done <- s.frame().text
		} else {
			done <- dumpScreen(sc)
		}
	})
	select {
	case s := <-done:
		return s
	case <-time.After(patience):
		return ""
	}
}

func dumpScreen(sc tcell.SimulationScreen) string {
	cells, w, h := sc.GetContents()
	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				b.WriteRune(c.Runes[0])
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// waitFor waits for a rendered frame containing want.
func waitFor(t *testing.T, a *App, sc tcell.SimulationScreen, want string) {
	t.Helper()
	waitScreenText(t, a, sc, want, true)
}

func waitGone(t *testing.T, a *App, sc tcell.SimulationScreen, gone string) {
	t.Helper()
	waitScreenText(t, a, sc, gone, false)
}

// resize changes the terminal size. tcell's simulation screen resizes its
// buffers but does not announce it, so the event is posted by hand.
func resize(sc tcell.SimulationScreen, w, h int) {
	sc.SetSize(w, h)
	_ = sc.PostEvent(tcell.NewEventResize(w, h))
}

// resizeApp makes the chosen size visible before a test reads words that
// were already on the previous frame, or samples its cells by coordinates.
func resizeApp(a *App, sc tcell.SimulationScreen, w, h int) {
	resize(sc, w, h)
	// Drawn on the loop and waited for: a queued draw alone returns at once,
	// and the next wait could still read the frame of the old size.
	onLoop(a, func() bool {
		a.tv.ForceDraw()
		return true
	})
}

func typeRunes(sc tcell.SimulationScreen, s string) {
	if observed, ok := sc.(*observedScreen); ok {
		observed.typeKeys(s)
		return
	}
	for _, r := range s {
		sc.InjectKey(tcell.KeyRune, r, tcell.ModNone)
		time.Sleep(10 * time.Millisecond)
	}
}

// ------------------------------------------------------------------- tests

func TestStartsOnTheProjectList(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "[1] Repositories")
	waitFor(t, a, sc, "[2] Merge requests")
	waitFor(t, a, sc, "[4] Settings")
	waitFor(t, a, sc, "acme/gateway")
	waitFor(t, a, sc, "acme/billing")
	waitFor(t, a, sc, "? help")
}

func TestTabKeysSwitchViews(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, "!7")
	if a.currentTab() != pageMRs {
		t.Fatalf("tab = %q", a.currentTab())
	}

	typeRunes(sc, "4")
	waitFor(t, a, sc, "GitLab servers")
	if a.currentTab() != pageSettings {
		t.Fatalf("tab = %q", a.currentTab())
	}

	typeRunes(sc, "1")
	waitFor(t, a, sc, "acme/billing")
	if a.currentTab() != pageProjects {
		t.Fatalf("tab = %q", a.currentTab())
	}
}

func TestFuzzyFilterNarrowsTheList(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")

	typeRunes(sc, "/gat")
	waitFor(t, a, sc, "acme/gateway")
	waitGone(t, a, sc, "acme/billing")

	// The first Esc only leaves filter mode, the narrowed list stays.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")
	if strings.Contains(a.screenText(sc), "acme/billing") {
		t.Error("the first Esc dropped the filter")
	}
	// The second one clears the filter.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/billing")
}

func TestProjectDetailPane(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	// Wide enough for the detail to sit beside the list.
	resizeApp(a, sc, 200, 44)

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Edge router")
	waitFor(t, a, sc, "REPO")
	waitFor(t, a, sc, "Add rate limiting") // last commits
	waitFor(t, a, sc, "LANGUAGES")
	waitFor(t, a, sc, "87.3%")
	waitFor(t, a, sc, "success") // latest pipeline
	waitFor(t, a, sc, "MIT")
	// The footer stays visible while the detail scrolls.
	typeRunes(sc, "G")
	waitFor(t, a, sc, "ON DISK")
	waitFor(t, a, sc, "not cloned")

	// Focus moved into the detail column.
	waitFor(t, a, sc, "DETAIL")
	if !onLoop(a, func() bool { return a.projectsPane.detailFocused }) {
		t.Error("focus did not move into the detail column")
	}

	// Esc returns to the list, a second Esc closes the column.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Edge router")
}

func TestMergeRequestDetailPane(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "acme/gateway !7")
	waitFor(t, a, sc, "feat/rate → main")
	waitFor(t, a, sc, "Jane Doe")
	waitFor(t, a, sc, "john")      // reviewer and commenter
	waitFor(t, a, sc, "mergeable") // merge status
	waitFor(t, a, sc, "DESCRIPTION")
	waitFor(t, a, sc, "token bucket")
	waitFor(t, a, sc, "COMMENTS")
	waitFor(t, a, sc, "retry loop")
	waitFor(t, a, sc, "1 of 2") // approvals
	waitFor(t, a, sc, "12 commit(s)")
	waitFor(t, a, sc, "12 file(s)")
	waitFor(t, a, sc, "3 behind main")
	waitFor(t, a, sc, "COMMITS (1 OF 12)") // section headings are upper cased
	// System notes stay out of the comment list.
	if strings.Contains(a.screenText(sc), "changed title") {
		t.Error("a system note leaked into the comments")
	}
}

func TestHelpOpensAndCloses(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "?")
	waitFor(t, a, sc, "unagit · keys")
	waitFor(t, a, sc, "REPOSITORIES")
	waitFor(t, a, sc, "clone without opening the editor")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "unagit · keys")
}

func TestSettingsOpensOnItsSections(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "4")
	for _, section := range sectionNames {
		waitFor(t, a, sc, section)
	}
	// The first section is shown straight away.
	waitFor(t, a, sc, "Default root")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/billing")
}

func TestProjectFilterOnTheMergeRequestList(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")

	typeRunes(sc, "/bill")
	waitFor(t, a, sc, "acme/billing")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // leave filter mode, keep selection
	typeRunes(sc, "m")

	waitFor(t, a, sc, "Invoice rounding")
	waitGone(t, a, sc, "Rate limiting")
	if a.mrProjectScope.Path != "acme/billing" {
		t.Errorf("scope = %+v", a.mrProjectScope)
	}

	typeRunes(sc, "F")
	waitFor(t, a, sc, "Rate limiting")
}

func TestDeleteIsRefusedWhenNothingIsOnDisk(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "d")
	waitFor(t, a, sc, "is not on disk")
	waitGone(t, a, sc, "Delete project")
}

func TestBranchPickerListsBranches(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	waitFor(t, a, sc, "feat/rate")
	waitFor(t, a, sc, "default")
	waitFor(t, a, sc, "Token bucket")

	// It opens on the list; / starts the filter. (Its own footer, whose
	// NORMAL is followed by three spaces, not the status line's two.)
	waitFor(t, a, sc, "NORMAL   ")
	typeRunes(sc, "/")
	waitFor(t, a, sc, "FILTER")
	typeRunes(sc, "feat")
	waitGone(t, a, sc, "Add rate limiting")
	waitFor(t, a, sc, "feat/rate")

	// Esc leaves the filter for the list and keeps what it narrowed to.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL   ")
	if strings.Contains(a.screenText(sc), "Add rate limiting") {
		t.Error("leaving the input dropped the filter")
	}
	typeRunes(sc, "j") // must move the selection, not type into the filter
	waitFor(t, a, sc, "feat/rate")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Branches - acme/gateway")
}

func TestPickerNavigatesWithJK(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")

	typeRunes(sc, "f") // limit merge requests to a project
	waitFor(t, a, sc, "Limit merge requests to a repository")
	waitFor(t, a, sc, "(all repositories)")

	// It opens on the list: move down twice, pick the highlighted project.
	waitFor(t, a, sc, "NORMAL   ")
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitGone(t, a, sc, "Limit merge requests to a repository")
	if a.mrProjectScope.Path != "acme/gateway" {
		t.Errorf("scope = %+v, want acme/gateway (third entry in the picker)", a.mrProjectScope)
	}
}

// TestReviewKeyAsksForTheDiffRefs checks Ctrl-R goes through GitLab for the
// commit the merge request is diffed against, rather than guessing.
func TestReviewKeyAsksForTheDiffRefs(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")

	before := srv.mrDetail.Load()
	sc.InjectKey(tcell.KeyCtrlR, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Opening acme/gateway !7 for review")
	waitFor(t, a, sc, "diffed against")
	if got := srv.mrDetail.Load(); got == before {
		t.Error("the merge request detail was never fetched")
	}
	// The task then tries to clone from the stub, which fails. Wait for it so
	// git is finished before the temporary directories are cleaned up.
	waitFor(t, a, sc, "Press Esc to close")
}

// sourceOf is the source branch a posted merge request names, or fallback.
func sourceOf(body []byte, fallback string) string {
	var req struct {
		Source string `json:"source_branch"`
	}
	if err := encjson.Unmarshal(body, &req); err != nil || req.Source == "" {
		return fallback
	}
	return req.Source
}
