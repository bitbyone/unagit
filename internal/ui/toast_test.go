package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
)

// TestToastsStackAndGo: toasts of every severity stack upwards from the
// bottom right corner, the newest in the corner, each filled with its
// severity's colour and headed by its icon, take no key, and go once their
// time is up - counted only while the terminal is in front.
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
		a.showToastAbout(sevInfo, "Pipeline running", "acme/api · Rate limits", "Pipeline #8 of !42 started")
		a.showToastAbout(sevSuccess, "Pipeline passed", "acme/api · Rate limits", "Pipeline #8 of !42 passed")
		a.showToastAbout(sevWarning, "Pipeline cancelled", "acme/web", "Pipeline #9 of main was cancelled")
		a.showToastAbout(sevError, "Pipeline failed", "acme/web", "Pipeline #9 of main failed in unit tests")
		return true
	})
	waitFor(t, a, sc, "failed in unit tests")
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 24}, {60, 20}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "failed in unit tests")
		text := a.screenText(sc)
		lines := strings.Split(text, "\n")
		failed, started := -1, -1
		for i, line := range lines {
			if strings.Contains(line, "Pipeline failed") {
				failed = i
			}
			if strings.Contains(line, "Pipeline running") {
				started = i
			}
		}
		if failed < size.h/2 {
			t.Fatalf("at %dx%d the newest is not at the bottom:\n%s", size.w, size.h, text)
		}
		if size.h >= 34 && (started < 0 || started > failed) {
			t.Fatalf("at %dx%d the older do not stack above the newest:\n%s", size.w, size.h, text)
		}
		assertLegible(t, a, sc, "toasts")
	}
	resizeApp(a, sc, 120, 34)
	waitFor(t, a, sc, "failed in unit tests")
	// The newest ends three rows above the bottom, in the danger colours.
	_, style := cellAt(a, sc, 120-3, 34-3)
	want := onLoop(a, func() [2]any { return [2]any{toastRole(sevError, "border"), toastRole(sevError, "background")} })
	if fg, bg, _ := style.Decompose(); fg != want[0] || bg != want[1] {
		t.Fatalf("the newest toast's corner is %v on %v, not the danger border on its fill", fg, bg)
	}
	if text := a.screenText(sc); !strings.Contains(text, onLoop(a, func() string { return toastIcons[sevError] })+"  Pipeline failed") {
		t.Fatalf("the danger toast is not headed by its icon:\n%s", text)
	}
	// A key goes to the list, not to a toast.
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Merge requests [2]")

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
	waitGone(t, a, sc, "failed in unit tests")
}

// TestToastsStayAsLongAsSettingsSay: five seconds unless Settings ›
// Notifications says otherwise for the toast's severity, and what it says
// is saved.
func TestToastsStayAsLongAsSettingsSay(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	life := func(sev severity) time.Duration {
		return onLoop(a, func() time.Duration {
			a.toasts = nil
			a.showToast(sev, "Pipeline running", "Pipeline #8 of !42 started")
			left := a.toasts[0].left
			a.toasts = nil
			return left
		})
	}
	if got := life(sevError); got != 5*time.Second {
		t.Fatalf("a toast stays %v by default, want 5s", got)
	}
	openSection(t, a, sc, sectionNotifications)
	waitFor(t, a, sc, "Danger stays")
	for _, size := range []struct{ w, h int }{{120, 34}, {80, 24}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "Danger stays")
		assertLegible(t, a, sc, "Settings › Notifications")
	}
	changeOnLoop(a, func() {
		a.settings.notices.GetFormItemByLabel("Danger stays").(*tview.DropDown).SetCurrentOption(slices.Index(toastChoices, 10))
	})
	pressButton(t, a, sc, a.settings.notices, "Save")
	waitTrue(t, "the length was not saved", func() bool { return onLoop(a, func() int { return a.cfg.ToastSeconds.Danger }) == 10 })
	if got := life(sevError); got != 10*time.Second {
		t.Fatalf("a danger toast stays %v, want 10s", got)
	}
	if got := life(sevInfo); got != 5*time.Second {
		t.Fatalf("an info toast stays %v, want the 5s it was left at", got)
	}
	saved, err := config.LoadFrom(a.cfg.Dir())
	if err != nil || saved.ToastSeconds != (config.ToastSeconds{Danger: 10}) {
		t.Fatalf("the file says %+v (%v)", saved.ToastSeconds, err)
	}
}
