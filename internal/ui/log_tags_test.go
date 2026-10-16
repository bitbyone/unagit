package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/watch"
)

// openTagForm opens the commit log of the clone, the cursor on the older of
// its two commits, and t over it; it answers that commit.
func openTagForm(t *testing.T, a *App, sc tcell.SimulationScreen, p *realProject) string {
	t.Helper()
	first := gitIn(t, p.clone, "rev-parse", "HEAD")
	commitIn(t, p.clone, "b.txt", "Count requests per client")
	p.rescan()
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	typeRunes(sc, "jt")
	waitFor(t, a, sc, "New Tag at "+first[:8])
	return first
}

// TestTagsFromTheCommitLog: t puts a tag on a commit - annotated with a
// message, pushed unless told not to - and never moves one that exists; T
// lists a commit's tags, pushes one origin lacks and deletes one here or
// everywhere, each deletion written in the Activity log.
func TestTagsFromTheCommitLog(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	first := openTagForm(t, a, sc, p)
	assertLegible(t, a, sc, "the new tag form")
	if !strings.Contains(a.screenText(sc), "[x] ") {
		t.Errorf("Push to origin is not ticked to begin with:\n%s", a.screenText(sc))
	}

	// An annotated tag, pushed.
	typeRunes(sc, "v0.1")
	form := frontForm(a)
	onLoop(a, func() bool {
		form.GetFormItemByLabel(labelTagMessage).(*tview.TextArea).SetText("The first release", false)
		return true
	})
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "tagged "+first[:8]+" v0.1, pushed")
	if got := gitIn(t, p.origin, "rev-parse", "v0.1^{commit}"); got != first {
		t.Errorf("origin has v0.1 on %s, want %s", got, first)
	}
	if got := gitIn(t, p.clone, "cat-file", "-t", "v0.1"); got != "tag" {
		t.Errorf("v0.1 is a %s, not an annotated tag", got)
	}
	waitFor(t, a, sc, glyphTag+" v0.1") // the log, read again, shows it

	// The same name again is refused, and the tag stays where it was.
	typeRunes(sc, "t")
	waitFor(t, a, sc, "New Tag at "+first[:8])
	typeRunes(sc, "v0.1")
	pressButton(t, a, sc, frontForm(a), "Create")
	waitFor(t, a, sc, "v0.1 already exists")
	closeMessage(t, a, sc)
	waitFor(t, a, sc, "New Tag at "+first[:8]) // the form stays, to be put right
	pressButton(t, a, sc, frontForm(a), "Cancel")
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")

	// A lightweight tag, kept here.
	typeRunes(sc, "t")
	waitFor(t, a, sc, "New Tag at "+first[:8])
	typeRunes(sc, "light")
	form = frontForm(a)
	onLoop(a, func() bool { form.GetFormItemByLabel(labelTagPush).(*tview.Checkbox).SetChecked(false); return true })
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "tagged "+first[:8]+" light")
	if got := gitIn(t, p.clone, "cat-file", "-t", "light"); got != "commit" {
		t.Errorf("light is a %s, not a lightweight tag", got)
	}
	if strings.Contains(gitIn(t, p.origin, "tag", "--list"), "light") {
		t.Error("a tag kept here was pushed")
	}

	// T lists both, and pushes the one origin lacks.
	typeRunes(sc, "T")
	waitFor(t, a, sc, "Tags of "+first[:8])
	waitFor(t, a, sc, "local only")
	text := a.screenText(sc)
	squeezed := strings.Join(strings.Fields(text), " ")
	for _, want := range []string{"light lightweight local only", "v0.1 annotated local · origin", "light is only a name for " + first[:8]} {
		if !strings.Contains(squeezed, want) {
			t.Errorf("%q is not listed:\n%s", want, text)
		}
	}
	assertLegible(t, a, sc, "the tags of a commit")
	typeRunes(sc, "p")
	waitFor(t, a, sc, "pushed light")
	if !strings.Contains(gitIn(t, p.origin, "tag", "--list"), "light") {
		t.Error("light was not pushed")
	}

	// Deleted here, origin keeps it.
	typeRunes(sc, "T")
	waitFor(t, a, sc, "Tags of "+first[:8])
	waitTrue(t, "light is not listed as on origin", func() bool {
		return strings.Contains(strings.Join(strings.Fields(a.screenText(sc)), " "), "light lightweight local · origin")
	})
	typeRunes(sc, "d")
	waitFor(t, a, sc, "Delete the tag light here?")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "deleted light")
	if strings.Contains(gitIn(t, p.clone, "tag", "--list"), "light") {
		t.Error("light is still here")
	}
	if !strings.Contains(gitIn(t, p.origin, "tag", "--list"), "light") {
		t.Error("deleting here took light from origin")
	}

	// Deleted everywhere.
	typeRunes(sc, "T")
	waitFor(t, a, sc, "Tags of "+first[:8])
	waitFor(t, a, sc, "v0.1")
	typeRunes(sc, "D")
	waitFor(t, a, sc, "Delete the tag v0.1")
	typeRunes(sc, "y")
	waitFor(t, a, sc, "deleted v0.1 on origin and here")
	if out := gitIn(t, p.origin, "tag", "--list"); strings.Contains(out, "v0.1") {
		t.Errorf("origin still has v0.1: %q", out)
	}
	if out := gitIn(t, p.clone, "tag", "--list"); out != "" {
		t.Errorf("tags left here: %q", out)
	}
	waitTrue(t, "the deletions are not in the Activity log", func() bool {
		n := 0
		for _, e := range watch.Open(a.cfg.WatchDir()).AllHistory(50) {
			if e.Heading == "Tag deleted" && strings.Contains(e.Line, "which was on "+first[:8]) {
				n++
			}
		}
		return n == 2
	})

	// A commit with no tag says what to do instead.
	typeRunes(sc, "kT")
	waitFor(t, a, sc, "has no tag - t puts one on it")
}

