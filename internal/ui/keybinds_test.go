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

// refEntry is one line of the reference.
type refEntry struct {
	id, name string
	keys     []string
}

func entryOf(act uiAction) refEntry {
	return refEntry{act.ident(), act.name, append([]string{act.keys}, act.also...)}
}

func entryOfKey(k pickKey) refEntry {
	return refEntry{k.ident(), k.name, append([]string{k.keys}, k.also...)}
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
			{keymap.SelectionActions, "Actions of the Selection…", []string{"Alt-Enter", "Ctrl-A"}},
			{keymap.ScreenActions, "Actions of the Screen…", []string{":"}},
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
	b.WriteString(`# The keys of unagit's actions as they are when keybinds.toml changes none of
# them; ` + "`unagit keys`" + ` prints this. Copy a line into keybinds.toml beside
# config.yaml and change it there:
#
#   [merge_requests]
#   review = "ctrl+v"           # one key
#   open = ["ctrl+e", "f4"]     # several
#   toggle_draft = []           # none
#
# [global] reaches an action in every section that has it, and a section's
# own line wins over it. A key given to an action is taken from whatever had
# it there. Keys are written ctrl+e, alt+o, alt+enter, ctrl+space, f5,
# shift+f5, alt+pageup, P. Settings keeps its keys.
#
# This file is written by the tests: go test ./internal/ui -run
# TestTheKeybindsReferenceIsCurrent -args -update-keybinds
`)
	global := map[string]string{}
	for _, section := range keybindSections {
		type line struct{ id, value, name string }
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
			lines = append(lines, line{e.id, value, e.name})
		}
		width := 0
		for _, l := range lines {
			width = max(width, len(l.id)+3+len(l.value))
		}
		fmt.Fprintf(&b, "\n[%s]\n", section)
		for _, l := range lines {
			fmt.Fprintf(&b, "%-*s  # %s\n", width, l.id+" = "+l.value, l.name)
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
