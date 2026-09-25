package incomm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

// fakeForge records what is posted and answers with ids in sequence.
type fakeForge struct {
	calls     []string
	next      int
	failAt    int    // 1-based call number that fails; 0 never
	rootPath  string // what a created discussion reports as its path ("" is the fallback)
	rootLine  int
	noThread  bool // a created discussion cannot be replied to
	lastReply string
}

func (f *fakeForge) note(kind string, thread string) (*forge.Note, error) {
	if f.failAt != 0 && len(f.calls) == f.failAt {
		return nil, errors.New("the forge said no")
	}
	f.next++
	return &forge.Note{ID: 100 + f.next, URL: fmt.Sprintf("https://forge/mr/1#note_%d", 100+f.next), Thread: thread}, nil
}

func (f *fakeForge) CreateDiscussion(_ context.Context, _ forge.MergeRequest, path string, line int, body string) (*forge.Note, error) {
	f.calls = append(f.calls, fmt.Sprintf("discussion %s:%d %q", path, line, body))
	thread := fmt.Sprintf("thread%d", f.next+1)
	if f.noThread {
		thread = ""
	}
	n, err := f.note("discussion", thread)
	if err == nil {
		n.Path, n.Line = f.rootPath, f.rootLine
	}
	return n, err
}

func (f *fakeForge) ReplyToDiscussion(_ context.Context, _ forge.MergeRequest, thread, body string) (*forge.Note, error) {
	f.calls = append(f.calls, fmt.Sprintf("reply %s %q", thread, body))
	return f.note("reply", thread)
}

func (f *fakeForge) CommentNote(_ context.Context, _ forge.MergeRequest, body string) (*forge.Note, error) {
	f.calls = append(f.calls, fmt.Sprintf("comment %q", body))
	return f.note("comment", "")
}

type recorded struct {
	dir, id, reply string
	src            Source
	audience       string
}

func publishNotes(t *testing.T) []Step {
	t.Helper()
	dir := worktreeWith(t, map[string]string{"notes_main.json": `{"version":2,"notes":[
 {"id":"a","file":"a.go","startLine":3,"author":"user","content":"root","audience":"external","replies":[
   {"id":"a1","author":"agent","authorTitle":"Opus 5","content":"reply","audience":"external"}]},
 {"id":"b","file":"b.go","startLine":4,"author":"user","content":"agent-only root","audience":"agent","replies":[
   {"id":"b1","author":"user","content":"marked reply","audience":"external"}]}
]}`})
	return Plan(ReadThreads(dir))
}

func TestPublishGoesRootThenRepliesAndRecordsEachPost(t *testing.T) {
	steps := publishNotes(t)
	f := &fakeForge{rootPath: "a.go", rootLine: 3}
	var records []recorded
	var logs []string
	p := Publisher{Poster: f, Log: func(s string) { logs = append(logs, s) },
		Record: func(_ context.Context, dir, id, reply string, src Source, audience string) error {
			// The write-back happens right after each post: nothing may be
			// posted that has not been recorded yet, except the current one.
			if got := len(f.calls); got != len(records)+1 {
				t.Errorf("%d posts before recording #%d", got, len(records)+1)
			}
			records = append(records, recorded{dir, id, reply, src, audience})
			return nil
		}}
	done, err := p.Publish(context.Background(), steps)
	if err != nil || done != 4 {
		t.Fatalf("done=%d err=%v", done, err)
	}
	want := []string{
		`discussion a.go:3 "root"`,
		`reply thread1 "**Agent (Opus 5):**\n\nreply"`,
		`discussion b.go:4 "agent-only root"`,
		`reply thread3 "marked reply"`,
	}
	for i, w := range want {
		if f.calls[i] != w {
			t.Errorf("call %d = %s, want %s", i, f.calls[i], w)
		}
	}
	// The root remembers its thread; the reply its id and link, not a thread.
	if r := records[0]; r.id != "a" || r.reply != "" || r.src.ID != 101 || r.src.Thread != "thread1" || r.audience != "" {
		t.Errorf("root record = %+v", r)
	}
	if r := records[1]; r.id != "a" || r.reply != "a1" || r.src.ID != 102 || r.src.Thread != "" || r.src.URL == "" {
		t.Errorf("reply record = %+v", r)
	}
	// A comment that only went out as a parent becomes addressed to the forge too.
	if r := records[2]; r.id != "b" || r.audience != "agent+external" {
		t.Errorf("parent record = %+v", r)
	}
	if len(logs) != 4 {
		t.Errorf("logs: %v", logs)
	}
}

