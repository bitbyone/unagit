package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/index"
)

// fakeGitLab serves the handful of endpoints the detail column needs.
func fakeGitLab(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	json := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
	mux.HandleFunc("/api/v4/projects/1", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"id":1,"name":"gateway","path_with_namespace":"acme/gateway",
			"description":"Edge router","visibility":"private","default_branch":"main",
			"star_count":3,"forks_count":1,"open_issues_count":4,"merge_method":"merge",
			"topics":["go","edge"],"created_at":"2024-02-01T10:00:00Z",
			"last_activity_at":"2026-09-20T10:00:00Z","web_url":"https://gl.test/acme/gateway",
			"license":{"name":"MIT"},"statistics":{"commit_count":1823,"repository_size":13107200}}`)
	})
	mux.HandleFunc("/api/v4/projects/1/repository/commits", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"short_id":"a1b2c3d","title":"Add rate limiting","author_name":"jane",
			"committed_date":"2026-09-21T08:00:00Z"}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/languages", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"Go":87.3,"Shell":12.7}`)
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":9,"status":"success","ref":"main","updated_at":"2026-09-21T09:00:00Z"}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/repository/branches", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"name":"main","default":true,"commit":{"short_id":"a1b2c3d","title":"Add rate limiting",
			"committed_date":"2026-09-21T08:00:00Z"}},
			{"name":"feat/rate","commit":{"short_id":"beef123","title":"Token bucket",
			"committed_date":"2026-09-20T10:00:00Z"}}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"iid":7,"title":"Rate limiting","description":"Adds a token bucket.",
			"source_branch":"feat/rate","target_branch":"main","project_id":1,
			"author":{"username":"jane","name":"Jane Doe"},
			"reviewers":[{"username":"john"}],"assignees":[{"username":"jane"}],
			"labels":["backend"],"detailed_merge_status":"mergeable",
			"blocking_discussions_resolved":true,"changes_count":"12","user_notes_count":2,
			"created_at":"2026-09-18T10:00:00Z","updated_at":"2026-09-21T07:00:00Z",
			"head_pipeline":{"status":"running"},"web_url":"https://gl.test/acme/gateway/-/merge_requests/7"}`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/notes", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":1,"body":"Looks good apart from the retry loop","system":false,
			"created_at":"2026-09-21T06:00:00Z","author":{"username":"john"}},
			{"id":2,"body":"changed title","system":true,"created_at":"2026-09-20T06:00:00Z",
			"author":{"username":"jane"}}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/commits", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"short_id":"beef123","title":"Token bucket","author_name":"jane",
			"committed_date":"2026-09-20T10:00:00Z"}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/approvals", func(w http.ResponseWriter, r *http.Request) {
		json(w, `{"approvals_required":2,"approvals_left":1,"approved_by":[{"user":{"username":"john"}}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newTestApp writes index files into a temporary config directory and starts
// the TUI on a simulation screen.
func newTestApp(t *testing.T) (*App, tcell.SimulationScreen) {
	t.Helper()
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	a := New(cfg, "test-token")
	return startApp(t, a)
}

func writeTestConfig(t *testing.T, gitlabURL string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)

	cfg := config.Default()
	cfg.GitLabURL = gitlabURL
	cfg.RootDir = t.TempDir()
	cfg.Groups = []config.Group{{ID: 1, FullPath: "acme"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	projects := []gitlab.Project{
		{ID: 1, Name: "gateway", PathWithNamespace: "acme/gateway", DefaultBranch: "main", LastActivityAt: time.Now()},
		{ID: 2, Name: "billing", PathWithNamespace: "acme/billing", DefaultBranch: "main", LastActivityAt: time.Now()},
	}
	mrs := []gitlab.MergeRequest{
		{IID: 7, ProjectID: 1, ProjectPath: "acme/gateway", Title: "Rate limiting", SourceBranch: "feat/rate", TargetBranch: "main", UpdatedAt: time.Now()},
		{IID: 9, ProjectID: 2, ProjectPath: "acme/billing", Title: "Invoice rounding", SourceBranch: "fix/round", TargetBranch: "main", UpdatedAt: time.Now()},
	}
	must(t, index.Save(config.IndexPath("projects"), index.Projects{UpdatedAt: time.Now(), Items: projects}))
	must(t, index.Save(config.IndexPath("mrs"), index.MergeRequests{UpdatedAt: time.Now(), Items: mrs}))
	must(t, index.Save(config.IndexPath("groups"), index.Groups{UpdatedAt: time.Now(),
		Items: []gitlab.Group{{ID: 1, FullPath: "acme", Name: "acme"}}}))
	return cfg
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func startApp(t *testing.T, a *App) (*App, tcell.SimulationScreen) {
	t.Helper()
	// Attach the simulation screen from this goroutine: SetScreen initialises
	// it, and the test reads its contents from here too.
	sc := tcell.NewSimulationScreen("UTF-8")
	a.tv.SetScreen(sc)
	sc.SetSize(160, 44)

	go func() { _ = a.Run() }()
	t.Cleanup(func() { a.tv.Stop() })
	return a, sc
}

// screenText renders the simulation screen into a string. The read is queued
// onto the tview event loop because tcell's simulation screen does not
// synchronise GetContents against its own drawing.
func (a *App) screenText(sc tcell.SimulationScreen) string {
	done := make(chan string, 1)
	a.tv.QueueUpdate(func() { done <- dumpScreen(sc) })
	select {
	case s := <-done:
		return s
	case <-time.After(2 * time.Second):
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

// waitFor polls the screen until it contains want.
func waitFor(t *testing.T, a *App, sc tcell.SimulationScreen, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(a.screenText(sc), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never contained %q:\n%s", want, a.screenText(sc))
}

func waitGone(t *testing.T, a *App, sc tcell.SimulationScreen, gone string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !strings.Contains(a.screenText(sc), gone) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen still contained %q:\n%s", gone, a.screenText(sc))
}

// resize changes the terminal size. tcell's simulation screen resizes its
// buffers but does not announce it, so the event is posted by hand.
func resize(sc tcell.SimulationScreen, w, h int) {
	sc.SetSize(w, h)
	_ = sc.PostEvent(tcell.NewEventResize(w, h))
}

func typeRunes(sc tcell.SimulationScreen, s string) {
	for _, r := range s {
		sc.InjectKey(tcell.KeyRune, r, tcell.ModNone)
		time.Sleep(10 * time.Millisecond)
	}
}

// ------------------------------------------------------------------- tests

func TestStartsOnTheProjectList(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "Projects [P]")
	waitFor(t, a, sc, "Merge requests [M]")
	waitFor(t, a, sc, "Settings [S]")
	waitFor(t, a, sc, "acme/gateway")
	waitFor(t, a, sc, "acme/billing")
	waitFor(t, a, sc, "? help")
}

func TestTabKeysSwitchViews(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, "!7")
	if a.currentTab() != pageMRs {
		t.Fatalf("tab = %q", a.currentTab())
	}

	typeRunes(sc, "S")
	waitFor(t, a, sc, "Configuration")
	if a.currentTab() != pageSettings {
		t.Fatalf("tab = %q", a.currentTab())
	}

	typeRunes(sc, "P")
	waitFor(t, a, sc, "acme/billing")
	if a.currentTab() != pageProjects {
		t.Fatalf("tab = %q", a.currentTab())
	}
}

func TestFuzzyFilterNarrowsTheList(t *testing.T) {
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
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Edge router")
	waitFor(t, a, sc, "PROJECT")
	waitFor(t, a, sc, "Add rate limiting") // last commits
	waitFor(t, a, sc, "LANGUAGES")
	waitFor(t, a, sc, "87.3%")
	waitFor(t, a, sc, "success") // latest pipeline
	waitFor(t, a, sc, "MIT")
	waitFor(t, a, sc, "ON DISK")
	waitFor(t, a, sc, "not cloned")

	// Focus moved into the detail column.
	waitFor(t, a, sc, "DETAIL")
	if !a.projectsPane.detailFocused {
		t.Error("focus did not move into the detail column")
	}

	// Esc returns to the list, a second Esc closes the column.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Edge router")
}

func TestMergeRequestDetailPane(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "M")
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
	// System notes stay out of the comment list.
	if strings.Contains(a.screenText(sc), "changed title") {
		t.Error("a system note leaked into the comments")
	}
}

func TestHelpOpensAndCloses(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "?")
	waitFor(t, a, sc, "unagit - keys")
	waitFor(t, a, sc, "Tabs")
	waitFor(t, a, sc, "clone or update, then open the editor")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "unagit - keys")
}

func TestSettingsShowsTheGroupTree(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "S")
	waitFor(t, a, sc, "Configuration")
	waitFor(t, a, sc, "✓ acme")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/billing")
}

func TestProjectFilterOnTheMergeRequestList(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")

	typeRunes(sc, "/bill")
	waitFor(t, a, sc, "acme/billing")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // leave filter mode, keep selection
	typeRunes(sc, "m")

	waitFor(t, a, sc, "Invoice rounding")
	waitGone(t, a, sc, "Rate limiting")
	if a.mrProjectScope != "acme/billing" {
		t.Errorf("scope = %q", a.mrProjectScope)
	}

	typeRunes(sc, "F")
	waitFor(t, a, sc, "Rate limiting")
}

func TestDeleteIsRefusedWhenNothingIsOnDisk(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "d")
	waitFor(t, a, sc, "is not on disk")
	waitGone(t, a, sc, "Delete project")
}

