package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// changeOnLoop changes the app on its own goroutine and redraws.
func changeOnLoop(a *App, change func()) {
	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() { change(); close(done) })
	<-done
}

// lineOf is the screen line holding needle, or -1.
func lineOf(screen, needle string) int {
	for i, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, needle) {
			return i
		}
	}
	return -1
}

// TestFirstGroupHeadingStaysOnScreen: tview scrolls only as far as the
// selected row, so the cursor coming back to the first row of the first group
// used to leave that group's heading hidden under the column header.
func TestFirstGroupHeadingStaysOnScreen(t *testing.T) {
	t.Parallel()
	for _, list := range []string{"merge requests", "repositories"} {
		t.Run(list, func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resizeApp(a, sc, 120, 16)
			heading := "acme  ("
			changeOnLoop(a, func() {
				// Enough rows to scroll.
				for i := 0; i < 30; i++ {
					a.projects = append(a.projects, forge.Project{ID: 100 + i, Instance: a.projects[0].Instance,
						PathWithNamespace: fmt.Sprintf("acme/tool%02d", i), DefaultBranch: "main",
						LastActivityAt: time.Now().Add(-time.Duration(2+i) * time.Hour)})
					a.mrs = append(a.mrs, forge.MergeRequest{ID: 1000 + i, IID: 100 + i, ProjectID: 1,
						ProjectPath: "acme/gateway", Instance: a.mrs[0].Instance,
						Title: fmt.Sprintf("Change %02d", i), UpdatedAt: time.Now().Add(-time.Duration(3+i) * time.Hour)})
				}
				a.projectsPane.reload()
				a.mrsPane.reload()
			})
			if list == "merge requests" {
				typeRunes(sc, "2")
				waitFor(t, a, sc, "Rate limiting")
				heading = "acme/gateway  ("
				sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
				waitFor(t, a, sc, "· grouped")
			} else {
				sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
				waitFor(t, a, sc, "· grouped")
			}
			waitFor(t, a, sc, heading)

			typeRunes(sc, "G")
			waitGone(t, a, sc, heading)
			typeRunes(sc, "g")
			waitFor(t, a, sc, heading)
		})
	}
}

// TestGroupingKeepsTheCursorInView: switching the grouping on or off moves the
// selected row, and the list must scroll to it wherever it lands - tview
// otherwise keeps following the end of a list that once fitted, and the
// cursor was left somewhere off screen.
func TestGroupingKeepsTheCursorInView(t *testing.T) {
	t.Parallel()
	// A long list, and one that fits the screen until the headings come.
	for _, size := range []struct{ rows, height int }{{30, 16}, {10, 22}} {
		for _, list := range []string{"merge requests", "repositories"} {
			for _, start := range []string{"g", "G", "Gkkkkkkkkkkkkkkk"} {
				t.Run(fmt.Sprintf("%s from %s, %d rows", list, start, size.rows), func(t *testing.T) {
					a, sc := newTestApp(t)
					waitFor(t, a, sc, "acme/gateway")
					resizeApp(a, sc, 120, size.height)
					changeOnLoop(a, func() {
						// Rows spread over many groups, so grouping moves them a lot.
						for i := 0; i < size.rows; i++ {
							a.projects = append(a.projects, forge.Project{ID: 100 + i, Instance: a.projects[0].Instance,
								PathWithNamespace: fmt.Sprintf("team%d/tool%02d", i%5, i), DefaultBranch: "main",
								LastActivityAt: time.Now().Add(-time.Duration(2+i) * time.Hour)})
							a.mrs = append(a.mrs, forge.MergeRequest{ID: 1000 + i, IID: 100 + i, ProjectID: 100 + i,
								ProjectPath: fmt.Sprintf("team%d/tool%02d", i%5, i%7), Instance: a.mrs[0].Instance,
								Title: fmt.Sprintf("Change %02d", i), UpdatedAt: time.Now().Add(-time.Duration(3+i) * time.Hour)})
						}
						a.projectsPane.reload()
						a.mrsPane.reload()
					})
					pane, label := a.projectsPane, func(i int) string { return a.projects[i].PathWithNamespace }
					if list == "merge requests" {
						typeRunes(sc, "2")
						waitFor(t, a, sc, "Rate limiting")
						pane, label = a.mrsPane, func(i int) string { return a.mrs[i].Title }
					}
					typeRunes(sc, start)

					for _, toggle := range []string{"· grouped", "flat again"} {
						want := onLoop(a, func() string { return label(pane.selectedIndex()) })
						sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
						if toggle == "· grouped" {
							waitFor(t, a, sc, toggle)
						} else {
							waitGone(t, a, sc, "· grouped")
						}
						if got := onLoop(a, func() string { return label(pane.selectedIndex()) }); got != want {
							t.Fatalf("the cursor moved from %q to %q", want, got)
						}
						short := want[strings.LastIndex(want, "/")+1:]
						if lineOf(a.screenText(sc), short) < 0 {
							t.Fatalf("after %q the selected %q is off screen:\n%s", toggle, want, a.screenText(sc))
						}
						if start == "g" && toggle == "· grouped" && lineOf(a.screenText(sc), "  (") < 0 {
							t.Fatalf("the first heading is off screen:\n%s", a.screenText(sc))
						}
					}
				})
			}
		}
	}
}

