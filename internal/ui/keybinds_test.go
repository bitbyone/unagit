package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/keymap"
	"github.com/tobola/unagit/internal/unibox"
	"github.com/tobola/unagit/internal/watch"
)

var updateKeybinds = flag.Bool("update-keybinds", false, "write keybinds.toml again from the actions")

// keybindSections is the order of the sections in the reference: where one
// is most of the time first.
var keybindSections = []string{keymap.Global, scopeRepositories, scopeMergeRequests, scopeWorktrees, scopeWorktreeView,
	scopeChanges, scopeLog, scopeBranches, scopeActivity, scopeSandboxes}

// keybindSectionAbout says what each section is, above it in the file.
var keybindSectionAbout = map[string]string{
	keymap.Global:      "Everywhere: the two action pickers, and what can be done from any screen. A section below that names the same action wins over this one on its screen.",
	scopeRepositories:  "Repositories (1): the repository under the cursor, the marked ones, and the list.",
	scopeMergeRequests: "Merge requests (2): the merge request under the cursor, the marked ones, and the list.",
	scopeWorktrees:     "Worktrees (3): the worktree under the cursor, the marked ones, and the list.",
	scopeWorktreeView:  "A worktree's view, opened with Enter on a worktree: the repository lit in it.",
	scopeChanges:       "Changes: the files not committed and their diffs, opened from a list or the worktree view; the Changes tab of unagit -s.",
	scopeLog:           "The commit log, opened from a list or the worktree view; the Log tab of unagit -s.",
	scopeBranches:      "A repository's branches, opened from Repositories; the Branches tab of unagit -s.",
	scopeActivity:      "Activity (4): the watches, the agents, the editors open, and the log.",
	scopeSandboxes:     "Sandboxes (5): unibox's environments, sandboxes and tasks.",
}

// briefly is the first clause of an action's about, which is enough to
// find the action by in a file of two hundred; the pickers say the rest.
func briefly(about string) string {
	cut := len(about)
	for _, sep := range []string{"; ", ". ", " - ", ": ", ", or "} {
		if i := strings.Index(about, sep); i >= 20 && i < cut {
			cut = i
		}
	}
	return strings.TrimSuffix(about[:cut], ".") + "."
}

// refEntry is one line of the reference.
type refEntry struct {
	id, name, about string
	keys            []string
}

func entryOf(act uiAction) refEntry {
	return refEntry{act.ident(), act.name, act.about, append([]string{act.keys}, act.also...)}
}

func entryOfKey(k pickKey) refEntry {
	return refEntry{k.ident(), k.name, k.about, append([]string{k.keys}, k.also...)}
}

// fileKeys is an action's keys as the file writes them, without the words
// a list answers itself (Enter, space), which no binding can move.
func (e refEntry) fileKeys() ([]string, bool) {
	var out []string
	for _, s := range e.keys {
		if s == "" {
			continue
		}
		k, ok := keymap.Display(s)
		if !ok {
			return nil, false
		}
		out = append(out, k.File())
	}
	return out, true
}

