package ui

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/keymap"
)

// The keys of the actions are the user's to change, in keybinds.toml beside
// config.yaml (website/guide/keys.md). An action is known there by its id - its
// name in snake case unless it says otherwise - in the section of the place
// it is done in; [global] reaches it everywhere. A terminal or a
// multiplexer that keeps a key for itself (Zellij's Ctrl-G, Windows
// Terminal's Alt-Enter) is the reason this exists.

// The sections of keybinds.toml: where an action is done.
const (
	scopeRepositories  = "repositories"
	scopeMergeRequests = "merge_requests"
	scopeWorktrees     = "worktrees"
	scopeActivity      = "activity"
	scopeSandboxes     = "sandboxes"
	scopeWorktreeView  = "worktree_view"
	scopeChanges       = "changes"
	scopeLog           = "log"
	scopeBranches      = "branches"
)

// keybindsReference is every action that can be bound, with its key, as
// keybinds.toml would write it: what `unagit keys` prints and what the file
// is checked against. TestTheKeybindsReferenceIsCurrent writes it.
//
//go:embed keybinds.toml
var keybindsEmbedded string

// keybindsReference is the reference with LF line ends, whatever a
// checkout on Windows gave the file: it is written into the user's file,
// and an update compares against it line by line.
var keybindsReference = strings.ReplaceAll(keybindsEmbedded, "\r\n", "\n")

// KeybindsReference is the reference, for `unagit keys`.
func KeybindsReference() string { return keybindsReference }

var (
	registryOnce sync.Once
	registry     keymap.Registry
	// registryOrder is each section's ids in the reference's order, which
	// is the order of the screen's actions: of two with one key (Clone and
	// Clone All Marked), the one on a single row comes first.
	registryOrder map[string][]string
)

// keyRegistry reads the reference once per process.
func keyRegistry() keymap.Registry {
	registryOnce.Do(func() {
		var raw map[string]map[string]any
		if _, err := toml.Decode(keybindsReference, &raw); err != nil {
			panic("the keybinds reference is not TOML: " + err.Error())
		}
		registry = keymap.Registry{}
		registryOrder = map[string][]string{}
		section := ""
		for _, line := range strings.Split(keybindsReference, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "["):
				section = strings.Trim(line, "[]")
			case section != "" && strings.Contains(line, "=") && !strings.HasPrefix(line, "#"):
				id := strings.TrimSpace(line[:strings.Index(line, "=")])
				registryOrder[section] = append(registryOrder[section], id)
			}
		}
		for section, ids := range raw {
			registry[section] = map[string][]keymap.Key{}
			for id, v := range ids {
				var spells []string
				switch v := v.(type) {
				case string:
					spells = []string{v}
				case []any:
					for _, s := range v {
						spells = append(spells, s.(string))
					}
				}
				keys := []keymap.Key{}
				for _, s := range spells {
					if k, err := keymap.Default(s); err == nil {
						keys = append(keys, k)
					}
				}
				registry[section][id] = keys
			}
		}
	})
	return registry
}

// CheckKeybinds reads the keybinds.toml of a configuration directory and
// says what is wrong in it, for `unagit keys --check`; no file is nothing
// wrong.
func CheckKeybinds(dir string) (path string, problems []string, err error) {
	path = filepath.Join(dir, keymap.FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil, nil
	}
	if err != nil {
		return path, nil, err
	}
	_, problems, err = keymap.Read(data, keyRegistry())
	return path, problems, err
}

// loadKeys reads the user's keys, leaving the defaults wherever the file
// is missing or wrong; what is wrong is said once the interface is up.
func (a *App) loadKeys() {
	a.keys = nil
	path := filepath.Join(a.cfg.Dir(), keymap.FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		a.keysProblem = fmt.Sprintf("cannot read %s, so every key is unagit's own: %v", tildePath(path), err)
		return
	}
	m, problems, err := keymap.Read(data, keyRegistry())
	if err != nil {
		a.keysProblem = tildePath(path) + ": " + err.Error()
		return
	}
	a.keys = m
	if len(problems) > 0 {
		a.keysProblem = fmt.Sprintf("%s: these keep unagit's own keys, the rest of it holds:\n%s",
			tildePath(path), strings.Join(problems, "\n"))
	}
}

