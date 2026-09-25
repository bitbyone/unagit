package incomm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// ---- resolving on the forge is imported ------------------------------------

func TestImportResolvesAThreadTheForgeHasResolved(t *testing.T) {
	_, dir := fakeIncomm(t)
	notes := []forge.Note{
		{ID: 1, Thread: "a", Body: "done", Path: "code.go", Line: 1, Resolvable: true, Resolved: true},
		{ID: 2, Thread: "b", Body: "still open", Path: "code.go", Line: 2, Resolvable: true},
		{ID: 3, Thread: "c", Body: "not resolvable", Path: "code.go", Line: 1, Resolved: true},
	}
	var logs []string
	if err := Import(context.Background(), dir, forge.MergeRequest{}, notes, func(s string) { logs = append(logs, s) }); err != nil {
		t.Fatal(err)
	}
	calls := commands(t, dir)
	// add, resolve (n1), add, add: only the first thread is resolved.
	if len(calls) != 4 || !strings.Contains(calls[1], "\nresolve\nn1\n") {
		t.Fatalf("calls: %q", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "\nresolve\nn2\n") || strings.Contains(c, "\nresolve\nn3\n") {
			t.Errorf("a thread that is open or not resolvable was resolved: %q", calls)
		}
	}
	if !strings.Contains(strings.Join(logs, "\n"), "resolved 1 threads") {
		t.Errorf("logs: %v", logs)
	}
}

func TestImportResolvesAnExistingOpenThreadAndNeverReopens(t *testing.T) {
	_, dir := fakeIncomm(t)
	writeJSON(t, filepath.Join(dir, "agent.json"), map[string]any{"notes": []map[string]any{
		{"id": "x1", "content": "one", "source": map[string]any{"id": 1}},
		{"id": "x2", "content": "two", "resolved": true, "source": map[string]any{"id": 2}},
		{"id": "x3", "content": "three", "resolved": true, "source": map[string]any{"id": 3}},
	}})
	notes := []forge.Note{
		{ID: 1, Thread: "a", Body: "one", Path: "code.go", Line: 1, Resolvable: true, Resolved: true}, // forge resolved, local open
		{ID: 2, Thread: "b", Body: "two", Path: "code.go", Line: 1, Resolvable: true, Resolved: true}, // both resolved
		{ID: 3, Thread: "c", Body: "three", Path: "code.go", Line: 1, Resolvable: true},               // forge reopened, local resolved
	}
	if err := Import(context.Background(), dir, forge.MergeRequest{}, notes, nil); err != nil {
		t.Fatal(err)
	}
	calls := commands(t, dir)
	if len(calls) != 1 || !strings.Contains(calls[0], "\nresolve\nx1\n") {
		t.Fatalf("only x1 should be resolved, and nothing reopened: %q", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "unresolve") {
			t.Fatalf("a thread was reopened: %q", calls)
		}
	}
}

func TestImportedOrphanKeepsTheForgesResolvedState(t *testing.T) {
	_, dir := fakeIncomm(t)
	notes := []forge.Note{
		{ID: 5, Thread: "g", Body: "gone", Path: "gone.go", Line: 1, Resolvable: true, Resolved: true},
	}
	if err := Import(context.Background(), dir, forge.MergeRequest{}, notes, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".incomm", "notes.json"))
	if err != nil || !strings.Contains(string(data), `"resolved": true`) {
		t.Fatalf("the orphan must be written resolved: %s %v", data, err)
	}
	// It is already resolved, so no resolve command is needed.
	for _, c := range commands(t, dir) {
		if strings.Contains(c, "\nresolve\n") {
			t.Errorf("an orphan written resolved was resolved again: %q", c)
		}
	}
}

// ---- resolving from Incomm is published ------------------------------------

func thread(id, file string, line int, resolved bool, root Comment, replies ...Comment) Thread {
	return Thread{Dir: "wt", File: file, Line: line, Resolved: resolved, Root: root, Replies: replies}
}

func onForge(id, thread string) Comment {
	return Comment{ID: id, Author: "user", Audience: audienceBoth, Content: id, Source: Source{ID: 10, Thread: thread}}
}

func pending(id string) Comment {
	return Comment{ID: id, Author: "user", Audience: audienceExternal, Content: id}
}

