package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// openMRDetail selects the first merge request and loads its detail column.
func openMRDetail(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")
}

// TestDetailShowsOnlyTheNewestComments keeps the column readable; the rest
// live in the modal.
func TestDetailShowsOnlyTheNewestComments(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
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
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	openMRDetail(t, a, sc)
	waitFor(t, a, sc, "retry loop")

	if strings.Contains(a.screenText(sc), "**retry loop**") {
		t.Error("the markdown was printed raw")
	}
}

// TestCommentsModalShowsTheWholeConversation
func TestCommentsModalShowsTheWholeConversation(t *testing.T) {
	t.Parallel()
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
	waitFor(t, a, sc, "i reply")
	waitFor(t, a, sc, "c comment")

	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Comments · acme/gateway !7")
	// Closing it hands the keyboard back to the detail column.
	waitFor(t, a, sc, "Jane Doe")
}

// TestWriteAComment drives the composer and checks what reached the server.
func TestWriteAComment(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
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
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
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
	t.Parallel()
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
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
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
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "jj") // the third row

	// Queued reads can overtake key events that have not been handled yet,
	// so the cursor is waited for rather than read once.
	before := 2
	waitSelected(t, a, a.mrsPane, before)

	resizeApp(a, sc, 100, 30)
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
	deadline := time.Now().Add(patience)
	got := -1
	for time.Now().Before(deadline) {
		if got = onLoop(a, p.selectedIndex); got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the cursor is on %d, want %d", got, want)
}

// TestCommentsOpenAtTheTop: the forge's conversation comes first and the
// local threads after it, so the modal opens on the first thing others said,
// not scrolled down to the last line. The terminal is kept short so the
// conversation does not fit and where it is scrolled to shows.
func TestCommentsOpenAtTheTop(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 140, 16)
	openMRDetail(t, a, sc)

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	waitGone(t, a, sc, "Loading the conversation")
	waitFor(t, a, sc, "oldest comment")
	if text := a.screenText(sc); strings.Contains(text, "retry loop") {
		t.Fatalf("the whole conversation fits, so the test shows nothing:\n%s", text)
	}
	// And j still scrolls down to the rest.
	for range 30 {
		typeRunes(sc, "j")
	}
	waitFor(t, a, sc, "retry loop")
}

// codeForge is the fake server's client with a thread on a range of code
// added to the conversation - written on an older version of the diff,
// which numbers its lines differently from the latest, and a later
// version's change to the line noted in it - the diffs and the file those
// are read in, and the replies kept instead of sent.
type codeForge struct {
	forge.Provider
	mu       sync.Mutex
	replies  []string
	resolved []string
	started  []string
}

const (
	// The latest diff has a line more before the commented ones.
	latestLimit = "@@ -8,5 +8,7 @@ func limit() {\n \tfor {\n+\t\tcount++\n \t\tif ok() {\n \t\t\treturn\n \t\t}\n+\t\ttime.Sleep(backoff)\n \t}\n"
	olderLimit  = "@@ -8,9 +8,10 @@ func limit() {\n \tfor {\n \t\tif ok() {\n \t\t\treturn\n \t\t}\n+\t\ttime.Sleep(backoff)\n \t\tlog(1)\n \t\tlog(2)\n \t\tlog(3)\n \t\tlog(4)\n \t\tlog(5)\n \t}\n"
	changeLimit = "@@ -11,3 +11,3 @@\n \t\t}\n-\t\ttime.Sleep(backoff)\n+\t\ttime.Sleep(jitter(backoff))\n \t}\n"
)

func (f *codeForge) MergeRequestNotes(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Note, error) {
	notes, err := f.Provider.MergeRequestNotes(ctx, mr, limit)
	return append(notes,
		forge.Note{ID: 70, Thread: "on-code", Body: "this sleeps on every retry", Path: "limit.go", Line: 12,
			Base: "b1", Head: "h1", From: forge.LineRef{Old: 11, New: 11}, To: forge.LineRef{New: 12},
			CreatedAt: time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC), Author: forge.User{Username: "carol"}, Resolvable: true},
		forge.Note{ID: 71, Thread: "on-code", System: true, Body: "changed this line in [version 2 of the diff](/x)",
			Changed:   &forge.LineChange{From: "h1", To: "h2", Line: forge.LineRef{New: 12}},
			CreatedAt: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC), Author: forge.User{Username: "dave"}}), err
}

func (f *codeForge) MergeRequestDiffs(context.Context, forge.MergeRequest) ([]forge.FileDiff, error) {
	return []forge.FileDiff{{Path: "limit.go", OldPath: "limit.go", Diff: latestLimit}}, nil
}