// actionID is the id an action is bound by when it names none: its name in
// snake case, Open With… being open_with.
func actionID(name string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		if r == '\'' || r == '’' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if gap && b.Len() > 0 {
				b.WriteByte('_')
			}
			gap = false
			b.WriteRune(r)
			continue
		}
		gap = true
	}
	return b.String()
}

func (act uiAction) ident() string {
	if act.id != "" {
		return act.id
	}
	return actionID(act.name)
}

func (k pickKey) ident() string {
	if k.id != "" {
		return k.id
	}
	return actionID(k.name)
}

// keysMatch reports whether a key event is one of an action's keys, given
// as the hints show them.
func keysMatch(keys string, also []string, ev *tcell.EventKey) bool {
	if keys != "" && !keyWords[keys] {
		if k, ok := keymap.Display(keys); ok && k.Matches(ev) {
			return true
		}
	}
	for _, s := range also {
		if k, ok := keymap.Display(s); ok && k.Matches(ev) {
			return true
		}
	}
	return false
}

// binding is what settle needs of an action or a dialog's key.
type binding struct {
	id   string
	keys string
	also []string
}

// settle gives actions of one place the keys the user chose for them
// there. An action the file names takes the keys it is given, and with
// them the keys from whatever had them by default, which goes without: the
// file is what the user wants that key to do.
func (a *App) settle(scope string, items []binding) {
	if a.keys == nil {
		return
	}
	claimed := map[string]string{}
	chosen := make([]bool, len(items))
	for i, it := range items {
		keys, ok := a.keys.Keys(scope, it.id)
		if !ok {
			continue
		}
		chosen[i] = true
		items[i].keys, items[i].also = "", nil
		for n, k := range keys {
			s := k.String()
			claimed[s] = it.id
			if n == 0 {
				items[i].keys = s
			} else {
				items[i].also = append(items[i].also, s)
			}
		}
	}
	taken := func(id, s string) bool { owner, ok := claimed[s]; return ok && owner != id }
	for i, it := range items {
		if chosen[i] {
			continue
		}
		if taken(it.id, it.keys) {
			items[i].keys = ""
		}
		var also []string
		for _, s := range it.also {
			if !taken(it.id, s) {
				also = append(also, s)
			}
		}
		items[i].also = also
	}
}

// bound is a list of actions with the user's keys for where they are done.
func (a *App) bound(scope string, acts []uiAction) []uiAction {
	if a.keys == nil || acts == nil || scope == "" {
		return acts
	}
	items := make([]binding, len(acts))
	for i, act := range acts {
		items[i] = binding{act.ident(), act.keys, act.also}
	}
	a.settle(scope, items)
	out := make([]uiAction, len(acts))
	for i, act := range acts {
		act.keys, act.also = items[i].keys, items[i].also
		out[i] = act
	}
	return out
}

// boundPickKeys is a dialog's keys with the user's keys for it.
func (a *App) boundPickKeys(scope string, keys []pickKey) []pickKey {
	if a.keys == nil || scope == "" {
		return keys
	}
	items := make([]binding, len(keys))
	for i, k := range keys {
		items[i] = binding{k.ident(), k.keys, k.also}
	}
	a.settle(scope, items)
	out := make([]pickKey, len(keys))
	for i, k := range keys {
		k.keys, k.also = items[i].keys, items[i].also
		out[i] = k
	}
	return out
}

// keyFor is the key an action has in a place, as the hints show it, for a
// hint written out by hand; "" when the user took it away.
func (a *App) keyFor(scope, id, def string) string {
	keys, ok := a.keys.Keys(scope, id)
	if !ok {
		return def
	}
	if len(keys) == 0 {
		return ""
	}
	return keys[0].String()
}

// hintFor is "key what" for a hint, or nothing when the action has no key.
func (a *App) hintFor(scope, id, def, what string) []string {
	if k := a.keyFor(scope, id, def); k != "" {
		return []string{k + " " + what}
	}
	return nil
}

// keyHints is a hint written from actions as they are bound: each pair is
// an action's id and the word the hint gives it, left out when no action of
// the list has that id or it has no key.
func keyHints(acts []uiAction, pairs ...string) []string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		for _, act := range acts {
			if act.ident() == pairs[i] {
				if act.keys != "" {
					out = append(out, act.keys+" "+pairs[i+1])
				}
				break
			}
		}
	}
	return out
}

// openerKeys is the keys of one of the two action pickers.
func (a *App) openerKeys(id string) []keymap.Key {
	if keys, ok := a.keys.Keys(keymap.Global, id); ok {
		return keys
	}
	return keyRegistry()[keymap.Global][id]
}

