package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// keyEvent is the event an action's key sends.
func keyEvent(spec string) *tcell.EventKey {
	if rest, ok := strings.CutPrefix(spec, "Ctrl-"); ok {
		return tcell.NewEventKey(tcell.KeyCtrlA+tcell.Key(strings.ToLower(rest)[0]-'a'), 0, tcell.ModCtrl)
	}
	if rest, ok := strings.CutPrefix(spec, "Alt-"); ok {
		return tcell.NewEventKey(tcell.KeyRune, []rune(strings.ToLower(rest))[0], tcell.ModAlt)
	}
	return tcell.NewEventKey(tcell.KeyRune, []rune(spec)[0], tcell.ModNone)
}

func TestAnActionMatchesItsOwnKeyAndNoOther(t *testing.T) {
	cases := []struct {
		spec string
		ev   *tcell.EventKey
		want bool
	}{
		{"Ctrl-W", tcell.NewEventKey(tcell.KeyCtrlW, 0, tcell.ModCtrl), true},
		{"Ctrl-W", tcell.NewEventKey(tcell.KeyRune, 'w', tcell.ModNone), false},
		{"Alt-P", tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModAlt), true},
		{"Alt-P", tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModNone), false},
		{"p", tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModAlt), false},
		{"P", tcell.NewEventKey(tcell.KeyRune, 'P', tcell.ModShift), true},
		{"P", tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModNone), false},
		{"Enter", tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), false},
		{"", tcell.NewEventKey(tcell.KeyRune, 'U', tcell.ModNone), false},
	}
	for _, c := range cases {
		if got := (uiAction{keys: c.spec}).matches(c.ev); got != c.want {
			t.Errorf("%q matches %v: %v, want %v", c.spec, c.ev.Name(), got, c.want)
		}
	}
}

// TestNoTwoActionsShareAKey: in each list a key belongs to one action, or to
// several of which only one can be done at a time - each of them says when.
func TestNoTwoActionsShareAKey(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	lists := onLoop(a, func() map[string][]uiAction {
		pr := a.projects[0]
		mr := a.mrs[0]
		single := worktreeRow{Instance: pr.Instance, Path: pr.PathWithNamespace, Branch: "b", Dir: "/x"}
		group := worktreeRow{Path: "g", Dir: "/g", Members: []worktreeRow{single}}
		v := &wtView{row: group, at: 1}
		return map[string][]uiAction{
			"repository":        append(a.repositoryActions(a.projectsPane, pr), a.repositoriesActions(a.projectsPane)...),
			"merge request":     append(a.mergeRequestActions(a.mrsPane, mr), a.mergeRequestsActions(a.mrsPane)...),
			"worktree":          append(a.worktreeListActions(a.worktreesPane, single), a.worktreesActions(a.worktreesPane)...),
			"grouped worktree":  append(a.worktreeListActions(a.worktreesPane, group), a.worktreesActions(a.worktreesPane)...),
			"a view's member":   append(a.worktreeViewActions(v), a.worktreeViewScreenActions()...),
			"marked repository": a.markedRepositoryActions(a.projectsPane, a.projects),
		}
	})
	for list, acts := range lists {
		always := map[string]string{}
		for _, act := range acts {
			if act.keys == "" {
				continue
			}
			if !keyWords[act.keys] && !act.matches(keyEvent(act.keys)) {
				t.Errorf("%s: %q does not answer its own key %s", list, act.name, act.keys)
			}
			if act.when != nil {
				continue
			}
			if other, ok := always[act.keys]; ok {
				t.Errorf("%s: %s is both %q and %q", list, act.keys, other, act.name)
			}
			always[act.keys] = act.name
		}
	}
}

// TestAltEnterListsWhatCanBeDoneWithTheRow: the actions of the row, the
// usual first and each with its key; Enter does the one under the cursor.
func TestAltEnterListsWhatCanBeDoneWithTheRow(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Actions · acme/gateway")
	text := a.screenText(sc)
	open, worktree, hide := lineOf(text, "Open in the editor"), lineOf(text, "Create a worktree for a branch"), lineOf(text, "Hide the repository")
	if open < 0 || worktree < 0 || hide < 0 || !(open < worktree && worktree < hide) {
		t.Errorf("the actions are not in the order they are wanted:\n%s", text)
	}
	if l := strings.Split(text, "\n")[worktree]; !strings.Contains(l, "Ctrl-W") {
		t.Errorf("the worktree action does not say its key: %q", l)
	}
	// Not cloned: nothing to pull, nothing to delete.
	for _, absent := range []string{"Pull: bring the clone", "Delete from disk"} {
		if strings.Contains(text, absent) {
			t.Errorf("%q is offered for a repository that is not cloned:\n%s", absent, text)
		}
	}
	assertLegible(t, a, sc, "the actions picker")

	typeRunes(sc, "/worktree")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "Worktree branch - acme/gateway")
}

// TestCtrlAIsAltEnter: for the terminals that keep Alt-Enter.
func TestCtrlAIsAltEnter(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "M")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlA, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Actions · acme/gateway !7")
	waitFor(t, a, sc, "Review: the whole change")
}