// keybindEntries is every action of every section, as the fixture's
// screens and dialogs have them.
func keybindEntries(t *testing.T) map[string][]refEntry {
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	newRealProject(t, a, "acme/gateway").rescan()

	// The dialogs are read as they are opened: a log of a clone, a log of
	// a merge request, the branches.
	var dialogs []pickKey
	collect := func(scope string) {
		dialogs = append(dialogs, onLoop(a, func() []pickKey { return a.offered[scope] })...)
	}
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway (main)")
	collect(scopeLog)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")
	logKeys := dialogs

	dialogs = nil
	typeRunes(sc, "b")
	waitFor(t, a, sc, "Branches - acme/gateway")
	collect(scopeBranches)
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Branches - acme/gateway")
	branchKeys := dialogs

	dialogs = logKeys
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway !7")
	waitFor(t, a, sc, "review from here")
	collect(scopeLog)
	logKeys = dialogs

	return onLoop(a, func() map[string][]refEntry {
		out := map[string][]refEntry{}
		add := func(scope string, acts ...[]uiAction) {
			for _, list := range acts {
				for _, act := range list {
					out[scope] = append(out[scope], entryOf(act))
				}
			}
		}
		out[keymap.Global] = []refEntry{
			{keymap.SelectionActions, "Actions of the Selection…", "Every action on what is selected, with its key; a key of one of them can be nothing else.", []string{"Alt-Enter", "Ctrl-A"}},
			{keymap.ScreenActions, "Actions of the Screen…", "Every action of the screen, with its key; a key of one of them can be nothing else.", []string{":"}},
		}
		add(keymap.Global, a.globalActions())

		pr, mr := a.projects[0], a.mrs[0]
		add(scopeRepositories, a.repositoryActions(a.projectsPane, pr), a.repositoriesActions(a.projectsPane),
			a.markedRepositoryActions(a.projectsPane, a.projects))
		add(scopeMergeRequests, a.mergeRequestActions(a.mrsPane, mr), a.mergeRequestsActions(a.mrsPane),
			a.markedMRActions(a.mrsPane, a.mrs))

		single := worktreeRow{Instance: pr.Instance, Path: pr.PathWithNamespace, Branch: "b", Dir: "/x", Group: "g"}
		group := worktreeRow{Path: "g", Dir: "/g", Members: []worktreeRow{single}}
		add(scopeWorktrees, a.worktreeListActions(a.worktreesPane, single), a.worktreeListActions(a.worktreesPane, group),
			a.worktreesActions(a.worktreesPane))
		add(scopeWorktreeView, a.worktreeViewActions(&wtView{row: group, at: 1}), a.worktreeViewActions(&wtView{row: group}),
			a.worktreeViewScreenActions())
		add(scopeChanges, a.changesSelectionActions(&changesView{}), a.changesScreenActions(&changesView{}))
		for _, k := range logKeys {
			out[scopeLog] = append(out[scopeLog], entryOfKey(k))
		}
		for _, k := range branchKeys {
			out[scopeBranches] = append(out[scopeBranches], entryOfKey(k))
		}

		p := a.activityPane
		add(scopeActivity, a.activityScreenActions(p), a.agentActionsOf(p, agentRow{Kind: "claude", Status: "working", Dir: "/x"}),
			a.watchedActions(p, watch.Watch{Instance: pr.Instance, Project: pr.PathWithNamespace, IID: mr.IID}),
			a.watchedActions(p, watch.Watch{Instance: pr.Instance, Project: pr.PathWithNamespace, Branch: "b"}))

		web := unibox.Env{Name: "web", Sandbox: "web", Agent: "claude", Files: []string{"sbxenv.yaml"}}
		box := unibox.Sandbox{Name: "web", Env: "web", Agent: "claude", State: "running"}
		task := unibox.Task{ID: "t-1", Sandbox: "web", Agent: "claude"}
		for _, r := range []sandboxRow{
			{kind: rowEnv, env: web, sandbox: box, made: true},
			{kind: rowSandbox, env: web, sandbox: box, made: true},
			{kind: rowTask, env: web, sandbox: box, made: true, task: task},
		} {
			add(scopeSandboxes, a.sandboxRowActions(r))
		}
		add(scopeSandboxes, a.sandboxesActions(a.sandboxesPane))
		return out
	})
}

// writeKeybindsReference writes the reference from the entries, and says
// where an id stands for two actions with different keys.
func writeKeybindsReference(t *testing.T, entries map[string][]refEntry) string {
	var b strings.Builder
	b.WriteString(`# The keys of unagit's actions as they are when keybinds.toml changes none
# of them; unagit keys prints this, and Settings > Keys writes it into
# keybinds.toml with every binding commented out.
#
# This file is written by the tests: go test ./internal/ui -run
# TestTheKeybindsReferenceIsCurrent -args -update-keybinds
`)
	global := map[string]string{}
	for _, section := range keybindSections {
		type line struct{ id, value, about string }
		var lines []line
		seen := map[string]string{}
		for _, e := range entries[section] {
			keys, ok := e.fileKeys()
			if !ok {
				continue
			}
			quoted := make([]string, len(keys))
			for i, k := range keys {
				quoted[i] = strconv.Quote(k)
			}
			value := "[" + strings.Join(quoted, ", ") + "]"
			if len(keys) == 1 {
				value = quoted[0]
			}
			if section == keymap.Global {
				global[e.id] = value
			} else if global[e.id] == value {
				continue
			}
			if was, ok := seen[e.id]; ok {
				if was != value {
					t.Errorf("[%s] %s is two actions with different keys, %s and %s: give one an id of its own", section, e.id, was, value)
				}
				continue
			}
			seen[e.id] = value
			lines = append(lines, line{e.id, value, e.name + ": " + briefly(e.about)})
		}
		b.WriteString("\n")
		for _, l := range keymap.Describe(keybindSectionAbout[section]) {
			b.WriteString(l + "\n")
		}
		fmt.Fprintf(&b, "[%s]\n", section)
		for _, l := range lines {
			b.WriteString("\n")
			for _, d := range keymap.Describe(l.about) {
				b.WriteString(d + "\n")
			}
			fmt.Fprintf(&b, "%s = %s\n", l.id, l.value)
		}
	}
	return b.String()
}

