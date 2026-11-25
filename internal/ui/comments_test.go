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
	waitFor(t, a, sc, "r reply")
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
	typeRunes(sc, "c")
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
	typeRunes(sc, "c")
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

// codeForge is the fake server's client with two more threads: one on a
// range of code, written on an older version of the merge request than
// the latest - which numbers the file's lines differently - with the
// forge's note that a later version changed its line, and one on the
// merge request as a whole. It serves the file at each commit, and keeps
// what it is asked to post instead of posting it.
type codeForge struct {
	forge.Provider
	mu       sync.Mutex
	replies  []string
	resolved []string
	started  []string
	changed  []string
}

func (f *codeForge) EditNote(_ context.Context, _ forge.MergeRequest, n forge.Note, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changed = append(f.changed, fmt.Sprintf("edit %d %s", n.ID, body))
	return nil
}

func (f *codeForge) DeleteNote(_ context.Context, _ forge.MergeRequest, n forge.Note) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changed = append(f.changed, fmt.Sprintf("delete %d", n.ID))
	return nil
}

// limitAt is limit.go at the commits of the fixture: b1 and b2 the bases
// of the older version and the latest, h1 the older head - which adds the
// sleep - and h2 the latest, which changed it.
func limitAt(ref string) (string, bool) {
	lines := []string{"package limit", "", "func limit() {", "\tfor {", "\t\tif ok() {", "\t\t\treturn", "\t\t}",
		"\t\tlog(1)", "\t\tlog(2)", "\t\tlog(3)", "\t\tlog(4)", "\t\tlog(5)", "\t}", "}"}
	sleep := func(call string) []string {
		return append(append(append([]string{}, lines[:7]...), "\t\ttime.Sleep("+call+")"), lines[7:]...)
	}
	switch ref {
	case "b1", "b2":
	case "h1":
		lines = sleep("backoff")
	case "h2", "feat/rate":
		lines = sleep("jitter(backoff)")
	default:
		return "", false
	}
	return strings.Join(lines, "\n") + "\n", true
}

func (f *codeForge) MergeRequestNotes(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Note, error) {
	notes, err := f.Provider.MergeRequestNotes(ctx, mr, limit)
	return append(notes,
		forge.Note{ID: 70, Thread: "on-code", Body: "this sleeps on every retry", Path: "limit.go", Line: 8,
			Base: "b1", Head: "h1", From: forge.LineRef{Old: 7, New: 7}, To: forge.LineRef{New: 8},
			CreatedAt: time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC), Author: forge.User{Username: "carol"}, Resolvable: true},
		forge.Note{ID: 71, Thread: "on-code", System: true, Body: "changed this line in [version 2 of the diff](/x)",
			Changed:   &forge.LineChange{From: "h1", To: "h2", Line: forge.LineRef{Old: 8, New: 8}},
			CreatedAt: time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC), Author: forge.User{Username: "dave"}},
		forge.Note{ID: 72, Thread: "general", Body: "please split this merge request",
			CreatedAt: time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC), Author: forge.User{Username: "erin"}}), err
}

func (f *codeForge) MergeRequestDetail(ctx context.Context, mr forge.MergeRequest) (*forge.MergeRequestDetail, error) {
	d, err := f.Provider.MergeRequestDetail(ctx, mr)
	if err != nil {
		return nil, err
	}
	d.DiffRefs.BaseSHA, d.DiffRefs.HeadSHA = "b2", "h2"
	return d, nil
}

func (f *codeForge) MergeRequestDiffs(context.Context, forge.MergeRequest) ([]forge.FileDiff, error) {
	return nil, nil
}

func (f *codeForge) FileAt(_ context.Context, _ forge.MergeRequest, path, ref string) (string, error) {
	if text, ok := limitAt(ref); ok && path == "limit.go" {
		return text, nil
	}
	return "", fmt.Errorf("no %s at %s", path, ref)
}

