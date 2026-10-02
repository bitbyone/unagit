package ui

import (
	"path/filepath"
	"strings"
	"testing"

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
