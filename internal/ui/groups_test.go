package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/workspace"
)

// markBoth makes both fixture repositories real clones and marks them in
// Repositories, then opens the grouped worktree form.
func markBoth(t *testing.T, a *App, sc tcell.SimulationScreen, prepare ...func(gw, bl *realProject)) (*realProject, *realProject, *tview.Form) {
	t.Helper()
	waitFor(t, a, sc, "acme/billing")
	gw := newRealProject(t, a, "acme/gateway")
	bl := newRealProject(t, a, "acme/billing")
	for _, f := range prepare {
		f(gw, bl)
	}
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

	// Nothing opens: Worktrees shows what was made.
	waitFor(t, a, sc, "created ")
	waitFor(t, a, sc, "REPOS")
	waitFor(t, a, sc, "feat-multi")
	waitFor(t, a, sc, "no upstream")
	// Enter opens the worktree's own view: the group, then each repository.
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "feat-multi · 2 repositories")
	for _, want := range []string{"every repository", "acme/gateway", "acme/billing", "feat/multi"} {
		waitFor(t, a, sc, want)
	}
	waitFor(t, a, sc, "up to date with origin/main") // read from git
	waitFor(t, a, sc, "clean")

	// origin's main moves on: r fetches, the view says how far each repository
	// is behind its base, and p on the group's block rebases every one.
	moved := gw.elsewhere("main")
	commitIn(t, moved, "later.txt", "main moved on again")
	gitIn(t, moved, "push", "-q", "origin", "main")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "1 new on origin/main")
	typeRunes(sc, "p")
	waitFor(t, a, sc, "1 updated · 1 up to date")
	gitIn(t, filepath.Join(dir, "gateway"), "merge-base", "--is-ancestor", "origin/main", "HEAD")

	// d takes the worktrees and the folder; the branches and clones stay. The
	// update's log closes on its own; Esc is the view's once it is in front.
	waitFocus(t, a, func() bool { name, _ := a.pages.GetFrontPage(); return name == pageWorktree })
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "every repository")
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

// TestGroupedWorktreeStartsANewBranchFromBases: the new branch is required;
// each select is the base it starts from, the main clone's branch first, any
// branch offered but one a worktree has out, and nothing said of where they
// are.
func TestGroupedWorktreeStartsANewBranchFromBases(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	// gateway's clone is on feat/rate, and a worktree has main's sibling out.
	_, _, form := markBoth(t, a, sc, func(gw, bl *realProject) {
		gitIn(t, gw.clone, "push", "-q", "origin", "main:feat/rate")
		gitIn(t, gw.clone, "checkout", "-q", "-b", "feat/rate", "--track", "origin/feat/rate")
		gw.worktree("feat/busy")
		gitIn(t, gw.clone, "push", "-q", "origin", "feat/busy")
	})
	options := func(label string) (current string, all []string) {
		return onLoopPair(a, func() (string, []string) {
			d := form.GetFormItemByLabel(label).(*tview.DropDown)
			at, cur := d.GetCurrentOption()
			for i := 0; i < d.GetOptionCount(); i++ {
				d.SetCurrentOption(i)
				_, text := d.GetCurrentOption()
				all = append(all, text)
			}
			d.SetCurrentOption(at)
			return cur, all
		})
	}
	text := a.screenText(sc)
	if !strings.Contains(text, "Base branch of each repository") {
		t.Errorf("the selects are not said to be bases:\n%s", text)
	}
	if strings.Contains(text, "checked out in") {
		t.Errorf("the selects say where branches are out:\n%s", text)
	}
	cur, all := options("gateway")
	if cur != "feat/rate" {
		t.Errorf("gateway starts from %q, want the clone's feat/rate", cur)
	}
	if strings.Contains(strings.Join(all, ","), "feat/busy") || !strings.Contains(strings.Join(all, ","), "main") {
		t.Errorf("gateway offers %v: main yes, the worktree's feat/busy no", all)
	}
	if cur, _ := options("billing"); cur != "main" {
		t.Errorf("billing starts from %q", cur)
	}

	// Without a new branch nothing is made.
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	typeRunes(sc, "same")
	waitFor(t, a, sc, "same")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "enter the new branch")
	if !onLoop(a, func() bool { return a.pages.HasPage(pageForm) }) {
		t.Error("the dialog closed without a branch")
	}
	if _, err := os.Stat(filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "same")); err == nil {
		t.Error("a refused group left its folder behind")
	}

	// Nor with a branch a repository has already: said at once, by name.
	onLoop(a, func() bool {
		form.GetFormItemByLabel(labelGroupBranch).(*tview.InputField).SetText("feat/busy")
		return true
	})
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "gateway already has feat/busy - give the group another name")
	if !onLoop(a, func() bool { return a.pages.HasPage(pageForm) }) {
		t.Error("the dialog closed on a branch that exists")
	}
}

// onLoopPair is onLoop for two values.
func onLoopPair[A, B any](a *App, read func() (A, B)) (A, B) {
	type pair struct {
		a A
		b B
	}
	p := onLoop(a, func() pair { x, y := read(); return pair{x, y} })
	return p.a, p.b
}