func (f *codeForge) ReplyToDiscussion(_ context.Context, _ forge.MergeRequest, thread, body string) (*forge.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, thread+": "+body)
	return &forge.Note{Thread: thread, Body: body}, nil
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

// openCodeConversation opens the conversation with codeForge's threads.
func openCodeConversation(t *testing.T, w, h int) (*App, tcell.SimulationScreen, *codeForge) {
	t.Helper()
	a, sc := newTestApp(t)
	resizeApp(a, sc, w, h)
	openMRDetail(t, a, sc)
	fake := onLoop(a, func() *codeForge {
		id := a.cfg.Instances[0].ID
		f := &codeForge{Provider: a.clients[id]}
		a.clients[id] = f
		// The reader wrote the comment on the merge request as a whole.
		if a.me == nil {
			a.me = map[string]string{}
		}
		a.me[id] = "erin"
		return f
	})
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	waitFor(t, a, sc, "this sleeps on every retry")
	return a, sc, fake
}

// TestCommentsAreChosenAnsweredAndReadBesideTheirCode: what is said of the
// merge request comes first; a thread on code is one block - the lines it
// is about, from the version it was written on, worked out from the file
// at either end, and its comments under them. j and k go over the code of
// a thread as over its comments, r answers in the thread, Enter on the
// note of a later change shows that change, and on a comment opens its
// file at it. However wide the terminal, the conversation is no wider
// than reads well.
func TestCommentsAreChosenAnsweredAndReadBesideTheirCode(t *testing.T) {
	t.Parallel()
	a, sc, fake := openCodeConversation(t, 220, 60)
	waitFor(t, a, sc, "limit.go:7-8")
	// The older version's lines, the four after the commented ones too.
	waitFor(t, a, sc, "log(4)")
	text := a.screenText(sc)
	if strings.Contains(text, "jitter") || strings.Contains(text, "log(5)") {
		t.Errorf("the code is not the version the comment was written on, or not round its lines:\n%s", text)
	}
	general, code := strings.Index(text, "please split this merge request"), strings.Index(text, "On the code")
	if general < 0 || code < 0 || general > code {
		t.Errorf("what is said of the merge request should come before the code:\n%s", text)
	}
	waitFor(t, a, sc, "dave changed this line in version 2 of the diff")
	width := onLoop(a, func() int {
		_, prim := a.pages.GetFrontPage()
		_, _, w, _ := prim.(*modalBox).content.(*tview.Flex).GetRect()
		return w
	})
	if width > commentsWidth {
		t.Errorf("the conversation is %d wide on a 220 wide terminal, want at most %d", width, commentsWidth)
	}

	// The thread on code is last: G is its change, k its comment, k again
	// its code - which answers as its comment does.
	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "jitter(backoff)")
	typeRunes(sc, "kk")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to carol")
	typeRunes(sc, "it backs off")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Reply to carol posted")
	if got := fake.kept(&fake.replies); len(got) != 1 || got[0] != "on-code: it backs off" {
		t.Fatalf("replies = %q, want one in the thread on code", got)
	}

	// Enter on the comment: its file, at the version it was written on,
	// the comment under its line.
	waitFor(t, a, sc, "Comments · acme/gateway !7")
	typeRunes(sc, "j")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · h1")
	waitFor(t, a, sc, "this sleeps on every retry")
	typeRunes(sc, "q")
	waitGone(t, a, sc, "limit.go · h1")

	// g goes to the oldest comment, which is on no file.
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "not on a file")
}

