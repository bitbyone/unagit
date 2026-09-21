package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestDetailFollowsTheSelection covers the everyday review loop: open the
// detail column once, go back to the list, and keep moving - the column keeps
// up without taking the focus away.
func TestDetailFollowsTheSelection(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // back to the list
	waitFor(t, a, sc, "NORMAL")

	typeRunes(sc, "j")
	waitFor(t, a, sc, "Bankers rounding everywhere")
	waitFor(t, a, sc, "Bob Ross")
	if strings.Contains(a.screenText(sc), "Jane Doe") {
		t.Error("the detail column still shows the previous merge request")
	}
	// Moving the cursor must not drag the focus into the detail column.
	if a.mrsPane.detailFocused {
		t.Error("following the selection stole the focus")
	}
	waitFor(t, a, sc, "NORMAL")

	typeRunes(sc, "k")
	waitFor(t, a, sc, "Jane Doe")
}

// TestProjectDetailFollowsTheSelection is the same for the project list.
func TestProjectDetailFollowsTheSelection(t *testing.T) {
	a, sc, _ := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Edge router")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")

	typeRunes(sc, "j")
	waitFor(t, a, sc, "Invoicing service")
	if strings.Contains(a.screenText(sc), "Edge router") {
		t.Error("the detail column still shows the previous project")
	}
}

// TestClosedDetailAsksForNothing: no column, no requests.
func TestClosedDetailAsksForNothing(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	before := srv.requests.Load()
	typeRunes(sc, "jkjk")
	time.Sleep(3 * detailDebounce)
	if got := srv.requests.Load(); got != before {
		t.Errorf("%d request(s) fired with the detail column closed", got-before)
	}
}

// TestDetailDebouncesRapidMovement: holding j must not fire a request per row.
func TestDetailDebouncesRapidMovement(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "NORMAL")

	before := srv.mrDetail.Load()
	typeRunes(sc, "jkjkjkj") // seven moves, well inside the debounce window
	time.Sleep(3 * detailDebounce)

	loads := srv.mrDetail.Load() - before
	if loads == 0 {
		t.Fatal("the detail column never caught up with the cursor")
	}
	if loads > 1 {
		t.Errorf("%d detail loads for one burst of movement, want 1", loads)
	}
}

// TestDetailStopsFollowingWhenClosed: closing the column silences it again.
func TestDetailStopsFollowingWhenClosed(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Jane Doe")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // to the list
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // close the column
	waitGone(t, a, sc, "Jane Doe")

	before := srv.requests.Load()
	typeRunes(sc, "jk")
	time.Sleep(3 * detailDebounce)
	if got := srv.requests.Load(); got != before {
		t.Errorf("%d request(s) fired after closing the detail column", got-before)
	}
}
