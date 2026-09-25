package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// openMRDetail selects the first merge request and loads its detail column.
func openMRDetail(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")
}

// TestDetailShowsOnlyTheNewestComments keeps the column readable; the rest
// live in the modal.
func TestDetailShowsOnlyTheNewestComments(t *testing.T) {
	a, sc := newTestApp(t)
	openMRDetail(t, a, sc)

	waitFor(t, a, sc, "COMMENTS (3 NEWEST OF 4)")
	waitFor(t, a, sc, "retry loop")    // newest
	waitFor(t, a, sc, "third comment") // third newest
	typeRunes(sc, "G")
	waitFor(t, a, sc, "1 more · press c to read them all")
	if strings.Contains(a.screenText(sc), "oldest comment") {
		t.Error("the fourth comment should not be in the detail column")
	}
}

// TestCommentsAreRenderedAsMarkdown: the asterisks become weight, not text.
func TestCommentsAreRenderedAsMarkdown(t *testing.T) {
	a, sc := newTestApp(t)
	openMRDetail(t, a, sc)
	waitFor(t, a, sc, "retry loop")

	if strings.Contains(a.screenText(sc), "**retry loop**") {
		t.Error("the markdown was printed raw")
	}
}

// TestCommentsModalShowsTheWholeConversation
func TestCommentsModalShowsTheWholeConversation(t *testing.T) {
	a, sc := newTestApp(t)
	openMRDetail(t, a, sc)

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	waitFor(t, a, sc, "oldest comment") // the one the detail column left out
	waitFor(t, a, sc, "retry loop")
	// A markdown list is drawn as a list.
	waitFor(t, a, sc, "• first thing")
	// System notes stay out.
	if strings.Contains(a.screenText(sc), "changed title") {
		t.Error("a system note leaked into the conversation")
	}
	waitFor(t, a, sc, "i  write a comment")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Comments · acme/gateway !7")
	// Closing it hands the keyboard back to the detail column.
	waitFor(t, a, sc, "Jane Doe")
}

// TestWriteAComment drives the composer and checks what reached the server.
func TestWriteAComment(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	openMRDetail(t, a, sc)

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Comment on acme/gateway !7")
	waitFor(t, a, sc, "Markdown is understood")

	typeRunes(sc, "looks good to me")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)

	waitFor(t, a, sc, "Comment posted on acme/gateway !7")
	waitGone(t, a, sc, "Comment on acme/gateway !7")

	posted, _ := srv.postedComment.Load().(string)
	if !strings.Contains(posted, `"body":"looks good to me"`) {
		t.Fatalf("posted = %q", posted)
	}
	// The conversation is still open underneath.
	waitFor(t, a, sc, "Comments · acme/gateway !7")
}

// TestComposerRefusesAnEmptyComment
func TestComposerRefusesAnEmptyComment(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	openMRDetail(t, a, sc)
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Comment on acme/gateway !7")

	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Nothing written yet")
	if srv.postedComment.Load() != nil {
		t.Error("an empty comment was sent")
	}
}

// TestApproveAsksFirst: it is visible to everyone, so it is confirmed.
func TestApproveAsksFirst(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	typeRunes(sc, "A")
	waitFor(t, a, sc, "Approve")
	waitFor(t, a, sc, "Everyone on the merge request will see it")
	if srv.approvals.Load() != 0 {
		t.Fatal("approved before being confirmed")
	}

	typeRunes(sc, "n") // cancel
	time.Sleep(100 * time.Millisecond)
	if srv.approvals.Load() != 0 {
		t.Fatal("cancelling still approved")
	}

	typeRunes(sc, "A")
	waitFor(t, a, sc, "Everyone on the merge request will see it")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Approved acme/gateway !7")
	if got := srv.approvals.Load(); got != 1 {
		t.Fatalf("approvals = %d", got)
	}
}

// TestApproveFromTheCommentsModal
func TestApproveFromTheCommentsModal(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	openMRDetail(t, a, sc)
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")

	typeRunes(sc, "A")
	waitFor(t, a, sc, "Everyone on the merge request will see it")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Approved acme/gateway !7")
	if got := srv.approvals.Load(); got != 1 {
		t.Fatalf("approvals = %d", got)
	}
	// And the conversation is still there.
	waitFor(t, a, sc, "Comments · acme/gateway !7")
}

