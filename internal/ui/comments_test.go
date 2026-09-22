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

	typeRunes(sc, "a")
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

	typeRunes(sc, "a")
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

	typeRunes(sc, "a")
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

	// The reply is marked, and its byline sits further right than its root's.
	replyLine, rootLine := lines[reply-1], lines[root-1]
	if !strings.Contains(replyLine, "↳") {
		t.Errorf("the reply is not marked: %q", replyLine)
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
