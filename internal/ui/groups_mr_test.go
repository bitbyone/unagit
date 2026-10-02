package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/workspace"
)

// TestRelatedListIsReplacedNotAddedTo: linking again writes the list afresh;
// what the author wrote stays, and nothing is listed twice.
func TestRelatedListIsReplacedNotAddedTo(t *testing.T) {
	a := forge.MergeRequest{ProjectPath: "acme/api", IID: 1, WebURL: "https://x/api/1", Title: "API"}
	b := forge.MergeRequest{ProjectPath: "acme/web", IID: 2, WebURL: "https://x/web/2", Title: "Web"}
	c := forge.MergeRequest{ProjectPath: "acme/cli", IID: 3, WebURL: "https://x/cli/3", Title: "CLI"}

	once := relatedDescription("Why this change.\n\nDetails.", a, []forge.MergeRequest{a, b})
	twice := relatedDescription(once, a, []forge.MergeRequest{a, b, c})
	if strings.Count(twice, relatedHeading) != 1 {
		t.Errorf("the list was added, not replaced:\n%s", twice)
	}
	for _, want := range []string{"Why this change.\n\nDetails.", "acme/web !2", "acme/cli !3"} {
		if !strings.Contains(twice, want) {
			t.Errorf("%q is missing:\n%s", want, twice)
		}
	}
	if strings.Contains(twice, "acme/api !1") {
		t.Errorf("a merge request links itself:\n%s", twice)
	}
	if again := relatedDescription(twice, a, []forge.MergeRequest{a, b, c}); again != twice {
		t.Errorf("the same group gave another description:\n%s\n---\n%s", twice, again)
	}
}

// TestALaterRoundLinksTheEarlierMergeRequests: a repository that gets its
// changes after the first round gets its merge request in a second, and the
// merge request opened in the first is linked to it.
func TestALaterRoundLinksTheEarlierMergeRequests(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/both")
	waitFor(t, a, sc, "feat-both")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-both")
	openForm := func() {
		onLoop(a, func() bool { a.refreshDisk(); return true })
		waitFor(t, a, sc, "no upstream")
		typeRunes(sc, "n")
		waitFor(t, a, sc, "New merge requests · feat-both")
		mrForm := onLoop(a, func() *tview.Form {
			_, primitive := a.pages.GetFrontPage()
			return primitive.(*modalBox).content.(*tview.Form)
		})
		pressButton(t, a, sc, mrForm, "Create")
	}

	// First round: only gateway has something to merge.
	commitIn(t, filepath.Join(dir, "gateway"), "g.txt", "Count requests per client")
	openForm()
	waitFor(t, a, sc, "1 merge request(s) created")
	waitFor(t, a, sc, "nothing to merge into main")
	typeRunes(sc, "c") // close the summary
	waitGone(t, a, sc, "Merge requests created")

	// Second round: billing catches up.
	commitIn(t, filepath.Join(dir, "billing"), "b.txt", "Bill per counted request")
	openForm()
	waitFor(t, a, sc, "1 merge request(s) created")
	for path, other := range map[string]string{
		"/api/v4/projects/1/merge_requests/42": "acme/billing/-/merge_requests/43",
		"/api/v4/projects/2/merge_requests/43": "acme/gateway/-/merge_requests/42",
	} {
		got, ok := srv.described.Load(path)
		if !ok || !strings.Contains(got.(string), other) {
			t.Errorf("%s does not link %s: %v", path, other, got)
		}
	}
}

