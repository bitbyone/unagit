package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// waitWritten waits until the fake forge was sent a change containing want.
func waitWritten(t *testing.T, srv *fakeServer, want string) string {
	t.Helper()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		for _, w := range srv.written() {
			if strings.Contains(w, want) {
				return w
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the forge was never sent %q; it was sent %q", want, srv.written())
	return ""
}

// waitRowFetched waits until !7's row has been asked about again after an
// action: the fixture's pipeline for it failed, which the list did not know.
// Ending the test sooner would leave the answer writing into a directory
// already being removed.
func waitRowFetched(t *testing.T, a *App, sc tcell.SimulationScreen) {
	t.Helper()
	deadline := time.Now().Add(patience)
	failed := func() bool {
		return onLoop(a, func() bool {
			for _, mr := range a.mrs {
				if mr.IID == 7 {
					return mr.Pipeline == "failed"
				}
			}
			return false
		})
	}
	for !failed() {
		if time.Now().After(deadline) {
			t.Fatalf("the row was never asked about again:\n%s", a.screenText(sc))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// openMergeForm opens M on !7, the first merge request of the list.
func openMergeForm(t *testing.T, a *App, sc tcell.SimulationScreen) *tview.Form {
	t.Helper()
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Merge acme/gateway !7")
	return currentForm(a)
}

// TestMergeAsksHowAndMerges: M opens the form, Merge sends what was chosen,
// with the head the list knows so a later push is not merged unseen.
func TestMergeAsksHowAndMerges(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	form := openMergeForm(t, a, sc)
	waitFor(t, a, sc, "feat/rate → main")
	assertLegible(t, a, sc, "the merge form")
	if len(srv.written()) != 0 {
		t.Fatal("something was sent before Merge was pressed")
	}

	pressButton(t, a, sc, form, "Merge")
	waitFor(t, a, sc, "Merged acme/gateway !7 into main")
	sent := waitWritten(t, srv, "/merge_requests/7/merge")
	for _, want := range []string{`"should_remove_source_branch":true`, `"squash":false`, `"merge_when_pipeline_succeeds":false`} {
		if !strings.Contains(sent, want) {
			t.Errorf("the merge sent %s, without %s", sent, want)
		}
	}
	waitRowFetched(t, a, sc)
}

// TestMergeWaitsForARunningPipelineWhenAsked: with the pipeline still going,
// the form offers to leave the merge to it, and does so by default; what
// stands in the way is listed first.
func TestMergeWaitsForARunningPipelineWhenAsked(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	onLoop(a, func() bool {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].Pipeline, a.mrs[i].Draft = "running", true
				a.mrs[i].ApprovalsRequired, a.mrs[i].ApprovedBy = 2, []string{"john"}
			}
		}
		a.mrsPane.reload()
		return true
	})
	form := openMergeForm(t, a, sc)
	for _, want := range []string{labelWhenPipeline, "it is a draft", "its pipeline is still running", "approved by 1 of the 2 needed"} {
		waitFor(t, a, sc, want)
	}
	pressButton(t, a, sc, form, "Merge")
	waitFor(t, a, sc, "merges when its pipeline succeeds")
	if sent := waitWritten(t, srv, "/merge"); !strings.Contains(sent, `"merge_when_pipeline_succeeds":true`) {
		t.Errorf("the merge did not wait for the pipeline: %s", sent)
	}
	waitRowFetched(t, a, sc)
}

// TestTheMergeFormFitsItsFrame draws the form, with every warning, at sizes
// down to a small terminal.
func TestTheMergeFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			onLoop(a, func() bool {
				for i := range a.mrs {
					if a.mrs[i].IID == 7 {
						a.mrs[i].Pipeline, a.mrs[i].Draft = "running", true
						a.mrs[i].Unresolved, a.mrs[i].UnresolvedKnown = 3, true
					}
				}
				a.mrsPane.reload()
				return true
			})
			resizeApp(a, sc, size.w, size.h)
			form := openMergeForm(t, a, sc)
			waitFor(t, a, sc, "3 thread(s) not resolved")
			assertFormInFrame(t, a, sc, form)
			text := a.screenText(sc)
			for _, want := range []string{"Rate limiting", labelSquash, labelDeleteBranch, labelWhenPipeline, "Merge", "Cancel", "m merge"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
		})
	}
}

