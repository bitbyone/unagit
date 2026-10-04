package ui

import (
	"strings"
	"testing"

	"github.com/tobola/unagit/internal/config"
)

// TestHideAnAuthorsMergeRequests: H keeps the author's merge requests out of
// the list and the header counts the author; View Options turns the filter
// off for a while and shows the author again for good, one by one.
func TestHideAnAuthorsMergeRequests(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			a.mrs[i].Author.Username = map[int]string{7: "renovate", 8: "renovate", 9: "jane"}[a.mrs[i].IID]
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g") // !7, renovate's
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "H")
	waitGone(t, a, sc, "Rate limiting")
	waitGone(t, a, sc, "Drop the old client")
	waitFor(t, a, sc, "Invoice rounding")
	waitFor(t, a, sc, glyphHidden+" 1 author(s)")

	typeRunes(sc, "v")
	waitFor(t, a, sc, "View · Merge requests")
	waitFor(t, a, sc, "renovate")
	assertLegible(t, a, sc, "the merge request view options")
	typeRunes(sc, "jjjjjj ") // hide the authors below: off
	waitFor(t, a, sc, "1 hidden author(s) shown")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, " ") // on again
	waitGone(t, a, sc, "Rate limiting")
	typeRunes(sc, "j ") // renovate: shown again for good
	waitFor(t, a, sc, "no author hidden")
	waitFor(t, a, sc, "Rate limiting")

	saved, err := config.LoadFrom(a.cfg.Dir())
	must(t, err)
	if len(saved.Filters.HiddenAuthors) != 0 || strings.Contains(a.screenText(sc), glyphHidden+" 1 author") {
		t.Errorf("renovate is still hidden: %+v", saved.Filters.HiddenAuthors)
	}
}