func (a *App) opens(id string, ev *tcell.EventKey) bool {
	// A right click opens the selection's picker, and only that.
	if ev == mouseActionsKey {
		return id == keymap.SelectionActions
	}
	// A picker's key that is a character - . and : - is that character
	// while something is typed into: a form in INSERT, a filter, a search.
	if ev.Key() == tcell.KeyRune && ev.Modifiers()&(tcell.ModAlt|tcell.ModCtrl) == 0 && a.typing() {
		return false
	}
	for _, k := range a.openerKeys(id) {
		if k.Matches(ev) {
			return true
		}
	}
	return false
}

// typing reports whether the keyboard is with a field that takes what is
// typed: an input or a text area, unless it is a form's in NORMAL, where
// letters press buttons and type nothing.
func (a *App) typing() bool {
	switch a.tv.GetFocus().(type) {
	case *tview.InputField, *tview.TextArea:
		if form, mode := a.focusedForm(); form != nil && !mode.insert {
			return false
		}
		return true
	}
	return false
}

// opensSelectionActions and opensScreenActions are the keys of the two
// pickers: . and Alt-Enter, the second for those used to it (macOS and
// Windows Terminal keep it for themselves); and :.
func (a *App) opensSelectionActions(ev *tcell.EventKey) bool {
	return a.opens(keymap.SelectionActions, ev)
}

func (a *App) opensScreenActions(ev *tcell.EventKey) bool {
	return a.opens(keymap.ScreenActions, ev)
}

// opensPicker reports whether a key opens one of the action pickers.
func (a *App) opensPicker(ev *tcell.EventKey) bool {
	return a.opensSelectionActions(ev) || a.opensScreenActions(ev)
}

// actionsKey is the selection picker's key as the hints name it.
func (a *App) actionsKey() string {
	if keys := a.openerKeys(keymap.SelectionActions); len(keys) > 0 {
		return keys[0].String()
	}
	return ""
}

// helpKeys is a row of the help's keys as the user has them where the row
// is about (scope; keymap.Global for keys that are the same everywhere):
// each key some action has by default becomes that action's key in the
// file - the n-th of its keys the n-th - and "-" when it has none any more,
// or when the file gave the key to another action. A word that is no
// action's key (j, a column's name, a glyph) stays as it is. kept is false
// when every key the row named was taken away, and the row has nothing to
// say.
func (a *App) helpKeys(scope, keys string) (out string, kept bool) {
	if a.keys == nil || scope == "" {
		return keys, true
	}
	reg, order := keyRegistry(), registryOrder
	sections := []string{scope}
	if scope != keymap.Global {
		sections = append(sections, keymap.Global)
	}
	claimed := func(k keymap.Key) bool {
		for _, section := range sections {
			for _, id := range order[section] {
				if bound, ok := a.keys.Keys(scope, id); ok && slices.Contains(bound, k) {
					return true
				}
			}
		}
		return false
	}
	rewrite := func(word string) (string, bool) {
		k, ok := keymap.Display(word)
		if !ok {
			return word, false
		}
		for _, section := range sections {
			for _, id := range order[section] {
				n := slices.Index(reg[section][id], k)
				if n < 0 {
					continue
				}
				bound, chosen := a.keys.Keys(scope, id)
				switch {
				case !chosen && claimed(k):
					return "-", true
				case !chosen:
					return word, true
				case n < len(bound):
					return bound[n].String(), true
				}
				return "-", true
			}
		}
		return word, false
	}
	var b strings.Builder
	named, left := 0, 0
	for i := 0; i < len(keys); {
		j := i
		for j < len(keys) && keys[j] != ' ' {
			j++
		}
		if j > i {
			word, isKey := rewrite(keys[i:j])
			if isKey {
				named++
				if word != "-" {
					left++
				}
			}
			b.WriteString(word)
		}
		for j < len(keys) && keys[j] == ' ' {
			b.WriteByte(' ')
			j++
		}
		i = j
	}
	return b.String(), named == 0 || left > 0
}

// keyName is how a sentence names an action's key - "Ctrl-O makes one" -
// as the user has it, or the action's name when the user took the key away.
func (a *App) keyName(scope, id, def, name string) string {
	if k := a.keyFor(scope, id, def); k != "" {
		return k
	}
	return name
}
