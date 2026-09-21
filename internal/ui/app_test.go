package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/index"
)

// newTestApp writes index files into a temporary config directory and starts
// the TUI on a simulation screen.
func newTestApp(t *testing.T) (*App, tcell.SimulationScreen) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("UNAGIT_CONFIG_DIR", dir)

	cfg := config.Default()
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
		{IID: 7, ProjectID: 1, ProjectPath: "acme/gateway", Title: "Rate limiting", SourceBranch: "feat/rate", UpdatedAt: time.Now()},
		{IID: 9, ProjectID: 2, ProjectPath: "acme/billing", Title: "Invoice rounding", SourceBranch: "fix/round", UpdatedAt: time.Now()},
	}
	if err := index.Save(config.IndexPath("projects"), index.Projects{UpdatedAt: time.Now(), Items: projects}); err != nil {
		t.Fatal(err)
	}
	if err := index.Save(config.IndexPath("mrs"), index.MergeRequests{UpdatedAt: time.Now(), Items: mrs}); err != nil {
		t.Fatal(err)
	}
	if err := index.Save(config.IndexPath("groups"), index.Groups{UpdatedAt: time.Now(), Items: []gitlab.Group{{ID: 1, FullPath: "acme", Name: "acme"}}}); err != nil {
		t.Fatal(err)
	}

	a := New(cfg, "test-token")
	// Attach the simulation screen from this goroutine: SetScreen initialises
	// it, and the test reads its contents from here too.
	sc := tcell.NewSimulationScreen("UTF-8")
	a.tv.SetScreen(sc)

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

func typeRunes(sc tcell.SimulationScreen, s string) {
	for _, r := range s {
		sc.InjectKey(tcell.KeyRune, r, tcell.ModNone)
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartsOnTheProjectList(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "PROJECTS")
	waitFor(t, a, sc, "acme/gateway")
	waitFor(t, a, sc, "acme/billing")
	waitFor(t, a, sc, "? help")
}

func TestTabSwitchesToMergeRequests(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitFor(t, a, sc, "MERGE REQUESTS")
	waitFor(t, a, sc, "Rate limiting")
	waitFor(t, a, sc, "!7")

	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitFor(t, a, sc, "PROJECTS")
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

func TestHelpOpensAndCloses(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "?")
	waitFor(t, a, sc, "unagit - keys")
	waitFor(t, a, sc, "Navigation")
	waitFor(t, a, sc, "clone if missing")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "unagit - keys")
}

func TestSettingsShowsTheGroupTree(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "s")
	waitFor(t, a, sc, "Configuration")
	waitFor(t, a, sc, "✓ acme")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "PROJECTS")
}

func TestProjectScopeFromTheProjectList(t *testing.T) {
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

	typeRunes(sc, "P")
	waitFor(t, a, sc, "Rate limiting")
}

func TestDeleteIsRefusedWhenNothingIsOnDisk(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "d")
	waitFor(t, a, sc, "is not on disk")
	waitGone(t, a, sc, "Delete project")
}