// TestOwnCommentsAreEditedAndDeleted: e edits a comment of the reader's
// own and d deletes it, after asking, both drawn at once; another's is
// left alone.
func TestOwnCommentsAreEditedAndDeleted(t *testing.T) {
	t.Parallel()
	a, sc, fake := openCodeConversation(t, 160, 60)
	typeRunes(sc, "g")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "only a comment of your own can be edited")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "only a comment of your own")

	// The general comments are carol's, bob's, ann's thread and erin's.
	typeRunes(sc, "jjjj")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Edit your comment")
	typeRunes(sc, " in two")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitGone(t, a, sc, "Edit your comment")
	waitFor(t, a, sc, "please split this merge request in two")
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete your comment?")
	typeRunes(sc, "y")
	waitGone(t, a, sc, "please split this merge request in two")
	if got := fake.kept(&fake.changed); len(got) != 2 || got[0] != "edit 72 please split this merge request in two" || got[1] != "delete 72" {
		t.Fatalf("changed = %q", got)
	}
}

// TestAFileIsReadWithItsCommentsAndCommentedOn: x resolves the chosen
// thread; Enter on a comment written on an older version asks which
// version to read, in a box no bigger than its rows. The file marks the
// lines with comments and counts them, shows the comments under their
// lines - h hides them - and the cursor goes over them as over the lines;
// on the latest version c comments the cursor's line, saved and drawn at
// once, or published, and P publishes what was saved.
func TestAFileIsReadWithItsCommentsAndCommentedOn(t *testing.T) {
	t.Parallel()
	a, sc, fake := openCodeConversation(t, 180, 50)
	typeRunes(sc, "Gk")

	typeRunes(sc, "x")
	waitFor(t, a, sc, "Thread resolved")
	if got := fake.kept(&fake.resolved); len(got) != 1 || got[0] != "on-code true" {
		t.Fatalf("resolved = %q", got)
	}

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Which version of limit.go")
	if rows := onLoop(a, func() int {
		_, prim := a.pages.GetFrontPage()
		_, _, _, h := prim.(*modalBox).content.(tview.Primitive).GetRect()
		return h
	}); rows > 12 {
		t.Errorf("the choice of two versions is %d rows high", rows)
	}
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · h1")
	waitFor(t, a, sc, glyphComment+"1")
	waitFor(t, a, sc, "this sleeps on every retry")
	typeRunes(sc, "h")
	waitGone(t, a, sc, "this sleeps on every retry")
	typeRunes(sc, "h")
	waitFor(t, a, sc, "this sleeps on every retry")
	// Hiding them put the cursor on the line; j is the comment under it,
	// which r answers.
	typeRunes(sc, "j")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to carol")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Reply to carol")
	typeRunes(sc, "c")
	waitFor(t, a, sc, "a new comment goes on the latest version")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "a new comment goes on the latest version")

	// The latest, the cursor on the same line: a comment saved there,
	// drawn under it at once.
	typeRunes(sc, "v")
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · latest")
	waitFor(t, a, sc, "jitter(backoff)")
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comment on limit.go:8")
	typeRunes(sc, "worth a test")
	sc.InjectKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Comment on limit.go:8 saved")
	waitFor(t, a, sc, "worth a test")
	saved := onLoop(a, func() int {
		drafts, _ := a.draftStore().List(a.draftKey(a.mrs[0]))
		return len(drafts)
	})
	if saved != 1 {
		t.Fatalf("%d comments saved, want 1", saved)
	}

	// And one published at once, a line up.
	typeRunes(sc, "k")
	typeRunes(sc, "c")
	waitFor(t, a, sc, "Comment on limit.go:7")
	typeRunes(sc, "ship it")
	pressButton(t, a, sc, frontForm(a), "Publish")
	waitFor(t, a, sc, "Comment on limit.go:7 published")
	if got := fake.kept(&fake.started); len(got) != 1 || got[0] != "limit.go:7 ship it" {
		t.Fatalf("started = %q", got)
	}
	typeRunes(sc, "q")
	// The saved comment is the last thread.
	typeRunes(sc, "G")
	waitFor(t, a, sc, "saved · not published")

	// P publishes what was saved, and it is saved no longer.
	typeRunes(sc, "P")
	waitFor(t, a, sc, "comment saved in unagit: worth a test")
	typeRunes(sc, "y")
	eventually(t, a, "the saved comment is published", func() bool {
		drafts, _ := a.draftStore().List(a.draftKey(a.mrs[0]))
		return len(drafts) == 0
	})
	if got := fake.kept(&fake.started); len(got) != 2 || got[1] != "limit.go:8 worth a test" {
		t.Fatalf("started = %q", got)
	}
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