func TestPlanResolvesWhatIsResolvedHereAndOpenThere(t *testing.T) {
	threads := []Thread{
		thread("a", "a.go", 1, true, onForge("a", "T1")),                                                              // resolved here, open there
		thread("b", "b.go", 2, true, onForge("b", "T2")),                                                              // resolved on both
		thread("c", "c.go", 3, false, onForge("c", "T3")),                                                             // open here, resolved there: never reopened
		thread("d", "d.go", 4, true, Comment{ID: "d", Author: "user", Content: "local only"}),                         // never published
		thread("e", "e.go", 5, true, onForge("e", "T5")),                                                              // the forge no longer has it
		thread("f", "f.go", 6, true, Comment{ID: "f", Author: "user", Audience: audienceBoth, Source: Source{ID: 9}}), // on the forge, no thread id
	}
	state := map[string]bool{"T1": false, "T2": true, "T3": true}
	var resolves []string
	for _, s := range PlanResolving(threads, state) {
		if s.Resolve {
			resolves = append(resolves, s.File)
		}
	}
	if len(resolves) != 1 || resolves[0] != "a.go" {
		t.Fatalf("resolves = %v, want only a.go", resolves)
	}
	// Without the forge's state nothing is resolved, and Plan never resolves.
	for _, s := range PlanResolving(threads, nil) {
		if s.Resolve {
			t.Fatal("no forge state, no resolve")
		}
	}
	for _, s := range Plan(threads) {
		if s.Resolve {
			t.Fatal("Plan must not resolve")
		}
	}
}

func TestPlanResolvesAThreadAfterItsPosts(t *testing.T) {
	root := pending("r")
	threads := []Thread{thread("r", "a.go", 3, true, root, pending("r1"))}
	steps := PlanResolving(threads, map[string]bool{})
	if len(steps) != 3 || !steps[0].IsRoot || steps[1].Comment.ID != "r1" || !steps[2].Resolve {
		t.Fatalf("steps = %+v", steps)
	}
	// Published only as a parent, an agent-only thread that is resolved goes out
	// and is resolved; one that never goes out is not resolved.
	agent := Comment{ID: "g", Author: "user", Audience: audienceAgent, Content: "note"}
	steps = PlanResolving([]Thread{thread("g", "g.go", 1, true, agent, pending("g1"))}, map[string]bool{})
	if len(steps) != 3 || !steps[0].Parent || !steps[2].Resolve {
		t.Fatalf("parent steps = %+v", steps)
	}
	if steps = PlanResolving([]Thread{thread("g", "g.go", 1, true, agent)}, map[string]bool{}); len(steps) != 0 {
		t.Fatalf("an agent-only thread must not be resolved on the forge: %+v", steps)
	}
	if got := steps; len(got) != 0 {
		t.Fatal("unreachable")
	}
}

func TestSummaryNamesAResolveAndAnOrphan(t *testing.T) {
	if got := (Step{File: "a.go", Line: 3, Resolve: true}).Summary(); got != "a.go:3  resolve thread" {
		t.Errorf("resolve summary = %q", got)
	}
	orphan := Step{File: "a.go", Line: 3, IsRoot: true, Orphaned: true, Comment: Comment{Author: "user", Content: "hello"}}
	if got := orphan.Summary(); !strings.Contains(got, "orphaned") || strings.Contains(got, "a.go:3") {
		t.Errorf("orphan summary = %q", got)
	}
}

