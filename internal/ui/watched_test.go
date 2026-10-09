package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/watch"
)

// watchServer is a GitLab with one merge request, !7 of acme/gateway, whose
// pipeline's status a test sets.
type watchServer struct {
	*httptest.Server
	mu       sync.Mutex
	status   string
	pipeline int
	sha      string
	state    string
	asked    atomic.Int64
	// details counts the merge request's full reads; print, when set, is
	// what GraphQL answers as its fingerprint (else GraphQL is a 404).
	details   atomic.Int64
	print     string
	comments  int
	approvers []string
	commits   int
	// remaining, when set, is what the rate limit headers say is left of
	// a hundred, resetting in ten minutes.
	remaining int
	// branches are the branches the server has.
	branches map[string]bool
}

func (s *watchServer) set(status string, pipeline int, sha string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.pipeline, s.sha = status, pipeline, sha
}

func newWatchServer(t *testing.T) *watchServer {
	t.Helper()
	s := &watchServer{status: "running", pipeline: 90, sha: "aaaa1111", state: "opened"}
	mux := http.NewServeMux()
	json := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7", func(w http.ResponseWriter, r *http.Request) {
		s.details.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		json(w, fmt.Sprintf(`{"iid":7,"title":"Rate limiting","state":%q,"sha":%q,"source_branch":"feat/rate",
			"target_branch":"main","project_id":1,"user_notes_count":%d,"web_url":"https://gl.test/acme/gateway/-/merge_requests/7"}`,
			s.state, s.sha, s.comments))
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/approvals", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		var by []string
		for _, u := range s.approvers {
			by = append(by, fmt.Sprintf(`{"user":{"username":%q}}`, u))
		}
		json(w, `{"approved_by":[`+strings.Join(by, ",")+`]}`)
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/commits", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("X-Total", fmt.Sprint(s.commits))
		json(w, `[]`)
	})
	mux.HandleFunc("/api/graphql", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.print == "" {
			http.NotFound(w, r)
			return
		}
		json(w, fmt.Sprintf(`{"data":{"w0":{"print":%q}}}`, s.print))
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines", func(w http.ResponseWriter, r *http.Request) {
		s.asked.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		json(w, fmt.Sprintf(`[{"id":%d,"status":%q,"sha":%q,"ref":"main"}]`, s.pipeline, s.status, s.sha))
	})
	mux.HandleFunc("/api/v4/projects/1/repository/branches/{branch}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.branches != nil && !s.branches[r.PathValue("branch")] {
			http.NotFound(w, r)
			return
		}
		json(w, fmt.Sprintf(`{"name":%q}`, r.PathValue("branch")))
	})
	mux.HandleFunc("/api/v4/projects/1/merge_requests/7/pipelines", func(w http.ResponseWriter, r *http.Request) {
		s.asked.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		json(w, fmt.Sprintf(`[{"id":%d,"status":%q,"sha":%q,"web_url":"https://gl.test/p/%d","user":{"username":"jane"}}]`,
			s.pipeline, s.status, s.sha, s.pipeline))
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines/{id}/jobs", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[{"id":5,"name":"unit tests","stage":"test","status":"failed"}]`)
	})
	mux.HandleFunc("/api/v4/projects/1/pipelines/{id}/bridges", func(w http.ResponseWriter, r *http.Request) {
		json(w, `[]`)
	})
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		left := s.remaining
		s.mu.Unlock()
		if left > 0 {
			w.Header().Set("RateLimit-Limit", "100")
			w.Header().Set("RateLimit-Remaining", fmt.Sprint(left))
			w.Header().Set("RateLimit-Reset", fmt.Sprint(time.Now().Add(10*time.Minute).Unix()))
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// watchApp starts a unagit on a configuration directory, following the
// watches quickly and counting its notifications. Its terminal is not in
// front, so a change is notified.
func watchApp(t *testing.T, cfg *config.Config, notified *atomic.Int64) (*App, tcell.SimulationScreen, chan struct{}) {
	t.Helper()
	app := newApp(cfg, testVault(t, cfg))
	app.ciAskEvery = 40 * time.Millisecond
	app.watchIdleEvery = 40 * time.Millisecond
	app.watchLookEvery = 20 * time.Millisecond
	app.notifier = func(title, body string) { notified.Add(1) }
	a, sc, stopped := startAppWithStop(t, app)
	a.quiet.focused.Store(false)
	a.quiet.focusKnown.Store(true)
	return a, sc, stopped
}

func watchMR7(t *testing.T, cfg *config.Config) watch.Watch {
	t.Helper()
	w := watch.Watch{Kind: watch.KindPipeline, Instance: cfg.Instances[0].ID, Project: "acme/gateway", ProjectID: 1, IID: 7, Since: time.Now()}
	if _, err := watch.Open(cfg.WatchDir()).Change(func(ws []watch.Watch) []watch.Watch { return append(ws, w) }); err != nil {
		t.Fatal(err)
	}
	return w
}

// waitState waits for the poller to have written a watch's status.
func waitState(t *testing.T, cfg *config.Config, key, status string) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		snap, _ := watch.Open(cfg.WatchDir()).State()
		if snap.States[key].Status == status {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap, _ := watch.Open(cfg.WatchDir()).State()
	t.Fatalf("the watch never read %q: %+v", status, snap.States[key])
}

func waitTrue(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(what)
}

// TestTwoUnagitsFollowAWatchOnce: of two instances on one configuration
// only one asks the server; a failure is said in both, notified once, and
// never in a box; when the poller exits the other carries on without
// notifying the same change again.
func TestTwoUnagitsFollowAWatchOnce(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	other, err := config.LoadFrom(cfg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	other.RootDir = cfg.RootDir
	var notified atomic.Int64
	a, sc, stoppedA := watchApp(t, cfg, &notified)
	b, scB, _ := watchApp(t, other, &notified)

	waitState(t, cfg, w.Key(), "running")
	polling := func() (bool, bool) { return a.watchPolling.Load(), b.watchPolling.Load() }
	if pa, pb := polling(); pa == pb {
		t.Fatalf("polling: a %v, b %v - exactly one should", pa, pb)
	}
	waitFor(t, a, sc, "[4] Activity")
	// Each said it was in front at its start, before the test turned its
	// terminal away; a notification waits for both to have said it is not.
	waitTrue(t, "an instance still says it is in front", func() bool {
		ps := watch.Open(cfg.WatchDir()).Presences()
		return len(ps) == 2 && !ps[0].Focused && !ps[1].Focused
	})
	if n := notified.Load(); n != 0 {
		t.Fatalf("the first reading was notified %d times", n)
	}

	srv.set("failed", 90, "aaaa1111")
	waitFor(t, a, sc, "failed in unit tests")
	waitFor(t, b, scB, "failed in unit tests")
	waitTrue(t, "the failure was never notified", func() bool { return notified.Load() >= 1 })
	if n := notified.Load(); n != 1 {
		t.Fatalf("the failure was notified %d times, want once", n)
	}
	waitTrue(t, "the tab does not count the change", func() bool { return tabBadge(a, sc, onLoop(a, func() string { return glyphDot })+"1") })
	for _, app := range []*App{a, b} {
		if front := onLoop(app, func() string { name, _ := app.pages.GetFrontPage(); return name }); front == pageMessage {
			t.Fatal("a background failure opened a message box")
		}
	}

	// The poller goes; the other takes over from what it left.
	survivor, survivorScreen := b, scB
	if a.watchPolling.Load() {
		a.tv.Stop()
		<-stoppedA
	} else {
		survivor, survivorScreen = a, sc
		b.tv.Stop()
	}
	waitTrue(t, "nobody took over the polling", survivor.watchPolling.Load)
	asked := srv.asked.Load()
	waitTrue(t, "the new poller does not ask", func() bool { return srv.asked.Load() > asked+1 })
	if n := notified.Load(); n != 1 {
		t.Fatalf("taking over notified the failure again: %d", n)
	}
	srv.set("success", 91, "bbbb2222")
	waitFor(t, survivor, survivorScreen, "Pipeline passed")
	// A new head is news of its own, besides the pipeline's.
	waitTrue(t, "the success was never notified", func() bool { return notified.Load() >= 2 })
}

// TestAWatchStartsFromTheListAndStopsOnItsScreen: Watch Pipelines on a
// merge request marks its row, the Watched tab lists it, and x there lets
// it go and takes the mark away.
func TestAWatchStartsFromTheListAndStopsOnItsScreen(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Watch Pipelines")
	typeRunes(sc, "watch pip")
	waitFor(t, a, sc, "Follow its pipelines")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "watching the pipelines of acme/gateway !7")
	mark := onLoop(a, func() string { return glyphWatched })
	waitTrue(t, "the merge request's row has no watched mark", func() bool {
		for _, line := range strings.Split(a.screenText(sc), "\n") {
			if strings.Contains(line, "Rate limiting") {
				return strings.Contains(line, mark)
			}
		}
		return false
	})

	typeRunes(sc, "4")
	waitFor(t, a, sc, "acme/gateway !7")
	waitFor(t, a, sc, "running")
	typeRunes(sc, "x")
	waitFor(t, a, sc, "stopped watching acme/gateway !7")
	waitFor(t, a, sc, "Nothing under way")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	if text := a.screenText(sc); strings.Contains(text, mark) {
		t.Fatalf("the mark stayed after the watch went:\n%s", text)
	}
	if ws, _ := watch.Open(cfg.WatchDir()).Watches(); len(ws) != 0 {
		t.Fatalf("watches.json still holds %+v", ws)
	}
}

// TestAMergedMergeRequestLetsItsWatchGo: once merged, the watch says so
// and is gone.
func TestAMergedMergeRequestLetsItsWatchGo(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	srv.mu.Lock()
	srv.state = "merged"
	srv.mu.Unlock()
	waitFor(t, a, sc, "was merged · no longer watched")
	waitTrue(t, "the watch of a merged merge request stayed", func() bool {
		ws, _ := watch.Open(cfg.WatchDir()).Watches()
		return len(ws) == 0
	})
}

// TestTheWatchedTabFits: the rows and the frame at several sizes, legible.
func TestTheWatchedTabFits(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	srv.set("failed", 90, "aaaa1111")
	waitState(t, cfg, w.Key(), "failed")
	waitFor(t, a, sc, "Pipeline failed")
	typeRunes(sc, "4")
	waitFor(t, a, sc, "failed · unit tests")
	assertLegible(t, a, sc, "Activity under a toast")
	// The toast stands over the rows' ends; they are measured without it.
	onLoop(a, func() bool { a.toasts = nil; return true })
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "acme/gateway !7")
		text := a.screenText(sc)
		t.Logf("Watched at %dx%d:\n%s", size.w, size.h, text)
		for _, line := range strings.Split(text, "\n") {
			if end := strings.TrimRight(line, " "); strings.Contains(line, "acme/gateway !7") && !strings.HasSuffix(end, "│") && !strings.HasSuffix(end, "╮") {
				t.Fatalf("row over its frame at %d: %q", size.w, line)
			}
		}
		assertLegible(t, a, sc, "Watched tab")
	}
	// Opened, the change is seen: the tab no longer counts it.
	dot := onLoop(a, func() string { return glyphDot })
	waitTrue(t, "the tab still counts the change seen", func() bool { return !tabBadge(a, sc, dot+"1") })
}

