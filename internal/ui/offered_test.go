package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// offered names the actions the pickers offer now, of a list read on the
// loop.
func offered(a *App, list func() []uiAction) []string {
	return onLoop(a, func() []string {
		var names []string
		for _, act := range available(list()) {
			names = append(names, act.name)
		}
		return names
	})
}

// waitOffered waits until an action is offered, or is not.
func waitOffered(t *testing.T, a *App, list func() []uiAction, name string, want bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if slices.Contains(offered(a, list), name) == want {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("%q offered: want %v, have %v", name, want, offered(a, list))
}

// TestOfferedOnlyWhatCanBeDone: a clean clone is offered no commit, push
// or diff of its changes; once it has an edit and a commit of its own, it
// is. A merge request with no branch worktree is offered no pull and no
// changes, and a review not yet made is offered to be prepared.
func TestOfferedOnlyWhatCanBeDone(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.rescan()
	repo := func() []uiAction {
		for _, pr := range a.projects {
			if pr.PathWithNamespace == p.path {
				return a.repositoryActions(a.projectsPane, pr)
			}
		}
		return nil
	}
	waitFor(t, a, sc, "✓")
	for _, name := range []string{"Commit…", "Push", "Show Uncommitted Changes"} {
		waitOffered(t, a, repo, name, false)
	}
	commitIn(t, p.clone, "b.txt", "Count requests")
	must(t, os.WriteFile(filepath.Join(p.clone, "a.txt"), []byte("edited\n"), 0o644))
	p.rescan()
	for _, name := range []string{"Commit…", "Push", "Show Uncommitted Changes"} {
		waitOffered(t, a, repo, name, true)
	}

	mr := func() []uiAction { return a.mergeRequestActions(a.mrsPane, a.mrs[0]) }
	have := offered(a, mr)
	for _, name := range []string{"Pull Branch", "Changes…", "Publish Comments"} {
		if slices.Contains(have, name) {
			t.Errorf("a merge request with no branch worktree, Incomm off, is offered %q", name)
		}
	}
	if !slices.Contains(have, "Prepare Review") {
		t.Error("a merge request with no review is not offered to prepare one")
	}
}

// TestLogAndBranchesOfferWhatTheRowCanHave: a commit no remote has is not
// offered its page or its pipelines; the commit checked out is not offered
// a checkout; a branch only on origin is not offered a local delete.
func TestLogAndBranchesOfferWhatTheRowCanHave(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	commitIn(t, p.clone, "b.txt", "Count requests")
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Show Diff in Hunk")
	text := a.screenText(sc)
	for _, name := range []string{"Open in Browser", "Show Pipelines…", "Check Out Commit"} {
		if strings.Contains(text, name) {
			t.Errorf("the newest commit, checked out and on no remote, is offered %q", name)
		}
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Show Diff in Hunk")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Check Out Commit")
	waitFor(t, a, sc, "Show Pipelines…")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Check Out Commit")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log ·")

	// The server's feat/rate is on origin alone.
	typeRunes(sc, "b")
	waitFor(t, a, sc, "feat/rate")
	found := false
	for _, key := range []string{"g", "G"} {
		typeRunes(sc, key)
		sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
		waitFor(t, a, sc, "Delete on Origin…")
		if strings.Contains(a.screenText(sc), "╭ feat/rate") {
			found = true
			if strings.Contains(a.screenText(sc), "Delete Locally…") {
				t.Error("a branch only on origin is offered a local delete")
			}
		}
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitGone(t, a, sc, "Delete on Origin…")
	}
	if !found {
		t.Fatalf("feat/rate was never under the cursor:\n%s", a.screenText(sc))
	}
}