func TestForgeResolvedReadsTheFirstNoteOfEachThread(t *testing.T) {
	at := time.Now()
	notes := []forge.Note{
		{ID: 2, Thread: "T1", Resolvable: true, Resolved: true, CreatedAt: at.Add(time.Minute)},
		{ID: 1, Thread: "T1", Resolvable: true, Resolved: false, CreatedAt: at},
		{ID: 3, Thread: "T2", Resolvable: true, Resolved: true, CreatedAt: at},
		{ID: 4, Thread: "T3", Resolvable: false, Resolved: true, CreatedAt: at},
		{ID: 5, Thread: "T4", System: true, Resolvable: true, CreatedAt: at},
		{ID: 6, Resolvable: true, Resolved: true, CreatedAt: at},
	}
	got := ForgeResolved(notes)
	if v, ok := got["T1"]; !ok || v {
		t.Errorf("T1 is decided by its first note, which is open: %v", got)
	}
	if !got["T2"] {
		t.Errorf("T2 is resolved: %v", got)
	}
	if _, ok := got["T3"]; ok {
		t.Errorf("a thread that cannot be resolved has no state: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("state = %v", got)
	}
}

func TestConfirmedKeepsOnlyWhatWasAgreedTo(t *testing.T) {
	a := Step{Dir: "wt", File: "a.go", Line: 3, IsRoot: true, Root: Comment{ID: "a"}, Comment: Comment{ID: "a"}}
	aRes := Step{Dir: "wt", File: "a.go", Line: 3, Resolve: true, Root: Comment{ID: "a"}}
	b := Step{Dir: "wt", File: "b.go", Line: 1, IsRoot: true, Root: Comment{ID: "b"}, Comment: Comment{ID: "b"}}
	c := Step{Dir: "wt", File: "c.go", Line: 1, IsRoot: true, Root: Comment{ID: "c"}, Comment: Comment{ID: "c"}}
	confirmed := []Step{a, aRes, b}
	// Now: a moved to line 7, b is gone, c is new.
	moved := a
	moved.Line = 7
	movedRes := aRes
	movedRes.Line = 7
	got := Confirmed([]Step{moved, movedRes, c}, confirmed)
	if len(got) != 2 || got[0].Line != 7 || !got[1].Resolve || got[1].Line != 7 {
		t.Fatalf("kept = %+v", got)
	}
}

func TestPublishResolvesAfterThePostsUsingTheNewThread(t *testing.T) {
	root := pending("r")
	steps := PlanResolving([]Thread{thread("r", "a.go", 3, true, root, pending("r1"))}, map[string]bool{})
	f := &fakeForge{rootPath: "a.go", rootLine: 3}
	var records int
	p := Publisher{Poster: f, Record: func(context.Context, string, string, string, Source, string) error { records++; return nil }}
	done, err := p.Publish(context.Background(), steps)
	if err != nil || done != 3 || records != 2 {
		t.Fatalf("done=%d records=%d err=%v", done, records, err)
	}
	want := []string{`discussion a.go:3 "r"`, `reply thread1 "r1"`, "resolve thread1 true"}
	for i, w := range want {
		if f.calls[i] != w {
			t.Errorf("call %d = %s, want %s", i, f.calls[i], w)
		}
	}
}

func TestPublishResolvesAThreadThatIsAlreadyOnTheForge(t *testing.T) {
	steps := PlanResolving([]Thread{thread("a", "a.go", 1, true, onForge("a", "T1"))}, map[string]bool{"T1": false})
	f := &fakeForge{}
	p := Publisher{Poster: f}
	if done, err := p.Publish(context.Background(), steps); err != nil || done != 1 {
		t.Fatalf("done=%d err=%v", done, err)
	}
	if len(f.calls) != 1 || f.calls[0] != "resolve T1 true" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestPublishSkipsResolvingOnceWhereTheForgeCannot(t *testing.T) {
	steps := PlanResolving([]Thread{
		thread("a", "a.go", 1, true, onForge("a", "T1")),
		thread("b", "b.go", 2, true, onForge("b", "T2")),
	}, map[string]bool{"T1": false, "T2": false})
	f := &fakeForge{resolveErr: forge.ErrNotSupported}
	var logs []string
	p := Publisher{Poster: f, Log: func(s string) { logs = append(logs, s) }}
	if _, err := p.Publish(context.Background(), steps); err != nil {
		t.Fatalf("an unsupported resolve is not an error: %v", err)
	}
	if len(f.calls) != 1 {
		t.Errorf("the second thread must not be tried once the forge said no: %v", f.calls)
	}
	said := 0
	for _, l := range logs {
		if strings.Contains(l, "cannot resolve threads") {
			said++
		}
	}
	if said != 1 {
		t.Errorf("it must say so exactly once: %v", logs)
	}
}

func TestAFailedResolveIsReportedButDoesNotUndoThePosts(t *testing.T) {
	root := pending("r")
	steps := PlanResolving([]Thread{
		thread("r", "a.go", 3, true, root),
		thread("s", "b.go", 4, false, pending("s")),
	}, map[string]bool{})
	f := &fakeForge{resolveErr: errors.New("403 forbidden"), rootPath: "a.go", rootLine: 3}
	records := 0
	p := Publisher{Poster: f, Record: func(context.Context, string, string, string, Source, string) error { records++; return nil }}
	done, err := p.Publish(context.Background(), steps)
	if err == nil || !strings.Contains(err.Error(), "could not be resolved") || !strings.Contains(err.Error(), "a.go:3") {
		t.Fatalf("err = %v", err)
	}
	// Both posts went out and were recorded; the failure stopped nothing.
	if records != 2 || done != 2 {
		t.Fatalf("records=%d done=%d", records, done)
	}
}

func TestAnOrphanedRootIsPostedOnTheConversationWithoutALine(t *testing.T) {
	root := pending("r")
	th := thread("r", "old/file.go", 42, false, root, pending("r1"))
	th.Orphaned = true
	steps := Plan([]Thread{th})
	f := &fakeForge{}
	p := Publisher{Poster: f, Record: func(context.Context, string, string, string, Source, string) error { return nil }}
	if _, err := p.Publish(context.Background(), steps); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 || !strings.HasPrefix(f.calls[0], "comment ") || strings.Contains(f.calls[0], ":42") || !strings.Contains(f.calls[0], "old/file.go") {
		t.Fatalf("an orphan must not be sent to a stale line: %v", f.calls)
	}
	// It has no thread, so its reply follows as a comment that points at it.
	if !strings.HasPrefix(f.calls[1], "comment ") || !strings.Contains(f.calls[1], "In reply to ") {
		t.Errorf("reply = %v", f.calls)
	}
}

// ---- Incomm re-anchors before anything is read ----------------------------

func TestReanchorMakesThePlanFollowTheCode(t *testing.T) {
	binary, err := CheckCLI(context.Background())
	if err != nil {
		t.Skip("no usable incomm installed: " + err.Error())
	}
	dir := t.TempDir()
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package main\n\nfunc a() {}\nfunc b() {}\nfunc c() {}\n")
	run := func(args ...string) []byte {
		cmd := exec.Command(binary, append([]string{"--root", dir, "--json"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("incomm %v: %v\n%s", args, err, out)
		}
		return out
	}
	run("add", "--file", "code.go", "--line", "4", "--content", "about b", "--author", "user",
		"--author-title", "Jan", "--audience", "external")
	line := func() (int, bool) {
		threads := ReadThreads(dir)
		if len(threads) != 1 {
			t.Fatalf("threads = %+v", threads)
		}
		return threads[0].Line, threads[0].Orphaned
	}
	if l, o := line(); l != 4 || o {
		t.Fatalf("before: %d %v", l, o)
	}

	// Lines land above the code: the stored line is now stale.
	write("// new\n// lines\npackage main\n\nfunc a() {}\nfunc b() {}\nfunc c() {}\n")
	if l, _ := line(); l != 4 {
		t.Fatalf("nothing has re-anchored yet, the line is still %d", l)
	}
	if err := Reanchor(context.Background(), dir, filepath.Join(dir, "no-incomm-here"), dir); err != nil {
		t.Fatal(err)
	}
	steps := Plan(ReadThreads(dir))
	if len(steps) != 1 || steps[0].Line != 6 || steps[0].Orphaned {
		t.Fatalf("the plan must follow the code to line 6: %+v", steps)
	}

	// The code is gone: the comment is orphaned, still listed, and marked.
	write("package main\n\nfunc a() {}\nfunc c() {}\n")
	if err := Reanchor(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	steps = Plan(ReadThreads(dir))
	if len(steps) != 1 || !steps[0].Orphaned || !strings.Contains(steps[0].Summary(), "orphaned") {
		t.Fatalf("an orphaned pending comment is listed and marked: %+v", steps)
	}

	// The comment was never touched by a view: it is still there, still external.
	var listed struct{ Notes []json.RawMessage }
	if err := json.Unmarshal(run("--view", "external", "list"), &listed); err != nil || len(listed.Notes) != 1 {
		t.Fatalf("external list: %v %v", listed, err)
	}
}

func TestReanchorWithoutAnyIncommStateNeedsNoCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no incomm anywhere
	if err := Reanchor(context.Background(), t.TempDir(), ""); err != nil {
		t.Fatalf("no worktree has Incomm state, so there is nothing to do: %v", err)
	}
}