// assertFormInFrame checks that every item and button of a form lies inside
// its frame and that nothing is drawn over the frame's right border.
func assertFormInFrame(t *testing.T, a *App, sc tcell.SimulationScreen, form *tview.Form) {
	t.Helper()
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
			item := form.GetFormItem(i)
			x, y, w, h := item.GetRect()
			out.items[fmt.Sprintf("item %d %q", i, item.GetLabel())] = rect{x, y, w, h}
		}
		for i := 0; i < form.GetButtonCount(); i++ {
			b := form.GetButton(i)
			x, y, w, h := b.GetRect()
			out.items["button "+buttonName(b.GetLabel())] = rect{x, y, w, h}
		}
		return out
	})
	if m.inner.w == 0 {
		t.Fatalf("the form has no room:\n%s", a.screenText(sc))
	}
	for name, r := range m.items {
		if !r.within(m.inner) {
			t.Errorf("%s is drawn at %v, outside the frame %v\n%s", name, r, m.inner, a.screenText(sc))
		}
	}
	for y := m.frame.y + 1; y < m.frame.y+m.frame.h-1; y++ {
		if r, _ := cellAt(a, sc, m.frame.x+m.frame.w-1, y); r != '│' {
			t.Errorf("row %d: the frame's right border is missing (%q):\n%s", y, r, a.screenText(sc))
			break
		}
	}
}

// TestCtrlDMarksADraftAndBack: the title is what GitLab keeps it in.
func TestCtrlDMarksADraftAndBack(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlD, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "acme/gateway !7 is a draft")
	waitWritten(t, srv, `"title":"Draft: Rate limiting"`)
	waitRowFetched(t, a, sc)
}

// TestReviewersAreChosenAndSavedOnEsc: a lists the members, space asks one,
// and nothing is sent until the list closes.
func TestReviewersAreChosenAndSavedOnEsc(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	typeRunes(sc, "s")
	waitFor(t, a, sc, "Reviewers · acme/gateway !7")
	waitFor(t, a, sc, "Mike Moe")
	assertLegible(t, a, sc, "the reviewers")
	typeRunes(sc, "/mike")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "1 asked")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // the filter
	if len(srv.written()) != 0 {
		t.Fatalf("sent before the list closed: %q", srv.written())
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Reviewers of acme/gateway !7: mike")
	waitWritten(t, srv, `"reviewer_ids":[13]`)
	waitRowFetched(t, a, sc)
}

// TestAssigneesAreChosenAndSavedOnEsc: s lists the members as a does,
// space assigns one, and nothing is sent until the list closes.
func TestAssigneesAreChosenAndSavedOnEsc(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "ga")
	waitFor(t, a, sc, "Assignees · acme/gateway !7")
	waitFor(t, a, sc, "Mike Moe")
	assertLegible(t, a, sc, "the assignees")
	typeRunes(sc, "/john")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "1 assigned")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // the filter
	if len(srv.written()) != 0 {
		t.Fatalf("sent before the list closed: %q", srv.written())
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Assignees of acme/gateway !7: john")
	waitWritten(t, srv, `"assignee_ids":[12]`)
	waitRowFetched(t, a, sc)
}

// TestCloseIsInThePickerAndAsks: closing has no key, and asks first.
func TestCloseIsInThePickerAndAsks(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Close Merge Request…")
	typeRunes(sc, "Close Merge")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "without merging it?")
	if len(srv.written()) != 0 {
		t.Fatal("closed before being confirmed")
	}
	typeRunes(sc, "y")
	waitFor(t, a, sc, "Closed acme/gateway !7")
	waitWritten(t, srv, `"state_event":"close"`)
	waitRowFetched(t, a, sc)
}

// TestPeopleColumnsAreHiddenUntilShownAndCountWhoDoesNotFit: ASSIGNEE and
// REVIEWER wait in View options until shown; then as many names as fit
// stand whole and the rest are counted. In the reviewers' list who is
// asked comes first, and x withdraws them.
func TestPeopleColumnsAreHiddenUntilShownAndCountWhoDoesNotFit(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].Assignees = []forge.User{{Username: "jane"}, {Username: "john"}, {Username: "mike"}}
				a.mrs[i].Reviewers = []forge.User{{Username: "mike"}}
			}
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	if text := a.screenText(sc); strings.Contains(text, "ASSIGNEE") || strings.Contains(text, "REVIEWER") {
		t.Fatalf("the people columns show before they are asked for:\n%s", text)
	}
	changeOnLoop(a, func() {
		a.cfg.Filters.ToggleColumn(config.ListMergeRequests, "assignees")
		a.cfg.Filters.ToggleColumn(config.ListMergeRequests, "reviewers")
		a.applyFilters()
	})
	waitFor(t, a, sc, "ASSIGNEE")
	waitFor(t, a, sc, "REVIEWER")
	for _, size := range []struct{ w, h int }{{100, 30}, {160, 34}} {
		resizeApp(a, sc, size.w, size.h)
		waitFor(t, a, sc, "Rate limiting")
		assertLegible(t, a, sc, "the people columns")
	}
	if line := lineAt(a.screenText(sc), "!7"); !strings.Contains(line, "jane") || !strings.Contains(line, "mike") {
		t.Errorf("at 160 columns the names are not there: %q", line)
	}

	resizeApp(a, sc, 120, 34)
	typeRunes(sc, "gs")
	waitFor(t, a, sc, "Reviewers · acme/gateway !7")
	waitFor(t, a, sc, "1 asked")
	text := a.screenText(sc)
	if strings.Index(text, "Mike Moe") > strings.Index(text, "Jane Doe") {
		t.Fatalf("who is asked is not on top:\n%s", text)
	}
	typeRunes(sc, "gx")
	waitFor(t, a, sc, "0 asked")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Reviewers of acme/gateway !7: nobody")
	waitWritten(t, srv, `"reviewer_ids":[0]`)
}