// TestTheCodeCanBeHiddenToReadTheConversation: h draws the threads on code
// as their comments alone, the place in the first one's frame, and back;
// the comment chosen stays chosen and on screen.
func TestTheCodeCanBeHiddenToReadTheConversation(t *testing.T) {
	t.Parallel()
	a, sc, _ := openCodeConversation(t, 160, 60)
	typeRunes(sc, "Gk")
	// Small enough that the conversation scrolls; choosing scrolls to it.
	resizeApp(a, sc, 160, 30)
	typeRunes(sc, "kj")
	waitFor(t, a, sc, "this sleeps on every retry")
	typeRunes(sc, "h")
	waitGone(t, a, sc, "log(4)")
	waitFor(t, a, sc, "limit.go:7-8")
	waitFor(t, a, sc, "this sleeps on every retry")
	// The note of the change is the next card down.
	typeRunes(sc, "j")
	waitFor(t, a, sc, "dave changed this line in version 2 of the diff")
	typeRunes(sc, "k")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to carol")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Reply to carol")
	typeRunes(sc, "h")
	waitFor(t, a, sc, "this sleeps on every retry")
	waitFor(t, a, sc, "time.Sleep(backoff)")
}

// TestTheFirstCommentIsChosenOnOpening: the keys act on a comment as soon
// as the conversation is open.
func TestTheFirstCommentIsChosenOnOpening(t *testing.T) {
	t.Parallel()
	a, sc, _ := openCodeConversation(t, 160, 60)
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to carol")
	waitFor(t, a, sc, "oldest comment")
}

// TestAThreadOnCodeIsLitWhileTheCursorIsInIt: the frame round a thread on
// code is lit while the cursor is anywhere in the thread - on its code or
// on any of its comments - framed as a pane with the focus, and a
// comment's card is chosen alone, as a card.
func TestAThreadOnCodeIsLitWhileTheCursorIsInIt(t *testing.T) {
	t.Parallel()
	thread := []forge.Note{
		{ID: 1, Thread: "t", Body: "first", Path: "a.go", Line: 1, Resolvable: true},
		{ID: 2, Thread: "t", Body: "answer"},
	}
	diffs := []forge.FileDiff{{Path: "a.go", OldPath: "a.go", Diff: "@@ -1 +1 @@\n-a\n+b\n"}}
	text, cards := (&conversation{notes: thread, latest: diffs}).draw(60)
	if len(cards) != 3 || !cards[0].code {
		t.Fatalf("cards = %+v", cards)
	}
	block := cards[0].block
	if block == nil || !block.frame || !block.pane || block.lit || block.width != 60 {
		t.Fatalf("the block's frame is %+v", block)
	}
	for _, c := range cards {
		if c.block != block {
			t.Errorf("card %d is not in the thread's block", c.note.ID)
		}
	}
	reply := cards[2].band
	if !reply.lit || !reply.frame || reply.pane || reply.left != 4 || reply.from < block.from || reply.to > block.to {
		t.Errorf("the reply's band is %+v, in a block %+v", reply, *block)
	}

	// On screen, the cursor on the reply: the block's frame in the focus
	// colour, as a pane's; the reply's frame its own, brighter, as a card's.
	view := tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetText(text)
	screen := tcell.NewSimulationScreen("UTF-8")
	must(t, screen.Init())
	screen.SetSize(62, 40)
	view.SetRect(0, 0, 62, 40)
	view.Draw(screen)
	before, _, _ := screenStyle(screen, reply.left, reply.from).Decompose()
	paintBand(screen, view, *block)
	paintBand(screen, view, reply)
	if fg, _, _ := screenStyle(screen, block.left, block.from).Decompose(); fg != colBorderFocus {
		t.Errorf("the thread's frame is %v, want the focus colour %v", fg, colBorderFocus)
	}
	if fg, _, _ := screenStyle(screen, reply.left, reply.from).Decompose(); fg != cardChosen(before, false) || fg == colBorderFocus {
		t.Errorf("the reply's frame is %v, want its own %v brighter", fg, before)
	}
}

