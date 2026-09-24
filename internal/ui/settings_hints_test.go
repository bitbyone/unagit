package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestSettingsHintsStayInsidePanels(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	for _, section := range []int{sectionGeneral, sectionGitLab, sectionGitHub, sectionGroups, sectionSecurity, sectionIntegrations} {
		a.tv.QueueUpdateDraw(func() { a.settings.selectSection(section) })
		onLoop(a, func() bool {
			if !strings.Contains(a.helpHint.GetText(true), "? help") {
				t.Error("Settings is missing the shared bottom help hint")
			}
			return true
		})
		if count := strings.Count(a.screenText(sc), "? help"); count != 1 {
			t.Errorf("section %d: help appears %d times, want once", section, count)
		}
		cells, width, _ := onLoopCells(a, sc)
		panels := onLoop(a, func() []*tview.Box {
			var content *tview.Box
			switch section {
			case sectionGeneral:
				content = a.settings.general.Box
			case sectionGitLab:
				content = a.settings.gitlab.Box
			case sectionGitHub:
				content = a.settings.github.Box
			case sectionGroups:
				content = a.settings.tree.Box
			case sectionSecurity:
				content = a.settings.security.Box
			case sectionIntegrations:
				content = a.settings.integrations.Box
			}
			return []*tview.Box{a.settings.list.Box, content}
		})
		for i, panel := range panels {
			rect := onLoop(a, func() [4]int { x, y, w, h := panel.GetRect(); return [4]int{x, y, w, h} })
			x, y, w, h := rect[0], rect[1], rect[2], rect[3]
			var line strings.Builder
			for col := x + 1; col < x+w-1; col++ {
				cell := cells[(y+h-2)*width+col]
				if len(cell.Runes) > 0 {
					line.WriteRune(cell.Runes[0])
				}
				if len(cell.Runes) > 0 && cell.Runes[0] != ' ' {
					fg, _, _ := cell.Style.Decompose()
					if fg != colDim {
						t.Errorf("section %d panel %d: hint is not dim", section, i)
					}
				}
			}
			if i == 0 {
				if strings.Contains(line.String(), "? help") {
					t.Error("sidebar duplicates the global help hint")
				}
				continue
			}
			want := "Esc back"
			switch section {
			case sectionGitLab, sectionGitHub:
				want = "d remove"
			case sectionGroups:
				want = "m refresh merge requests"
			}
			if !strings.Contains(line.String(), want) {
				t.Errorf("section %d panel %d: bottom interior row %q lacks %q", section, i, line.String(), want)
			}
		}
	}
	a.tv.QueueUpdateDraw(func() { a.note("settings saved") })
	waitFor(t, a, sc, "settings saved")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "? help")
}