// TestGroupRepositories: Ctrl-G in Repositories gathers them under the group
// or subgroup they live in, each named by what is left of its path, and the
// merge request list keeps a grouping of its own.
func TestGroupRepositories(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.projects = append(a.projects, forge.Project{ID: 3, Instance: a.projects[0].Instance,
			PathWithNamespace: "acme/tools/cli", DefaultBranch: "main",
			LastActivityAt: time.Now().Add(-30 * time.Minute)})
		a.projectsPane.reload()
	})
	waitFor(t, a, sc, "acme/tools/cli")

	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Repositories grouped by group")
	screen := a.screenText(sc)
	acme, gateway, tools, cli, billing := lineOf(screen, "acme  (2)"), lineOf(screen, "○ gateway"),
		lineOf(screen, "acme/tools  (1)"), lineOf(screen, "○ cli"), lineOf(screen, "○ billing")
	// By activity: gateway, then cli, then billing - so acme sits first, and
	// billing joins it there.
	if !(acme >= 0 && acme < gateway && gateway < billing && billing < tools && tools < cli) {
		t.Fatalf("order: acme %d, gateway %d, billing %d, acme/tools %d, cli %d:\n%s",
			acme, gateway, billing, tools, cli, screen)
	}
	if !strings.Contains(screen, "· grouped") {
		t.Errorf("the header does not say the list is grouped:\n%s", screen)
	}
	if got := a.projectsPane.selectedIndex(); got < 0 {
		t.Fatal("no repository is selected")
	}

	// The merge requests are not grouped by it.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	if strings.Contains(a.screenText(sc), "acme/gateway  (") {
		t.Error("grouping the repositories grouped the merge requests too")
	}

	typeRunes(sc, "1")
	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Repositories listed flat again")
	waitFor(t, a, sc, "acme/tools/cli")
}

// TestClearingTheFilterGoesToTheTop: a filter that is changed or cleared puts
// the cursor on the first row, and the view goes with it, to the very top.
func TestClearingTheFilterGoesToTheTop(t *testing.T) {
	t.Parallel()
	for _, grouped := range []bool{false, true} {
		t.Run(fmt.Sprintf("grouped %v", grouped), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resizeApp(a, sc, 120, 16)
			changeOnLoop(a, func() {
				for i := 0; i < 30; i++ {
					a.projects = append(a.projects, forge.Project{ID: 100 + i, Instance: a.projects[0].Instance,
						PathWithNamespace: fmt.Sprintf("team%d/tool%02d", i%5, i), DefaultBranch: "main",
						LastActivityAt: time.Now().Add(-time.Duration(2+i) * time.Hour)})
				}
				a.cfg.Filters.GroupRepositories = grouped
				a.projectsPane.reload()
			})
			// A filter few enough rows match to fit the screen, which is what
			// sets tview following the end of the list; the cursor goes down
			// them.
			typeRunes(sc, "/team4")
			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			typeRunes(sc, "G")
			waitFor(t, a, sc, "tool29")

			sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
			waitGone(t, a, sc, "tool29")
			screen := a.screenText(sc)
			want := "acme/gateway"
			if grouped {
				want = "acme  ("
			}
			if line := lineOf(screen, want); line < 0 || line > 5 {
				t.Fatalf("the top of the list is not on screen:\n%s", screen)
			}
			if got := onLoop(a, func() string { return a.projects[a.projectsPane.selectedIndex()].PathWithNamespace }); got != "acme/gateway" {
				t.Errorf("the cursor is on %s", got)
			}
		})
	}
}

// TestDetailStacksBelowItsListsWidth: the repository list, whose rows carry
// tags and paths, puts its detail under itself below 180 columns; the merge
// requests below 130.
func TestDetailStacksBelowItsListsWidth(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tab   string
		pane  func(a *App) *pane
		limit int
	}{
		{"1", func(a *App) *pane { return a.projectsPane }, 180},
		{"2", func(a *App) *pane { return a.mrsPane }, 130},
	} {
		t.Run(c.tab, func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			typeRunes(sc, c.tab)
			for _, width := range []int{c.limit - 1, c.limit} {
				resizeApp(a, sc, width, 40)
				waitFocus(t, a, func() bool {
					_, _, w, _ := c.pane(a).body.GetRect()
					return w == width
				})
				want := tview.FlexColumn
				if width < c.limit {
					want = tview.FlexRow
				}
				waitFocus(t, a, func() bool { return c.pane(a).bodyDirection == want })
			}
		})
	}
}