// TestNewTagFormFitsItsFrame draws the form at several terminal sizes: every
// field inside the frame, the frame's border whole, every label on screen.
func TestNewTagFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			p := newRealProject(t, a, "acme/gateway")
			resizeApp(a, sc, size.w, size.h)
			openTagForm(t, a, sc, p)
			form := frontForm(a)
			type measured struct {
				inner, frame rect
				items        []rect
			}
			m := onLoop(a, func() measured {
				var out measured
				x, y, w, h := form.GetInnerRect()
				out.inner = rect{x, y, w, h}
				x, y, w, h = form.GetRect()
				out.frame = rect{x, y, w, h}
				for i := 0; i < form.GetFormItemCount(); i++ {
					x, y, w, h := form.GetFormItem(i).GetRect()
					out.items = append(out.items, rect{x, y, w, h})
				}
				for i := 0; i < form.GetButtonCount(); i++ {
					x, y, w, h := form.GetButton(i).GetRect()
					out.items = append(out.items, rect{x, y, w, h})
				}
				return out
			})
			inner, frame, items := m.inner, m.frame, m.items
			for _, r := range items {
				if !r.within(inner) {
					t.Errorf("an item is drawn at %v, outside the frame %v:\n%s", r, inner, a.screenText(sc))
				}
			}
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is missing (%q):\n%s", y, r, a.screenText(sc))
					break
				}
			}
			text := a.screenText(sc)
			for _, want := range []string{"Name", "Message", "empty: a lightweight tag", "Push to origin", "Create", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			// The message, empty and not focused, is still drawn as a field:
			// its placeholder once covered it in the form's own background.
			field, surface := onLoop(a, func() rect {
				x, y, w, h := form.GetFormItemByLabel(labelTagMessage).GetRect()
				return rect{x, y, w, h}
			}), onLoop(a, func() tcell.Color { return colSurface })
			for _, at := range [][2]int{{field.x + field.w - 2, field.y}, {field.x + field.w - 2, field.y + field.h - 1}} {
				if _, style := cellAt(a, sc, at[0], at[1]); func() tcell.Color { _, bg, _ := style.Decompose(); return bg }() != surface {
					t.Errorf("the empty message is not drawn as a field at %v:\n%s", at, a.screenText(sc))
				}
			}
			assertLegible(t, a, sc, "the new tag form")
		})
	}
}
