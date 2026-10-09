package ui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/session"
	"github.com/tobola/unagit/internal/watch"
)

// activityFixture is an app on the Activity screen with three watches - a
// failed pipeline, a running one, a quiet branch - their stories, and
// three editors open, the last visit an hour ago.
func activityFixture(t *testing.T) (*App, tcell.SimulationScreen, watch.Watch) {
	t.Helper()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	mr := watch.Watch{Kind: watch.KindPipeline, Instance: "gl", Project: "acme/gateway", IID: 334, Title: "Rate limiting for the public API", Since: now.Add(-48 * time.Hour)}
	onLoop(a, func() bool {
		mr2 := watch.Watch{Kind: watch.KindPipeline, Instance: "gl", Project: "acme/api", IID: 341, Title: "Cache the tokens", Since: now.Add(-5 * time.Hour)}
		br := watch.Watch{Kind: watch.KindPipeline, Instance: "gl", Project: "acme/web", Branch: "feature/x", Since: now.Add(-72 * time.Hour)}
		a.watches = []watch.Watch{mr, mr2, br}
		a.watchSnap = watch.Snapshot{Seq: 3, States: map[string]watch.State{
			mr.Key():  {Pipeline: 8121, Status: "failed", Failed: "test:unit", Changed: now.Add(-2 * time.Minute), Known: true, Head: "9f8e7d6c5b", HeadBy: "John Smith", Comments: 5, Seq: 3},
			mr2.Key(): {Pipeline: 8130, Status: "running", Changed: now.Add(-4 * time.Minute)},
			br.Key():  {Pipeline: 8000, Status: "success", Changed: now.Add(-26 * time.Hour), Base: "main", Behind: 3},
		}}
		a.activityLog = []watch.Event{
			{Key: mr.Key(), What: mr.Label(), Heading: "Pipeline failed", Line: "Pipeline #8121 of !334 failed in test:unit", Level: watch.Danger, At: now.Add(-2 * time.Minute)},
			{Key: mr2.Key(), What: mr2.Label(), Heading: "Pipeline running", Line: "Pipeline #8130 of !341 started", Level: watch.Info, At: now.Add(-4 * time.Minute)},
			{Key: mr.Key(), What: mr.Label(), Heading: "MR approved", Line: "!334 was approved by msmith", Level: watch.Success, At: now.Add(-3 * time.Hour)},
			{Key: br.Key(), What: br.Label(), Heading: "Pipeline passed", Line: "Pipeline #8000 of feature/x passed", Level: watch.Success, At: now.Add(-26 * time.Hour)},
		}
		a.editorsOpen = []session.Record{
			{Dir: "/tmp/unagit", Project: "tobola/unagit", Editor: "nvim", Branch: "fix/ci", Socket: "/tmp/unagit-test.sock", Since: now.Add(-3 * time.Hour)},
			{Dir: "/tmp/incomm", Project: "tobola/incomm", Editor: "nvim", Branch: "main", Since: now.Add(-26 * time.Hour)},
			{Dir: "/tmp/api", Project: "acme/api", Editor: "idea", IID: 341, Mode: "review", Since: now.Add(-20 * time.Minute)},
		}
		a.cfg.State.ActivityVisit = now.Add(-time.Hour)
		a.switchTab(pageActivity)
		return true
	})
	waitFor(t, a, sc, "NEEDS YOU")
	return a, sc, mr
}

// TestActivityLaysOutItsPanels: the cards, Watching, the list in its
// sections, the detail where there is room and the log at several sizes,
// every frame whole and everything legible; the log keeps three lines of
// events at least, and the line of the last visit sets apart what is new.
func TestActivityLaysOutItsPanels(t *testing.T) {
	t.Parallel()
	a, sc, _ := activityFixture(t)
	for _, size := range []struct{ w, h int }{{160, 44}, {120, 34}, {100, 26}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "NEEDS YOU")
		waitTrue(t, "the log was not laid out", func() bool { return onLoop(a, func() bool { return a.activity.logH > 0 }) })
		text := a.screenText(sc)
		t.Logf("Activity at %dx%d:\n%s", size.w, size.h, text)
		lines := strings.Split(text, "\n")
		wants := []string{"Open", "Watching", "NEEDS YOU", "UNDER WAY", "QUIET", "Log", "Pipeline failed"}
		if size.h >= activityTall {
			// Below, the log has room for the newest events alone.
			wants = append(wants, "since your last visit")
		}
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%q is missing at %dx%d", want, size.w, size.h)
			}
		}
		needs, under, quiet := -1, -1, -1
		for i, line := range lines {
			switch {
			case strings.Contains(line, "NEEDS YOU"):
				needs = i
			case strings.Contains(line, "UNDER WAY"):
				under = i
			case strings.Contains(line, "QUIET"):
				quiet = i
			}
			if i > 0 && !strings.Contains(line, "NORMAL") && strings.TrimSpace(line) != "" {
				end := strings.TrimRight(line, " ")
				if !strings.HasSuffix(end, "│") && !strings.HasSuffix(end, "╮") && !strings.HasSuffix(end, "╯") && !strings.HasPrefix(strings.TrimSpace(line), "/") {
					t.Fatalf("a frame is broken at %dx%d: %q", size.w, size.h, line)
				}
			}
		}
		if !(needs < under && under < quiet) {
			t.Fatalf("the sections are out of order at %dx%d", size.w, size.h)
		}
		logRows := onLoop(a, func() int { return a.activity.logH })
		if logRows < activityLogLeast+2 {
			t.Fatalf("the log has %d rows at %dx%d", logRows, size.w, size.h)
		}
		wide := onLoop(a, func() bool { return a.activityPane.detailShown })
		if size.w >= activityWide && !wide {
			t.Fatalf("no detail beside the list at %d", size.w)
		}
		assertLegible(t, a, sc, "Activity")
	}
}

