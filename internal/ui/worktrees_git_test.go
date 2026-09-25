package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/workspace"
)

// The tests here use real git: a bare origin, the clone unagit would have made,
// and linked worktrees of it.

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitIn(t *testing.T, dir, file, message string, more ...string) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(dir, file), []byte(message+"\n"), 0o644))
	gitIn(t, dir, "add", ".")
	args := []string{"commit", "-q", "-m", message}
	for _, m := range more {
		args = append(args, "-m", m)
	}
	gitIn(t, dir, args...)
}

type realProject struct {
	t      *testing.T
	a      *App
	path   string
	origin string
	clone  string
}

// newRealProject makes acme/gateway a real clone of a bare origin, where the
// app expects to find it.
func newRealProject(t *testing.T, a *App, path string) *realProject {
	t.Helper()
	instance := a.cfg.Instances[0].ID
	clone := a.projectDir(instance, path)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitIn(t, base, "init", "-q", "--bare", "--initial-branch=main", origin)
	must(t, os.MkdirAll(filepath.Dir(clone), 0o755))
	gitIn(t, base, "clone", "-q", origin, clone)
	commitIn(t, clone, "a.txt", "initial")
	gitIn(t, clone, "branch", "-M", "main")
	gitIn(t, clone, "push", "-q", "-u", "origin", "main")
	return &realProject{t: t, a: a, path: path, origin: origin, clone: clone}
}

// worktree adds a branch worktree, where Ctrl-W would have put it.
func (p *realProject) worktree(branch string) string {
	dir := filepath.Join(workspace.WorktreeRoot(p.clone), "wt-"+workspace.Sanitize(branch))
	must(p.t, os.MkdirAll(filepath.Dir(dir), 0o755))
	gitIn(p.t, p.clone, "worktree", "add", "-q", "-b", branch, dir)
	return dir
}

// elsewhere is a second clone, standing in for a colleague who pushes.
func (p *realProject) elsewhere(branch string) string {
	dir := filepath.Join(p.t.TempDir(), "elsewhere")
	gitIn(p.t, filepath.Dir(dir), "clone", "-q", "-b", branch, p.origin, dir)
	return dir
}

func (p *realProject) rescan() {
	p.a.tv.QueueUpdateDraw(func() { p.a.refreshDisk() })
}

func TestRemoteColumnShowsWhereEachBranchStands(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")

	p.worktree("feat/none")

	sync := p.worktree("feat/sync")
	gitIn(t, sync, "push", "-q", "-u", "origin", "feat/sync")

	ahead := p.worktree("feat/ahead")
	gitIn(t, ahead, "push", "-q", "-u", "origin", "feat/ahead")
	commitIn(t, ahead, "x.txt", "local only")

	behind := p.worktree("feat/behind")
	gitIn(t, behind, "push", "-q", "-u", "origin", "feat/behind")
	other := p.elsewhere("feat/behind")
	commitIn(t, other, "y.txt", "theirs")
	gitIn(t, other, "push", "-q", "origin", "feat/behind")

	gone := p.worktree("feat/gone")
	gitIn(t, gone, "push", "-q", "-u", "origin", "feat/gone")
	gitIn(t, other, "push", "-q", "origin", "--delete", "feat/gone")

	gitIn(t, p.clone, "fetch", "-q", "--prune")
	p.rescan()

	typeRunes(sc, "W")
	waitFor(t, a, sc, "5/5 worktrees")
	for _, want := range []string{"no upstream", "in sync", "origin/feat/sync", "↑1 unpushed", "↓1 behind", "upstream gone", "REMOTE"} {
		waitFor(t, a, sc, want)
	}
}

func TestRemoteColumnIsQuestionMarkWhenTheCloneCannotBeRead(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", now)
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })
	typeRunes(sc, "W")
	waitFor(t, a, sc, "1/1 worktrees")
	rowOfBranch := func() string {
		for _, line := range strings.Split(a.screenText(sc), "\n") {
			if strings.Contains(line, "feat/x") {
				return line
			}
		}
		return ""
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(rowOfBranch(), " ? ") && time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
	}
	if row := rowOfBranch(); !strings.Contains(row, " ? ") || strings.Contains(row, "no upstream") {
		t.Errorf("a worktree git cannot read shows ?, and must not claim to have no upstream: %q", row)
	}
}

