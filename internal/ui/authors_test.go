package ui

import (
	"strings"
	"sync"
	"testing"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/index"
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
	waitFor(t, a, sc, "1 author(s) hidden, shown")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, " ") // on again
	waitGone(t, a, sc, "Rate limiting")
	typeRunes(sc, "j ") // renovate: shown again for good
	waitFor(t, a, sc, "nothing hidden")
	waitFor(t, a, sc, "Rate limiting")

	saved, err := config.LoadFrom(a.cfg.Dir())
	must(t, err)
	if len(saved.Filters.HiddenAuthors) != 0 || strings.Contains(a.screenText(sc), glyphHidden+" 1 author") {
		t.Errorf("renovate is still hidden: %+v", saved.Filters.HiddenAuthors)
	}
}

// TestAuthorsByName: a refresh learns the names the list did not come with,
// shows them in the list and the detail, keeps them in index-users.json,
// and does not ask for them again at the next refresh.
func TestAuthorsByName(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Invoice rounding")
	typeRunes(sc, "R")
	waitFor(t, a, sc, "Bob Ross")
	if n := srv.namesAsked.Load(); n != 2 {
		t.Errorf("asked for %d name(s), want bob's and jane's", n)
	}
	if got := onLoop(a, func() string { return a.people.Name(a.cfg.Instances[0].ID, "bob") }); got != "Bob Ross" {
		t.Errorf("bob is %q", got)
	}
	people, err := index.Load[index.Users](a.cfg.IndexPath("users"))
	must(t, err)
	if people.Name(a.cfg.Instances[0].ID, "bob") != "Bob Ross" {
		t.Errorf("the name was not kept: %+v", people)
	}
	// jane gave no name and is still jane.
	waitFor(t, a, sc, "jane")

	hold := make(chan struct{})
	srv.holdMRList.Store(hold)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(hold) }) })
	typeRunes(sc, "R")
	waitFor(t, a, sc, "refreshing merge requests")
	release.Do(func() { close(hold) })
	waitGone(t, a, sc, "refreshing merge requests")
	if n := srv.namesAsked.Load(); n != 2 {
		t.Errorf("the second refresh asked for names again: %d in all", n)
	}
}

// TestHiddenPeopleComeFirst: View Options lists the hidden authors, each
// marked as a person, before the repositories whose merge requests are
// hidden, whichever was hidden first.
func TestHiddenPeopleComeFirst(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.cfg.Filters.ToggleMRsOf(a.cfg.Instances[0].ID, "acme/billing")
		a.cfg.Filters.ToggleAuthor(a.cfg.Instances[0].ID, "renovate")
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "v")
	waitFor(t, a, sc, glyphUser+" renovate")
	text := a.screenText(sc)
	if lineOf(text, glyphUser+" renovate") > lineOf(text, "acme/billing · its merge requests") {
		t.Errorf("the hidden author is not before the repositories:\n%s", text)
	}
	assertLegible(t, a, sc, "hidden people and repositories")
}
