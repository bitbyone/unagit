package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOnlyOneInstancePollsAndAnotherTakesOver(t *testing.T) {
	t.Parallel()
	s := Open(t.TempDir())
	first, ok, err := s.TryPoll()
	if err != nil || !ok {
		t.Fatalf("the first instance should poll: %v %v", ok, err)
	}
	other := Open(s.Dir())
	if _, ok, err := other.TryPoll(); ok || err != nil {
		t.Fatalf("a second instance polled beside the first: %v %v", ok, err)
	}
	first.Release()
	second, ok, err := other.TryPoll()
	if err != nil || !ok {
		t.Fatalf("the second instance did not take over: %v %v", ok, err)
	}
	second.Release()
}

func TestTwoWritersBothKeepTheirWatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := Watch{Kind: KindPipeline, Instance: "gl", Project: "acme/api", IID: i + 1, Since: time.Now()}
			if _, err := Open(dir).Change(func(ws []Watch) []Watch { return append(ws, w) }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := Open(dir).Watches()
	if err != nil || len(got) != 20 {
		t.Fatalf("got %d watches, want 20 (%v)", len(got), err)
	}
}

func TestAReaderNeverSeesHalfAFile(t *testing.T) {
	t.Parallel()
	s := Open(t.TempDir())
	big := Snapshot{States: map[string]State{}}
	for i := range 500 {
		big.States[string(rune('a'+i%26))+time.Duration(i).String()] = State{Status: "running", Title: "a title long enough to make the file large"}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			if err := s.WriteState(big); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		b, err := os.ReadFile(filepath.Join(s.Dir(), "state.json"))
		if err != nil || len(b) == 0 {
			continue
		}
		var snap Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			t.Fatalf("read half a file: %v", err)
		}
	}
}

func TestEventsAreNumberedAndKeptShort(t *testing.T) {
	t.Parallel()
	var snap Snapshot
	for range keptEvents + 10 {
		snap.Add(Event{Key: "k", Line: "pipeline failed"})
	}
	if len(snap.Events) != keptEvents || snap.Seq != keptEvents+10 {
		t.Fatalf("kept %d events at seq %d", len(snap.Events), snap.Seq)
	}
	if after := snap.After(snap.Seq - 2); len(after) != 2 {
		t.Fatalf("after: %d events, want 2", len(after))
	}
}

func TestAPresenceOfAProcessGoneIsSweptUp(t *testing.T) {
	t.Parallel()
	s := Open(t.TempDir())
	if err := s.SetPresence(Presence{ID: "me", PID: os.Getpid(), Focused: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPresence(Presence{ID: "gone", PID: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	got := s.Presences()
	if len(got) != 1 || got[0].ID != "me" {
		t.Fatalf("presences: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "present", "gone")); !os.IsNotExist(err) {
		t.Fatal("the presence of a process gone was left behind")
	}
}
