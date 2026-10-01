package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
)

// openTagSettings opens Settings › Tags with the keyboard in the table.
func openTagSettings(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	changeOnLoop(a, func() {
		a.settings.selectSection(sectionTags)
		a.settings.focusContent()
	})
	waitFor(t, a, sc, "REPOSITORIES")
}

// TestTagsOnRepositories: Ctrl-T puts tags on a repository, one toggle at a
// time, and they are drawn as pills after its name; f narrows the list to the
// tags chosen, any of them, and F shows every repository again.
func TestTagsOnRepositories(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	inst := onLoop(a, func() string { return a.projects[0].Instance })

	sc.InjectKey(tcell.KeyCtrlT, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Tags of acme/gateway")
	typeRunes(sc, " jj ") // oss, then work
	waitFor(t, a, sc, "2 on")
	assertLegible(t, a, sc, "the tag picker")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Tags of acme/gateway")

	line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), "acme/gateway")]
	gateway, oss, work := strings.Index(line, "acme/gateway"), strings.Index(line, "oss"), strings.Index(line, "work")
	if !(gateway >= 0 && gateway < oss && oss < work) || !strings.Contains(line, "oss") {
		t.Fatalf("the tags are not pills after the name: %q", line)
	}
	// The pills keep their colours on the selected row as well as off it:
	// the band must not turn them into plain text between two ends.
	x := len([]rune(line[:strings.Index(line, "oss")]))
	for _, moved := range []bool{false, true} {
		if moved {
			typeRunes(sc, "j")
			waitFor(t, a, sc, "acme/gateway")
		}
		_, style := cellAt(a, sc, x, lineOf(a.screenText(sc), "acme/gateway"))
		fg, bg, _ := style.Decompose()
		mint := tagColourOf("mint")
		if bg != tcell.GetColor(mint.fill) || fg != tcell.GetColor(mint.ink) {
			t.Errorf("selected %v: oss is drawn %v on %v, not in its own colours", !moved, fg, bg)
		}
	}
	assertLegible(t, a, sc, "tagged repositories")
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(saved.TagsOf(inst, "acme/gateway"), " "); got != "oss work" {
		t.Errorf("saved tags = %q", got)
	}

	typeRunes(sc, "f")
	waitFor(t, a, sc, "Show the repositories tagged")
	typeRunes(sc, "jj ")
	waitFor(t, a, sc, "any of 1")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "acme/billing")
	if !strings.Contains(a.screenText(sc), "1/2 repositories") {
		t.Errorf("the header does not count what the tags left:\n%s", a.screenText(sc))
	}
	typeRunes(sc, "F")
	waitFor(t, a, sc, "acme/billing")
}

// TestTagSettings: the default tags are there, a new one is made with a
// colour, a renamed one stays on its repositories, a removed one leaves them,
// and s changes how the pills end.
func TestTagSettings(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	inst := onLoop(a, func() string { return a.projects[0].Instance })
	changeOnLoop(a, func() { a.cfg.ToggleTag(inst, "acme/gateway", "oss") })
	openTagSettings(t, a, sc)
	for _, name := range []string{"oss", "personal", "work", "private"} {
		waitFor(t, a, sc, name)
	}
	assertLegible(t, a, sc, "the tag settings")

	typeRunes(sc, "a")
	waitFor(t, a, sc, "New tag")
	form := currentForm(a)
	setField(t, a, form, 0, "infra")
	changeOnLoop(a, func() { form.GetFormItem(1).(*tview.DropDown).SetCurrentOption(paletteIndex("peach")) })
	// j and k move in the open colour list, as in every other list.
	sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFocus(t, a, func() bool { return form.GetFormItem(1).(*tview.DropDown).IsOpen() })
	typeRunes(sc, "jjk")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFocus(t, a, func() bool { return !form.GetFormItem(1).(*tview.DropDown).IsOpen() })
	if got := onLoop(a, func() int { i, _ := form.GetFormItem(1).(*tview.DropDown).GetCurrentOption(); return i }); got != paletteIndex("peach")+1 {
		t.Errorf("after j j k the colour is %d, want %d", got, paletteIndex("peach")+1)
	}
	changeOnLoop(a, func() { form.GetFormItem(1).(*tview.DropDown).SetCurrentOption(paletteIndex("peach")) })
	pressButton(t, a, sc, form, "Save")
	waitFor(t, a, sc, "infra")
	if tg, ok := onLoop(a, func() config.Tag { tg, _ := a.cfg.Tag("infra"); return tg }), true; !ok || tg.Color != "peach" {
		t.Errorf("infra = %+v", tg)
	}

	// Rename oss, the first row.
	typeRunes(sc, "g")
	typeRunes(sc, "e")
	waitFor(t, a, sc, "Tag · oss")
	form = currentForm(a)
	setField(t, a, form, 0, "open")
	pressButton(t, a, sc, form, "Save")
	waitFor(t, a, sc, "open")
	if got := onLoop(a, func() string { return strings.Join(a.cfg.TagsOf(inst, "acme/gateway"), " ") }); got != "open" {
		t.Errorf("after the rename the repository wears %q", got)
	}

	typeRunes(sc, "d")
	waitFor(t, a, sc, "1 repository wears it")
	typeRunes(sc, "y")
	waitGone(t, a, sc, "1 repository wears it")
	if got := onLoop(a, func() int { return len(a.cfg.TagsOf(inst, "acme/gateway")) }); got != 0 {
		t.Errorf("the removed tag is still worn")
	}

	typeRunes(sc, "s")
	waitFor(t, a, sc, "ends: half circles")
	waitFor(t, a, sc, "◖personal◗")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "ends: square")
	waitFor(t, a, sc, " personal ")
}

