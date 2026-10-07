package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/workspace"
)

func fakeZoxide(t *testing.T) (log, scores string, prepare func(*App)) {
	t.Helper()
	dir := t.TempDir()
	log, scores = filepath.Join(dir, "calls"), filepath.Join(dir, "scores")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	bin := filepath.Join(dir, "zoxide")
	script := "#!/bin/sh\nprintf '%s\\t' \"$@\" >> " + quote(log) + "\nprintf '\\n' >> " + quote(log) + "\nif [ \"$1\" = query ]; then cat " + quote(scores) + "; fi\n"
	must(t, os.WriteFile(bin, []byte(script), 0o755))
	must(t, os.WriteFile(scores, nil, 0o644))
	must(t, os.WriteFile(log, nil, 0o644))
	return log, scores, func(a *App) {
		a.findExecutable = func(name string) (string, error) {
			if name == "zoxide" {
				return bin, nil
			}
			return exec.LookPath(name)
		}
	}
}

func visitCalls(t *testing.T, log, action, dir string) int {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), action+"\t--\t"+dir+"\t\n")
}

func TestZoxideVisitsAndRemoval(t *testing.T) {
	t.Parallel()
	log, _, prepare := fakeZoxide(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	project := newRealProject(t, a, "acme/gateway")
	project.rescan()
	if got := visitCalls(t, log, "add", project.clone); got != 0 {
		t.Fatal("creating a clone recorded a visit")
	}
	// A window editor exercises openEditor without taking the test's terminal.
	var ed editors.Editor
	for _, e := range editors.Detect(editors.CustomSpec{Command: "/usr/bin/true", Terminal: false}) {
		if e.ID == editors.Custom {
			ed = e
		}
	}
	ed.Name = "test"
	changeOnLoop(a, func() { a.openNow(project.clone, session.Record{Project: project.path}, &ed) })
	waitFor(t, a, sc, "opened in test")
	waitEditorIdle(t, a)
	if got := visitCalls(t, log, "add", project.clone); got != 1 {
		t.Fatalf("opened clone added %d times", got)
	}
	review := onLoop(a, func() string { return a.reviewDir(a.cfg.Instances[0].ID, project.path, 7, "feat/rate") })
	gitIn(t, project.clone, "worktree", "add", "-q", "--detach", review)
	manager := onLoop(a, func() *workspace.Manager { return a.newManager(a.cfg.Instances[0].ID, project.path, nil) })
	must(t, manager.RemoveWorktreeDir(project.path, review))
	if got := visitCalls(t, log, "remove", review); got != 1 {
		t.Fatalf("removed review forgotten %d times", got)
	}
	changeOnLoop(a, func() {
		off := false
		a.cfg.Integrations.Zoxide = &off
		a.refreshZoxide()
		a.clearSaid()
		a.openNow(project.clone, session.Record{Project: project.path}, &ed)
	})
	waitEditorIdle(t, a)
	// Wait for the window launcher's result after the worker has returned.
	waitFor(t, a, sc, "opened in test")
	if got := visitCalls(t, log, "add", project.clone); got != 1 {
		t.Fatalf("disabled integration added another visit: %d", got)
	}
}

func TestZoxideScoresOrderRepositoriesAndWorktrees(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a := &App{cfg: &config.Config{RootDir: "/root"}, zoxideScores: map[string]float64{
		"/root/acme/a": 1, "/root/acme/.unagit/a/review-7-x": 90,
		"/root/acme/.unagit/ab/wt-other":           900,
		"/root/acme/.unagit/a/review-7-x/internal": 5000,
		"/root/acme/b": 30, "/root/acme/z": 0,
		"/root/acme/a.reviews/legacy": 91,
	}}
	projects := []forge.Project{
		{PathWithNamespace: "acme/unknown-old", LastActivityAt: now.Add(-time.Hour)},
		{PathWithNamespace: "acme/b", LastActivityAt: now},
		{PathWithNamespace: "acme/a", LastActivityAt: now.Add(-2 * time.Hour)},
		{PathWithNamespace: "acme/unknown-new", LastActivityAt: now.Add(time.Hour)},
		{PathWithNamespace: "acme/z", LastActivityAt: now.Add(-3 * time.Hour)},
	}
	a.cfg.Filters.SetOrder(config.ListRepositories, config.SortFrecency)
	hits := allHits(len(projects))
	a.sortProjects(hits, projects)
	sameOrder(t, "repository visits", order(hits, func(i int) string { return projects[i].PathWithNamespace }),
		[]string{"acme/a", "acme/b", "acme/z", "acme/unknown-new", "acme/unknown-old"})
	if n, _ := a.repositoryScore(projectKey{Path: "acme/a"}); n != 91 {
		t.Fatalf("repository inherited a sibling's visits: %v", n)
	}
	a.worktrees = []worktreeRow{
		{Dir: "/unknown-old", Moved: now.Add(-time.Hour)},
		{Dir: "/root/acme/.unagit/a/review-7-x", Moved: now},
		{Dir: "/unknown-new", Moved: now},
		{Dir: "/root/acme/.unagit/ab/wt-other", Moved: now},
	}
	a.cfg.Filters.SetOrder(config.ListWorktrees, config.SortFrecency)
	hits = allHits(len(a.worktrees))
	a.sortWorktrees(hits)
	sameOrder(t, "worktree visits", order(hits, func(i int) string { return a.worktrees[i].Dir }),
		[]string{"/root/acme/.unagit/ab/wt-other", "/root/acme/.unagit/a/review-7-x", "/unknown-new", "/unknown-old"})
}

func TestZoxideCardAndSortPicker(t *testing.T) {
	t.Parallel()
	_, scores, prepare := fakeZoxide(t)
	a, sc, _ := newTestAppSrv(t, prepare)
	waitFor(t, a, sc, "acme/gateway")
	root := onLoop(a, func() string { return a.cfg.Root() })
	must(t, os.WriteFile(scores, []byte("4 "+filepath.Join(root, "a path")+"\n9 /elsewhere\n"), 0o644))

	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		resizeApp(a, sc, size.w, size.h)
		openSection(t, a, sc, sectionIntegrations)
		changeOnLoop(a, func() {
			v := a.settings.integrations
			v.current = len(v.cards) - 1
			a.tv.SetFocus(v)
			v.paintFocus(true)
		})
		waitFor(t, a, sc, "1 directory known under your roots")
		text := a.screenText(sc)
		for _, want := range []string{"Zoxide", "Remember directories", "e toggle", "c check"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing at %dx%d:\n%s", want, size.w, size.h, text)
			}
		}
		t.Logf("Zoxide at %dx%d:\n%s", size.w, size.h, text)
		lines := strings.Split(text, "\n")
		if !strings.Contains(lines[len(lines)-3], "╰") {
			t.Fatalf("integration drew over the bottom border:\n%s", text)
		}
		assertLegible(t, a, sc, "Zoxide integration")
	}
	typeRunes(sc, "e")
	waitFor(t, a, sc, "disabled")
	saved, err := config.LoadFrom(onLoop(a, func() string { return a.cfg.Dir() }))
	must(t, err)
	if saved.Integrations.Zoxide == nil || *saved.Integrations.Zoxide {
		t.Fatal("disabled choice was not saved")
	}
	typeRunes(sc, "e")
	waitFor(t, a, sc, "1 directory known under your roots")
	typeRunes(sc, "1")
	changeOnLoop(a, func() { a.showSortPicker() })
	waitFor(t, a, sc, "by frecency")
	assertLegible(t, a, sc, "frecency order picker")
	typeRunes(sc, "jjjjj")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "by frecency")
	waitEditorState(t, a, func() bool {
		return a.cfg.Filters.Order(config.ListRepositories) == config.SortFrecency && !a.modalOpen()
	})
	resizeApp(a, sc, 160, 44)
	typeRunes(sc, "?")
	waitFor(t, a, sc, "keys · Repositories")
	t.Logf("Repositories help:\n%s", a.screenText(sc))
	assertLegible(t, a, sc, "help with zoxide order")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "keys · Repositories")
	typeRunes(sc, "3?")
	waitFor(t, a, sc, "keys · Worktrees")
	waitFor(t, a, sc, "or frecency")
	t.Logf("Worktrees help:\n%s", a.screenText(sc))
	assertLegible(t, a, sc, "worktree help with zoxide order")
}

