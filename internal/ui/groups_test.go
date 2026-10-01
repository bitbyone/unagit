package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/workspace"
)

// markBoth makes both fixture repositories real clones and marks them in
// Repositories, then opens the grouped worktree form.
func markBoth(t *testing.T, a *App, sc tcell.SimulationScreen) (*realProject, *realProject, *tview.Form) {
	t.Helper()
	waitFor(t, a, sc, "acme/billing")
	gw := newRealProject(t, a, "acme/gateway")
	bl := newRealProject(t, a, "acme/billing")
	gw.rescan()
	typeRunes(sc, "  ")
	waitFor(t, a, sc, "SELECT 2")
	sc.InjectKey(tcell.KeyCtrlW, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Grouped worktree · 2 repositories")
	form := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	return gw, bl, form
}

// waitForPath polls until a path exists, or fails with the screen.
func waitForPath(t *testing.T, a *App, sc tcell.SimulationScreen, path string, exists bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, err := os.Stat(path)
		if (err == nil) == exists {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: exists should be %v\n%s", path, exists, a.screenText(sc))
}

func TestGroupedWorktreeHoldsEveryMarkedRepository(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	gw, bl, _ := markBoth(t, a, sc)
	// A commit origin has and the clones do not: the new branch starts there.
	other := gw.elsewhere("main")
	commitIn(t, other, "z.txt", "upstream moved on")
	gitIn(t, other, "push", "-q", "origin", "main")

	typeRunes(sc, "feat/multi")
	waitFor(t, a, sc, "feat/multi")
	waitFor(t, a, sc, "feat-multi") // the folder follows the branch
	form := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	pressButton(t, a, sc, form, "Create")

	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-multi")
	waitForPath(t, a, sc, filepath.Join(dir, workspace.GroupFile), true)
	for name, p := range map[string]*realProject{"gateway": gw, "billing": bl} {
		member := filepath.Join(dir, name)
		if got := gitIn(t, member, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/multi" {
			t.Errorf("%s is on %q, not the new branch", name, got)
		}
		want := gitIn(t, p.origin, "rev-parse", "main")
		if got := gitIn(t, member, "rev-parse", "HEAD"); got != want {
			t.Errorf("%s starts at %s, not at origin's main %s", name, got, want)
		}
	}
	if onLoop(a, func() int { return len(a.projectsPane.marks) }) != 0 {
		t.Error("the marks outlived the grouped worktree made of them")
	}

	waitFor(t, a, sc, "opened ")
	typeRunes(sc, "W")
	waitFor(t, a, sc, "REPOS")
	waitFor(t, a, sc, "feat-multi")
	waitFor(t, a, sc, "no upstream")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "grouped worktree · 2 repositories")
	for _, want := range []string{"acme/gateway", "acme/billing", "feat/multi"} {
		waitFor(t, a, sc, want)
	}
	waitFor(t, a, sc, "branch has no upstream") // the state, read from git

	// d takes the worktrees and the folder; the branches and clones stay.
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete grouped worktree")
	typeRunes(sc, "d")
	waitForPath(t, a, sc, dir, false)
	for _, p := range []*realProject{gw, bl} {
		if !strings.Contains(gitIn(t, p.clone, "branch", "--list", "feat/multi"), "feat/multi") {
			t.Errorf("deleting the group took the branch of %s", p.path)
		}
		if strings.Contains(gitIn(t, p.clone, "worktree", "list"), "feat-multi") {
			t.Errorf("%s still lists the deleted worktree", p.path)
		}
	}
}

// TestGroupedWorktreeIsWholeOrNothing: a branch git cannot check out a second
// time stops the group before anything is made.
func TestGroupedWorktreeIsWholeOrNothing(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	typeRunes(sc, "same")
	waitFor(t, a, sc, "same")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "already checked out")
	if _, err := os.Stat(filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "same")); err == nil {
		t.Error("a refused group left its folder behind")
	}
}

// TestGroupedWorktreeChecksOutExistingBranches: without a new branch, each
// repository is on the branch picked for it, tracking origin.
func TestGroupedWorktreeChecksOutExistingBranches(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	gw, bl, form := markBoth(t, a, sc)
	gitIn(t, gw.clone, "push", "-q", "origin", "main:feat/rate")
	// gateway takes feat/rate; billing keeps main, which its clone has checked
	// out, so it is moved off it first.
	gitIn(t, bl.clone, "checkout", "-q", "-b", "elsewhere")
	onLoop(a, func() bool {
		form.GetFormItemByLabel("gateway").(*tview.DropDown).SetCurrentOption(1)
		return true
	})
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	typeRunes(sc, "mixed")
	waitFor(t, a, sc, "mixed")
	pressButton(t, a, sc, form, "Create")

	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "mixed")
	waitForPath(t, a, sc, filepath.Join(dir, workspace.GroupFile), true)
	if got := gitIn(t, filepath.Join(dir, "gateway"), "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/feat/rate" {
		t.Errorf("gateway tracks %q", got)
	}
	if got := gitIn(t, filepath.Join(dir, "billing"), "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("billing is on %q", got)
	}
	waitFor(t, a, sc, "opened ")
	typeRunes(sc, "W")
	waitFor(t, a, sc, "2 branches")
}

// TestGroupedWorktreeFormFitsItsFrame draws the form at several sizes, as the
// merge request form is.
func TestGroupedWorktreeFormFitsItsFrame(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {120, 34}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := newTestAppSrv(t)
			resize(sc, size.w, size.h)
			_, _, form := markBoth(t, a, sc)
			frame := onLoop(a, func() rect {
				x, y, w, h := form.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is missing (%q):\n%s", y, r, a.screenText(sc))
					break
				}
			}
			text := a.screenText(sc)
			for _, want := range []string{"New branch", "Folder", "gateway", "billing", "Create", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			assertLegible(t, a, sc, "the grouped worktree form")
			// The select opens on the branches, legibly too.
			onLoop(a, func() bool {
				form.SetFocus(form.GetFormItemIndex("gateway"))
				a.tv.SetFocus(form)
				return true
			})
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			waitFor(t, a, sc, "feat/rate")
			assertLegible(t, a, sc, "an open branch select")
		})
	}
}
