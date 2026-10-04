package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

func TestWithDetailTakesWhatTheForgeSaysAndKeepsUnagitsOwn(t *testing.T) {
	old := forge.MergeRequest{
		IID: 7, ID: 70, Title: "Rate limiting", TargetBranch: "main", Comments: 4,
		Instance: "gl", ProjectPath: "acme/gateway",
	}
	later := time.Now().Add(time.Hour)
	det := &forge.MergeRequestDetail{MergeRequest: forge.MergeRequest{
		Title: "Rate limiting (v2)", Draft: true, State: "opened", SourceBranch: "feat/rate",
		TargetBranch: "develop", UpdatedAt: later, WebURL: "https://gl/x/-/merge_requests/7",
		// The forge does not know these two; they must survive.
		Instance: "", ProjectPath: "",
	}, UserNotesCount: 0}

	got := withDetail(old, det)
	if got.Title != "Rate limiting (v2)" || !got.Draft || got.TargetBranch != "develop" || !got.UpdatedAt.Equal(later) {
		t.Errorf("fresh values not taken: %+v", got)
	}
	if got.Comments != 0 {
		t.Errorf("comments = %d, want the forge's 0 (a comment was deleted)", got.Comments)
	}
	if got.Instance != "gl" || got.ProjectPath != "acme/gateway" || got.ID != 70 || got.IID != 7 {
		t.Errorf("unagit's own fields were lost: %+v", got)
	}
}

func TestApplyMRUpdateChangesOneRowAndKeepsTheIndexTime(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")

	indexed := onLoop(a, func() time.Time { return a.mrsUpdated })
	var target forge.MergeRequest
	onLoop(a, func() int {
		for _, mr := range a.mrs {
			if mr.IID == 7 {
				target = mr
			}
		}
		return 0
	})
	fresh := target
	fresh.Title, fresh.Comments = "Rate limiting, reworked", 9
	a.tv.QueueUpdateDraw(func() { a.applyMRUpdate(fresh, true, false) })

	waitFor(t, a, sc, "Rate limiting, reworked")
	// The other rows are as they were, and the list was not re-indexed.
	if s := a.screenText(sc); !strings.Contains(s, "Invoice rounding") || !strings.Contains(s, "Drop the old client") {
		t.Errorf("another row went missing:\n%s", s)
	}
	if got := onLoop(a, func() time.Time { return a.mrsUpdated }); !got.Equal(indexed) {
		t.Errorf("index time moved from %v to %v", indexed, got)
	}
	// It reached the saved index too, so the next start shows it.
	saved, err := index.Load[index.MergeRequests](config.IndexPath("mrs"))
	must(t, err)
	found := false
	for _, mr := range saved.Items {
		if mr.IID == 7 {
			found = mr.Title == "Rate limiting, reworked" && mr.Comments == 9
		}
	}
	if !found {
		t.Errorf("saved index was not updated: %+v", saved.Items)
	}
}

func TestOpeningTheDetailBringsItsRowUpToDate(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")

	comments := func() int {
		return onLoop(a, func() int {
			for _, mr := range a.mrs {
				if mr.IID == 7 {
					return mr.Comments
				}
			}
			return -1
		})
	}
	if got := comments(); got != 4 {
		t.Fatalf("the index says %d comments, want 4", got)
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe") // the detail has loaded; the server says 2 comments

	deadline := time.Now().Add(3 * time.Second)
	for comments() != 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := comments(); got != 2 {
		t.Fatalf("the row still says %d comments after the detail loaded, want 2", got)
	}
}

func TestDetailShowsTheFreshTimeWithoutMovingTheRow(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")

	order := func() []int {
		return onLoop(a, func() []int {
			var iids []int
			for _, i := range a.filterMRs("") {
				iids = append(iids, a.mrs[i].IID)
			}
			return iids
		})
	}
	before := order()
	if len(before) != 3 || before[0] != 7 {
		t.Fatalf("order before: %v", before)
	}
	var target forge.MergeRequest
	onLoop(a, func() int {
		for _, mr := range a.mrs {
			if mr.IID == 7 {
				target = mr
			}
		}
		return 0
	})
	older := target
	older.UpdatedAt = target.UpdatedAt.Add(-5 * time.Hour)

	// Found by the detail: the row says the new time, but stays where it is.
	a.tv.QueueUpdateDraw(func() { a.applyMRUpdate(older, false, true) })
	deadline := time.Now().Add(2 * time.Second)
	shown := func() time.Time {
		return onLoop(a, func() time.Time {
			for _, mr := range a.mrs {
				if mr.IID == 7 {
					return mr.UpdatedAt
				}
			}
			return time.Time{}
		})
	}
	for !shown().Equal(older.UpdatedAt) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !shown().Equal(older.UpdatedAt) {
		t.Fatal("the row did not take the fresh time")
	}
	if got := order(); got[0] != 7 {
		t.Errorf("the row moved while the detail was following the cursor: %v", got)
	}

	// Opening the request lets it take its place.
	a.tv.QueueUpdateDraw(func() { a.applyMRUpdate(older, false, false) })
	deadline = time.Now().Add(2 * time.Second)
	for order()[0] == 7 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := order(); got[0] == 7 {
		t.Errorf("opening should let the row move to where its time puts it: %v", got)
	}
}

// TestRRefreshesTheRow: r asks about the one merge request under the cursor -
// its pipeline and approvals come with it - fetches the one clone, and the
// one worktree; R is the whole list.
func TestRRefreshesTheRow(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/x")
	p.rescan()

	// A clone: origin moved, r sees it.
	other := p.elsewhere("main")
	commitIn(t, other, "theirs.txt", "theirs")
	gitIn(t, other, "push", "-q", "origin", "main")
	typeRunes(sc, "gr")
	waitFor(t, a, sc, "fetched acme/gateway")
	waitFor(t, a, sc, "↓1")

	// A merge request: what a full refresh reads, for this row alone.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "gr")
	waitFor(t, a, sc, "!7 is up to date")
	line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), "Rate limiting")]
	if !strings.Contains(line, "1/2") || !strings.Contains(line, "✗") {
		t.Errorf("the row did not get its approvals and pipeline: %q", line)
	}
	if onLoop(a, func() bool { return a.mrsUpdated.IsZero() }) {
		t.Error("refreshing a row lost the time of the last full refresh")
	}

	// A worktree.
	typeRunes(sc, "3")
	waitFor(t, a, sc, "feat/x")
	typeRunes(sc, "gr")
	waitFor(t, a, sc, "fetched acme/gateway")
}
