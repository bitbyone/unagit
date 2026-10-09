package watch

import (
	"fmt"
	"testing"
	"time"
)

// TestHistoryKeepsEachThingsStory: events go to their thing's history,
// newest first when read; all of them read together in time; once drops
// what only says again the last; a history keeps its newest events and
// forgets the old.
func TestHistoryKeepsEachThingsStory(t *testing.T) {
	t.Parallel()
	s := Open(t.TempDir())
	now := time.Now()
	ev := func(key, line string, ago time.Duration) Event {
		return Event{Key: key, Heading: "H", Line: line, At: now.Add(-ago)}
	}
	if err := s.AppendHistory(false, ev("gl:a/b!1", "one", 3*time.Minute), ev("gl:a/b!1", "two", 2*time.Minute), ev("agent:x", "waits", time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h := s.History("gl:a/b!1"); len(h) != 2 || h[0].Line != "two" || h[1].Line != "one" {
		t.Fatalf("history %+v", h)
	}
	// The same words twice are two things that happened, unless once.
	s.AppendHistory(false, ev("gl:a/b!1", "two", 0))
	if h := s.History("gl:a/b!1"); len(h) != 3 {
		t.Fatalf("the same news again was dropped: %d", len(h))
	}
	s.AppendHistory(true, ev("agent:x", "waits", 0))
	if h := s.History("agent:x"); len(h) != 1 {
		t.Fatalf("an agent's state said again was kept: %d", len(h))
	}
	all := s.AllHistory(0)
	if len(all) != 4 || all[0].Key != "gl:a/b!1" || all[len(all)-1].Line != "one" {
		t.Fatalf("all %+v", all)
	}
	if got := s.AllHistory(2); len(got) != 2 {
		t.Fatalf("a limit of 2 gave %d", len(got))
	}
	// A long story keeps its newest; an old one is forgotten.
	var many []Event
	for i := range historyKept + 10 {
		many = append(many, ev("gl:long", fmt.Sprint(i), time.Duration(historyKept+10-i)*time.Second))
	}
	s.AppendHistory(false, many...)
	s.AppendHistory(false, ev("gl:long", "last", 0))
	if h := s.History("gl:long"); len(h) != historyKept || h[0].Line != "last" {
		t.Fatalf("kept %d, newest %q", len(h), h[0].Line)
	}
	s.AppendHistory(false, ev("gl:old", "long ago", historyAge+time.Hour))
	s.AllHistory(0)
	if h := s.History("gl:old"); len(h) != 0 {
		t.Fatalf("an old story was kept: %+v", h)
	}
	if s.HistoryStamp().IsZero() {
		t.Fatal("no stamp for the histories")
	}
}
