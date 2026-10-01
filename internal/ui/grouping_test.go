package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

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
	for _, list := range []string{"merge requests", "repositories"} {
		t.Run(list, func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, 120, 16)
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
				typeRunes(sc, "M")
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

// TestGroupRepositories: Ctrl-G in Repositories gathers them under the group
// or subgroup they live in, each named by what is left of its path, and the
// merge request list keeps a grouping of its own.
func TestGroupRepositories(t *testing.T) {
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
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	if strings.Contains(a.screenText(sc), "acme/gateway  (") {
		t.Error("grouping the repositories grouped the merge requests too")
	}

	typeRunes(sc, "R")
	sc.InjectKey(tcell.KeyCtrlG, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Repositories listed flat again")
	waitFor(t, a, sc, "acme/tools/cli")
}