// TestColonListsWhatTheScreenCanDo: the screen's own actions, a new
// repository among them though it has no key.
func TestColonListsWhatTheScreenCanDo(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, ":")
	waitFor(t, a, sc, "Create a new repository")
	text := a.screenText(sc)
	if lineOf(text, "Refresh the list") > lineOf(text, "Quit unagit") {
		t.Errorf("quitting is listed before refreshing:\n%s", text)
	}
	if strings.Contains(text, "Go to Repositories") {
		t.Errorf("going where one is is offered:\n%s", text)
	}
}

// TestANewRepositoryIsCreatedClonedAndListed: the form sends what was chosen
// to the group picked, and the repository comes back cloned, in the list,
// under the cursor.
func TestANewRepositoryIsCreatedClonedAndListed(t *testing.T) {
	a, sc, srv := newTestAppSrv(t)
	waitFor(t, a, sc, "acme/gateway")
	// What GitLab would have made, with its README.
	origin := filepath.Join(t.TempDir(), "tool.git")
	gitIn(t, filepath.Dir(origin), "init", "-q", "--bare", "--initial-branch=main", origin)
	seed := filepath.Join(t.TempDir(), "seed")
	gitIn(t, filepath.Dir(seed), "clone", "-q", origin, seed)
	commitIn(t, seed, "README.md", "Initial commit")
	gitIn(t, seed, "push", "-q", "origin", "HEAD:main")
	srv.newRepoURL.Store(origin)

	form := openNewRepository(t, a, sc)
	typeRunes(sc, "tool") // the name is the first field, and is typed into
	waitFor(t, a, sc, "tool")
	onLoop(a, func() bool {
		form.GetFormItemByLabel(labelRepoDesc).(*tview.InputField).SetText("A tool")
		return true
	})
	pressButton(t, a, sc, form, "Create")
	waitFor(t, a, sc, "created and cloned acme/tool")

	body, _ := srv.postedProject.Load().(string)
	for _, want := range []string{`"name":"tool"`, `"namespace_id":1`, `"description":"A tool"`,
		`"visibility":"private"`, `"initialize_with_readme":true`, `"default_branch":"main"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the request lacks %s: %s", want, body)
		}
	}
	dir := onLoop(a, func() string { return a.projectDir(a.cfg.Instances[0].ID, "acme/tool") })
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("not cloned to %s: %v", dir, err)
	}
	got := onLoop(a, func() string {
		i := a.projectsPane.selectedIndex()
		if i < 0 {
			return ""
		}
		return a.projects[i].PathWithNamespace
	})
	if got != "acme/tool" {
		t.Errorf("the cursor is on %q", got)
	}
}

// openNewRepository opens the form from the screen's actions.
func openNewRepository(t *testing.T, a *App, sc tcell.SimulationScreen) *tview.Form {
	t.Helper()
	typeRunes(sc, ":")
	waitFor(t, a, sc, "Create a new repository")
	typeRunes(sc, "/new repository")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "New repository")
	return currentForm(a)
}

// TestTheNewRepositoryFormFitsItsFrame draws the form at several sizes.
func TestTheNewRepositoryFormFitsItsFrame(t *testing.T) {
	for _, size := range []struct{ w, h int }{{160, 44}, {100, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			a, sc := newTestApp(t)
			waitFor(t, a, sc, "acme/gateway")
			resize(sc, size.w, size.h)
			form := openNewRepository(t, a, sc)
			frame := onLoop(a, func() rect {
				x, y, w, h := form.GetRect()
				return rect{x, y, w, h}
			})
			for y := frame.y + 1; y < frame.y+frame.h-1; y++ {
				if r, _ := cellAt(a, sc, frame.x+frame.w-1, y); r != '│' {
					t.Errorf("row %d: the frame's right border is drawn over:\n%s", y, a.screenText(sc))
					break
				}
			}
			text := a.screenText(sc)
			for _, want := range []string{labelRepoName, labelRepoWhere, labelRepoDesc, labelRepoVisibility,
				labelRepoBranch, labelRepoReadme, labelRepoLicense, labelRepoGitignore, "Create", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Errorf("%q is not on screen:\n%s", want, text)
				}
			}
			assertLegible(t, a, sc, "the new repository form")
		})
	}
}

// TestSettingsHasActionsToo: a section lists what its keys do, and an action
// picked there is the key pressed.
func TestSettingsHasActionsToo(t *testing.T) {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	openSection(t, a, sc, sectionTags)
	waitFor(t, a, sc, "a add")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModAlt)
	waitFor(t, a, sc, "Add a tag")
	waitFor(t, a, sc, "Pill ends")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // the first: add a tag
	waitFor(t, a, sc, "Colour")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	typeRunes(sc, "c")
	waitGone(t, a, sc, "Colour")

	typeRunes(sc, ":")
	waitFor(t, a, sc, "Go to Security")
	typeRunes(sc, "/security")
	waitFor(t, a, sc, "FILTER")
	sc.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	waitFor(t, a, sc, "c change passphrase")
}