// TestTagFormFitsItsFrame draws the tag form at several sizes: every field and
// button inside the frame, the frame whole, the labels on screen, and the
// colour list legible once it is open.
func TestTagFormFitsItsFrame(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}, {64, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, size.w, size.h)
			openTagSettings(t, a, sc)
			typeRunes(sc, "e")
			waitFor(t, a, sc, "Tag · oss")
			form := currentForm(a)

			type measured struct {
				inner, frame rect
				items        map[string]rect
			}
			m := onLoop(a, func() measured {
				out := measured{items: map[string]rect{}}
				x, y, w, h := form.GetInnerRect()
				out.inner = rect{x, y, w, h}
				x, y, w, h = form.GetRect()
				out.frame = rect{x, y, w, h}
				for i := 0; i < form.GetFormItemCount(); i++ {
					x, y, w, h := form.GetFormItem(i).GetRect()
					out.items[form.GetFormItem(i).GetLabel()] = rect{x, y, w, h}
				}
				for i := 0; i < form.GetButtonCount(); i++ {
					x, y, w, h := form.GetButton(i).GetRect()
					out.items[form.GetButton(i).GetLabel()] = rect{x, y, w, h}
				}
				return out
			})
			for label, r := range m.items {
				if !r.within(m.inner) {
					t.Errorf("%q is drawn at %v, outside the frame %v\n%s", label, r, m.inner, a.screenText(sc))
				}
			}
			for y := m.frame.y + 1; y < m.frame.y+m.frame.h-1; y++ {
				if r, _ := cellAt(a, sc, m.frame.x+m.frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is missing (%q):\n%s", y, r, a.screenText(sc))
					break
				}
			}
			text := a.screenText(sc)
			for _, want := range []string{"Name", "Colour", "mint", "Save", "Cancel", "Alt-s save"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			assertLegible(t, a, sc, "the tag form")

			// The colour list, open.
			sc.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
			sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
			waitFocus(t, a, func() bool { return form.GetFormItem(1).(*tview.DropDown).IsOpen() })
			assertLegible(t, a, sc, "the colour list")
		})
	}
}

// TestGroupTagsReachTheRepositories: t in Settings › Groups & roots tags a
// group, its repositories wear the tag, and Ctrl-T on one of them says where
// the tag comes from and can take it off there alone.
func TestGroupTagsReachTheRepositories(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	changeOnLoop(a, func() {
		a.settings.selectSection(sectionGroups)
		a.settings.focusContent()
	})
	waitFor(t, a, sc, "incl. subgroups")
	typeRunes(sc, "j") // from the server to the group
	typeRunes(sc, "t")
	waitFor(t, a, sc, "Tags of acme")
	typeRunes(sc, "jj ") // work
	waitFor(t, a, sc, "1 on")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Tags of acme")
	if line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), "incl. subgroups")]; !strings.Contains(line, "work") {
		t.Errorf("the group does not show its tag: %q", line)
	}

	typeRunes(sc, "R")
	waitFor(t, a, sc, "acme/billing")
	for _, repo := range []string{"acme/gateway", "acme/billing"} {
		if line := strings.Split(a.screenText(sc), "\n")[lineOf(a.screenText(sc), repo)]; !strings.Contains(line, "work") {
			t.Errorf("%s does not wear the group's tag: %q", repo, line)
		}
	}

	sc.InjectKey(tcell.KeyCtrlT, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "from a group")
	typeRunes(sc, "jj ")
	waitFor(t, a, sc, "taken off here")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Tags of")
	inst := onLoop(a, func() string { return a.projects[0].Instance })
	if got := onLoop(a, func() string { return strings.Join(a.cfg.TagsOf(inst, "acme/gateway"), " ") }); got != "" {
		t.Errorf("gateway still wears %q", got)
	}
	if got := onLoop(a, func() string { return strings.Join(a.cfg.TagsOf(inst, "acme/billing"), " ") }); got != "work" {
		t.Errorf("billing wears %q, the group's tag should stay on it", got)
	}
}

// TestViewOptionsHideTheTags: Alt-V switches the tags out of the list and back;
// they go on filtering.
func TestViewOptionsHideTheTags(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	inst := onLoop(a, func() string { return a.projects[0].Instance })
	changeOnLoop(a, func() {
		a.cfg.ToggleTag(inst, "acme/gateway", "oss")
		a.projectsPane.reload()
	})
	waitFor(t, a, sc, "\ue0b6oss\ue0b4")

	sc.InjectKey(tcell.KeyRune, 'v', tcell.ModAlt)
	waitFor(t, a, sc, "View · Repositories")
	typeRunes(sc, " ")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "View · Repositories")
	if strings.Contains(a.screenText(sc), "oss") {
		t.Errorf("the tags are still shown:\n%s", a.screenText(sc))
	}
	changeOnLoop(a, func() {
		a.cfg.Filters.ToggleTagFilter("oss")
		a.applyFilters()
	})
	waitGone(t, a, sc, "acme/billing")

	sc.InjectKey(tcell.KeyRune, 'v', tcell.ModAlt)
	waitFor(t, a, sc, "View · Repositories")
	typeRunes(sc, " ")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "\ue0b6oss\ue0b4")
}
