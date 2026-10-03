package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/chezmoi"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/workspace"
)

// newChezmoiApp starts the app with a chezmoi whose checkout is a real
// repository and whose origin is the fixture's acme/gateway.
func newChezmoiApp(t *testing.T) (*App, tcell.SimulationScreen, string) {
	t.Helper()
	srv := fakeGitLab(t)
	cfg := writeTestConfig(t, srv.URL)
	checkout := filepath.Join(t.TempDir(), "chezmoi")
	must(t, os.MkdirAll(checkout, 0o755))
	gitIn(t, checkout, "init", "-q", "--initial-branch=main")
	a := New(cfg, testVault(t, cfg))
	a.findChezmoi = func() (chezmoi.Checkout, error) {
		return chezmoi.Checkout{Dir: checkout, Origin: srv.URL + "/acme/gateway.git"}, nil
	}
	a, sc := startApp(t, a)
	waitFor(t, a, sc, "Managed by Chezmoi")
	return a, sc, checkout
}

// TestChezmoiKeepsItsRepository: the repository chezmoi keeps is its
// checkout - cloned, opened there, its path in the list - and wears a badge
// that is not a tag: first, in its own colours on every kind of row, and
// absent from the tags and the configuration.
func TestChezmoiKeepsItsRepository(t *testing.T) {
	a, sc, checkout := newChezmoiApp(t)
	inst := onLoop(a, func() string { return a.projects[0].Instance })
	changeOnLoop(a, func() { a.cfg.ToggleTag(inst, "acme/gateway", "oss"); a.applyFilters() })

	screen := a.screenText(sc)
	line := strings.Split(screen, "\n")[lineOf(screen, "acme/gateway")]
	badge, oss := strings.Index(line, "Managed by Chezmoi"), strings.Index(line, "oss")
	if badge < 0 || oss < badge {
		t.Fatalf("the badge is not first among the tags: %q", line)
	}
	if !strings.Contains(line, "●") {
		t.Errorf("chezmoi's checkout does not count as cloned: %q", line)
	}
	if got := onLoop(a, func() string { return a.projectDir(inst, "acme/gateway") }); got != checkout {
		t.Errorf("projectDir = %q, want chezmoi's %q", got, checkout)
	}
	if billing := strings.Split(screen, "\n")[lineOf(screen, "acme/billing")]; strings.Contains(billing, "Chezmoi") {
		t.Errorf("the badge is on another repository too: %q", billing)
	}

	x := len([]rune(line[:strings.Index(line, "↗")]))
	badgeKept := func(state string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			_, style := cellAt(a, sc, x, lineOf(a.screenText(sc), "acme/gateway"))
			fg, bg, _ := style.Decompose()
			if bg == tcell.GetColor(chezmoiFill) && fg == tcell.GetColor(chezmoiInk) {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("%s: the badge is drawn %v on %v, not in its own colours", state, fg, bg)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	badgeKept("under the cursor")
	typeRunes(sc, "j")
	badgeKept("plain")
	typeRunes(sc, "k ")
	waitFor(t, a, sc, "SELECT 1")
	badgeKept("marked")
	typeRunes(sc, "k")
	badgeKept("marked, under the cursor")
	assertLegible(t, a, sc, "the chezmoi badge")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)

	// Hiding the tags keeps the badge, which is not one.
	changeOnLoop(a, func() { a.cfg.Filters.HideTags = true; a.projectsPane.reload() })
	waitGone(t, a, sc, "oss")
	if !strings.Contains(a.screenText(sc), "Managed by Chezmoi") {
		t.Error("hiding the tags hid the badge")
	}
	for _, t2 := range onLoop(a, func() []config.Tag { return a.cfg.TagList() }) {
		if strings.Contains(strings.ToLower(t2.Name), "chezmoi") {
			t.Errorf("the badge became a tag: %+v", t2)
		}
	}

	// e cannot move it, and deleting it would delete only worktrees.
	changeOnLoop(a, func() { a.showProjectDirectory(a.projects[0]) })
	waitFor(t, a, sc, "chezmoi keeps this repository")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	changeOnLoop(a, func() { a.confirmDeleteProject(a.projects[0]) })
	waitFor(t, a, sc, "no worktrees to delete")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	if !workspace.Exists(checkout) {
		t.Fatal("chezmoi's checkout is gone")
	}

	// Off, the repository is unagit's to clone again, and nothing about
	// chezmoi was written to the configuration but the choice.
	changeOnLoop(a, func() {
		off := false
		a.cfg.Integrations.Chezmoi = &off
		a.detectChezmoi()
	})
	waitGone(t, a, sc, "Managed by Chezmoi")
	if got := onLoop(a, func() string { return a.projectDir(inst, "acme/gateway") }); got == checkout {
		t.Error("the integration is off and the repository is still chezmoi's")
	}
	must(t, onLoop(a, func() error { return a.cfg.Save() }))
	raw, err := os.ReadFile(filepath.Join(config.Dir(), "config.yaml"))
	must(t, err)
	if strings.Contains(string(raw), checkout) {
		t.Errorf("chezmoi's checkout was saved in the configuration:\n%s", raw)
	}
}

// TestChezmoiInTheDetail: the detail heads the repository with the badge and
// says where the checkout and the worktrees are; the help names the marker.
func TestChezmoiInTheDetail(t *testing.T) {
	a, sc, checkout := newChezmoiApp(t)
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Checkout")
	screen := a.screenText(sc)
	if !strings.Contains(screen, tildePath(checkout)) {
		t.Errorf("the detail does not say where the checkout is:\n%s", screen)
	}
	assertLegible(t, a, sc, "the chezmoi detail")
	typeRunes(sc, "?")
	waitFor(t, a, sc, "↗ Chezmoi")
}

// TestChezmoiBadgeNarrows: a narrow column gets a shorter badge, never a
// count or half a word.
func TestChezmoiBadgeNarrows(t *testing.T) {
	for room, want := range map[int]string{40: " ↗ Managed by Chezmoi ", 15: " ↗ Chezmoi ", 4: " ↗ ", 2: ""} {
		markup, w := chezmoiBadge(room)
		if got := stripTags(markup); got != want || w != len([]rune(want)) {
			t.Errorf("chezmoiBadge(%d) = %q (%d), want %q", room, got, w, want)
		}
	}
}

func stripTags(markup string) string {
	var b strings.Builder
	in := false
	for _, r := range markup {
		switch {
		case r == '[':
			in = true
		case r == ']' && in:
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