func (f *codeForge) CompareDiffs(_ context.Context, _ forge.MergeRequest, from, to string) ([]forge.FileDiff, error) {
	switch from + ".." + to {
	case "b1..h1":
		return []forge.FileDiff{{Path: "limit.go", OldPath: "limit.go", Diff: olderLimit}}, nil
	case "h1..h2":
		return []forge.FileDiff{{Path: "limit.go", OldPath: "limit.go", Diff: changeLimit}}, nil
	}
	return nil, fmt.Errorf("no compare of %s..%s", from, to)
}

func (f *codeForge) ResolveDiscussion(_ context.Context, _ forge.MergeRequest, thread string, resolved bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, fmt.Sprintf("%s %v", thread, resolved))
	return nil
}

func (f *codeForge) CreateDiscussion(_ context.Context, _ forge.MergeRequest, path string, line int, body string) (*forge.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, fmt.Sprintf("%s:%d %s", path, line, body))
	return &forge.Note{Path: path, Line: line, Body: body}, nil
}

// kept is what the fake was asked to do, read under its lock.
func (f *codeForge) kept(what *[]string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), (*what)...)
}

func (f *codeForge) FileAt(_ context.Context, _ forge.MergeRequest, path, ref string) (string, error) {
	if path != "limit.go" || ref != "h1" && ref != "feat/rate" {
		return "", fmt.Errorf("no %s at %s", path, ref)
	}
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "line %d of the file\n", i)
	}
	return b.String(), nil
}

func (f *codeForge) ReplyToDiscussion(_ context.Context, _ forge.MergeRequest, thread, body string) (*forge.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, thread+": "+body)
	return &forge.Note{Thread: thread, Body: body}, nil
}

// TestCommentsAreChosenAnsweredAndReadBesideTheirCode: a thread on code is
// one block - the lines it is about, from the diff it was written on, and
// its comments under them; j and k choose a comment, i answers it in its
// thread, Enter shows the whole diff of its file, or on the note of a later
// change, that change; e shows the whole file. However wide the terminal,
// the conversation is no wider than reads well.
func TestCommentsAreChosenAnsweredAndReadBesideTheirCode(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 220, 60)
	openMRDetail(t, a, sc)
	fake := onLoop(a, func() *codeForge {
		id := a.cfg.Instances[0].ID
		f := &codeForge{Provider: a.clients[id]}
		a.clients[id] = f
		return f
	})

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	waitFor(t, a, sc, "limit.go:11-12")
	// The older diff's lines, the four after the commented ones too.
	waitFor(t, a, sc, "log(4)")
	if text := a.screenText(sc); strings.Contains(text, "count++") || strings.Contains(text, "log(5)") {
		t.Errorf("the code is not the diff the comment was written on, or not round its lines:\n%s", text)
	}
	waitFor(t, a, sc, "this sleeps on every retry")
	waitFor(t, a, sc, "dave changed this line in version 2 of the diff")
	width := onLoop(a, func() int {
		_, prim := a.pages.GetFrontPage()
		_, _, w, _ := prim.(*modalBox).content.(*tview.Flex).GetRect()
		return w
	})
	if width > commentsWidth {
		t.Errorf("the conversation is %d wide on a 220 wide terminal, want at most %d", width, commentsWidth)
	}

	// The thread on code is the newest: G chooses its change, k its comment.
	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "jitter(backoff)")
	typeRunes(sc, "k")
	typeRunes(sc, "i")
	waitFor(t, a, sc, "Reply to carol")
	typeRunes(sc, "it backs off")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Reply to carol posted")
	fake.mu.Lock()
	replies := append([]string(nil), fake.replies...)
	fake.mu.Unlock()
	if len(replies) != 1 || replies[0] != "on-code: it backs off" {
		t.Fatalf("replies = %q, want one in the thread on code", replies)
	}

	waitFor(t, a, sc, "Comments · acme/gateway !7")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go:11-12 · acme/gateway !7")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "limit.go:11-12 · acme/gateway !7")

	typeRunes(sc, "e")
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "line 12 of the file")
	typeRunes(sc, "G")
	waitFor(t, a, sc, "line 40 of the file")
	typeRunes(sc, "q")
	waitGone(t, a, sc, "line 40 of the file")
	waitFor(t, a, sc, "Comments · acme/gateway !7")

	// g goes to the oldest comment, which is on no line.
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "not on a line")
}