// TestPeopleFieldCountsWhoDoesNotFit: whole names while they fit, the rest
// counted, and the first cut short only when it alone does not fit.
func TestPeopleFieldCountsWhoDoesNotFit(t *testing.T) {
	t.Parallel()
	names := []string{"Jane Doe", "John Roe", "Mike Moe"}
	cases := []struct {
		width int
		want  string
	}{
		{30, "Jane Doe, John Roe, Mike Moe"},
		{21, "Jane Doe, John Roe +1"},
		{20, "Jane Doe +2"},
		{14, "Jane Doe +2"},
		{8, "Jane… +2"},
	}
	for _, c := range cases {
		got := stripTags(peopleField(names, c.width, colText))
		if strings.TrimRight(got, " ") != c.want || len([]rune(got)) != c.width {
			t.Errorf("width %d: %q, want %q in exactly %d", c.width, got, c.want, c.width)
		}
	}
}

// TestTheChosenStandAboveALineAndTheCursorFollows: who is assigned stands
// above a line, and the cursor starts below it on whom the user assigns
// most often; one assigned moves above with the cursor on them, one taken
// off goes back below with the cursor at the top, and with nobody chosen
// there is no line. Whom the user assigns is counted for next time.
func TestTheChosenStandAboveALineAndTheCursorFollows(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		a.cfg.Instances[0].PeopleUses = map[string]map[string]int{config.RoleAssignee: {"mike": 3}}
		for i := range a.mrs {
			if a.mrs[i].IID == 7 {
				a.mrs[i].Assignees = []forge.User{{Username: "john"}}
			}
		}
	})
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "ga")
	waitFor(t, a, sc, "1 assigned")
	order := func() []string {
		var out []string
		for _, line := range strings.Split(a.screenText(sc), "\n") {
			switch {
			case strings.Contains(line, "John Roe"):
				out = append(out, "john")
			case strings.Contains(line, "Mike Moe"):
				out = append(out, "mike")
			case strings.Contains(line, "Jane Doe"):
				out = append(out, "jane")
			case strings.Contains(line, "│────────────────────"):
				out = append(out, "-")
			}
		}
		return out
	}
	if got := strings.Join(order(), " "); got != "john - mike jane" {
		t.Fatalf("the order is %q, want the assigned, the line, then the usual", got)
	}
	assertLegible(t, a, sc, "the assignees with a line")

	typeRunes(sc, " ") // mike, under the cursor below the line
	waitFor(t, a, sc, "2 assigned")
	typeRunes(sc, "x") // the cursor moved up with mike
	waitFor(t, a, sc, "1 assigned")
	if got := strings.Join(order(), " "); got != "john - mike jane" {
		t.Fatalf("taken off, mike is not back at the top below the line: %q", got)
	}
	typeRunes(sc, "x") // the cursor went to the top: john
	waitFor(t, a, sc, "0 assigned")
	if got := strings.Join(order(), " "); got != "john mike jane" {
		t.Fatalf("with nobody chosen: %q, want no line", got)
	}
	typeRunes(sc, "j ") // mike
	waitFor(t, a, sc, "1 assigned")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Assignees of acme/gateway !7: mike")
	waitWritten(t, srv, `"assignee_ids":[13]`)
	if n := onLoop(a, func() int { return a.cfg.Instances[0].PeopleUses[config.RoleAssignee]["mike"] }); n != 4 {
		t.Fatalf("mike assigned %d times, want 4", n)
	}
}