func TestPlacesUseZoxideScores(t *testing.T) {
	log, scores, prepare := fakeZoxide(t)
	cfg := writeTestConfig(t, "http://unused.test")
	off := false
	cfg.Integrations.Chezmoi = &off
	a := &App{cfg: cfg}
	prepare(a)
	bin, _ := a.executable("zoxide")
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, path := range []string{"acme/gateway", "acme/billing"} {
		dir := a.projectDir(cfg.Instances[0].ID, path)
		must(t, os.MkdirAll(dir, 0o755))
		gitIn(t, dir, "init", "-q", "--initial-branch=main")
	}
	billing := a.projectDir(cfg.Instances[0].ID, "acme/billing")
	gateway := a.projectDir(cfg.Instances[0].ID, "acme/gateway")
	must(t, os.WriteFile(scores, []byte(fmt.Sprintf("90 %s\n2 %s\n", gateway, billing)), 0o644))
	if got := Places(cfg); len(got) != 2 || got[0].Dir != gateway {
		t.Fatalf("places = %v", got)
	}
	cfg.Integrations.Zoxide = &off
	if got := Places(cfg); len(got) != 2 || got[0].Dir != billing {
		t.Fatalf("disabled places = %v", got)
	}
	if got := visitCalls(t, log, "add", gateway); got != 0 {
		t.Fatal("listing places recorded a visit")
	}
}
