package ui

import (
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

// Serial: the theme's glyphs belong to the process.
func TestPrivateRepositoriesKeepTheirMarkBeforeTheForge(t *testing.T) {
	restoreDefaultTheme(t)
	t.Cleanup(func() { nerdFont = false })
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.projects[0].Visibility = forge.VisibilityPrivate
		a.projects[1].Visibility = forge.VisibilityPublic
		for _, visibility := range []string{forge.VisibilityInternal, ""} {
			pr := a.projects[1]
			pr.ID += len(a.projects)
			pr.PathWithNamespace = "acme/" + map[string]string{forge.VisibilityInternal: "internal", "": "unknown"}[visibility]
			pr.Visibility = visibility
			a.projects = append(a.projects, pr)
		}
		a.projectsPane.reload()
	})
	for _, nerd := range []bool{false, true} {
		changeOnLoop(a, func() { nerdFont = nerd; setTheme(theme); a.projectsPane.reload() })
		mark := onLoop(a, func() string { return glyphPrivate })
		forgeIcon := onLoop(a, func() string { return a.forgeIcon(a.projects[0].Instance) })
		if nerd && mark != "\uf023" || !nerd && mark != "p" {
			t.Fatalf("nerd=%v: the private mark is %q", nerd, mark)
		}
		for _, grouped := range []bool{false, true} {
			changeOnLoop(a, func() { a.cfg.Filters.GroupRepositories = grouped; a.projectsPane.reload() })
			for _, width := range []int{120, 80, 60} {
				resizeApp(a, sc, width, 26)
				text := a.screenText(sc)
				prefix := mark + " "
				if forgeIcon != "" {
					prefix += forgeIcon + " "
				}
				if strings.Count(text, " "+mark+" ") != 1 || width >= 80 && !strings.Contains(lineAt(text, "gateway"), prefix) {
					t.Errorf("nerd=%v grouped=%v width=%d: no private mark before the forge:\n%s", nerd, grouped, width, text)
				}
				for _, name := range []string{"billing", "internal", "unknown"} {
					if width < 80 {
						continue
					}
					if line := lineAt(text, name); line == "" || strings.Contains(line, " "+mark+" ") {
						t.Errorf("%s has the wrong visibility mark: %q", name, line)
					}
				}
				assertLegible(t, a, sc, "the private repository mark")
				if nerd && !grouped && width == 80 {
					t.Logf("repositories at 80x26:\n%s", text)
				}
			}
		}
	}
	changeOnLoop(a, func() {
		a.projects[0].PathWithNamespace = "acme/this-is-a-very-long-group-name/this-is-a-very-long-private-repository-name"
		a.cfg.Filters.GroupRepositories = false
		a.projectsPane.reload()
	})
	text := a.screenText(sc)
	mark := onLoop(a, func() string { return glyphPrivate })
	if !strings.Contains(text, mark) || strings.Contains(text, "this-is-a-very-long-private-repository-name") {
		t.Errorf("the shortened name lost its mark:\n%s", text)
	}
	resizeApp(a, sc, 120, 34)
	typeRunes(sc, "?")
	waitFor(t, a, sc, "private: visible")
	assertLegible(t, a, sc, "the repository help")
}
