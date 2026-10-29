package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
)

// soloFixture is a repository unagit never cloned: a bare origin, a clone
// of it with one commit pushed, a versioned file changed and one
// unversioned. It returns the clone and the origin.
func soloFixture(t *testing.T) (dir, origin string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", origin)
	dir = filepath.Join(root, "toolbox")
	gitIn(t, root, "clone", "-q", origin, dir)
	gitIn(t, dir, "checkout", "-q", "-b", "main")
	commitIn(t, dir, "app.go", "Start the toolbox")
	gitIn(t, dir, "push", "-q", "-u", "origin", "main")
	must(t, os.WriteFile(filepath.Join(dir, "app.go"), []byte("Start the toolbox\nand run it\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("an idea\n"), 0o644))
	return dir, origin
}

// newSoloApp starts standalone on dir, with a configuration of its own,
// only read.
func newSoloApp(t *testing.T, dir string) (*App, tcell.SimulationScreen, *config.Config) {
	t.Helper()
	cfg := config.Default()
	cfg.SetDir(t.TempDir())
	cfg.FavouriteEditor, cfg.Editor = "custom", "true"
	cfg.SetReadOnly()
	app, err := NewStandalone(cfg, filepath.Join(dir, "."))
	if err != nil {
		t.Fatal(err)
	}
	a, sc := startApp(t, app)
	return a, sc, cfg
}

func TestStandaloneOpensOnTheChangesOfItsRepository(t *testing.T) {
	t.Parallel()
	dir, _ := soloFixture(t)
	a, sc, cfg := newSoloApp(t, dir)
	waitFor(t, a, sc, "notes.txt")
	waitFor(t, a, sc, "in sync")
	text := a.screenText(sc)
	for _, want := range []string{"toolbox", "main", "Changes [1]", "Log [2]", "Branches [3]", "Shelf [4]", "app.go", "? help"} {
		if !strings.Contains(text, want) {
			t.Errorf("standalone does not show %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"Repositories", "Merge requests", "passphrase", "Settings"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("standalone shows %q, which is the full unagit's:\n%s", unwanted, text)
		}
	}
	// Going from tab to tab and back reads the repository again; none of
	// it is written into unagit's configuration directory.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Start the toolbox")
	typeRunes(sc, "1")
	waitFor(t, a, sc, "notes.txt")
	entries, err := os.ReadDir(cfg.Dir())
	must(t, err)
	for _, e := range entries {
		t.Errorf("standalone wrote %s into the configuration directory", e.Name())
	}
}

func TestStandaloneTabsAreScreensThatStay(t *testing.T) {
	t.Parallel()
	dir, _ := soloFixture(t)
	gitIn(t, dir, "branch", "experiment")
	a, sc, _ := newSoloApp(t, dir)
	waitFor(t, a, sc, "notes.txt")

	typeRunes(sc, "2")
	waitFor(t, a, sc, "Commit Log · main")
	waitFor(t, a, sc, "Start the toolbox")
	// Esc and q close a dialog; a tab is not one, and Esc leaves it be.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "3")
	waitFor(t, a, sc, "experiment")
	if text := a.screenText(sc); !strings.Contains(text, "out in this repository") {
		t.Errorf("the branch checked out is not said to be out in the repository:\n%s", text)
	}
	typeRunes(sc, "4")
	waitFor(t, a, sc, "Shelf · toolbox (main) · empty")
	if got := onLoop(a, a.currentTab); got != pageSoloShelf {
		t.Errorf("the tab in front is %q, not the shelf", got)
	}
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Start the toolbox")
}

func TestStandaloneCommitsAndPushesWithTheUsersGit(t *testing.T) {
	t.Parallel()
	dir, origin := soloFixture(t)
	a, sc, _ := newSoloApp(t, dir)
	waitFor(t, a, sc, "notes.txt")
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Commit and Push")
	typeRunes(sc, "Run the toolbox")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "o commit")
	typeRunes(sc, "p")
	waitFor(t, a, sc, "push to origin?")
	typeRunes(sc, "p")
	waitFor(t, a, sc, "and pushed")
	if got := gitIn(t, origin, "log", "-1", "--format=%s", "main"); got != "Run the toolbox" {
		t.Errorf("origin's main is at %q, not the commit made in standalone", got)
	}
	// The header follows: what is left is the unversioned file alone.
	waitFor(t, a, sc, "/1 uncommitted")
}

func TestStandaloneLeavesTheServerOut(t *testing.T) {
	t.Parallel()
	dir, _ := soloFixture(t)
	a, sc, _ := newSoloApp(t, dir)
	waitFor(t, a, sc, "notes.txt")
	// The actions offered depend on where the branch stands, read behind
	// the interface: on a slower machine the picker could open before.
	waitFor(t, a, sc, "1/1 uncommitted")
	typeRunes(sc, ":")
	waitFor(t, a, sc, "Go to Shelf")
	text := a.screenText(sc)
	for _, want := range []string{"Pull", "Fetch", "Commit…"} {
		if !strings.Contains(text, want) {
			t.Errorf("the actions do not offer %q:\n%s", want, text)
		}
	}
	acts := onLoop(a, a.soloScreenActions)
	names := map[string]bool{}
	for _, act := range acts {
		names[act.name] = true
	}
	for _, want := range []string{"Rebase onto…", "Recover from Reflog…", "Rewrite History…", "Quit"} {
		if !names[want] {
			t.Errorf("the actions do not offer %q", want)
		}
	}
	for _, act := range append(acts, onLoop(a, a.globalActions)...) {
		if soloLeavesOut[act.name] || strings.HasPrefix(act.name, "Watch") || act.name == "Find Repository…" {
			t.Errorf("standalone offers %q, which needs a server", act.name)
		}
	}
	keys := map[string]string{}
	for _, act := range acts {
		if act.keys == "" {
			continue
		}
		if other, ok := keys[act.keys]; ok {
			t.Errorf("%s is both %q and %q", act.keys, other, act.name)
		}
		keys[act.keys] = act.name
	}
}

func TestStandaloneLayout(t *testing.T) {
	t.Parallel()
	dir, _ := soloFixture(t)
	for _, size := range []struct{ w, h int }{{160, 44}, {120, 34}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := newSoloApp(t, dir)
			resizeApp(a, sc, size.w, size.h)
			for _, tab := range soloTabs {
				typeRunes(sc, string(tab.key))
				waitFor(t, a, sc, map[string]string{pageSoloChanges: "notes.txt", pageSoloLog: "Start the toolbox",
					pageSoloBranches: "Branches - toolbox", pageSoloShelf: "Shelf · toolbox"}[tab.page])
				text := a.screenText(sc)
				lines := strings.Split(text, "\n")
				if !strings.HasPrefix(strings.TrimSpace(lines[0]), "toolbox") {
					t.Errorf("%s: the top line is not the repository: %q", tab.title, lines[0])
				}
				if !strings.Contains(lines[1], "Changes") || !strings.Contains(lines[1], "Shelf") {
					t.Errorf("%s: the second line is not the tabs: %q", tab.title, lines[1])
				}
				// The screen's frame is whole on every row between the tabs
				// and the status line.
				for y := 3; y < size.h-2; y++ {
					if r, _ := cellAt(a, sc, size.w-1, y); r != '│' {
						t.Errorf("%s: the frame's right edge is broken at row %d:\n%s", tab.title, y, text)
						break
					}
				}
				if !strings.Contains(lines[size.h-1], "? help") {
					t.Errorf("%s: the bottom line has no help: %q", tab.title, lines[size.h-1])
				}
				assertLegible(t, a, sc, "standalone "+tab.title)
			}
		})
	}
}

func TestStandaloneRefusesADirectoryOutsideARepository(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.SetDir(t.TempDir())
	_, err := NewStandalone(cfg, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Errorf("standalone outside a repository says %v", err)
	}
}
