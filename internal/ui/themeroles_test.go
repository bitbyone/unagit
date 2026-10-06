package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestARoleFallsBackUntilAThemeNamesIt: a role a theme leaves out is the
// role it falls back on, down to a base colour; naming one role changes
// it and what falls back on it, nothing else.
func TestARoleFallsBackUntilAThemeNamesIt(t *testing.T) {
	t.Parallel()
	th := loadThemes("").byName[defaultThemeName]
	th.Roles = map[string]string{"column.mr": "#123456", "repositories.size": "#654321"}
	got := resolveRoles(th)
	for key, want := range map[string]string{
		"repositories.mr":     "#123456",      // through column.mr
		"merge_requests.iid":  th.Text.Accent, // through column.server, not column.mr
		"repositories.size":   "#654321",      // its own
		"worktrees.size":      th.Text.Muted,
		"repositories.hidden": th.Text.Dim, // through column.hidden
		"repositories.wt":     th.State.Warning,
	} {
		if got[key].Hex() != colour(want).Hex() {
			t.Errorf("%s = %06x, want %s", key, got[key].Hex(), want)
		}
	}
	th.Roles = map[string]string{"column.nonsense": "#ffffff"}
	if th.validate() == nil {
		t.Error("a colour that is no role was taken")
	}
}

// TestEveryRoleComesToAColour: in every built-in theme each role resolves
// to a colour, and every fallback leads to one.
func TestEveryRoleComesToAColour(t *testing.T) {
	t.Parallel()
	set := loadThemes("")
	for _, name := range set.names {
		th := set.byName[name]
		if err := th.validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		for _, r := range colourRoles {
			if c := resolveRoles(th)[r.key]; c == tcell.ColorDefault && th.Background != "default" {
				t.Errorf("%s: %s comes to nothing", name, r.key)
			}
		}
		if len(th.Heat) < 2 {
			t.Errorf("%s has no heat", name)
		}
	}
}

// TestHeatSpreadsFromColdToHot: two colours are spread over sixteen shades
// from the one to the other, a size is placed among them on a logarithmic
// scale, and without heat a size keeps its role's colour. Serial: the heat
// is the process's.
func TestHeatSpreadsFromColdToHot(t *testing.T) {
	shades := heatShades([]string{"#5a7f8c", "#8f5a5a"})
	if len(shades) != heatSteps {
		t.Fatalf("%d shades, want %d", len(shades), heatSteps)
	}
	if shades[0].Hex() != 0x5a7f8c || shades[heatSteps-1].Hex() != 0x8f5a5a {
		t.Errorf("the ends are %06x and %06x", shades[0].Hex(), shades[heatSteps-1].Hex())
	}
	// Half way, the hue is past green towards yellow, not grey.
	mid := toHSL(shades[heatSteps/2])
	if mid.s < 0.15 || mid.h > 120 || mid.h < 40 {
		t.Errorf("the middle shade is %+v, not a faded yellow-green", mid)
	}
	saved := heatScale
	defer func() { heatScale = saved }()
	heatScale = shades
	fallback := tcell.NewRGBColor(1, 2, 3)
	if got := heatColour(1<<10, 1<<10, 1<<30, fallback); got != shades[0] {
		t.Error("the least is not the coldest")
	}
	if got := heatColour(1<<30, 1<<10, 1<<30, fallback); got != shades[heatSteps-1] {
		t.Error("the most is not the hottest")
	}
	if got := heatColour(1<<20, 1<<10, 1<<30, fallback); got != shades[(heatSteps-1)/2] && got != shades[heatSteps/2] {
		t.Error("the size half way on a logarithmic scale is not in the middle")
	}
	heatScale = nil
	if got := heatColour(1<<20, 1<<10, 1<<30, fallback); got != fallback {
		t.Error("without heat the size lost its role's colour")
	}
}

// TestTheDefaultBranchStandsApart: in Repositories the default branch is
// drawn in its own colour and any other branch in another.
func TestTheDefaultBranchStandsApart(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	if role("repositories.branch").Hex() == role("repositories.default_branch").Hex() {
		t.Fatal("the default theme draws both branches alike")
	}
	p := newRealProject(t, a, "acme/gateway")
	p.rescan()
	typeRunes(sc, "j") // off the row, so its own colours show
	branchColour := func(name string) int32 {
		deadline := time.Now().Add(patience)
		for {
			text := a.screenText(sc)
			row := lineOf(text, "acme/gateway")
			line := strings.Split(text, "\n")[row]
			if at := strings.Index(line, " "+name+" "); at >= 0 && lineOf(text, "acme/billing") > row {
				_, style := cellAt(a, sc, len([]rune(line[:at]))+1, row)
				fg, bg, _ := style.Decompose()
				// Dim is a repository not cloned yet: the scan has not
				// caught up.
				if _, selected, _ := styleSelected.Decompose(); bg != selected && fg.Hex() != colDim.Hex() {
					return fg.Hex()
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s is not on the row:\n%s", name, text)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := branchColour("main"); got != role("repositories.default_branch").Hex() {
		t.Errorf("the default branch is %06x", got)
	}
	gitIn(t, p.clone, "checkout", "-q", "-b", "feat/x")
	p.rescan()
	if got := branchColour("feat/x"); got != role("repositories.branch").Hex() {
		t.Errorf("a branch of work is %06x", got)
	}
}