// TestActivityPanelsAreReachedByKeys: Tab goes round the panels, e, d and
// L go to the cards, the detail and the log, Esc comes back to the list,
// and a row of the log chooses its thing in the list.
func TestActivityPanelsAreReachedByKeys(t *testing.T) {
	t.Parallel()
	a, sc, mr := activityFixture(t)
	resizeApp(a, sc, 150, 42)
	waitFor(t, a, sc, "NEEDS YOU")
	focused := func() string {
		return onLoop(a, func() string {
			v := a.activity
			switch {
			case v.pane.table.HasFocus():
				return "list"
			case v.pane.detail.HasFocus():
				return "detail"
			case v.log.HasFocus():
				return "log"
			case v.cards.HasFocus():
				return "cards"
			case v.watching.HasFocus():
				return "watching"
			}
			return "?"
		})
	}
	var round []string
	for range 5 {
		sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
		waitTrue(t, "Tab did not move", func() bool { return focused() != "?" })
		time.Sleep(20 * time.Millisecond)
		round = append(round, focused())
	}
	if got := strings.Join(round, " "); got != "detail log cards watching list" {
		t.Fatalf("Tab went %s", got)
	}
	for _, step := range []struct{ key, want string }{{"e", "cards"}, {"d", "detail"}, {"L", "log"}} {
		sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
		waitTrue(t, "Esc did not go back to the list", func() bool { return focused() == "list" })
		typeRunes(sc, step.key)
		waitTrue(t, step.key+" did not go to the "+step.want, func() bool { return focused() == step.want })
	}
	// Moving in the log leaves the list where it is; Enter goes to the
	// row's thing - the first, the failed !334.
	before := onLoop(a, func() int { return a.activityPane.selectedIndex() })
	typeRunes(sc, "j")
	time.Sleep(50 * time.Millisecond)
	if after := onLoop(a, func() int { return a.activityPane.selectedIndex() }); after != before {
		t.Fatal("moving in the log moved the list")
	}
	typeRunes(sc, "k")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "Enter in the log did not go to the list on its thing", func() bool {
		return focused() == "list" && onLoop(a, func() string {
			it, _ := a.activityItemAt(a.activityPane.selectedIndex())
			return it.key()
		}) == mr.Key()
	})

	// h j k l go on past the edge of what a panel holds: from the list's
	// first row up to the cards, past the last card to Watching, down to
	// the detail beneath it, back left to the list, past its last row down
	// to the log, and up from the log's first row to the list again.
	typeRunes(sc, "g")
	for _, step := range []struct{ keys, want string }{
		{"k", "cards"}, {"lll", "watching"}, {"j", "detail"}, {"h", "list"},
		{"G", "list"}, {"j", "log"}, {"k", "list"},
	} {
		typeRunes(sc, step.keys)
		waitTrue(t, step.keys+" did not reach the "+step.want, func() bool { return focused() == step.want })
	}
	// The panels name their keys in their titles.
	text := a.screenText(sc)
	for _, title := range []string{"Activity  a", "Open  e", "Watching  W", "Log  L"} {
		if !strings.Contains(text, title) {
			t.Errorf("no title %q", title)
		}
	}
	// A card's actions are the editor's.
	typeRunes(sc, "e")
	waitTrue(t, "e did not go to the cards", func() bool { return focused() == "cards" })
	typeRunes(sc, "hh") // the first card, a Neovim
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Attach In…")
	waitFor(t, a, sc, "Close Editor")
}

// TestTheLogComesToTheFront: z opens every event at full length, and
// Enter goes to its row.
func TestTheLogComesToTheFront(t *testing.T) {
	t.Parallel()
	a, sc, _ := activityFixture(t)
	typeRunes(sc, "z")
	waitFor(t, a, sc, "WHAT HAPPENED")
	waitFor(t, a, sc, "Pipeline #8000 of feature/x passed")
	assertLegible(t, a, sc, "the log in front")
	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "the log in front did not close", func() bool { return !onLoop(a, a.modalOpen) })
	waitTrue(t, "Enter did not go to the branch's row", func() bool {
		return onLoop(a, func() bool {
			it, _ := a.activityItemAt(a.activityPane.selectedIndex())
			return it.kind == activityWatch && it.watch.Branch == "feature/x"
		})
	})
}