func TestBranchPickerListsBranches(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branch - acme/gateway")
	waitFor(t, a, sc, "feat/rate")
	waitFor(t, a, sc, "default")
	waitFor(t, a, sc, "Token bucket")

	// The picker filters too.
	waitFor(t, a, sc, "FILTER")
	typeRunes(sc, "feat")
	waitGone(t, a, sc, "Add rate limiting")
	waitFor(t, a, sc, "feat/rate")

	// The first Esc leaves the input so j/k drive the selection, the second
	// one closes the modal.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")
	waitFor(t, a, sc, "j/k move")
	if strings.Contains(a.screenText(sc), "Add rate limiting") {
		t.Error("leaving the input dropped the filter")
	}
	typeRunes(sc, "j") // must move the selection, not type into the filter
	waitFor(t, a, sc, "feat/rate")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Branch - acme/gateway")
}

func TestPickerNavigatesWithJK(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	typeRunes(sc, "f") // limit merge requests to a project
	waitFor(t, a, sc, "Limit merge requests to project")
	waitFor(t, a, sc, "(all projects)")

	// Leave the input, move down twice, pick the highlighted project.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")
	typeRunes(sc, "jj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitGone(t, a, sc, "Limit merge requests to project")
	if a.mrProjectScope != "acme/gateway" {
		t.Errorf("scope = %q, want acme/gateway (third entry in the picker)", a.mrProjectScope)
	}
}