func TestPublishStopsAtTheFirstFailureAndKeepsWhatWentOut(t *testing.T) {
	steps := publishNotes(t)
	f := &fakeForge{failAt: 2, rootPath: "a.go", rootLine: 3}
	var records []recorded
	p := Publisher{Poster: f, Record: func(_ context.Context, dir, id, reply string, src Source, audience string) error {
		records = append(records, recorded{dir, id, reply, src, audience})
		return nil
	}}
	done, err := p.Publish(context.Background(), steps)
	if err == nil || !strings.Contains(err.Error(), "the forge said no") {
		t.Fatalf("err = %v", err)
	}
	if done != 1 || len(records) != 1 || len(f.calls) != 2 {
		t.Fatalf("done=%d records=%d calls=%v: the first post must stay recorded, nothing after the failure is tried", done, len(records), f.calls)
	}
}

func TestPublishStopsWhenARecordFails(t *testing.T) {
	steps := publishNotes(t)
	f := &fakeForge{rootPath: "a.go", rootLine: 3}
	p := Publisher{Poster: f, Record: func(context.Context, string, string, string, Source, string) error {
		return errors.New("disk full")
	}}
	done, err := p.Publish(context.Background(), steps)
	if err == nil || !strings.Contains(err.Error(), "could not be recorded") || !strings.Contains(err.Error(), "comment 101") {
		t.Fatalf("err = %v", err)
	}
	if done != 0 || len(f.calls) != 1 {
		t.Fatalf("nothing more may go out once a post is not recorded: done=%d calls=%v", done, f.calls)
	}
}

func TestPublishSaysWhenARootFellBackAndRepliesAsCommentsWithoutAThread(t *testing.T) {
	steps := publishNotes(t)[:2]
	f := &fakeForge{noThread: true} // no path/line reported: the conversation fallback
	var logs []string
	p := Publisher{Poster: f, Log: func(s string) { logs = append(logs, s) },
		Record: func(context.Context, string, string, string, Source, string) error { return nil }}
	if _, err := p.Publish(context.Background(), steps); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || !strings.HasPrefix(f.calls[1], `comment "In reply to `) {
		t.Fatalf("calls = %v", f.calls)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "not part of the diff") || !strings.Contains(joined, "cannot be replied to as a thread") {
		t.Errorf("logs: %s", joined)
	}
}

func TestPublishRepliesToAThreadThatIsAlreadyOnTheForge(t *testing.T) {
	dir := worktreeWith(t, map[string]string{"notes_main.json": `{"version":2,"notes":[
 {"id":"c","file":"c.go","startLine":5,"author":"user","content":"on the forge","audience":"agent+external","source":{"id":4,"url":"https://f/4","thread":"existing"},"replies":[
   {"id":"c1","author":"user","content":"marked","audience":"agent+external"}]}]}`})
	f := &fakeForge{}
	p := Publisher{Poster: f, Record: func(context.Context, string, string, string, Source, string) error { return nil }}
	if done, err := p.Publish(context.Background(), Plan(ReadThreads(dir))); err != nil || done != 1 {
		t.Fatalf("done=%d err=%v", done, err)
	}
	if len(f.calls) != 1 || f.calls[0] != `reply existing "marked"` {
		t.Fatalf("calls = %v", f.calls)
	}
}