// TestCommentsAreGroupedIntoThreads: a reply belongs under what it answers,
// not wherever its timestamp happens to fall.
func TestCommentsAreGroupedIntoThreads(t *testing.T) {
	a, sc := newTestApp(t)
	openMRDetail(t, a, sc)
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	waitFor(t, a, sc, "retry loop")

	lines := strings.Split(a.screenText(sc), "\n")
	line := func(needle string) int {
		for i, l := range lines {
			if strings.Contains(l, needle) {
				return i
			}
		}
		t.Fatalf("%q is not on screen:\n%s", needle, strings.Join(lines, "\n"))
		return -1
	}

	// The conversations are in the order they were started, and the reply
	// follows the comment it answers even though other comments are older.
	oldest := line("oldest comment")
	third := line("third comment")
	root := line("first thing")
	reply := line("retry loop")
	if !(oldest < third && third < root && root < reply) {
		t.Fatalf("order on screen: oldest %d, third %d, root %d, reply %d", oldest, third, root, reply)
	}

	// Every comment is a box of its own, and the reply's box sits further right
	// than its root's, which is how a reply is told from a comment.
	replyLine, rootLine := lines[reply-1], lines[root-1]
	if !strings.Contains(replyLine, "john") || !strings.Contains(rootLine, "ann") {
		t.Errorf("a byline is not in the row above its text:\n%q\n%q", rootLine, replyLine)
	}
	if column(replyLine, "john") <= column(rootLine, "ann") {
		t.Errorf("the reply is not indented:\n%q\n%q", rootLine, replyLine)
	}
}

// column is where a word starts on screen. The lines are full of box drawing,
// so a byte offset is not a column.
func column(line, word string) int {
	at := strings.Index(line, word)
	if at < 0 {
		return -1
	}
	return len([]rune(line[:at]))
}

// TestEnterOpensTheRowYouAreOn is a bug that made the lists nearly unusable:
// opening the detail narrows the table, which redraws it, and the redraw put
// the cursor back on the first row - so Enter on the fourth merge request
// showed the first one instead.
func TestEnterOpensTheRowYouAreOn(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	typeRunes(sc, "j") // onto the second row, acme/billing !9
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)

	waitFor(t, a, sc, "Bankers rounding everywhere")
	if strings.Contains(a.screenText(sc), "Adds a token bucket") {
		t.Fatal("the detail is of the first row, not the selected one")
	}
	// And it stays there once the follow debounce has had its say.
	time.Sleep(2 * detailDebounce)
	if !strings.Contains(a.screenText(sc), "Bankers rounding everywhere") {
		t.Fatalf("the detail drifted back to another row:\n%s", a.screenText(sc))
	}
	if got := onLoop(a, a.mrsPane.selectedIndex); got != 1 {
		t.Errorf("selected index = %d, want 1", got)
	}
}

// TestRedrawKeepsTheCursor: a refresh, a resize or a filter toggle must not
// move it either.
func TestRedrawKeepsTheCursor(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "jj") // the third row

	// Queued reads can overtake key events that have not been handled yet,
	// so the cursor is waited for rather than read once.
	before := 2
	waitSelected(t, a, a.mrsPane, before)

	resize(sc, 100, 30)
	time.Sleep(150 * time.Millisecond)
	if got := onLoop(a, a.mrsPane.selectedIndex); got != before {
		t.Errorf("a resize moved the cursor from %d to %d", before, got)
	}

	done := make(chan struct{})
	a.tv.QueueUpdateDraw(func() { a.applyFilters(); close(done) })
	<-done
	if got := onLoop(a, a.mrsPane.selectedIndex); got != before {
		t.Errorf("a redraw moved the cursor from %d to %d", before, got)
	}

	// A new filter is different: there the best match is what you want.
	typeRunes(sc, "/inv")
	waitSelected(t, a, a.mrsPane, 1)
}

// waitSelected waits for the cursor to land on a data row.
func waitSelected(t *testing.T, a *App, p *pane, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	got := -1
	for time.Now().Before(deadline) {
		if got = onLoop(a, p.selectedIndex); got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the cursor is on %d, want %d", got, want)
}
