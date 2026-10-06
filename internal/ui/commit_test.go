package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/workspace"
)

// TestCommitEverythingInAGroup: c commits every repository of a grouped
// worktree, new files included, under one message or a repository's own.
func TestCommitEverythingInAGroup(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/c")
	waitFor(t, a, sc, "feat-c")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-c")
	must(t, os.WriteFile(filepath.Join(dir, "gateway", "a.txt"), []byte("changed\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "gateway", "new.txt"), []byte("new\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "billing", "a.txt"), []byte("changed\n"), 0o644))

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Commit · feat-c · 3 file(s)")
	waitFor(t, a, sc, "gateway 2 files")
	waitFor(t, a, sc, "billing message")
	commitForm := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	onLoop(a, func() bool {
		commitForm.GetFormItemByLabel("Message for all").(*tview.TextArea).SetText("Count and bill\n\nBoth sides.", false)
		commitForm.GetFormItemByLabel("billing message").(*tview.InputField).SetText("Bill what was counted")
		return true
	})
	pressButton(t, a, sc, commitForm, "Commit")
	// The log names each commit as it is made; the status line, once all are.
	waitFor(t, a, sc, "committed gateway")

	if got := gitIn(t, filepath.Join(dir, "gateway"), "log", "-1", "--format=%B"); got != "Count and bill\n\nBoth sides." {
		t.Errorf("gateway's message: %q", got)
	}
	if got := gitIn(t, filepath.Join(dir, "gateway"), "show", "--name-only", "--format=", "HEAD"); !strings.Contains(got, "new.txt") {
		t.Errorf("the new file was not committed: %q", got)
	}
	if got := gitIn(t, filepath.Join(dir, "billing"), "log", "-1", "--format=%s"); got != "Bill what was counted" {
		t.Errorf("billing's message: %q", got)
	}
	for _, name := range []string{"gateway", "billing"} {
		if st := gitIn(t, filepath.Join(dir, name), "status", "--porcelain"); st != "" {
			t.Errorf("%s still has %q", name, st)
		}
	}
}

// TestCommitFormFitsItsFrame draws the commit form of a group at several sizes.
func TestCommitFormFitsItsFrame(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	// Committing the group is exercised above. Here the form only needs the
	// rows and counts it is given, and each size must draw that same form.
	a.tv.QueueUpdateDraw(func() {
		a.showCommitForm(worktreeRow{Path: "feat-c", Members: []worktreeRow{}}, []commitTarget{
			{name: "gateway", edits: 1}, {name: "billing", edits: 1},
		})
	})
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 26}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			resize(sc, size.w, size.h)
			a.tv.QueueUpdateDraw(func() {})
			waitFor(t, a, sc, "billing message")
			commitForm := onLoop(a, func() *tview.Form {
				_, primitive := a.pages.GetFrontPage()
				return primitive.(*modalBox).content.(*tview.Form)
			})
			frame := onLoop(a, func() rect {
				x, y, w, h := commitForm.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is drawn over:\n%s", y, a.screenText(sc))
					break
				}
			}
			for _, want := range []string{"Message for all", "gateway message", "billing message", "billing 1 file"} {
				if !strings.Contains(a.screenText(sc), want) {
					t.Errorf("%q is not on screen:\n%s", want, a.screenText(sc))
				}
			}
			assertLegible(t, a, sc, "the commit form")
		})
	}
}

// TestCommitOfOneRepositoryInAGroupNamesIt: with changes in one repository of
// a group, the message field is that repository's, and so is the commit.
func TestCommitOfOneRepositoryInAGroupNamesIt(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t)
	_, _, form := markBoth(t, a, sc)
	typeRunes(sc, "feat/one")
	waitFor(t, a, sc, "feat-one")
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created ")
	dir := filepath.Join(workspace.GroupsRoot(a.cfg.Root()), "feat-one")
	must(t, os.WriteFile(filepath.Join(dir, "billing", "a.txt"), []byte("changed\n"), 0o644))

	typeRunes(sc, "c")
	waitFor(t, a, sc, "Commit · feat-one · billing · 1 file(s)")
	waitFor(t, a, sc, "billing message")
	text := a.screenText(sc)
	if strings.Contains(text, "Message for all") || strings.Contains(text, "gateway message") {
		t.Errorf("the form offers more than the one repository:\n%s", text)
	}
	commitForm := onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
	onLoop(a, func() bool {
		commitForm.GetFormItemByLabel("billing message").(*tview.TextArea).SetText("Round half even", false)
		return true
	})
	pressButton(t, a, sc, commitForm, "Commit")
	waitFor(t, a, sc, "committed billing")
	if got := gitIn(t, filepath.Join(dir, "billing"), "log", "-1", "--format=%s"); got != "Round half even" {
		t.Errorf("billing's message: %q", got)
	}
}
