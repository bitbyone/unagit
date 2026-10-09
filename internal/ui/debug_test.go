package ui

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestDebugIsThereOnlyWithTheFlag: Settings lists Debug only with --debug,
// and there each row fires what it names - a toast, a watched change, a
// desktop notification - legible at every size.
func TestDebugIsThereOnlyWithTheFlag(t *testing.T) {
	t.Parallel()
	plain, plainSc := newTestApp(t)
	waitFor(t, plain, plainSc, "acme/gateway")
	typeRunes(plainSc, "6")
	waitFor(t, plain, plainSc, "Integrations")
	if strings.Contains(plain.screenText(plainSc), debugSectionName) {
		t.Fatal("Debug is listed without --debug")
	}

	var notified atomic.Int64
	a, sc, _ := newTestAppSrv(t, func(a *App) {
		a.debug = true
		a.debugWait = 20 * time.Millisecond
		a.notifier = func(title, body string) { notified.Add(1) }
	})
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "6")
	waitFor(t, a, sc, debugSectionName)
	changeOnLoop(a, func() {
		a.settings.selectSection(sectionDebug)
		a.settings.focusContent()
	})
	waitFor(t, a, sc, "six at once")
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "says focus")
		assertLegible(t, a, sc, "Settings › Debug")
	}
	resizeApp(a, sc, 120, 34)

	// A watched failure, while in front: a toast and no notification.
	pick := func(name string) {
		changeOnLoop(a, func() {
			for i, tr := range a.settings.debug.triggers {
				if tr.name == name {
					a.settings.debug.table.Select(i, 0)
				}
			}
		})
	}
	pick("Pipeline failed")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "failed in test:unit")
	assertLegible(t, a, sc, "a danger toast over Settings")
	if n := notified.Load(); n != 0 {
		t.Fatalf("news in front was notified %d times", n)
	}
	pick("through the system, in 4 s")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitTrue(t, "the desktop notification never went", func() bool { return notified.Load() == 1 })
}

// TestDebugShowsEverySituationAsItWillCome: each situation Debug fires is
// news the poller makes, with a heading and a sentence that names the
// watch, and no situation is left out.
func TestDebugShowsEverySituationAsItWillCome(t *testing.T) {
	t.Parallel()
	headings := map[string]bool{}
	for _, c := range debugNewsCases() {
		if len(c.events) == 0 {
			t.Fatalf("%s makes no news", c.name)
		}
		for _, e := range c.events {
			if e.Heading == "" || !strings.Contains(e.Line, "!334") && !strings.Contains(e.Line, "feature/token-bucket") {
				t.Errorf("%s: %q, %q does not say what and where", c.name, e.Heading, e.Line)
			}
			headings[e.Heading] = true
		}
	}
	for _, want := range []string{"Pipeline running", "Pipeline passed", "Pipeline failed", "Pipeline cancelled",
		"Manual job waiting", "New commit in MR", "New commits in MR", "MR force-pushed", "New comment in MR",
		"New comments in MR", "MR approved", "Approval withdrawn", "Base moved on", "MR merged", "MR closed", "Branch deleted"} {
		if !headings[want] {
			t.Errorf("no situation says %q", want)
		}
	}
}