// TestGroupedWorktreeBranchesFromTheClonesBranch: with the clone on another
// branch, the group's branch starts there.
func TestGroupedWorktreeBranchesFromTheClonesBranch(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc, func(gw, bl *realProject) {
		gitIn(t, gw.clone, "checkout", "-q", "-b", "elsewhere")
		commitIn(t, gw.clone, "e.txt", "on elsewhere")
	})
	typeRunes(sc, "feat/mixed")
	waitFor(t, a, sc, "feat-mixed")
	pressButton(t, a, sc, form, "Create")

	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-mixed")
	waitForPath(t, a, sc, filepath.Join(dir, workspace.GroupFile), true)
	gitIn(t, filepath.Join(dir, "gateway"), "merge-base", "--is-ancestor", "elsewhere", "HEAD")
	if got := gitIn(t, filepath.Join(dir, "billing"), "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/mixed" {
		t.Errorf("billing is on %q", got)
	}
	waitFor(t, a, sc, "created ")
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

// TestCtrlWShowsTheNewWorktree: a worktree made with Ctrl-W is not opened;
// Worktrees comes up with the cursor on it.
func TestCtrlWShowsTheNewWorktree(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	newRealProject(t, a, "acme/gateway")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlW, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Worktree branch")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New worktree branch")
	typeRunes(sc, "feat/fresh")
	waitFor(t, a, sc, "feat/fresh")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing
	typeRunes(sc, "r")
	waitFor(t, a, sc, "created ")
	got := onLoop(a, func() string {
		if a.currentTab() != pageWorktrees {
			return "not in Worktrees"
		}
		i := a.worktreesPane.selectedIndex()
		if i < 0 {
			return "nothing selected"
		}
		return a.worktrees[i].Branch
	})
	if got != "feat/fresh" {
		t.Errorf("the cursor is on %q", got)
	}
	if strings.Contains(a.screenText(sc), "opened ") {
		t.Error("the editor was started")
	}
}

// TestGroupMergeRequestsLinkEachOther: n on a grouped worktree opens a merge
// request in every repository, into the branch each was made from, under one
// title, then gives every description the links to the others.
func TestGroupMergeRequestsLinkEachOther(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/both")
	waitFor(t, a, sc, "feat-both")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-both")
	commitIn(t, filepath.Join(dir, "gateway"), "g.txt", "Count requests per client")
	commitIn(t, filepath.Join(dir, "billing"), "b.txt", "Bill per counted request")
	onLoop(a, func() bool { a.refreshDisk(); return true })
	waitFor(t, a, sc, "no upstream")

	typeRunes(sc, "n")
	waitFor(t, a, sc, "New merge requests · feat-both")
	for _, want := range []string{"gateway into", "billing into", "Both"} {
		waitFor(t, a, sc, want)
	}
	mrForm := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	pressButton(t, a, sc, mrForm, "Create")
	waitFor(t, a, sc, "2 merge request(s) created")

	for _, posted := range []*atomic.Value{&srv.postedMR, &srv.postedMR2} {
		body, _ := posted.Load().(string)
		for _, want := range []string{`"source_branch":"feat/both"`, `"target_branch":"main"`, `"title":"Both"`,
			"Count requests per client", "Bill per counted request"} {
			if !strings.Contains(body, want) {
				t.Errorf("a merge request was created without %s: %s", want, body)
			}
		}
	}
	for path, other := range map[string]string{
		"/api/v4/projects/1/merge_requests/42": "acme/billing/-/merge_requests/43",
		"/api/v4/projects/2/merge_requests/43": "acme/gateway/-/merge_requests/42",
	} {
		got, ok := srv.described.Load(path)
		if !ok {
			t.Errorf("%s was not given the links", path)
			continue
		}
		if !strings.Contains(got.(string), "Related merge requests:") || !strings.Contains(got.(string), other) {
			t.Errorf("%s does not link %s: %s", path, other, got)
		}
	}
	// Both branches are on origin now.
	for _, name := range []string{"gateway", "billing"} {
		gitIn(t, filepath.Join(dir, name), "rev-parse", "--verify", "origin/feat/both")
	}
}

// TestCtrlWOffersOnlyBranchesThatCanHaveAWorktree: main is out in the main
// clone, so git would refuse it a worktree, and it is not offered; feat/rate
// is.
func TestCtrlWOffersOnlyBranchesThatCanHaveAWorktree(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	newRealProject(t, a, "acme/gateway")
	onLoop(a, func() bool { a.refreshDisk(); return true })
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlW, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Worktree branch")
	waitFor(t, a, sc, "feat/rate")
	for _, l := range strings.Split(a.screenText(sc), "\n") {
		if strings.Contains(l, "Add rate limiting") {
			t.Errorf("main, checked out in the main clone, is offered:\n%s", a.screenText(sc))
		}
	}
}

// TestAMarkedRowIsABandOfItsOwn: a marked row has a background of its own
// from one end to the other, not the cursor's grey, and the cursor on it is a
// brighter step of the same hue.
func TestAMarkedRowIsABandOfItsOwn(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/billing")
	typeRunes(sc, " ") // marks the first row and moves to the next
	waitFor(t, a, sc, "SELECT 1")
	marked := onLoop(a, func() string { return a.projects[a.projectsPane.marked()[0]].PathWithNamespace })
	row := rowOf(t, a, sc, marked)
	line := []rune(strings.Split(a.screenText(sc), "\n")[row])
	for _, x := range []int{3, len(line) / 2, len(line) - 3} {
		if _, bg, _ := cellStyleAt(a, sc, x, row).Decompose(); bg != colMarked {
			t.Errorf("column %d of the marked row is on %v, want %v:\n%s", x, bg, colMarked, a.screenText(sc))
		}
	}
	typeRunes(sc, "k")
	_, want, _ := styleMarkedSelected.Decompose()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, bg, _ := cellStyleAt(a, sc, len(line)/2, row).Decompose()
		if bg == want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the cursor on a marked row is on %v, want %v", bg, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
	assertLegible(t, a, sc, "a marked row")
}
