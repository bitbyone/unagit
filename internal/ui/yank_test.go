package ui

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// clipboard stands in for the system one for the length of a test.
type clipboard struct {
	mu   sync.Mutex
	text string
}

func (c *clipboard) get() string { c.mu.Lock(); defer c.mu.Unlock(); return c.text }

func fakeClipboard(t *testing.T, works bool) *clipboard {
	t.Helper()
	c := &clipboard{}
	saved := copyToClipboard
	copyToClipboard = func(text string) error {
		if !works {
			return errors.New("no clipboard program found")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.text = text
		return nil
	}
	t.Cleanup(func() { copyToClipboard = saved })
	return c
}

// TestYankCopiesTheLinkFirst: yy is the link, and the menu names the other
// things there are to copy.
func TestYankCopiesTheLinkFirst(t *testing.T) {
	c := fakeClipboard(t, true)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	onLoop(a, func() bool { a.mrs[0].WebURL = "https://gl.example/acme/gateway/-/merge_requests/7"; return true })

	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy acme/gateway!7")
	text := a.screenText(sc)
	for _, want := range []string{"Link", "Reference", "acme/gateway!7", "Source branch", "feat/rate", "Markdown link"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not offered:\n%s", want, text)
		}
	}
	// The menu opens on the list with the link under the cursor: yy.
	typeRunes(sc, "y")
	waitFor(t, a, sc, "copied link")
	if got := c.get(); got != "https://gl.example/acme/gateway/-/merge_requests/7" {
		t.Errorf("clipboard = %q", got)
	}

	// j moves to the next one, and y takes it: a line for a chat, then the
	// reference.
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy acme/gateway!7")
	typeRunes(sc, "jy")
	waitFor(t, a, sc, "copied link with text")
	if got := c.get(); got != "acme/gateway · !7 · Rate limiting · feat/rate https://gl.example/acme/gateway/-/merge_requests/7" {
		t.Errorf("clipboard = %q, want the link with text", got)
	}
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy acme/gateway!7")
	typeRunes(sc, "jjy")
	waitFor(t, a, sc, "copied reference")
	if got := c.get(); got != "acme/gateway!7" {
		t.Errorf("clipboard = %q, want the reference", got)
	}

	// / still filters.
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy acme/gateway!7")
	typeRunes(sc, "/source")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "copied source branch")
	if got := c.get(); got != "feat/rate" {
		t.Errorf("clipboard = %q, want the source branch", got)
	}
}

// TestYankFallsBackToTheTerminal: with no clipboard program, as over ssh, the
// terminal is asked to set it.
func TestYankFallsBackToTheTerminal(t *testing.T) {
	fakeClipboard(t, false)
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Copy acme/gateway")
	typeRunes(sc, "/path")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "terminal's clipboard")
	if got := onLoop(a, func() string { return string(sc.GetClipboardData()) }); got != "acme/gateway" {
		t.Errorf("terminal clipboard = %q, want the repository path", got)
	}
}