// TestACommentIsReadAmongTheLinesItIsAbout: the code of a thread is a few
// lines before the commented ones and more after, counted on the side
// each is on, within its hunk; a line no longer in the diff has none.
func TestACommentIsReadAmongTheLinesItIsAbout(t *testing.T) {
	t.Parallel()
	diffs := []forge.FileDiff{{Path: "a.go", OldPath: "a.go",
		Diff: "@@ -1,12 +1,12 @@\n l1\n l2\n l3\n l4\n-two\n+deux\n l6\n l7\n l8\n l9\n l10\n l11\n"}}
	shown := func(n forge.Note) []string {
		lines, from, to := commentedRange(n, diffs)
		if from < 0 {
			return nil
		}
		start, end := codeWindow(lines, from, to)
		var out []string
		for _, l := range lines[start : end+1] {
			out = append(out, l.text)
		}
		return out
	}
	added := shown(forge.Note{Path: "a.go", Line: 5})
	if strings.Join(added, " ") != "l3 l4 two deux l6 l7 l8 l9" {
		t.Errorf("a comment on the new line 5 shows %q", added)
	}
	deleted := shown(forge.Note{Path: "a.go", Line: 5, Orphaned: true})
	if strings.Join(deleted, " ") != "l2 l3 l4 two deux l6 l7 l8" {
		t.Errorf("a comment on the deleted line 5 shows %q", deleted)
	}
	ranged := shown(forge.Note{Path: "a.go", Line: 7, From: forge.LineRef{Old: 5}, To: forge.LineRef{Old: 7, New: 7}})
	if strings.Join(ranged, " ") != "l2 l3 l4 two deux l6 l7 l8 l9 l10 l11" {
		t.Errorf("a comment on lines 5 to 7 shows %q", ranged)
	}
	if got := shown(forge.Note{Path: "a.go", Line: 40}); got != nil {
		t.Errorf("a line not in the diff has no code: %q", got)
	}
	if got := shown(forge.Note{Path: "b.go", Line: 1}); got != nil {
		t.Errorf("a file not in the diff has no code: %q", got)
	}
	block := renderConversationWith(forge.Note{ID: 1, Thread: "t", Body: "x", Path: "a.go", Line: 5}, diffs, 60)
	for _, r := range strings.Split(block, "\n") {
		if w := tview.TaggedStringWidth(r); w != 60 {
			t.Errorf("a row of the block is %d wide, want 60: %q", w, r)
		}
	}
}

// renderConversationWith draws one thread beside its diff.
func renderConversationWith(n forge.Note, diffs []forge.FileDiff, width int) string {
	text, _ := (&conversation{notes: []forge.Note{n}, latest: diffs}).draw(width)
	return text
}

// TestCommentsAreResolvedWrittenOnTheFileAndSaved: x resolves the chosen
// thread; e on a comment written on an older version asks which version
// to read, and on the latest a comment goes on the cursor's line - saved,
// to be published with P, or published at once.
func TestCommentsAreResolvedWrittenOnTheFileAndSaved(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 180, 50)
	openMRDetail(t, a, sc)
	fake := onLoop(a, func() *codeForge {
		id := a.cfg.Instances[0].ID
		f := &codeForge{Provider: a.clients[id]}
		a.clients[id] = f
		return f
	})
	typeRunes(sc, "c")
	waitFor(t, a, sc, "this sleeps on every retry")
	typeRunes(sc, "Gk")

	typeRunes(sc, "x")
	waitFor(t, a, sc, "Thread resolved")
	if got := fake.kept(&fake.resolved); len(got) != 1 || got[0] != "on-code true" {
		t.Fatalf("resolved = %q", got)
	}

	// The version it was written on: its lines, and no comment there.
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "a comment goes on the latest version")
	typeRunes(sc, "q")
	waitGone(t, a, sc, "a comment goes on the latest version")

	// The latest: a comment saved on the cursor's line.
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Which version of limit.go")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · latest")
	typeRunes(sc, "jc")
	waitFor(t, a, sc, "Comment on limit.go:13")
	typeRunes(sc, "worth a test")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Comment on limit.go:13 saved")
	saved := onLoop(a, func() int {
		drafts, _ := a.draftStore().List(a.draftKey(a.mrs[0]))
		return len(drafts)
	})
	if saved != 1 {
		t.Fatalf("%d comments saved, want 1", saved)
	}

	// And one published at once.
	typeRunes(sc, "jc")
	waitFor(t, a, sc, "Comment on limit.go:14")
	typeRunes(sc, "ship it")
	pressButton(t, a, sc, frontForm(a), "Publish")
	waitFor(t, a, sc, "Comment on limit.go:14 published")
	if got := fake.kept(&fake.started); len(got) != 1 || got[0] != "limit.go:14 ship it" {
		t.Fatalf("started = %q", got)
	}
	typeRunes(sc, "q")
	waitFor(t, a, sc, "saved · not published")
	waitFor(t, a, sc, "worth a test")

	// P publishes what was saved, and it is saved no longer.
	typeRunes(sc, "P")
	waitFor(t, a, sc, "comment saved in unagit: worth a test")
	typeRunes(sc, "y")
	eventually(t, a, "the saved comment is published", func() bool {
		drafts, _ := a.draftStore().List(a.draftKey(a.mrs[0]))
		return len(drafts) == 0
	})
	if got := fake.kept(&fake.started); len(got) != 2 || got[1] != "limit.go:13 worth a test" {
		t.Fatalf("started = %q", got)
	}
}
