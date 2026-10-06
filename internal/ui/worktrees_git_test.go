package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	cmd.Env = gitTestEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitTestEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
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

var fixtureRoot string

// Only the initial history is shared. CopyFS gives each test its own objects,
// refs and index, so its pushes and worktrees cannot alter another fixture.
var projectSeed = sync.OnceValues(func() (string, error) {
	base, err := os.MkdirTemp(fixtureRoot, "project-seed-")
	if err != nil {
		return "", err
	}
	origin, clone := filepath.Join(base, "origin.git"), filepath.Join(base, "clone")
	run := func(dir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitTestEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	// Sample hooks are never run, but copying them into every fixture costs
	// as many files as the actual history. An empty template leaves them out.
	if err := run(base, "-c", "init.templateDir=", "init", "-q", "--bare", "--initial-branch=main", origin); err != nil {
		return "", err
	}
	if err := run(base, "-c", "init.templateDir=", "clone", "-q", origin, clone); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(clone, "a.txt"), []byte("initial\n"), 0o644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "initial"}, {"branch", "-M", "main"}, {"push", "-q", "-u", "origin", "main"}} {
		if err := run(clone, args...); err != nil {
			return "", err
		}
	}
	return base, nil
})

// newRealProject makes a real clone and a private bare origin where the app
// expects to find them, without rebuilding the same history for every test.
func newRealProject(t *testing.T, a *App, path string) *realProject {
	t.Helper()
	instance := a.cfg.Instances[0].ID
	clone := a.projectDir(instance, path)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	seed, err := projectSeed()
	must(t, err)
	must(t, os.CopyFS(origin, os.DirFS(filepath.Join(seed, "origin.git"))))
	must(t, os.CopyFS(clone, os.DirFS(filepath.Join(seed, "clone"))))
	gitIn(t, clone, "remote", "set-url", "origin", origin)
	return &realProject{t: t, a: a, path: path, origin: origin, clone: clone}
}

func TestRealProjectFixturesKeepTheirOwnHistory(t *testing.T) {
	t.Parallel()
	a, _ := newTestApp(t)
	gw := newRealProject(t, a, "acme/gateway")
	bl := newRealProject(t, a, "acme/billing")
	initial := gitIn(t, bl.origin, "rev-parse", "main")
	commitIn(t, gw.clone, "a.txt", "only gateway changes")
	gitIn(t, gw.clone, "push", "-q", "origin", "main")
	for _, dir := range []string{bl.clone, bl.origin} {
		if got := gitIn(t, dir, "rev-parse", "main"); got != initial {
			t.Errorf("billing's history changed in %s: %s", dir, got)
		}
	}
	contents, err := os.ReadFile(filepath.Join(bl.clone, "a.txt"))
	must(t, err)
	if string(contents) != "initial\n" {
		t.Errorf("billing's working tree changed: %q", contents)
	}
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
	t.Parallel()
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

	typeRunes(sc, "3")
	waitFor(t, a, sc, "5/5 worktrees")
	for _, want := range []string{"no upstream", "in sync", "origin/feat/sync", "↑1 unpushed", "↓1 behind", "upstream gone", "REMOTE"} {
		waitFor(t, a, sc, want)
	}
}

func TestRemoteColumnIsQuestionMarkWhenTheCloneCannotBeRead(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	now := time.Now()
	makeWorktree(t, a, "acme/gateway", "wt-feat-x", "ref: refs/heads/feat/x", now)
	a.tv.QueueUpdateDraw(func() { a.refreshDisk() })
	typeRunes(sc, "3")
	waitFor(t, a, sc, "1/1 worktrees")
	rowOfBranch := func() string {
		for _, line := range strings.Split(a.screenText(sc), "\n") {
			if strings.Contains(line, "feat/x") {
				return line
			}
		}
		return ""
	}
	deadline := time.Now().Add(patience)
	for !strings.Contains(rowOfBranch(), " ? ") && time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
	}
	if row := rowOfBranch(); !strings.Contains(row, " ? ") || strings.Contains(row, "no upstream") {
		t.Errorf("a worktree git cannot read shows ?, and must not claim to have no upstream: %q", row)
	}
}