func screenStyle(screen tcell.Screen, x, y int) tcell.Style {
	_, _, style, _ := screen.GetContent(x, y)
	return style
}

// TestTheCommentsActionsAreInThePicker: what the conversation and a file
// can do is listed by the selection's picker too, and done from there.
func TestTheCommentsActionsAreInThePicker(t *testing.T) {
	t.Parallel()
	a, sc, _ := openCodeConversation(t, 160, 60)
	typeRunes(sc, ".")
	waitFor(t, a, sc, "Actions · comments")
	for _, want := range []string{"Reply…", "New Comment…", "Toggle Code", "Reload"} {
		waitFor(t, a, sc, want)
	}
	typeRunes(sc, "toggle code")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · comments")
	waitGone(t, a, sc, "log(4)")

	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "jitter(backoff)")
	typeRunes(sc, "k")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · h1")
	typeRunes(sc, ".")
	waitFor(t, a, sc, "Actions · limit.go")
	for _, want := range []string{"Reply…", "Toggle Comments", "Switch Version…"} {
		waitFor(t, a, sc, want)
	}
}

// TestTheMouseChoosesAndOpensComments: a click chooses a comment, the
// right button lists what can be done with it and a double click opens it,
// in the conversation and in a file, as the keys do.
func TestTheMouseChoosesAndOpensComments(t *testing.T) {
	t.Parallel()
	a, sc, _ := openCodeConversation(t, 160, 60)
	x, y := at(t, a, sc, "third comment")
	click(sc, x, y, tcell.ModNone)
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to bob")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Reply to bob")

	x, y = at(t, a, sc, "please split this merge request")
	press(sc, x, y, tcell.ButtonSecondary, tcell.ModNone)
	release(sc, x, y, tcell.ModNone)
	waitFor(t, a, sc, "Actions · comments")
	waitFor(t, a, sc, "Edit Comment…")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Actions · comments")

	x, y = at(t, a, sc, "this sleeps on every retry")
	click(sc, x, y, tcell.ModNone)
	click(sc, x, y, tcell.ModNone)
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · h1")
	waitFor(t, a, sc, "Enter compare")
	x, y = at(t, a, sc, "dave changed this line")
	click(sc, x, y, tcell.ModNone)
	click(sc, x, y, tcell.ModNone)
	waitFor(t, a, sc, "jitter(backoff)")
}

// TestALongLineOfCodeIsCutToTheBlock: a line longer than the block - an
// ARN, a generated name - is cut at its edge, never carried onto the next
// row, where it would break the frame.
func TestALongLineOfCodeIsCutToTheBlock(t *testing.T) {
	t.Parallel()
	long := `    role_arn = "arn:aws:iam::123456789012:role/` + strings.Repeat("Terraform", 20) + `Execution"`
	diffs := []forge.FileDiff{{Path: "main.tf", OldPath: "main.tf", Diff: "@@ -1,2 +1,2 @@\n assume_role {\n-" + long + "\n+" + long + "x\n"}}
	block := renderConversationWith(forge.Note{ID: 1, Thread: "t", Body: "x", Path: "main.tf", Line: 2}, diffs, 80)
	for _, r := range strings.Split(block, "\n") {
		if w := tview.TaggedStringWidth(r); w != 80 {
			t.Errorf("a row of the block is %d wide, want 80: %q", w, r)
		}
	}
	if !strings.Contains(block, "…") {
		t.Errorf("the long line is not cut:\n%s", block)
	}
}

