package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
)

// chooseOption picks the option of a form's select that reads label, as a
// choice there would, so what follows from it happens too.
func chooseOption(t *testing.T, a *App, form *tview.Form, field, label string) {
	t.Helper()
	ok := onLoop(a, func() bool {
		drop := form.GetFormItemByLabel(field).(*tview.DropDown)
		for i := 0; i < drop.GetOptionCount(); i++ {
			if _, text := drop.SetCurrentOption(i).GetCurrentOption(); strings.TrimSpace(text) == label {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("%s has no option %q", field, label)
	}
}

func chosen(a *App, form *tview.Form, field string) string {
	return onLoop(a, func() string {
		_, text := form.GetFormItemByLabel(field).(*tview.DropDown).GetCurrentOption()
		return strings.TrimSpace(text)
	})
}

// TestOpenAsksWithWhatAndWhere: O on a repository asks with what and where,
// offers the places the tool can go, opens there, and opens filled in the
// same way the next time.
func TestOpenAsksWithWhatAndWhere(t *testing.T) {
	t.Parallel()
	tool, prepareMux := fakeMux(t)
	_, prepareAgents := fakeAgents(t, "", "claude")
	a, sc, _ := newTestAppSrv(t, prepareMux, prepareAgents)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")

	typeRunes(sc, "O")
	waitFor(t, a, sc, labelOpenWhere)
	form := currentForm(a)
	chooseOption(t, a, form, labelOpenWith, "Claude Code")
	chooseOption(t, a, form, labelOpenWhere, "Zellij Vertical Split")
	pressButton(t, a, sc, form, "Open")
	waitEditorState(t, a, func() bool {
		return strings.Contains(a.transient, "opened Claude Code in Zellij: "+p.path)
	})
	var split []string
	for _, call := range tool.Calls(t) {
		if call.Args[3] == "new-pane" {
			split = call.Args
		}
	}
	if len(split) == 0 || split[5] != "right" || !strings.HasSuffix(split[len(split)-1], "/claude") {
		t.Fatalf("split: %v", split)
	}
	saved, err := config.LoadFrom(a.cfg.Dir())
	must(t, err)
	if got := saved.State.OpenForm["repository"]; got.With != "agent:claude" || got.Where != "zellij-right" {
		t.Fatalf("kept %+v", got)
	}
	// It is the machine's, not the configuration's.
	if raw, _ := os.ReadFile(a.cfg.Path()); strings.Contains(string(raw), "open_form") || strings.Contains(string(raw), "place_uses") {
		t.Fatalf("config.yaml keeps what using unagit taught it:\n%s", raw)
	}

	typeRunes(sc, "O")
	waitFor(t, a, sc, labelOpenWhere)
	form = currentForm(a)
	if with, where := chosen(a, form, labelOpenWith), chosen(a, form, labelOpenWhere); with != "Claude Code" || where != "Zellij Vertical Split" {
		t.Fatalf("Open… opened on %q in %q", with, where)
	}
}

// TestTheOpenFormFitsItsFrame draws Open… for a merge request - three
// selects - at several sizes: every select inside the frame, the frame's
// border whole, every label on screen, legible with a select open.
func TestTheOpenFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			_, prepareMux := fakeMux(t)
			_, prepareAgents := fakeAgents(t, "", "claude", "codex")
			a, sc, _ := newTestAppSrv(t, prepareMux, prepareAgents)
			waitFor(t, a, sc, "acme/gateway")
			resizeApp(a, sc, size.w, size.h)
			typeRunes(sc, "2")
			waitFor(t, a, sc, "Rate limiting")
			typeRunes(sc, "O")
			waitFor(t, a, sc, labelOpenWhere)
			form := currentForm(a)
			text := a.screenText(sc)
			for _, want := range []string{labelOpenMode, labelOpenWith, labelOpenWhere, "Branch"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			inner, frame := onLoop(a, func() [2]rect {
				x, y, w, h := form.GetInnerRect()
				fx, fy, fw, fh := form.GetRect()
				return [2]rect{{x, y, w, h}, {fx, fy, fw, fh}}
			})[0], onLoop(a, func() rect { x, y, w, h := form.GetRect(); return rect{x, y, w, h} })
			for _, label := range []string{labelOpenMode, labelOpenWith, labelOpenWhere} {
				r := onLoop(a, func() rect {
					x, y, w, h := form.GetFormItemByLabel(label).GetRect()
					return rect{x, y, w, h}
				})
				if !r.within(inner) {
					t.Errorf("%s is drawn at %v, outside the frame %v\n%s", label, r, inner, text)
				}
			}
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Fatalf("border drawn over on row %d:\n%s", y, text)
				}
			}
			assertLegible(t, a, sc, "the open form")
			// The With select open, its list over the form.
			changeOnLoop(a, func() { form.SetFocus(1) })
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			waitFor(t, a, sc, "Codex")
			assertLegible(t, a, sc, "the open form with a select open")
		})
	}
}
