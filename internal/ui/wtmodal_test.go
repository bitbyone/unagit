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

// drawGroup gives the production view a group's rows and git facts. Workflow
// tests above and below still make real worktrees; a palette or border check
// need not create them again for every theme and terminal size.
func drawGroup(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	dir := filepath.Join(a.cfg.Root(), ".unagit", "groups", "feat-view")
	instance := a.cfg.Instances[0].ID
	members := []worktreeRow{
		{Instance: instance, Path: "acme/gateway", Branch: "feat/view", Dir: filepath.Join(dir, "gateway"), Group: dir},
		{Instance: instance, Path: "acme/billing", Branch: "feat/view", Dir: filepath.Join(dir, "billing"), Group: dir},
	}
	a.tv.QueueUpdateDraw(func() {
		a.wtRemote = map[string]remoteState{
			members[0].Dir: {Base: "main", Onto: "origin/main", Own: 1},
			members[1].Dir: {Base: "main", Onto: "origin/main"},
		}
		a.makeWorktreeView(worktreeRow{Path: "feat-view", Branch: "feat/view", Dir: dir, Members: members})
		a.wtView.loaded = true
		a.wtView.facts = map[string]wtFacts{
			members[0].Dir: {loaded: true, onto: "origin/main", ownCount: 1, own: []string{"abc123  Count requests per client (jane, just now)"}},
			members[1].Dir: {loaded: true, onto: "origin/main"},
		}
		a.renderWorktreeView()
	})
	waitFor(t, a, sc, "every repository")
}

// TestAWorktreeIsAViewOfBlocks: Enter opens the view, the group first; j lights
// a repository and the keys turn to it; l lists its commits; Esc goes back.
func TestAWorktreeIsAViewOfBlocks(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
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
	waitFor(t, a, sc, "Remove from Group…")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · acme/gateway")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (feat/view)")
	waitFor(t, a, sc, "Count requests per client")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log · acme/gateway")

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
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/whole")
	waitFor(t, a, sc, "feat-whole")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	deadline := time.Now().Add(patience)
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
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/solo")
	p.rescan()
	typeRunes(sc, "3")
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
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	drawGroup(t, a, sc)
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			resizeApp(a, sc, size.w, size.h)
			waitFor(t, a, sc, "R all")
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