// planForge is codeForge with a comment longer than the view, the oldest,
// as a plan from Atlantis is: a code block of numbered lines.
type planForge struct{ *codeForge }

func (f *planForge) MergeRequestNotes(ctx context.Context, mr forge.MergeRequest, limit int) ([]forge.Note, error) {
	notes, err := f.codeForge.MergeRequestNotes(ctx, mr, limit)
	var b strings.Builder
	b.WriteString("Ran Plan for dir\n\n```\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&b, "  + resource %02d will be created\n", i)
	}
	b.WriteString("```\n")
	return append(notes, forge.Note{ID: 80, Thread: "plan", Body: b.String(), Author: forge.User{Username: "atlantis"},
		CreatedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)}), err
}

// openPlanConversation opens the conversation with the long comment first.
func openPlanConversation(t *testing.T) (*App, tcell.SimulationScreen) {
	t.Helper()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 40)
	openMRDetail(t, a, sc)
	onLoop(a, func() bool {
		id := a.cfg.Instances[0].ID
		a.clients[id] = &planForge{&codeForge{Provider: a.clients[id]}}
		return true
	})
	typeRunes(sc, "c")
	waitFor(t, a, sc, "resource 01 will be created")
	return a, sc
}

// TestJAndKGoAWholeCommentAtATime: j and k go from a comment to the next
// whatever its length, its start shown - in a view shorter than the
// comment too. A comment longer than commentClip rows is clipped, what is
// not shown said under it, and read to its end with the cursor in it (v),
// which moves its window and not the conversation.
func TestJAndKGoAWholeCommentAtATime(t *testing.T) {
	t.Parallel()
	a, sc := openPlanConversation(t)
	waitFor(t, a, sc, "more lines")
	if strings.Contains(a.screenText(sc), "resource 20 will be created") {
		t.Error("the long comment is not clipped")
	}
	typeRunes(sc, "j")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to carol")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Reply to carol")

	// Back on the long one in a view shorter than it: its start shows.
	resizeApp(a, sc, 160, 18)
	typeRunes(sc, "k")
	waitFor(t, a, sc, "Ran Plan for dir")
	resizeApp(a, sc, 160, 40)

	typeRunes(sc, "vG")
	waitFor(t, a, sc, "resource 60 will be created")
	waitFor(t, a, sc, "lines above")
	if strings.Contains(a.screenText(sc), "resource 01 will be created") {
		t.Error("the window did not move down the comment")
	}
}

// TestTextIsSelectedInACommentAndCopied: v puts a cursor in the comment;
// v there selects from it by characters, V by whole lines, y copies the
// selection and Y the whole comment as written; Esc goes back.
func TestTextIsSelectedInACommentAndCopied(t *testing.T) {
	clip := fakeClipboard(t, true)
	a, sc := openPlanConversation(t)
	typeRunes(sc, "v")
	waitFor(t, a, sc, "VISUAL")
	// The first lines are the heading and a blank; the block's lines are
	// drawn indented, as markdown draws code.
	typeRunes(sc, "jj")
	typeRunes(sc, "llll")
	typeRunes(sc, "vjj$")
	typeRunes(sc, "y")
	eventually(t, a, "the selection copied", func() bool { return clip.get() != "" })
	want := "+ resource 01 will be created\n    + resource 02 will be created\n    + resource 03 will be created"
	if got := clip.get(); got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
	waitGone(t, a, sc, "VISUAL")

	typeRunes(sc, "v")
	waitFor(t, a, sc, "VISUAL")
	typeRunes(sc, "jjVj")
	typeRunes(sc, "y")
	eventually(t, a, "the lines copied", func() bool { return strings.HasPrefix(clip.get(), "    + resource 01") })
	if got := clip.get(); got != "    + resource 01 will be created\n    + resource 02 will be created" {
		t.Fatalf("copied %q", got)
	}

	typeRunes(sc, "v")
	waitFor(t, a, sc, "VISUAL")
	typeRunes(sc, "Y")
	eventually(t, a, "the comment copied", func() bool { return strings.HasPrefix(clip.get(), "Ran Plan for dir") })

	typeRunes(sc, "v")
	waitFor(t, a, sc, "VISUAL")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "VISUAL")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Reply to atlantis")
}

