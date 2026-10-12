package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// TestFindRepositoryBringsOneIn: + opens typing, over what unagit knows -
// the hidden one too - each saying where it stands. Typing asks the forge,
// a spinner in the dialog's edge while it does; its answer is matched
// with the rest, so its good match goes above a poorer one known before,
// the cursor on it. Enter on it lists it on its own, its merge requests
// with it, and goes to it; on the hidden one it shows it again. Settings ›
// Groups lists the one on its own under its server, and space takes it
// back.
func TestFindRepositoryBringsOneIn(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		id := a.cfg.Instances[0].ID
		a.cfg.Filters.Hidden = []config.Hidden{{Instance: id, Path: "acme/billing"}}
		// A poorer match for "tool", known before the forge is asked.
		a.projects = append(a.projects, forge.Project{ID: 6, PathWithNamespace: "acme/t-o-o-l", Instance: id})
		a.reindexProjects()
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
	for path, state := range map[string]string{"acme/gateway": "listed", "acme/billing": "hidden"} {
		if line := lineAt(text, path); !strings.Contains(line, state) {
			t.Errorf("%s does not say %q: %q", path, state, line)
		}
	}
	assertLegible(t, a, sc, "the repositories to find")

	hold := make(chan struct{})
	srv.holdSearch.Store(hold)
	typeRunes(sc, "tool")
	waitFor(t, a, sc, "searching acme")
	text = a.screenText(sc)
	text = text[strings.Index(text, "Find Repository"):]
	if strings.Contains(text, "other/tool") || lineOf(text, "acme/t-o-o-l") < 0 {
		t.Errorf("before the forge answers, only what is known matches:\n%s", text)
	}
	close(hold)
	waitFor(t, a, sc, "other/tool")
	waitGone(t, a, sc, "searching acme")
	text = a.screenText(sc)
	text = text[strings.Index(text, "Find Repository"):]
	if lineOf(text, "other/tool") > lineOf(text, "acme/t-o-o-l") {
		t.Errorf("the forge's better match is not first:\n%s", text)
	}
	if line := lineAt(text, "other/tool"); !strings.Contains(line, "new") {
		t.Errorf("the one found does not say it is new: %q", line)
	}
	assertLegible(t, a, sc, "a repository found by the forge")
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

	// The hidden one is shown again.
	held := make(chan struct{})
	defer close(held)
	srv.holdSearch.Store(held)
	typeRunes(sc, "+")
	waitFor(t, a, sc, "Find Repository · 4")
	typeRunes(sc, "billing")
	waitFor(t, a, sc, "searching acme")
	// Searching is the request leaving; under load it reached the server
	// only after Enter had closed the dialog and called it off.
	waitTrue(t, "the forge was never asked for billing", func() bool {
		srv.searchesMu.Lock()
		defer srv.searchesMu.Unlock()
		return slices.Contains(srv.searches, "billing")
	})
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "acme/billing shown again")
	if onLoop(a, func() bool { return a.cfg.Filters.IsHidden(a.cfg.Instances[0].ID, "acme/billing") }) {
		t.Error("acme/billing is still hidden")
	}
	srv.searchesMu.Lock()
	searches := append([]string(nil), srv.searches...)
	srv.searchesMu.Unlock()
	if len(searches) != 2 || searches[0] != "tool" || searches[1] != "billing" {
		t.Errorf("the forge was asked %q; once a pause in typing, never for less than three letters", searches)
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
				a.projects = []forge.Project{{ID: 9, PathWithNamespace: long, Instance: a.cfg.Instances[0].ID, LastActivityAt: time.Now(),
					Description: "Scales the nodes of every cluster up and down with the pods waiting for room."}}
				a.reindexProjects()
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