// TestTheKeybindsReferenceIsCurrent: keybinds.toml beside this file is
// every action that can be bound, with its key, as the code has them; it
// is what a user's keybinds.toml is checked against and what unagit keys
// prints, so a new action or a new key shows there or this fails.
func TestTheKeybindsReferenceIsCurrent(t *testing.T) {
	t.Parallel()
	got := writeKeybindsReference(t, keybindEntries(t))
	if *updateKeybinds {
		if err := os.WriteFile("keybinds.toml", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	// Git on Windows checks the file out with CRLF line ends.
	want := strings.ReplaceAll(keybindsReference, "\r\n", "\n")
	if got != want {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := range max(len(gotLines), len(wantLines)) {
			g, w := "", ""
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Errorf("keybinds.toml line %d is %q, the actions make it %q; write it again with\n"+
					"go test ./internal/ui -run TestTheKeybindsReferenceIsCurrent -args -update-keybinds", i+1, w, g)
				break
			}
		}
	}
}

// withKeybinds writes a keybinds.toml into the test's configuration before
// the app starts.
func withKeybinds(t *testing.T, text string) func(*App) {
	return func(a *App) {
		if err := os.WriteFile(filepath.Join(a.cfg.Dir(), keymap.FileName), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// boundKey is the key an action of a list has, as bound.
func boundKey(a *App, p *pane, id string) string {
	return onLoop(a, func() string {
		_, sel := p.selection()
		_, scr := p.screen()
		for _, act := range a.bound(p.scope, append(sel, joinActions(scr, a.globalActions())...)) {
			if act.ident() == id {
				return strings.Join(append([]string{act.keys}, act.also...), " ")
			}
		}
		return "missing"
	})
}

// TestKeybindsMoveAnAction: an action given another key in keybinds.toml
// answers it, the picker says so, its old key does nothing, and an action
// whose key it took goes without; the pickers move too.
func TestKeybindsMoveAnAction(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, withKeybinds(t, `
[global]
actions = "ctrl+space"

[repositories]
show_commit_log = ["ctrl+t", "f4"]
`))
	waitFor(t, a, sc, "acme/gateway")
	if got := boundKey(a, a.projectsPane, "show_commit_log"); got != "Ctrl-T F4" {
		t.Errorf("Show Commit Log has %q, want Ctrl-T F4", got)
	}
	if got := boundKey(a, a.projectsPane, "edit_tags"); got != "" {
		t.Errorf("Edit Tags kept %q, the key the file gave Show Commit Log", got)
	}
	if got := boundKey(a, a.mrsPane, "show_commit_log"); got != "Ctrl-L" {
		t.Errorf("Merge requests' Show Commit Log has %q; the file changed it in repositories only", got)
	}
	if onLoop(a, func() bool { return a.opensSelectionActions(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModAlt)) }) {
		t.Error("Alt-Enter still opens the actions")
	}

	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlSpace, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Show Commit Log")
	text := a.screenText(sc)
	if line := strings.Split(text, "\n")[lineOf(text, "Show Commit Log")]; !strings.Contains(line, "Ctrl-T F4") {
		t.Errorf("the picker does not show the keys the file gave: %q", line)
	}
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Show Commit Log")

	sc.InjectKey(tcell.KeyF4, 0, tcell.ModNone)
	waitFor(t, a, sc, "Commit Log · acme/gateway")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")
	sc.InjectKey(tcell.KeyCtrlT, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway")
	sc.InjectKey(tcell.KeyEsc, 0, tcell.ModNone)
	waitGone(t, a, sc, "Commit Log")
}

// TestKeybindsReachADialog: a dialog's key moves as a screen's does, and
// its hint follows.
func TestKeybindsReachADialog(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, withKeybinds(t, `
[log]
review_from_here = "f6"
`))
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "2")
	waitFor(t, a, sc, "Rate limiting")
	typeRunes(sc, "g")
	sc.InjectKey(tcell.KeyCtrlL, 0, tcell.ModCtrl)
	waitFor(t, a, sc, "Commit Log · acme/gateway !7")
	waitFor(t, a, sc, "F6 review from here")
	if strings.Contains(a.screenText(sc), "Ctrl-R review") {
		t.Errorf("the hint still names Ctrl-R:\n%s", a.screenText(sc))
	}
}

// TestKeybindsSayWhatIsWrong: a slip in the file is said once the screen
// is up, and the rest of the file holds.
func TestKeybindsSayWhatIsWrong(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, withKeybinds(t, `
[repositories]
show_commit_log = "ctrl+h"
toggle_favourite = "f7"
`))
	waitFor(t, a, sc, "ctrl+h is Backspace")
	if got := boundKey(a, a.projectsPane, "toggle_favourite"); got != "F7" {
		t.Errorf("Toggle Favourite has %q after a slip elsewhere, want F7", got)
	}
	if got := boundKey(a, a.projectsPane, "show_commit_log"); got != "Ctrl-L" {
		t.Errorf("the slip's action has %q, want its own Ctrl-L", got)
	}
}

