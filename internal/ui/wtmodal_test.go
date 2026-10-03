package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/workspace"
)

// lookGroup makes a grouped worktree of both fixture repositories with a
// commit in the gateway, and opens its view.
func lookGroup(t *testing.T, a *App, sc tcell.SimulationScreen) string {
	t.Helper()
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/view")
	waitFor(t, a, sc, "feat-view")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-view")
	commitIn(t, filepath.Join(dir, "gateway"), "g.txt", "Count requests per client")
	onLoop(a, func() bool { a.refreshDisk(); return true })
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "every repository")
	return dir
}

// TestAWorktreeIsAViewOfBlocks: Enter opens the view, the group first; j lights
// a repository and the keys turn to it; l lists its commits; Esc goes back.
func TestAWorktreeIsAViewOfBlocks(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	lookGroup(t, a, sc)
	waitFor(t, a, sc, "1 commit(s) of its own")
	waitFor(t, a, sc, "a add · r refresh") // the group's keys
	if strings.Contains(a.screenText(sc), "x take out") {
		t.Error("the group's block offers taking a repository out")
	}

	typeRunes(sc, "j")
	waitFor(t, a, sc, "x take out") // a repository's keys
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · acme/gateway")
	waitFor(t, a, sc, "Take this repository out of the group")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · acme/gateway")
	typeRunes(sc, "l")
	waitFor(t, a, sc, "Commits of feat/view since main")
	waitFor(t, a, sc, "Count requests per client")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commits of feat/view")

	// Said over the view, not under it in the status line; Esc puts it away.
	typeRunes(sc, "c")
	waitFor(t, a, sc, "no merge request is open from feat/view")
	waitFor(t, a, sc, "Esc close")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Esc close")
	waitFor(t, a, sc, "every repository")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "every repository")
	waitFor(t, a, sc, "REPOS")
}

// TestTheWorktreeViewIsDrawnWhole: from Enter until git has answered, the
// view says it is reading and shows no block; the first blocks it shows are
// every block, each with all of its rows.
func TestTheWorktreeViewIsDrawnWhole(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/whole")
	waitFor(t, a, sc, "feat-whole")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	deadline := time.Now().Add(10 * time.Second)
	for {
		text := a.screenText(sc)
		if strings.Contains(text, "every repository") {
			if n := strings.Count(text, "Base "); n != 2 {
				t.Fatalf("the view was drawn with %d of 2 repositories complete:\n%s", n, text)
			}
			if strings.Contains(text, "looking") {
				t.Fatalf("the view shows rows still being read:\n%s", text)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the view never came:\n%s", text)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAWorktreeOfItsOwnIsAViewToo: a single worktree is one block, with the
// same keys.
func TestAWorktreeOfItsOwnIsAViewToo(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/solo")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "feat/solo")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "c comments")
	text := a.screenText(sc)
	if strings.Contains(text, "every repository") || strings.Contains(text, "x take out") {
		t.Errorf("a worktree of its own is shown as a group:\n%s", text)
	}
}

// TestTheWorktreeViewFitsItsFrame draws the view at several sizes.
func TestTheWorktreeViewFitsItsFrame(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := newTestAppSrv(t)
			resize(sc, size.w, size.h)
			lookGroup(t, a, sc)
			waitFor(t, a, sc, "Esc back")
			lines := strings.Split(a.screenText(sc), "\n")
			top := -1
			for i, l := range lines {
				if strings.Contains(l, "feat-view · 2 repositories") {
					top = i
				}
			}
			if top < 0 {
				t.Fatalf("no frame:\n%s", a.screenText(sc))
			}
			right := -1
			for i, r := range []rune(lines[top]) {
				if r == '╮' {
					right = i
				}
			}
			for i := top + 1; i < len(lines) && !strings.Contains(lines[i], "╰"); i++ {
				if r := []rune(lines[i]); len(r) <= right || r[right] != '│' {
					t.Errorf("row %d: the frame's right border is drawn over:\n%s", i, a.screenText(sc))
					break
				}
			}
			assertLegible(t, a, sc, "the worktree view")
		})
	}
}