func TestColumnsGiveWayInOrderAndRemoteStays(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/rate")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "no upstream")
	// The columns are narrowed down a step at a time; each must go in its
	// turn, and REMOTE never.
	order := []string{"PATH", "CREATED", "SIZE", "MR", "EDITS"}
	goneAt := map[string]int{}
	for w := 160; w >= 40; w -= 2 {
		h := worktreeHeaderAt(t, a, sc, w)
		if !slices.Contains(h, "REMOTE") {
			t.Fatalf("REMOTE gave way at %d: %v", w, h)
		}
		for _, name := range order {
			if _, gone := goneAt[name]; !gone && !slices.Contains(h, name) {
				goneAt[name] = w
			}
		}
	}
	for i := 1; i < len(order); i++ {
		before, after := order[i-1], order[i]
		if at, ok := goneAt[after]; ok && at > goneAt[before] {
			t.Errorf("%s went at %d, before %s at %d", after, at, before, goneAt[before])
		}
	}
	if _, ok := goneAt["PATH"]; !ok {
		t.Errorf("the directory never gave way: %v", goneAt)
	}
}

// worktreeHeaderAt is the worktree list's headings on a terminal w wide,
// once the list has been laid out again for it.
func worktreeHeaderAt(t *testing.T, a *App, sc tcell.SimulationScreen, w int) []string {
	t.Helper()
	resizeApp(a, sc, w, 30)
	changeOnLoop(a, func() { a.worktreesPane.reload() })
	for _, line := range strings.Split(a.screenText(sc), "\n") {
		if strings.Contains(line, "REPOSITORY") {
			return strings.Fields(strings.Trim(line, "│ "))
		}
	}
	return nil
}

func TestMergeRequestColumnNamesTheOpenRequestOfABranch(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	p.worktree("feat/rate") // the index has !7 with this source branch
	p.worktree("feat/free")
	p.rescan()

	typeRunes(sc, "3")
	waitFor(t, a, sc, "2/2 worktrees")
	waitFor(t, a, sc, "!7")
	if got := strings.Count(a.screenText(sc), "!7"); got != 1 {
		t.Errorf("only the branch with an open request should show one, !7 appears %d times", got)
	}
}

func TestPushSendsANewBranchWithItsUpstream(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "n.txt", "a change")
	p.rescan()
	typeRunes(sc, "3")
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
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/more")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/more")
	commitIn(t, dir, "m.txt", "one more")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "↑1 unpushed")
	typeRunes(sc, "P")
	waitFor(t, a, sc, "in sync")
	if got, want := gitIn(t, p.origin, "rev-parse", "feat/more"), gitIn(t, dir, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin has %s, the worktree is at %s", got, want)
	}
}

func TestPushIsRefusedWhenOriginIsAhead(t *testing.T) {
	t.Parallel()
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
	typeRunes(sc, "3")
	waitFor(t, a, sc, "diverged")

	typeRunes(sc, "P")
	waitFor(t, a, sc, "pull or rebase first")
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
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "n.txt", "Add the new thing", "It does this.\n\nAnd that.")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/new-thing")
	p.rescan()
	typeRunes(sc, "3")
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
	for _, label := range []string{"Draft", labelDeleteBranch, labelSquash} {
		if onLoop(a, func() bool { return form.GetFormItemByLabel(label) != nil }) == false {
			t.Errorf("the form lacks %q", label)
		}
	}

	// Create it, as a draft, with squash.
	a.tv.QueueUpdateDraw(func() {
		form.GetFormItemByLabel("Draft").(*tview.Checkbox).SetChecked(true)
		form.GetFormItemByLabel(labelSquash).(*tview.Checkbox).SetChecked(true)
		form.GetFormItemByLabel("Title").(*tview.InputField).SetText("Add the new thing, properly")
	})
	time.Sleep(100 * time.Millisecond)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing
	typeRunes(sc, "r")
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
	typeRunes(sc, "2")
	waitFor(t, a, sc, "!42")
}