func TestPipelineChanges(t *testing.T) {
	t.Parallel()
	w := watch.Watch{Kind: watch.KindPipeline, Instance: "gl", Project: "acme/api", IID: 42}
	running := watch.State{Pipeline: 1, Status: "running", SHA: "a1"}
	failed := watch.State{Pipeline: 1, Status: "failed", SHA: "a1", Failed: "lint"}
	lines := func(evs []watch.Event) string {
		var out []string
		for _, e := range evs {
			out = append(out, e.Heading+": "+e.Line+" ("+string(e.Level)+")")
		}
		return strings.Join(out, " | ")
	}
	active := func(st watch.State, head string, commits, comments int, approvers ...string) watch.State {
		st.Head, st.Commits, st.Comments, st.Approvers, st.Known = head, commits, comments, approvers, true
		return st
	}
	behind := func(st watch.State, base string, n int) watch.State {
		st.Base, st.Behind = base, n
		return st
	}
	cases := []struct {
		name          string
		before, after watch.State
		known         bool
		ended         string
		want          string
	}{
		{"the first reading is not news", watch.State{}, failed, false, "", ""},
		{"nothing changed", running, running, true, "", ""},
		{"it failed", running, failed, true, "", "Pipeline failed: Pipeline #1 of !42 failed in lint (danger)"},
		{"it passed", running, watch.State{Pipeline: 1, Status: "success", SHA: "a1"}, true, "", "Pipeline passed: Pipeline #1 of !42 passed (success)"},
		{"another began on the same head", watch.State{Pipeline: 1, Status: "success", SHA: "a1"}, watch.State{Pipeline: 2, Status: "running", SHA: "a1"}, true, "", "Pipeline running: Pipeline #2 of !42 started for a1 (info)"},
		{"pending began to run", watch.State{Pipeline: 1, Status: "pending", SHA: "a1"}, running, true, "", ""},
		{"it waits for a hand", running, watch.State{Pipeline: 1, Status: "manual", SHA: "a1"}, true, "", "Manual job waiting: Pipeline #1 of !42 waits for a manual job to be started (warning)"},
		{"cancelled", running, watch.State{Pipeline: 1, Status: "canceled", SHA: "a1"}, true, "", "Pipeline cancelled: Pipeline #1 of !42 was cancelled (warning)"},
		{"merged", running, running, true, "merge request merged", "MR merged: !42 was merged · no longer watched (success)"},
		{"closed", running, running, true, "merge request closed", "MR closed: !42 was closed without merging · no longer watched (warning)"},
		{"the branch went", running, running, true, "branch deleted on origin", "Branch deleted: !42 was deleted on origin · no longer watched (warning)"},
		{"a push started another", active(failed, "a1", 3, 0), active(watch.State{Pipeline: 2, Status: "pending", SHA: "b2"}, "b2", 5, 0), true, "",
			"New commits in MR: !42 has 2 new commits (info) | Pipeline running: Pipeline #2 of !42 started for b2 (info)"},
		{"a force push", active(running, "a1", 3, 0), active(running, "c3", 3, 0), true, "", "MR force-pushed: !42 was rewritten, its head now c3 (warning)"},
		{"comments", active(running, "a1", 3, 1), active(running, "a1", 3, 2), true, "", "New comment in MR: !42 has a new comment (info)"},
		{"approved", active(running, "a1", 3, 0, "jane"), active(running, "a1", 3, 0, "jane", "john"), true, "", "MR approved: !42 was approved by john (success)"},
		{"withdrawn", active(running, "a1", 3, 0, "jane"), active(running, "a1", 3, 0), true, "", "Approval withdrawn: jane withdrew the approval of !42 (warning)"},
		{"activity first read", running, active(running, "a1", 3, 2, "jane"), true, "", ""},
		{"the base moved on", behind(running, "main", 0), behind(running, "main", 3), true, "", "Base moved on: !42 is 3 commits behind main (info)"},
		{"caught up", behind(running, "main", 3), behind(running, "main", 0), true, "", ""},
		{"the base first read", running, behind(running, "main", 3), true, "", ""},
	}
	for _, c := range cases {
		if got := lines(watchChanges(w, c.before, c.after, c.known, c.ended)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestTheJobsWatchTheirPipelines: the jobs of a merge request list Watch
// Pipelines among their actions, with no key of its own in the hint, and
// once watched it is Stop Watching Pipelines.
func TestTheJobsWatchTheirPipelines(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	srv.set("failed", 90, "aaaa1111")
	cfg := writeTestConfig(t, srv.URL)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "J")
	waitFor(t, a, sc, "unit tests")
	if text := a.screenText(sc); strings.Contains(text, " watch") {
		t.Fatalf("a key-less action is in the hint:\n%s", text)
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Watch Pipelines")
	typeRunes(sc, "watch pip")
	waitFor(t, a, sc, "Follow its pipelines")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "watching the pipelines of")
	waitFor(t, a, sc, "unit tests")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Stop Watching Pipelines")
}

// TestAWatchMovesTheListsCIColumn: what a watch reads is the merge request
// list's CI column too, without a refresh, and the tab turns a mark while a
// watched pipeline runs.
func TestAWatchMovesTheListsCIColumn(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	pipeline := func() string {
		return onLoop(a, func() string {
			for _, mr := range a.mrs {
				if mr.IID == 7 {
					return mr.Pipeline
				}
			}
			return "no !7"
		})
	}
	waitTrue(t, "the list never took the running pipeline", func() bool { return pipeline() == "running" })
	if n := onLoop(a, a.watchesRunning); n != 1 {
		t.Fatalf("%d watches running, want 1", n)
	}
	waitTrue(t, "the tab does not count the running pipeline", func() bool {
		return strings.Contains(a.screenText(sc), "Activity ") && onLoop(a, func() bool { return a.ciWatching })
	})
	srv.set("failed", 90, "aaaa1111")
	waitTrue(t, "the list never took the failure", func() bool { return pipeline() == "failed" })
	waitFor(t, a, sc, "failed in unit tests")
	waitTrue(t, "the marks still turn with nothing running", func() bool { return !onLoop(a, func() bool { return a.ciWatching }) })
}

// TestAPipelineStartedOnTheServerIsNotified: a watched merge request whose
// pipeline has ended gets a new one, started on the server - that start is
// news and is notified, as its end is.
func TestAPipelineStartedOnTheServerIsNotified(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	srv.set("success", 90, "aaaa1111")
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	var mu sync.Mutex
	var bodies []string
	app := newApp(cfg, testVault(t, cfg))
	app.ciAskEvery = 40 * time.Millisecond
	app.watchIdleEvery = 40 * time.Millisecond
	app.watchLookEvery = 20 * time.Millisecond
	app.notifier = func(title, body string) {
		mu.Lock()
		bodies = append(bodies, title)
		mu.Unlock()
		notified.Add(1)
	}
	a, sc, _ := startAppWithStop(t, app)
	changeOnLoop(a, func() { a.quiet.focused.Store(false); a.quiet.focusKnown.Store(true) })
	waitState(t, cfg, w.Key(), "success")
	waitTrue(t, "the instance still says it is in front", func() bool {
		ps := watch.Open(cfg.WatchDir()).Presences()
		return len(ps) == 1 && !ps[0].Focused
	})

	srv.set("running", 91, "aaaa1111") // Run pipeline, on the server
	waitTrue(t, "the start was never notified", func() bool { return notified.Load() >= 1 })
	waitFor(t, a, sc, "Pipeline running")
	srv.set("success", 91, "aaaa1111")
	waitTrue(t, "the end was never notified", func() bool { return notified.Load() >= 2 })
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(bodies, " | ") != "Pipeline running | Pipeline passed" {
		t.Fatalf("notified %q", bodies)
	}
	// Each says what it is about, and what became of it is in notify.log.
	snap, _ := watch.Open(cfg.WatchDir()).State()
	if e := snap.Events[len(snap.Events)-1]; e.Title != "Rate limiting" {
		t.Errorf("the news is not about the merge request: %+v", e)
	}
	log, _ := os.ReadFile(filepath.Join(cfg.WatchDir(), "notify.log"))
	if !strings.Contains(string(log), "Pipeline #91 of !7 started") || !strings.Contains(string(log), "->  sent through the notifier") {
		t.Errorf("notify.log says:\n%s", log)
	}
}

// tabBadge reports whether the tab bar shows a count after Activity.
func tabBadge(a *App, sc tcell.SimulationScreen, badge string) bool {
	line, _, _ := strings.Cut(a.screenText(sc), "\n")
	_, after, ok := strings.Cut(line, "Activity")
	return ok && strings.Contains(strings.Split(after, "Settings")[0], badge)
}
