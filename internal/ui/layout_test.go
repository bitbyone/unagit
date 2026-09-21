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

// longMRs replaces the merge request index with entries whose titles and
// branches are long enough to compete for the available width.
func longMRs(t *testing.T) {
	t.Helper()
	author := func(name string) struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} {
		return struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		}{Username: name}
	}
	mrs := []gitlab.MergeRequest{
		{IID: 29747, ProjectID: 1, ProjectPath: "acme/gateway",
			Title:        "MY2N-29747: Wrap rendered content into the email template and fix the footer",
			SourceBranch: "feature/MY2N-29747-wrap-rendered-content",
			Author:       author("Metlicka"), UpdatedAt: time.Now()},
		{IID: 17, ProjectID: 2, ProjectPath: "acme/billing",
			Title:        "Invoice rounding",
			SourceBranch: "renovate/golang-x-crypto-vulnerability",
			Author:       author("ci"), UpdatedAt: time.Now()},
	}
	must(t, index.Save(config.IndexPath("mrs"), index.MergeRequests{UpdatedAt: time.Now(), Items: mrs}))
}

func TestColumnsAdaptToTheTerminalWidth(t *testing.T) {
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	longMRs(t)
	a, sc := startApp(t, New(cfg, "test-token"))
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "29747")

	// Wide: the whole title fits, the branch is still there.
	wide := a.screenText(sc)
	if !strings.Contains(wide, "fix the footer") {
		t.Errorf("the full title should fit at 160 columns:\n%s", wide)
	}
	if !strings.Contains(wide, "feature/MY2N-29747-wrap-r") {
		t.Errorf("branch column missing at 160 columns:\n%s", wide)
	}

	// Narrow: the title gives way, the branch must not fall off the edge.
	resize(sc, 84, 20)
	deadline := time.Now().Add(3 * time.Second)
	var narrow string
	for time.Now().Before(deadline) {
		narrow = a.screenText(sc)
		if strings.Contains(narrow, "renovate/golang") && !strings.Contains(narrow, "fix the footer") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.Contains(narrow, "fix the footer") {
		t.Errorf("the title was not truncated at 84 columns:\n%s", narrow)
	}
	if !strings.Contains(narrow, "renovate/golang") {
		t.Errorf("the branch column was pushed off the screen at 84 columns:\n%s", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if len(line) > 0 && strings.Count(line, "│") == 1 && strings.Contains(line, "acme/") {
			t.Errorf("a row overflowed its box:\n%s", line)
		}
	}
}

// TestSelectedRowIsABand checks the highlight covers the row rather than just
// the words in it.
func TestSelectedRowIsABand(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	row := rowOf(t, a, sc, "acme/gateway")
	bg := func(x int) tcell.Color {
		_, b, _ := cellStyleAt(a, sc, x, row).Decompose()
		return b
	}
	_, selBg, _ := styleSelected.Decompose()

	// Sample the start, the middle of the text and the empty tail of the row.
	for _, x := range []int{2, 20, 100, 140} {
		if got := bg(x); got != selBg {
			t.Errorf("column %d of the selected row has background %v, want %v", x, got, selBg)
		}
	}
	if _, headerBg, _ := cellStyleAt(a, sc, 20, row-1).Decompose(); headerBg == selBg {
		t.Error("the header row is highlighted too")
	}
}

func rowOf(t *testing.T, a *App, sc tcell.SimulationScreen, needle string) int {
	t.Helper()
	for i, line := range strings.Split(a.screenText(sc), "\n") {
		if strings.Contains(line, needle) {
			return i
		}
	}
	t.Fatalf("%q is not on screen", needle)
	return -1
}

// TestModalsDimTheBackground checks the scrim restyles what is underneath
// instead of just covering part of it.
func TestModalsDimTheBackground(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	typeRunes(sc, "?")
	waitFor(t, a, sc, "unagit - keys")

	fg, _, _ := cellStyleAt(a, sc, 1, 1).Decompose()
	if fg != tcell.Color237 {
		t.Errorf("the background behind the modal was not dimmed: %v", fg)
	}
}

func cellStyleAt(a *App, sc tcell.SimulationScreen, x, y int) tcell.Style {
	done := make(chan tcell.Style, 1)
	a.tv.QueueUpdate(func() {
		cells, w, _ := sc.GetContents()
		done <- cells[y*w+x].Style
	})
	select {
	case s := <-done:
		return s
	case <-time.After(2 * time.Second):
		return tcell.StyleDefault
	}
}

func TestSettingsCyclesGroupScope(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "S")
	waitFor(t, a, sc, "incl. subgroups") // the fixture starts fully selected

	typeRunes(sc, " ")
	waitFor(t, a, sc, "· acme")
	if a.cfg.GroupScope(1) != "" {
		t.Fatalf("scope = %q, want unselected", a.cfg.GroupScope(1))
	}

	typeRunes(sc, " ")
	waitFor(t, a, sc, "this group only")
	if got := a.cfg.GroupScope(1); got != config.ScopeGroup {
		t.Fatalf("scope = %q, want %q", got, config.ScopeGroup)
	}

	typeRunes(sc, " ")
	waitFor(t, a, sc, "incl. subgroups")
	if got := a.cfg.GroupScope(1); got != config.ScopeSubgroups {
		t.Fatalf("scope = %q, want %q", got, config.ScopeSubgroups)
	}

	// The choice is written to disk straight away.
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.GroupScope(1) != config.ScopeSubgroups {
		t.Errorf("saved scope = %q", saved.GroupScope(1))
	}
}
