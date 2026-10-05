package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// TestTheStarredList: starred repositories are listed with their language
// and stars, Enter reads a README drawn from its markdown, Esc comes back;
// one kept from the stars joins Repositories with its badge, and stays
// there across a start.
func TestTheStarredList(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	star := forge.Project{ID: 1, PathWithNamespace: "acme/gateway", Name: "gateway", Language: "Go", Stars: 42,
		Starred: true, Description: "Edge router", WebURL: "https://gl.test/acme/gateway"}
	far := forge.Project{ID: 77, PathWithNamespace: "rivo/tview", Name: "tview", Language: "Go", Stars: 11000,
		Starred: true, Description: "Terminal UI library"}
	changeOnLoop(a, func() {
		star.Instance, far.Instance = a.cfg.Instances[0].ID, a.cfg.Instances[0].ID
		a.listStarred([]forge.Project{star, far}, 0)
	})
	waitFor(t, a, sc, "Starred repositories · 2")
	waitFor(t, a, sc, "rivo/tview")
	waitFor(t, a, sc, glyphStarred+" 11000")
	assertLegible(t, a, sc, "the starred repositories")

	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "README · acme/gateway")
	waitFor(t, a, sc, "The edge router.")
	if strings.Contains(a.screenText(sc), "**") {
		t.Error("the README is not drawn from its markdown")
	}
	assertLegible(t, a, sc, "a README")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Starred repositories · 2")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Starred repositories")

	changeOnLoop(a, func() {
		a.keepStarred(far)
		a.projectsPane.reload()
	})
	waitFor(t, a, sc, "rivo/tview")
	waitFor(t, a, sc, glyphStarred+" GitLab")
	assertLegible(t, a, sc, "a starred repository in the list")
	kept, err := index.Load[[]forge.Project](a.cfg.IndexPath("starred"))
	must(t, err)
	if len(kept) != 1 || kept[0].PathWithNamespace != "rivo/tview" {
		t.Errorf("index-starred.json holds %+v", kept)
	}
}