// TestEveryWatchInOneDialog: W lists what is watched from any screen, and
// x there stops one, the list staying open for the next.
func TestEveryWatchInOneDialog(t *testing.T) {
	t.Parallel()
	srv := newWatchServer(t)
	cfg := writeTestConfig(t, srv.URL)
	w := watchMR7(t, cfg)
	var notified atomic.Int64
	a, sc, _ := watchApp(t, cfg, &notified)
	waitState(t, cfg, w.Key(), "running")
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "W")
	waitFor(t, a, sc, "KIND")
	waitFor(t, a, sc, "merge request")
	assertLegible(t, a, sc, "the watches")
	typeRunes(sc, "x")
	waitFor(t, a, sc, "stopped watching acme/gateway !7")
	if ws, _ := watch.Open(cfg.WatchDir()).Watches(); len(ws) != 0 {
		t.Fatalf("watches.json still holds %+v", ws)
	}
	// Its history says it began and ended.
	waitTrue(t, "the watch's history does not say it was let go", func() bool {
		h := watch.Open(cfg.WatchDir()).History(w.Key())
		return len(h) > 0 && h[0].Heading == "Not watched"
	})
}

// TestAnAgentsStatesAreItsHistory: a new agent, a change of state and an
// agent gone are events of its history; a state read again is not.
func TestAnAgentsStatesAreItsHistory(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	rec := &session.Record{Dir: "/tmp/gw", Project: "acme/gateway", Editor: "claude", Pane: "p1", Since: time.Now()}
	working := agentRow{Kind: "claude", Status: "working", Dir: rec.Dir, Record: rec}
	waiting := working
	waiting.Status = "blocked"
	headings := func(evs []watch.Event) string {
		var out []string
		for _, e := range evs {
			out = append(out, e.Heading)
		}
		return strings.Join(out, ", ")
	}
	onLoop(a, func() bool {
		for _, c := range []struct {
			before, after []agentRow
			want          string
		}{
			{nil, []agentRow{working}, "Agent working"},
			{[]agentRow{working}, []agentRow{working}, ""},
			{[]agentRow{working}, []agentRow{waiting}, "Agent waiting"},
			{[]agentRow{waiting}, nil, "Agent closed"},
		} {
			if got := headings(a.agentChanges(c.before, c.after)); got != c.want {
				t.Errorf("%v -> %v: %q, want %q", c.before, c.after, got, c.want)
			}
		}
		return true
	})
}

// TestTheLogLightsTheRowsOfTheChosenThing: the log's rows of the thing
// under the list's cursor are lit, faintly, and follow the cursor at once.
func TestTheLogLightsTheRowsOfTheChosenThing(t *testing.T) {
	t.Parallel()
	a, sc, mr := activityFixture(t)
	lit := func() []string {
		return onLoop(a, func() []string {
			var out []string
			v := a.activity
			for row := 1; row < v.log.GetRowCount() && row < len(v.logKeys); row++ {
				if _, bg, _ := v.log.GetCell(row, 0).Style.Decompose(); v.logKeys[row] != "" && bg == role("activity.lit") {
					out = append(out, v.logKeys[row])
				}
			}
			return out
		})
	}
	waitTrue(t, "the failed merge request's rows are not lit", func() bool {
		got := lit()
		return len(got) == 2 && got[0] == mr.Key() && got[1] == mr.Key()
	})
	typeRunes(sc, "j")
	waitTrue(t, "the lit rows did not follow the cursor", func() bool {
		got := lit()
		return len(got) == 1 && got[0] != mr.Key()
	})
}

// TestOnePanelHasTheFocusAfterADialog: a dialog opened from a card and
// closed gives the focus back to the cards, which alone are lit.
func TestOnePanelHasTheFocusAfterADialog(t *testing.T) {
	t.Parallel()
	a, sc, _ := activityFixture(t)
	typeRunes(sc, "e")
	waitTrue(t, "e did not go to the cards", func() bool { return onLoop(a, func() bool { return a.activity.cards.HasFocus() }) })
	typeRunes(sc, "z")
	waitFor(t, a, sc, "WHAT HAPPENED")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitTrue(t, "the focus did not come back to the cards alone", func() bool {
		return onLoop(a, func() bool {
			v := a.activity
			return !a.modalOpen() && v.cards.HasFocus() && v.cards.GetBorderColor() == colBorderFocus &&
				v.pane.table.GetBorderColor() != colBorderFocus
		})
	})
	// Enter in the detail does what Enter on the row does.
	typeRunes(sc, "d")
	waitTrue(t, "d did not go to the detail", func() bool { return onLoop(a, func() bool { return a.activityPane.detail.HasFocus() }) })
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "Enter in the detail did nothing", func() bool { return onLoop(a, a.modalOpen) || onLoop(a, func() bool { return a.transient != "" }) })
}
