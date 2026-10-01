package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/workspace"
)

// mrOnOrigin pushes commits of feat/rate to the branch and to the ref GitLab
// publishes merge request !7 at, the one the fake server lists first, and has
// the fake server list them as the merge request's commits.
func mrOnOrigin(t *testing.T, srv *fakeServer, p *realProject, subjects ...string) string {
	t.Helper()
	work := p.elsewhere("main")
	gitIn(t, work, "fetch", "-q", "origin")
	if strings.Contains(gitIn(t, work, "branch", "-r"), "origin/feat/rate") {
		gitIn(t, work, "checkout", "-q", "-B", "feat/rate", "origin/feat/rate")
	} else {
		gitIn(t, work, "checkout", "-q", "-b", "feat/rate")
	}
	for i, s := range subjects {
		commitIn(t, work, fmt.Sprintf("rate%d.txt", i), s)
	}
	gitIn(t, work, "push", "-q", "origin", "feat/rate")
	gitIn(t, work, "push", "-q", "-f", "origin", "HEAD:refs/merge-requests/7/head")
	log := gitIn(t, work, "log", "--format=%H%x1f%P%x1f%an%x1f%cI%x1f%s", "origin/main..HEAD")
	var listed []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		f := strings.Split(line, "\x1f")
		listed = append(listed, map[string]any{"id": f[0], "parent_ids": strings.Fields(f[1]),
			"author_name": f[2], "committed_date": f[3], "title": f[4]})
	}
	body, err := json.Marshal(listed)
	must(t, err)
	srv.mrCommits.Store(string(body))
	return gitIn(t, work, "rev-parse", "HEAD")
}

// TestReviewFromACommitStartsOnWhatIsNew: v lists the commits, marks nothing
// before any review, and after one puts the cursor on the first commit the
// review has not been given. (Enter opens an editor, which tview cannot suspend
// for without a race under test; what it leaves on disk is the workspace
// package's to check.)
func TestReviewFromACommitStartsOnWhatIsNew(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	head := mrOnOrigin(t, srv, p, "Add a token bucket", "Count per client")

	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "v")
	waitFor(t, a, sc, "Review !7 from a commit to the head")
	text := a.screenText(sc)
	for _, want := range []string{"Add a token bucket", "Count per client"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q is not listed:\n%s", want, text)
		}
	}
	if strings.Contains(text, "●") {
		t.Errorf("nothing was reviewed yet, so nothing can be new:\n%s", text)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Review !7 from a commit")

	// A whole review, as Ctrl-R leaves it, then the author answers the
	// comments in a new commit.
	mr := a.mrs[0]
	for _, m := range a.mrs {
		if m.IID == 7 {
			mr = m
		}
	}
	project := a.mrProject(mr)
	if _, err := a.newManager(mr.Instance, project.PathWithNamespace, nil).
		EnsureMRReview(mr, project, workspace.Review{HeadSHA: head}); err != nil {
		t.Fatal(err)
	}
	answer := mrOnOrigin(t, srv, p, "Answer the review")

	typeRunes(sc, "v")
	waitFor(t, a, sc, "1 new since your last review")
	row := "● " + answer[:8] + "  Answer the review"
	text = a.screenText(sc)
	if !strings.Contains(text, row) {
		t.Fatalf("the new commit is not marked:\n%s", text)
	}
	// The cursor is drawn as the selection band; it must be on the new commit.
	for y, line := range strings.Split(text, "\n") {
		for _, subject := range []string{"Count per client", row} {
			x := strings.Index(line, subject)
			if x < 0 {
				continue
			}
			_, style := cellAt(a, sc, len([]rune(line[:x])), y)
			_, bg, _ := style.Decompose()
			_, selected, _ := styleSelected.Decompose()
			if want := subject == row; (bg == selected) != want {
				t.Errorf("%q selected = %v, want %v:\n%s", subject, bg == selected, want, text)
			}
		}
	}
}

// TestReviewStartPickerFits draws the picker at several sizes: every commit
// on screen and the frame whole.
func TestReviewStartPickerFits(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc, srv := newTestAppSrv(t)
			waitFor(t, a, sc, "acme/gateway")
			p := newRealProject(t, a, "acme/gateway")
			mrOnOrigin(t, srv, p, "Add a token bucket", "Count per client", "Answer the review")
			resize(sc, size.w, size.h)
			typeRunes(sc, "M")
			waitFor(t, a, sc, "Rate limiting")
			typeRunes(sc, "v")
			waitFor(t, a, sc, "Review !7 from a commit")
			text := a.screenText(sc)
			for _, want := range []string{"Add a token bucket", "Count per client", "/ filter"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			// Newest on top, as git log and the forge list them.
			if strings.Index(text, "Answer the review") > strings.Index(text, "Add a token bucket") {
				t.Errorf("the newest commit is not on top:\n%s", text)
			}
			assertLegible(t, a, sc, "the commit picker")
		})
	}
}
