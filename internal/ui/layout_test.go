package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// longMRs replaces the merge request index with entries whose titles and
// branches are long enough to compete for the available width.
func longMRs(t *testing.T, instanceID string) {
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
	mrs := []forge.MergeRequest{
		{IID: 29747, ProjectID: 1, ProjectPath: "acme/gateway", Instance: instanceID,
			Title:        "MY2N-29747: Wrap rendered content into the email template and fix the footer",
			SourceBranch: "feature/MY2N-29747-wrap-rendered-content",
			Author:       author("Metlicka"), UpdatedAt: time.Now()},
		{IID: 17, ProjectID: 2, ProjectPath: "acme/billing", Instance: instanceID,
			Title:        "Invoice rounding",
			SourceBranch: "renovate/golang-x-crypto-vulnerability",
			Author:       author("ci"), UpdatedAt: time.Now()},
	}
	must(t, index.Save(config.IndexPath("mrs"), index.MergeRequests{UpdatedAt: time.Now(), Items: mrs}))
}

func TestColumnsAdaptToTheTerminalWidth(t *testing.T) {
	cfg := writeTestConfig(t, fakeGitLab(t).URL)
	longMRs(t, cfg.Instances[0].ID)
	a, sc := startApp(t, New(cfg, testVault(t, cfg)))
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

// TestModalsDimTheBackground checks the scrim darkens what is underneath while
// leaving it readable, rather than blanking it out.
func TestModalsDimTheBackground(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")

	row := rowOf(t, a, sc, "acme/billing")
	col := strings.Index(strings.Split(a.screenText(sc), "\n")[row], "acme/billing") + 2
	beforeRune, beforeStyle := cellAt(a, sc, col, row)

	typeRunes(sc, "?")
	waitFor(t, a, sc, "unagit - keys")

	afterRune, afterStyle := cellAt(a, sc, col, row)
	if afterRune != beforeRune {
		t.Fatalf("the text behind the modal changed: %q -> %q", beforeRune, afterRune)
	}
	before, _, _ := beforeStyle.Decompose()
	after, _, _ := afterStyle.Decompose()
	if after == before {
		t.Fatalf("the background behind the modal was not dimmed (still %v)", after)
	}
	if luminance(after) >= luminance(before) {
		t.Errorf("dimmed colour %v is not darker than %v", after, before)
	}
	if luminance(after) == 0 {
		t.Errorf("the text behind the modal was blacked out: %v", after)
	}
}

func luminance(c tcell.Color) int32 {
	hex := c.Hex()
	if hex < 0 {
		return -1
	}
	return (hex>>16)&0xff + (hex>>8)&0xff + hex&0xff
}

func cellAt(a *App, sc tcell.SimulationScreen, x, y int) (rune, tcell.Style) {
	type cell struct {
		r rune
		s tcell.Style
	}
	done := make(chan cell, 1)
	a.tv.QueueUpdate(func() {
		cells, w, _ := sc.GetContents()
		c := cells[y*w+x]
		r := ' '
		if len(c.Runes) > 0 {
			r = c.Runes[0]
		}
		done <- cell{r, c.Style}
	})
	select {
	case c := <-done:
		return c.r, c.s
	case <-time.After(2 * time.Second):
		return 0, tcell.StyleDefault
	}
}

func cellStyleAt(a *App, sc tcell.SimulationScreen, x, y int) tcell.Style {
	_, style := cellAt(a, sc, x, y)
	return style
}

// openSection walks the Settings sidebar to a section and moves into it.
func openSection(t *testing.T, a *App, sc tcell.SimulationScreen, section int) {
	t.Helper()
	typeRunes(sc, "S")
	waitFor(t, a, sc, sectionNames[section])
	a.tv.QueueUpdateDraw(func() { a.settings.selectSection(section) })
	waitFor(t, a, sc, sectionNames[section])
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
}

func TestSettingsCyclesGroupScope(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "incl. subgroups") // the fixture starts fully selected

	id := a.cfg.Instances[0].ID

	// The cursor starts on the server node; step onto the group below it.
	typeRunes(sc, "j")
	typeRunes(sc, " ")
	waitFor(t, a, sc, "· acme")
	if got := a.cfg.Instance(id).GroupScope(1); got != "" {
		t.Fatalf("scope = %q, want unselected", got)
	}

	typeRunes(sc, " ")
	waitFor(t, a, sc, "this group only")
	if got := a.cfg.Instance(id).GroupScope(1); got != config.ScopeGroup {
		t.Fatalf("scope = %q, want %q", got, config.ScopeGroup)
	}

	typeRunes(sc, " ")
	waitFor(t, a, sc, "incl. subgroups")
	if got := a.cfg.Instance(id).GroupScope(1); got != config.ScopeSubgroups {
		t.Fatalf("scope = %q, want %q", got, config.ScopeSubgroups)
	}

	// The choice is written to disk straight away.
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Instance(id).GroupScope(1) != config.ScopeSubgroups {
		t.Errorf("saved scope = %q", saved.Instance(id).GroupScope(1))
	}
}
