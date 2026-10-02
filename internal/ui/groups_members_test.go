package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/workspace"
)

// TestAGroupGrowsAndShrinks: a takes a repository into a grouped worktree on
// the group's branch, x lets it go again, its branch kept.
func TestAGroupGrowsAndShrinks(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/billing")
	newRealProject(t, a, "acme/gateway")
	bl := newRealProject(t, a, "acme/billing")
	onLoop(a, func() bool { a.refreshDisk(); return true })

	// A group of the gateway alone.
	typeRunes(sc, "g ")
	waitFor(t, a, sc, "SELECT 1")
	sc.InjectKey(tcell.KeyCtrlW, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Grouped worktree · 1 repositories")
	form := currentForm(a)
	typeRunes(sc, "feat/grow")
	waitFor(t, a, sc, "feat-grow")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-grow")

	typeRunes(sc, "a")
	waitFor(t, a, sc, "Add a repository to - feat-grow")
	waitFor(t, a, sc, "acme/billing")
	if strings.Contains(a.screenText(sc), "acme/gateway") {
		t.Errorf("a repository in the group is offered again:\n%s", a.screenText(sc))
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Add acme/billing to feat-grow")
	waitFor(t, a, sc, "The group's branch feat/grow is made in billing")
	pressButton(t, a, sc, currentForm(a), "Add")
	waitFor(t, a, sc, "added acme/billing to feat-grow")

	if got := gitIn(t, filepath.Join(dir, "billing"), "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/grow" {
		t.Errorf("billing joined on %q", got)
	}
	g, err := workspace.ReadGroup(dir)
	must(t, err)
	if len(g.Members) != 2 || g.Members[1].Dir != "billing" || g.Members[1].Base != "main" {
		t.Errorf("the group says %+v", g.Members)
	}
	waitForRow(t, a, sc, "feat-grow", "2")

	typeRunes(sc, "x")
	waitFor(t, a, sc, "Take a repository out of - feat-grow")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Take out of the group")
	typeRunes(sc, "t")
	waitForPath(t, a, sc, filepath.Join(dir, "billing"), false)
	waitForRow(t, a, sc, "feat-grow", "1") // the description is written last
	g, err = workspace.ReadGroup(dir)
	must(t, err)
	if len(g.Members) != 1 || g.Members[0].Dir != "gateway" {
		t.Errorf("the group says %+v", g.Members)
	}
	if !strings.Contains(gitIn(t, bl.clone, "branch", "--list", "feat/grow"), "feat/grow") {
		t.Error("taking billing out took its branch")
	}

	// The last one stays.
	typeRunes(sc, "x")
	waitFor(t, a, sc, "d deletes the group")
	if _, err := os.Stat(filepath.Join(dir, "gateway")); err != nil {
		t.Errorf("the last repository went: %v", err)
	}
}

// TestPushLeavesEmptyBranchesAndUTakesThemBack: P pushes only the branches
// that have commits of their own; U deletes a branch on origin again, leaving
// the local one.
func TestPushLeavesEmptyBranchesAndUTakesThemBack(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	gw, bl, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/push")
	waitFor(t, a, sc, "feat-push")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-push")
	onLoop(a, func() bool { a.refreshDisk(); return true })
	waitFor(t, a, sc, "no upstream")

	onOrigin := func(p *realProject) bool {
		return strings.Contains(gitIn(t, p.origin, "branch", "--list", "feat/push"), "feat/push")
	}
	typeRunes(sc, "P")
	waitFor(t, a, sc, "nothing to push from feat-push")
	if onOrigin(gw) || onOrigin(bl) {
		t.Fatal("an empty branch was pushed")
	}

	commitIn(t, filepath.Join(dir, "gateway"), "g.txt", "Count requests")
	onLoop(a, func() bool { a.refreshDisk(); return true })
	// The gateway's commit is counted before P is pressed.
	deadline := time.Now().Add(5 * time.Second)
	for onLoop(a, func() int { return a.wtRemote[filepath.Join(dir, "gateway")].Own }) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the gateway's own commit was never counted")
		}
		time.Sleep(30 * time.Millisecond)
	}
	typeRunes(sc, "P")
	deadline = time.Now().Add(5 * time.Second)
	for !onOrigin(gw) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !onOrigin(gw) || onOrigin(bl) {
		t.Fatalf("pushed: gateway %v, billing %v; want the gateway alone", onOrigin(gw), onOrigin(bl))
	}

	onLoop(a, func() bool { a.refreshDisk(); return true })
	waitFor(t, a, sc, "1/2") // the gateway in sync, billing not pushed
	typeRunes(sc, "U")
	waitFor(t, a, sc, "Delete these branches on origin?")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "deleted on origin: feat/push")
	if onOrigin(gw) {
		t.Error("the branch is still on origin")
	}
	if up, err := exec.Command("git", "-C", filepath.Join(dir, "gateway"), "rev-parse", "--abbrev-ref", "@{upstream}").Output(); err == nil {
		t.Errorf("the local branch still tracks %s", up)
	}
	if !strings.Contains(gitIn(t, gw.clone, "branch", "--list", "feat/push"), "feat/push") {
		t.Error("the local branch went too")
	}
}