func TestNewMergeRequestListsSeveralCommitsUnderTheBranchName(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "1.txt", "First step")
	commitIn(t, dir, "2.txt", "Second step")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/new-thing")
	p.rescan()
	typeRunes(sc, "3")
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
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/new-thing")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "in sync")

	form := openForm(t, a, sc) // no commits ahead of main: nothing is proposed
	a.tv.QueueUpdateDraw(func() { form.GetFormItemByLabel("Title").(*tview.InputField).SetText("  ") })
	time.Sleep(100 * time.Millisecond)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // stop typing
	typeRunes(sc, "r")
	waitFor(t, a, sc, "enter a title")
	if srv.postedMR.Load() != nil {
		t.Error("a merge request without a title was sent")
	}
}

func TestNewMergeRequestOffersToPushFirst(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	resizeApp(a, sc, 160, 44)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/new-thing")
	commitIn(t, dir, "n.txt", "Only here")
	p.rescan()
	typeRunes(sc, "3")
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
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	dir := p.worktree("feat/rate") // !7 is open for it in the index
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/rate")
	p.rescan()
	typeRunes(sc, "3")
	waitFor(t, a, sc, "in sync")

	typeRunes(sc, "n")
	waitFor(t, a, sc, "!7 is already open for this branch")
	if srv.postedMR.Load() != nil {
		t.Error("a second merge request was created")
	}
}

func TestNewMergeRequestIsRefusedWhenBehind(t *testing.T) {
	t.Parallel()
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
	typeRunes(sc, "3")
	waitFor(t, a, sc, "↓1 behind")

	typeRunes(sc, "n")
	waitFor(t, a, sc, "pull or rebase first")
	if srv.postedMR.Load() != nil {
		t.Error("a merge request was created for a branch that is behind")
	}
}

func TestMergeRequestDefaultsAndBranchNames(t *testing.T) {
	t.Parallel()
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

// TestTheWorktreePickerSaysWhereABranchIsOut: a branch git will not check
// out again - out in the main clone, or in a worktree unagit did not make -
// is listed after the rest, dimmed, with where it is out; picking it says
// why and leaves the list open.
func TestTheWorktreePickerSaysWhereABranchIsOut(t *testing.T) {
	t.Parallel()
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	away := filepath.Join(t.TempDir(), "away")
	gitIn(t, p.clone, "worktree", "add", "-q", "-b", "feat/away", away)
	srv.liveBranches.Store(func(int) []string { return []string{"main", "feat/free"} })
	p.rescan()

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlW, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Worktree branch - acme/gateway")
	waitFor(t, a, sc, "out in the main clone")
	waitFor(t, a, sc, "out in worktree away  ")
	text := a.screenText(sc)
	if free, main := lineOf(text, "feat/free"), lineOf(text, "out in the main clone"); free < 0 || free > main {
		t.Errorf("the branches that can be taken do not come first:\n%s", text)
	}
	assertLegible(t, a, sc, "the branches that are out")

	typeRunes(sc, "G")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "git checks a branch out only once")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitFor(t, a, sc, "Worktree branch - acme/gateway")
}

// TestWorktreesUseTheWholeWidth: on a wide terminal nothing is cut while
// room is left - the branch and the directory are whole - PATH comes before
// EDITS, and what remains is a gap behind PATH, so the last column ends at
// the frame's right edge on every row.
func TestWorktreesUseTheWholeWidth(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	p := newRealProject(t, a, "acme/gateway")
	branch := "feature/a-rather-long-branch-name-to-see-it-whole"
	dir := p.worktree(branch)
	p.rescan()
	resizeApp(a, sc, 320, 30)
	typeRunes(sc, "3")
	waitFor(t, a, sc, branch)
	waitFor(t, a, sc, tildePath(dir))
	text := a.screenText(sc)
	header := lineAt(text, "REPOSITORY")
	if strings.Index(header, "PATH") > strings.Index(header, "EDITS") {
		t.Errorf("PATH does not come before EDITS: %q", header)
	}
	for _, line := range []string{header, lineAt(text, branch)} {
		trimmed := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(line, " "), "│"), " ")
		if gap := len([]rune(line)) - len([]rune(trimmed)); gap > 4 {
			t.Errorf("the row ends %d cells short of the frame: %q", gap, line)
		}
	}
}