func TestColumnsGiveWayInOrderAndRemoteStays(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/rate")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "no upstream")
	header := func() string {
		for _, line := range strings.Split(a.screenText(sc), "\n") {
			if strings.Contains(line, "REPOSITORY") {
				return line
			}
		}
		return ""
	}
	waitForHeader := func(present, absent []string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		ok := func() bool {
			h := header()
			for _, w := range present {
				if !strings.Contains(h, w) {
					return false
				}
			}
			for _, w := range absent {
				if strings.Contains(h, w) {
					return false
				}
			}
			return h != ""
		}
		for !ok() && time.Now().Before(deadline) {
			time.Sleep(30 * time.Millisecond)
		}
		if !ok() {
			t.Fatalf("header %q, want %v and not %v", header(), present, absent)
		}
	}
	waitForHeader([]string{"REMOTE", "MR", "PATH"}, nil) // 160 wide: everything
	resize(sc, 70, 30)
	waitForHeader([]string{"REMOTE", "MR"}, []string{"PATH"}) // the directory goes first
	resize(sc, 50, 30)
	waitForHeader([]string{"REMOTE"}, []string{"PATH", " MR "}) // then the merge request; REMOTE stays
}

func TestMergeRequestColumnNamesTheOpenRequestOfABranch(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/rate") // the index has !7 with this source branch
	p.worktree("feat/free")
	p.rescan()

	typeRunes(sc, "W")
	waitFor(t, a, sc, "2/2 worktrees")
	waitFor(t, a, sc, "!7")
	if got := strings.Count(a.screenText(sc), "!7"); got != 1 {
		t.Errorf("only the branch with an open request should show one, !7 appears %d times", got)
	}
}

func TestPushSendsANewBranchWithItsUpstream(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "n.txt", "a change")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "no upstream")

	typeRunes(sc, "P")
	waitFor(t, a, sc, "in sync")
	if got, want := gitIn(t, p.origin, "rev-parse", "feat/new-thing"), gitIn(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin has %s, the worktree is at %s", got, want)
	}
	if got := gitIn(t, dir, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/feat/new-thing" {
		t.Errorf("upstream = %q", got)
	}

	// A second P has nothing to send.
	typeRunes(sc, "P")
	waitFor(t, a, sc, "already on origin")
}

func TestPushSendsMoreCommitsWithoutChangingTheUpstream(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/more")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/more")
	commitIn(t, dir, "m.txt", "one more")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "↑1 unpushed")
	typeRunes(sc, "P")
	waitFor(t, a, sc, "in sync")
	if got, want := gitIn(t, p.origin, "rev-parse", "feat/more"), gitIn(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin has %s, the worktree is at %s", got, want)
	}
}

func TestPushIsRefusedWhenOriginIsAhead(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/behind")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/behind")
	other := p.elsewhere("feat/behind")
	commitIn(t, other, "o.txt", "theirs")
	gitIn(t, other, "push", "-q", "origin", "feat/behind")
	commitIn(t, dir, "m.txt", "mine")
	gitIn(t, p.clone, "fetch", "-q")
	before := gitIn(t, p.origin, "rev-parse", "feat/behind")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "diverged")

	typeRunes(sc, "P")
	waitFor(t, a, sc, "unagit never forces")
	time.Sleep(200 * time.Millisecond)
	if got := gitIn(t, p.origin, "rev-parse", "feat/behind"); got != before {
		t.Error("a refused push must not touch origin")
	}
}

// openForm presses n on the only worktree and hands back the merge request form.
func openForm(t *testing.T, a *App, sc tcell.SimulationScreen) *tview.Form {
	t.Helper()
	typeRunes(sc, "n")
	waitFor(t, a, sc, "New merge request")
	return onLoop(a, func() *tview.Form {
		_, primitive := a.pages.GetFrontPage()
		return primitive.(*modalBox).content.(*tview.Form)
	})
}

func TestNewMergeRequestProposesTheSingleCommit(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "n.txt", "Add the new thing", "It does this.\n\nAnd that.")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/new-thing")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "in sync")

	form := openForm(t, a, sc)
	values := onLoop(a, func() []string {
		_, target := form.GetFormItemByLabel("Target branch").(*tview.DropDown).GetCurrentOption()
		return []string{
			form.GetFormItemByLabel("Title").(*tview.InputField).GetText(),
			form.GetFormItemByLabel("Description").(*tview.TextArea).GetText(),
			target,
		}
	})
	if values[0] != "Add the new thing" || values[1] != "It does this.\n\nAnd that." || values[2] != "main" {
		t.Fatalf("defaults = %q", values)
	}
	// The server has main and feat/rate; the source branch is not offered as a
	// target, and the GitLab options are on offer.
	if opts := onLoop(a, func() int { return form.GetFormItemByLabel("Target branch").(*tview.DropDown).GetOptionCount() }); opts != 2 {
		t.Errorf("target branches = %d, want 2", opts)
	}
	for _, label := range []string{"Draft", "Delete source branch when merged", "Squash commits"} {
		if onLoop(a, func() bool { return form.GetFormItemByLabel(label) != nil }) == false {
			t.Errorf("the form lacks %q", label)
		}
	}

	// Create it, as a draft, with squash.
	a.tv.QueueUpdateDraw(func() {
		form.GetFormItemByLabel("Draft").(*tview.Checkbox).SetChecked(true)
		form.GetFormItemByLabel("Squash commits").(*tview.Checkbox).SetChecked(true)
		form.GetFormItemByLabel("Title").(*tview.InputField).SetText("Add the new thing, properly")
	})
	time.Sleep(100 * time.Millisecond)
	sc.InjectKey(tcell.KeyRune, 'r', tcell.ModAlt)
	waitFor(t, a, sc, "Merge request !42 created")
	body, _ := srv.postedMR.Load().(string)
	for _, want := range []string{`"source_branch":"feat/new-thing"`, `"target_branch":"main"`,
		`"title":"Draft: Add the new thing, properly"`, `"squash":true`, `"remove_source_branch":false`,
		`"description":"It does this.\n\nAnd that."`} {
		if !strings.Contains(body, want) {
			t.Errorf("the request lacks %s:\n%s", want, body)
		}
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Merge request !42 created")

	// The new request is in the list without a refresh, and the row says so.
	waitFor(t, a, sc, "!42")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "!42")
}

