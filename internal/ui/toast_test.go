package ui

import (
	"strings"
	"testing"
	"time"
)

// TestToastsStackAndGo: toasts of every severity stack in the top right
// corner, the newest on top, each bordered in its severity's colour, take
// no key, and go once their time is up - counted only while the terminal is
// in front.
func TestToastsStackAndGo(t *testing.T) {
	t.Parallel()
	// The toasts' ticks alone: a spinner's would take them otherwise.
	ticks, others := make(chan time.Time), make(chan time.Time)
	t.Cleanup(func() { close(ticks); close(others) })
	a, sc, _ := newTestAppSrv(t, func(a *App) {
		a.newAnimationTicker = func(every time.Duration) (<-chan time.Time, func()) {
			if every == toastTick {
				return ticks, func() {}
			}
			return others, func() {}
		}
	})
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		a.showToast(sevInfo, "acme/api !42", "pipeline started")
		a.showToast(sevSuccess, "acme/api !42", "pipeline passed")
		a.showToast(sevWarning, "acme/web main", "pipeline cancelled")
		a.showToast(sevError, "acme/web main", "pipeline failed · unit tests")
		return true
	})
	waitFor(t, a, sc, "pipeline failed · unit tests")
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 24}, {60, 20}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "pipeline failed · unit tests")
		text := a.screenText(sc)
		lines := strings.Split(text, "\n")
		failed, started := -1, -1
		for i, line := range lines {
			if strings.Contains(line, "pipeline failed") {
				failed = i
			}
			if strings.Contains(line, "pipeline started") {
				started = i
			}
		}
		if size.h >= 24 && (started < 0 || failed > started) {
			t.Fatalf("at %dx%d the newest is not on top:\n%s", size.w, size.h, text)
		}
		assertLegible(t, a, sc, "toasts")
	}
	resizeApp(a, sc, 120, 34)
	_, style := cellAt(a, sc, 120-3, 2)
	if fg, _, _ := style.Decompose(); fg != onLoop(a, func() any { return toastColour(sevError) }) {
		t.Fatalf("the newest toast's border is %v, not the danger colour", fg)
	}
	// A key goes to the list, not to a toast.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "[2] Merge requests")

	// Out of front, the time stands still.
	onLoop(a, func() bool {
		a.quiet.focusKnown.Store(true)
		a.quiet.focused.Store(false)
		for _, t := range a.toasts {
			t.left = time.Millisecond
		}
		return true
	})
	// A tick is wholly counted once the next one is taken.
	ticks <- time.Now()
	ticks <- time.Now()
	ticks <- time.Now()
	if n := onLoop(a, func() int { return len(a.toasts) }); n != 4 {
		t.Fatalf("%d toasts left while the terminal was away, want 4", n)
	}
	onLoop(a, func() bool { a.quiet.focused.Store(true); return true })
	// Ticked until they are gone: the counting ends with the last of them.
	waitTrue(t, "the toasts stayed in front", func() bool {
		select {
		case ticks <- time.Now():
		case <-time.After(20 * time.Millisecond):
		}
		return onLoop(a, func() int { return len(a.toasts) }) == 0
	})
	waitGone(t, a, sc, "pipeline failed · unit tests")
}
