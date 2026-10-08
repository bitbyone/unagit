package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
		s.mu.Lock()
		defer s.mu.Unlock()
		json(w, fmt.Sprintf(`{"iid":7,"title":"Rate limiting","state":%q,"sha":%q,"source_branch":"feat/rate",
			"target_branch":"main","project_id":1,"web_url":"https://gl.test/acme/gateway/-/merge_requests/7"}`, s.state, s.sha))
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
	s.Server = httptest.NewServer(mux)
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
	waitFor(t, a, sc, "[5] Watched")
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
	waitFor(t, a, sc, "pipeline failed · unit tests")
	waitFor(t, b, scB, "pipeline failed · unit tests")
	waitTrue(t, "the failure was never notified", func() bool { return notified.Load() >= 1 })
	if n := notified.Load(); n != 1 {
		t.Fatalf("the failure was notified %d times, want once", n)
	}
	waitFor(t, a, sc, "Watched "+onLoop(a, func() string { return glyphDot })+"1")
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
	waitFor(t, survivor, survivorScreen, "pipeline passed")
	waitTrue(t, "the success was never notified", func() bool { return notified.Load() == 2 })
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

	typeRunes(sc, "5")
	waitFor(t, a, sc, "acme/gateway !7")
	waitFor(t, a, sc, "running")
	typeRunes(sc, "x")
	waitFor(t, a, sc, "stopped watching acme/gateway !7")
	waitFor(t, a, sc, "Nothing watched")
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
	waitFor(t, a, sc, "merge request merged · no longer watched")
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
	waitFor(t, a, sc, "pipeline failed")
	typeRunes(sc, "5")
	waitFor(t, a, sc, "failed · unit tests")
	assertLegible(t, a, sc, "Watched tab under a toast")
	// The toast stands over the rows' ends; they are measured without it.
	onLoop(a, func() bool { a.toasts = nil; return true })
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "acme/gateway !7")
		text := a.screenText(sc)
		t.Logf("Watched at %dx%d:\n%s", size.w, size.h, text)
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "acme/gateway !7") && !strings.HasSuffix(strings.TrimRight(line, " "), "│") {
				t.Fatalf("row over its frame at %d: %q", size.w, line)
			}
		}
		assertLegible(t, a, sc, "Watched tab")
	}
	// Opened, the change is seen: the tab no longer counts it.
	dot := onLoop(a, func() string { return glyphDot })
	waitGone(t, a, sc, "Watched "+dot+"1")
}

func TestPipelineChanges(t *testing.T) {
	t.Parallel()
	w := watch.Watch{Kind: watch.KindPipeline, Instance: "gl", Project: "acme/api", IID: 42}
	running := watch.State{Pipeline: 1, Status: "running", SHA: "a1"}
	failed := watch.State{Pipeline: 1, Status: "failed", SHA: "a1", Failed: "lint"}
	lines := func(evs []watch.Event) string {
		var out []string
		for _, e := range evs {
			out = append(out, e.Line+" ("+string(e.Level)+")")
		}
		return strings.Join(out, " | ")
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
		{"it failed", running, failed, true, "", "pipeline failed · lint (danger)"},
		{"it passed", running, watch.State{Pipeline: 1, Status: "success", SHA: "a1"}, true, "", "pipeline passed (success)"},
		{"a push started another", failed, watch.State{Pipeline: 2, Status: "pending", SHA: "b2"}, true, "", "new head b2 · pipeline started (info)"},
		{"another began on the same head", watch.State{Pipeline: 1, Status: "success", SHA: "a1"}, watch.State{Pipeline: 2, Status: "running", SHA: "a1"}, true, "", "pipeline started (info)"},
		{"pending began to run", watch.State{Pipeline: 1, Status: "pending", SHA: "a1"}, running, true, "", ""},
		{"it waits for a hand", running, watch.State{Pipeline: 1, Status: "manual", SHA: "a1"}, true, "", "pipeline waits for a manual job (warning)"},
		{"cancelled", running, watch.State{Pipeline: 1, Status: "canceled", SHA: "a1"}, true, "", "pipeline cancelled (warning)"},
		{"merged", running, running, true, "merged", "merge request merged · no longer watched (success)"},
		{"closed", running, running, true, "closed", "merge request closed · no longer watched (warning)"},
	}
	for _, c := range cases {
		if got := lines(pipelineChanges(w, c.before, c.after, c.known, c.ended)); got != c.want {
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
		return strings.Contains(a.screenText(sc), "Watched ") && onLoop(a, func() bool { return a.ciWatching })
	})
	srv.set("failed", 90, "aaaa1111")
	waitTrue(t, "the list never took the failure", func() bool { return pipeline() == "failed" })
	waitFor(t, a, sc, "pipeline failed · unit tests")
	waitTrue(t, "the marks still turn with nothing running", func() bool { return !onLoop(a, func() bool { return a.ciWatching }) })
}