func TestNewMergeRequestListsSeveralCommitsUnderTheBranchName(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "1.txt", "First step")
	commitIn(t, dir, "2.txt", "Second step")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/new-thing")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "in sync")

	form := openForm(t, a, sc)
	values := onLoop(a, func() []string {
		return []string{
			form.GetFormItemByLabel("Title").(*tview.InputField).GetText(),
			form.GetFormItemByLabel("Description").(*tview.TextArea).GetText(),
		}
	})
	if values[0] != "New thing" || values[1] != "- First step\n- Second step" {
		t.Errorf("defaults = %q", values)
	}
}

func TestNewMergeRequestNeedsATitle(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/new-thing")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "in sync")

	form := openForm(t, a, sc) // no commits ahead of main: nothing is proposed
	a.tv.QueueUpdateDraw(func() { form.GetFormItemByLabel("Title").(*tview.InputField).SetText("  ") })
	time.Sleep(100 * time.Millisecond)
	sc.InjectKey(tcell.KeyRune, 'r', tcell.ModAlt)
	waitFor(t, a, sc, "enter a title")
	if srv.postedMR.Load() != nil {
		t.Error("a merge request without a title was sent")
	}
}

func TestNewMergeRequestOffersToPushFirst(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "n.txt", "Only here")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "no upstream")

	typeRunes(sc, "n")
	waitFor(t, a, sc, "not on origin yet")
	waitFor(t, a, sc, "push it and continue?")
	if srv.postedMR.Load() != nil {
		t.Fatal("created before the push was agreed to")
	}
	typeRunes(sc, "p") // Push and continue
	waitFor(t, a, sc, "New merge request")
	if got := gitIn(t, p.origin, "rev-parse", "feat/new-thing"); got != gitIn(t, dir, "rev-parse", "HEAD") {
		t.Error("the branch was not pushed before the form")
	}
}

func TestNewMergeRequestIsRefusedWhenOneIsAlreadyOpen(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/rate") // !7 is open for it in the index
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/rate")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "in sync")

	typeRunes(sc, "n")
	waitFor(t, a, sc, "!7 is already open for this branch")
	if srv.postedMR.Load() != nil {
		t.Error("a second merge request was created")
	}
}

func TestNewMergeRequestIsRefusedWhenBehind(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/behind")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/behind")
	other := p.elsewhere("feat/behind")
	commitIn(t, other, "o.txt", "theirs")
	gitIn(t, other, "push", "-q", "origin", "feat/behind")
	gitIn(t, p.clone, "fetch", "-q")
	p.rescan()
	typeRunes(sc, "W")
	waitFor(t, a, sc, "↓1 behind")

	typeRunes(sc, "n")
	waitFor(t, a, sc, "unagit never forces")
	if srv.postedMR.Load() != nil {
		t.Error("a merge request was created for a branch that is behind")
	}
}

func TestMergeRequestDefaultsAndBranchNames(t *testing.T) {
	for in, want := range map[string]string{
		"feat/add-user-form": "Add user form",
		"fix_the_thing":      "Fix the thing",
		"plain":              "Plain",
		"a/b/c-d":            "C d",
		"":                   "",
	} {
		if got := humanBranch(in); got != want {
			t.Errorf("humanBranch(%q) = %q, want %q", in, got, want)
		}
	}
	if title, desc := mergeRequestDefaults("feat/x", nil); title != "" || desc != "" {
		t.Errorf("no commits, nothing proposed: %q %q", title, desc)
	}
}