// TestTheMouseSelectsTextInAComment: with the cursor in a comment, a drag
// over its text selects it, and y copies what was selected.
func TestTheMouseSelectsTextInAComment(t *testing.T) {
	clip := fakeClipboard(t, true)
	a, sc := openPlanConversation(t)
	typeRunes(sc, "v")
	waitFor(t, a, sc, "VISUAL")
	x1, y1 := at(t, a, sc, "resource 03")
	x2, y2 := at(t, a, sc, "resource 04")
	press(sc, x1, y1, tcell.ButtonPrimary, tcell.ModNone)
	press(sc, x2+len("resource 04")-1, y2, tcell.ButtonPrimary, tcell.ModNone)
	release(sc, x2+len("resource 04")-1, y2, tcell.ModNone)
	typeRunes(sc, "y")
	eventually(t, a, "the drag copied", func() bool { return clip.get() != "" })
	if got := clip.get(); got != "resource 03 will be created\n    + resource 04" {
		t.Fatalf("copied %q", got)
	}
}

// TestAFileScrollsARowAtATime: j past the bottom edge of a file brings the
// next row in, it does not turn the page - Ctrl-D does that.
func TestAFileScrollsARowAtATime(t *testing.T) {
	t.Parallel()
	a, sc, _ := openCodeConversation(t, 180, 50)
	typeRunes(sc, "Gk")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · h1")
	resizeApp(a, sc, 180, 22)
	typeRunes(sc, "g")
	type at struct{ top, height, to int }
	where := func() at {
		return onLoop(a, func() at {
			v := a.tv.GetFocus().(*bandView)
			top, _ := v.GetScrollOffset()
			_, _, _, h := v.GetInnerRect()
			band, _ := v.band()
			return at{top, h, band.to}
		})
	}
	scrolled := false
	for i := 0; i < 20; i++ {
		before := where()
		typeRunes(sc, "j")
		now := where()
		if now.to >= now.top+now.height || now.to < now.top {
			t.Fatalf("j %d: the cursor's row %d is out of the view %d+%d", i, now.to, now.top, now.height)
		}
		if now.top != before.top {
			scrolled = true
			if want := now.to - now.height + 1; now.top != want {
				t.Fatalf("j %d: scrolled from %d to %d, want %d - the cursor's row at the bottom", i, before.top, now.top, want)
			}
		}
	}
	if !scrolled {
		t.Fatal("the file never scrolled: the view is too tall for the test")
	}
}

// TestAFileOpensWithItsCommentInTheMiddle: the file opened on a comment has
// it in the middle of the view, the code above it in sight, until the
// cursor moves.
func TestAFileOpensWithItsCommentInTheMiddle(t *testing.T) {
	t.Parallel()
	a, sc, _ := openCodeConversation(t, 180, 50)
	typeRunes(sc, "Gk")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Which version of limit.go")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "limit.go · h1")
	resizeApp(a, sc, 180, 25)
	type at struct{ top, height, from, to int }
	where := func() at {
		return onLoop(a, func() at {
			v := a.tv.GetFocus().(*bandView)
			top, _ := v.GetScrollOffset()
			_, _, _, h := v.GetInnerRect()
			band, _ := v.band()
			return at{top, h, band.from, band.to}
		})
	}
	now := where()
	if now.from == now.to {
		t.Fatalf("the cursor is on a line, not on the comment: rows %d-%d", now.from, now.to)
	}
	if want := now.from - (now.height-(now.to-now.from+1))/2; want <= 0 || now.top != want {
		t.Fatalf("the comment at rows %d-%d is shown from row %d of a view %d high, want %d", now.from, now.to, now.top, now.height, want)
	}
}
