package ui

import (
	"fmt"
	"strings"
	"testing"
)

type rect struct{ x, y, w, h int }

func (r rect) within(o rect) bool {
	return r.x >= o.x && r.y >= o.y && r.x+r.w <= o.x+o.w && r.y+r.h <= o.y+o.h
}

func (r rect) String() string { return fmt.Sprintf("x=%d y=%d w=%d h=%d", r.x, r.y, r.w, r.h) }

// TestMergeRequestFormFitsItsFrame draws the form at several terminal sizes and
// checks every field, checkbox and button lies inside the frame it is drawn in,
// that the labels are not cut, and that the checkboxes can be seen.
func TestMergeRequestFormFitsItsFrame(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {120, 34}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, _ := newTestAppSrv(t)
			waitFor(t, a, sc, "acme/gateway")
			p := newRealProject(t, a, "acme/gateway")
			dir := p.worktree("feature/audit-log")
			commitIn(t, dir, "n.txt", "Add the audit log", "")
			gitIn(t, dir, "push", "-q", "-u", "origin", "feature/audit-log")
			p.rescan()
			resize(sc, size.w, size.h)
			typeRunes(sc, "W")
			waitFor(t, a, sc, "in sync")
			form := openForm(t, a, sc)
			waitFor(t, a, sc, "Description")

			type measured struct {
				inner   rect
				fields  map[string]rect
				buttons map[string]rect
			}
			m := onLoop(a, func() measured {
				var out measured
				x, y, w, h := form.GetInnerRect()
				out.inner = rect{x, y, w, h}
				out.fields, out.buttons = map[string]rect{}, map[string]rect{}
				for i := 0; i < form.GetFormItemCount(); i++ {
					item := form.GetFormItem(i)
					x, y, w, h := item.GetRect()
					out.fields[item.GetLabel()] = rect{x, y, w, h}
				}
				for i := 0; i < form.GetButtonCount(); i++ {
					b := form.GetButton(i)
					x, y, w, h := b.GetRect()
					out.buttons[b.GetLabel()] = rect{x, y, w, h}
				}
				return out
			})
			if m.inner.w == 0 {
				t.Fatalf("the form has no room:\n%s", a.screenText(sc))
			}
			for label, r := range m.fields {
				if !r.within(m.inner) {
					t.Errorf("%q is drawn at %v, outside the frame %v\n%s", label, r, m.inner, a.screenText(sc))
				}
			}
			for label, r := range m.buttons {
				if !r.within(m.inner) {
					t.Errorf("button %q is drawn at %v, outside the frame %v\n%s", label, r, m.inner, a.screenText(sc))
				}
			}
			// A field wider than the room it has is drawn over the frame's own edge,
			// whatever rectangle tview gave it, so the frame itself is the check:
			// its right border must be there on every row.
			frame := onLoop(a, func() rect {
				x, y, w, h := form.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is missing (%q), something is drawn over it:\n%s",
						y, r, a.screenText(sc))
					break
				}
			}
			text := a.screenText(sc)
			for _, want := range []string{"Title", "Target branch", "Description", "Draft",
				"Delete source branch", "Squash commits", "Create", "Cancel", "Alt-r create"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			// An unticked box has to be visible as one.
			if strings.Count(text, "[ ]") < 3 {
				t.Errorf("the checkboxes cannot be seen:\n%s", text)
			}
		})
	}
}
