package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// TestFindRepositoryBringsOneIn: + opens on the filter, over every
// repository of the account - the group's, the hidden one, one of a group
// not chosen - each saying where it stands. Enter on the stranger lists it
// on its own, its merge requests with it, and goes to it; on the hidden
// one it shows it again. Settings › Groups lists the one on its own under
// its server, and space takes it back.
func TestFindRepositoryBringsOneIn(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		a.cfg.Filters.Hidden = []config.Hidden{{Instance: a.cfg.Instances[0].ID, Path: "acme/billing"}}
		a.applyFilters()
		return true
	})
	waitGone(t, a, sc, "acme/billing")

	typeRunes(sc, "+")
	waitFor(t, a, sc, "Find Repository · 3")
	// The list behind has the same paths; the picker's rows are below its
	// title.
	text := a.screenText(sc)
	text = text[strings.Index(text, "Find Repository"):]
	for path, state := range map[string]string{"acme/gateway": "listed", "acme/billing": "hidden", "other/tool": ""} {
		line := lineAt(text, path)
		if state != "" && !strings.Contains(line, state) || state == "" && (strings.Contains(line, "listed") || strings.Contains(line, "hidden")) {
			t.Errorf("%s does not say %q: %q", path, state, line)
		}
	}
	assertLegible(t, a, sc, "the repositories to find")

	// It opens typing: the letters are a search.
	typeRunes(sc, "tool")
	waitFor(t, a, sc, "A tool of another team")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Find Repository")
	waitFor(t, a, sc, "other/tool added on its own")
	if added := onLoop(a, func() []config.Repository { return a.cfg.Instances[0].Repositories }); len(added) != 1 || added[0].FullPath != "other/tool" || added[0].ID != 5 {
		t.Errorf("chosen on its own: %+v", added)
	}
	if _, got := onLoopPair(a, func() (string, string) { return a.selectedProjectOf(a.projectsPane) }); got != "other/tool" {
		t.Errorf("the cursor is on %q", got)
	}
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Sharpen the tool")
	typeRunes(sc, "1")

	// The hidden one is shown again; the list is not read twice.
	typeRunes(sc, "+")
	waitFor(t, a, sc, "Find Repository · 3")
	typeRunes(sc, "billing")
	waitFor(t, a, sc, "Invoices and rounding")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/billing shown again")
	if onLoop(a, func() bool { return a.cfg.Filters.IsHidden(a.cfg.Instances[0].ID, "acme/billing") }) {
		t.Error("acme/billing is still hidden")
	}
	if n := srv.catalogueRead.Load(); n != 1 {
		t.Errorf("the repositories were read %d times", n)
	}

	openSection(t, a, sc, sectionGroups)
	waitFor(t, a, sc, "other/tool  (on its own)")
	typeRunes(sc, "j")
	typeRunes(sc, " ")
	waitFor(t, a, sc, "other/tool taken back")
	if added := onLoop(a, func() []config.Repository { return a.cfg.Instances[0].Repositories }); len(added) != 0 {
		t.Errorf("still chosen: %+v", added)
	}
}

// TestARefreshReadsTheRepositoriesOnTheirOwn: p lists a repository chosen
// on its own beside the group's, and m reads its merge requests.
func TestARefreshReadsTheRepositoriesOnTheirOwn(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		a.cfg.Instances[0].AddRepository(config.Repository{ID: 5, FullPath: "other/tool"})
		a.refreshProjects()
		return true
	})
	waitFor(t, a, sc, "3 repositories indexed")
	waitFor(t, a, sc, "other/tool")
	onLoop(a, func() bool { a.refreshMRs(); return true })
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Sharpen the tool")
}

// TestFindRepositoryFitsItsFrame draws the picker at several terminal
// sizes, a long path among the rows: the frame keeps its border on every
// row, and each repository and the hint are on screen.
func TestFindRepositoryFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {120, 34}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			long := "platform/infrastructure/terraform-modules/kubernetes-cluster-autoscaler"
			onLoop(a, func() bool {
				a.catalogue = append(a.catalogue, forge.Project{ID: 9, PathWithNamespace: long, Instance: a.cfg.Instances[0].ID,
					Description: "Scales the nodes of every cluster up and down with the pods waiting for room."})
				a.catalogueAt = time.Now()
				return true
			})
			resizeApp(a, sc, size.w, size.h)
			typeRunes(sc, "+")
			waitFor(t, a, sc, "Find Repository · 1")
			waitFor(t, a, sc, "Enter add")
			text := a.screenText(sc)
			lines := strings.Split(text, "\n")
			top := lineOf(text, "Find Repository")
			row := []rune(lines[top])
			left := -1
			right := -1
			for i, r := range row {
				if r == '╭' && left < 0 {
					left = i
				}
				if r == '╮' {
					right = i
				}
			}
			if left < 0 || right < 0 {
				t.Fatalf("no frame:\n%s", text)
			}
			bottom := top + 1
			for ; bottom < len(lines) && []rune(lines[bottom])[left] != '╰'; bottom++ {
				r := []rune(lines[bottom])
				if r[left] != '│' || r[right] != '│' {
					t.Errorf("row %d of the frame lost its border:\n%s", bottom, text)
					break
				}
			}
			if bottom == len(lines) {
				t.Fatalf("the frame has no bottom:\n%s", text)
			}
			if !strings.Contains(text, "autoscaler") || !strings.Contains(text, "ACTIVE") {
				t.Errorf("the long path is cut at its end, or ACTIVE left out:\n%s", text)
			}
			assertLegible(t, a, sc, "the repositories to find")
		})
	}
}