// TestAGroupsCommentsReachTheRightMergeRequest: comments kept in the group's
// one Incomm store are counted in Worktrees and belong, path and all, to the
// merge request of the repository they are on.
func TestAGroupsCommentsReachTheRightMergeRequest(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	onLoop(a, func() bool { a.cfg.Integrations.Incomm = true; return true })
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/talk")
	waitFor(t, a, sc, "feat-talk")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-talk")
	if _, err := os.Stat(filepath.Join(dir, ".incomm")); err != nil {
		t.Fatalf("the group has no Incomm store of its own: %v", err)
	}
	must(t, os.WriteFile(filepath.Join(dir, ".incomm", "notes.json"), []byte(`{"version":2,"notes":[
 {"id":"g1","file":"gateway/a.txt","startLine":1,"author":"user","content":"on the gateway","audience":"external",
  "replies":[{"id":"g2","author":"agent","content":"agreed","audience":"agent"}]},
 {"id":"b1","file":"billing/a.txt","startLine":1,"author":"user","content":"on billing","audience":"agent"}
]}`), 0o644))

	// The gateway's merge request, as the index would have it.
	mr := onLoop(a, func() forge.MergeRequest {
		mr := forge.MergeRequest{ID: 900, IID: 77, ProjectID: 1, ProjectPath: "acme/gateway", SourceBranch: "feat/talk",
			TargetBranch: "main", Title: "Talk", State: "opened", Instance: a.cfg.Instances[0].ID}
		a.mrs = append(a.mrs, mr)
		a.refreshDisk()
		return mr
	})
	waitFor(t, a, sc, "COM")
	waitForRow(t, a, sc, "feat-talk", "3") // two on the gateway, one on billing

	threads := onLoop(a, func() []string {
		var out []string
		for _, th := range a.localThreads(mr) {
			out = append(out, th.File+" "+th.Root.Content)
		}
		return out
	})
	if len(threads) != 1 || threads[0] != "a.txt on the gateway" {
		t.Errorf("the gateway's merge request reads %q", threads)
	}
	if pending := onLoop(a, func() int {
		return a.diskOf(mr.Instance, "acme/gateway").MRs[mr.IID].Pending
	}); pending != 1 {
		t.Errorf("the gateway's merge request has %d waiting, want the one external comment", pending)
	}
}

// TestARepositoryCanBeLeftOut: "(no merge request)" keeps a repository out of
// the round, and its merge request is not opened.
func TestARepositoryCanBeLeftOut(t *testing.T) {
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
	waitFor(t, a, sc, "billing into")
	mrForm := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	onLoop(a, func() bool {
		mrForm.GetFormItemByLabel("billing into").(*tview.DropDown).SetCurrentOption(0)
		return true
	})
	pressButton(t, a, sc, mrForm, "Create")
	waitFor(t, a, sc, "1 merge request(s) created")
	if body, _ := srv.postedMR2.Load().(string); body != "" {
		t.Errorf("billing was left out, yet its merge request was opened: %s", body)
	}
	if body, _ := srv.postedMR.Load().(string); !strings.Contains(body, `"source_branch":"feat/both"`) {
		t.Errorf("gateway's merge request was not opened: %s", body)
	}
}

// TestAClosedMergeRequestIsLetGoOnRefresh: r in Worktrees finds a merge request
// closed on the forge, drops it, and the worktree is free to open a new one.
func TestAClosedMergeRequestIsLetGoOnRefresh(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/both")
	waitFor(t, a, sc, "feat-both")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-both")
	commitIn(t, filepath.Join(dir, "gateway"), "g.txt", "Count requests per client")
	onLoop(a, func() bool { a.refreshDisk(); return true })
	waitFor(t, a, sc, "no upstream")
	typeRunes(sc, "n")
	waitFor(t, a, sc, "billing into")
	mrForm := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	pressButton(t, a, sc, mrForm, "Create")
	waitFor(t, a, sc, "1 merge request(s) created")
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Merge requests created")
	waitFor(t, a, sc, "!42")

	srv.closed42.Store(true)
	typeRunes(sc, "r")
	waitFor(t, a, sc, "no longer open: !42 closed")
	deadline := time.Now().Add(5 * time.Second)
	for strings.Contains(rowWith(a, sc, "feat-both"), "!42") {
		if time.Now().After(deadline) {
			t.Fatalf("the row still names the closed merge request: %q", rowWith(a, sc, "feat-both"))
		}
		time.Sleep(30 * time.Millisecond)
	}
	if onLoop(a, func() bool {
		for _, mr := range a.mrs {
			if mr.IID == 42 {
				return true
			}
		}
		return false
	}) {
		t.Error("the closed merge request is still in the index")
	}
	if _, err := os.Stat(filepath.Join(dir, "gateway")); err != nil {
		t.Errorf("the worktree went with the merge request: %v", err)
	}
}