// TestHintsNameTheBoundKeys: the hints written for a screen name the keys
// as bound, and leave out what has none.
func TestHintsNameTheBoundKeys(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, withKeybinds(t, `
[global]
actions = ["ctrl+space", "alt+enter"]

[worktree_view]
open = "f4"
pull = []
`))
	waitFor(t, a, sc, "acme/gateway")
	pr := onLoop(a, func() forge.Project { return a.projects[0] })
	r := worktreeRow{Instance: pr.Instance, Path: pr.PathWithNamespace, Branch: "b", Dir: "/x"}
	hint := onLoop(a, func() string { return a.worktreeViewHint(&wtView{row: r}) })
	for _, want := range []string{"Ctrl-Space actions", "F4 open", "P push"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint does not say %q: %s", want, hint)
		}
	}
	for _, gone := range []string{"Ctrl-O", "p pull", "Alt-Enter"} {
		if strings.Contains(hint, gone) {
			t.Errorf("the hint still says %q: %s", gone, hint)
		}
	}
}

// TestTheHelpNamesTheBoundKeys: ? says the keys the file gave, where the
// row is about - a row of the log or the Changes keeps theirs - and a
// row whose key is gone, or does something else now, goes with it.
func TestTheHelpNamesTheBoundKeys(t *testing.T) {
	t.Parallel()
	a, sc, _ := newTestAppSrv(t, withKeybinds(t, `
[global]
actions = ["ctrl+space", "alt+enter"]

[repositories]
open = "ctrl+e"
hide_or_unhide = []
commit = "p"
`))
	waitFor(t, a, sc, "acme/gateway")
	rows := onLoop(a, func() []helpLine { return a.helpBound(contextHelpRows(helpRepoList)) })
	keyOf := func(text string) string {
		for _, line := range rows {
			if strings.HasPrefix(line.text, text) {
				return line.keys
			}
		}
		return "gone"
	}
	for text, want := range map[string]string{
		"open as it is on disk":           "Ctrl-E",
		"in changes: open the repo":       "Ctrl-O",
		"every action on the selection":   "Ctrl-Space",
		"the same, where the terminal":    "Alt-Enter",
		"hide or show the repository":     "gone",
		"pull; rebases your work":         "gone",
		"commit versioned files; push":    "p",
		"the same, in an editor you":      "Alt-O",
		"new sandbox task, from anywhere": "Ctrl-S",
	} {
		if got := keyOf(text); got != want {
			t.Errorf("%q: the help says %q, want %q", text, got, want)
		}
	}
}

// TestSettingsWritesEveryActionCommentedOut: w in Settings › Keys starts
// the file naming every action and changing none; uncommenting a line and
// reading the file again (r) puts that key on with no restart, and a file
// that is there already is never written over.
func TestSettingsWritesEveryActionCommentedOut(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	a.tv.QueueUpdateDraw(func() { a.settings.selectSection(sectionKeys); a.settings.focusContent() })
	waitFor(t, a, sc, "none yet")
	typeRunes(sc, "w")
	waitFor(t, a, sc, "read, nothing wrong")
	if testing.Verbose() {
		t.Log("\n" + a.screenText(sc))
	}

	path := filepath.Join(onLoop(a, func() string { return a.cfg.Dir() }), keymap.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, problems, err := keymap.Read(data, keyRegistry())
	if err != nil || len(problems) > 0 {
		t.Fatalf("the template does not read: %v %v", err, problems)
	}
	for section, ids := range keyRegistry() {
		for id := range ids {
			if _, chosen := m.Keys(section, id); chosen {
				t.Errorf("the template binds [%s] %s", section, id)
			}
			if !strings.Contains(string(data), "# "+id+" ") {
				t.Errorf("the template does not name [%s] %s", section, id)
			}
		}
	}

	edited := strings.Replace(string(data), `# open = "ctrl+o"`, `open = "f4"`, 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	typeRunes(sc, "w")
	waitFor(t, a, sc, "is there already")
	if again, _ := os.ReadFile(path); string(again) != edited {
		t.Error("w wrote over the user's file")
	}
	typeRunes(sc, "\x1b")
	waitGone(t, a, sc, "is there already")
	typeRunes(sc, "r")
	waitFor(t, a, sc, "Read ")
	if got := boundKey(a, a.projectsPane, "open"); got != "F4" {
		t.Errorf("open is %q after reading the file again, want F4", got)
	}
}

// TestSettingsBringsAnOldFileUp: a file written by an older release - an
// action it did not have, a key that changed since - says so in Settings ›
// Keys, and u makes it this release's template again, with the line the
// user uncommented kept and on at once.
func TestSettingsBringsAnOldFileUp(t *testing.T) {
	t.Parallel()
	tmpl := keybindsTemplate()
	if got, ch := keymap.Update(tmpl, tmpl); got != tmpl || ch.Rewritten {
		t.Fatalf("the template is not what an update makes of it: %+v", ch)
	}
	clone := "\n" + strings.Join(keymap.Describe("Clone: "+briefly(cloneAbout(t))), "\n") + "\n# clone = \"C\"\n"
	if !strings.Contains(tmpl, clone) {
		t.Fatalf("no clone paragraph in the template:\n%s", clone)
	}
	mine := strings.Replace(tmpl, "\n# open = \"ctrl+o\"\n", "\nopen = \"f4\"\n", 1)
	old := strings.Replace(mine, clone, "", 1)
	old = strings.Replace(old, "# pull = \"p\"", "# pull = \"alt+p\"", 1)

	a, sc, _ := newTestAppSrv(t, withKeybinds(t, old))
	resizeApp(a, sc, 140, 40)
	waitFor(t, a, sc, "acme/gateway")
	typeRunes(sc, "S")
	waitFor(t, a, sc, "Default root")
	a.tv.QueueUpdateDraw(func() { a.settings.selectSection(sectionKeys); a.settings.focusContent() })
	waitFor(t, a, sc, "1 new action, 1 key unagit changed")
	typeRunes(sc, "u")
	waitFor(t, a, sc, "Updated ")
	path := filepath.Join(onLoop(a, func() string { return a.cfg.Dir() }), keymap.FileName)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mine {
		t.Errorf("the update is not this release's file with the user's line:\n%s", got)
	}
	if k := boundKey(a, a.projectsPane, "open"); k != "F4" {
		t.Errorf("open is %q, want F4", k)
	}
}

func cloneAbout(t *testing.T) string {
	for _, e := range keybindEntries(t)[scopeRepositories] {
		if e.id == "clone" {
			return e.about
		}
	}
	t.Fatal("no clone action")
	return ""
}
