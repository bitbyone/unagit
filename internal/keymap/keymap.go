package keymap

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the file in unagit's configuration directory that holds the
// user's keys.
const FileName = "keybinds.toml"

// Global is the section whose keys reach an action wherever it is; a
// screen's own section wins over it.
const Global = "global"

// The keys of the two action pickers, which every screen answers before
// its actions: an action cannot have one.
const (
	SelectionActions = "actions"
	ScreenActions    = "screen_actions"
)

// Registry is every action that can be bound, by section and id, with the
// keys it has when nothing changes them.
type Registry map[string]map[string][]Key

// Map is the keys keybinds.toml gives, settled: what a section says, else
// what global says.
type Map struct {
	scoped map[string]map[string][]Key
	global map[string][]Key
}

// Keys is the keys of an action in a section, and whether the file chose
// them; an empty list takes the action's key away.
func (m *Map) Keys(section, id string) ([]Key, bool) {
	if m == nil {
		return nil, false
	}
	if keys, ok := m.scoped[section][id]; ok {
		return keys, true
	}
	keys, ok := m.global[id]
	return keys, ok
}

// Read reads keybinds.toml. A file that is not TOML is an error and
// changes nothing; anything else wrong in it - a section or an action that
// does not exist, a key no terminal sends apart, two actions given one key
// - is a problem, said in a sentence, and that one binding is left out, so
// a slip leaves the rest of the file working and every default in place.
func Read(data []byte, known Registry) (*Map, []string, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, nil, fmt.Errorf("%s is not TOML, so no key of it is used: %v", FileName, err)
	}
	var problems []string
	sections := map[string]map[string][]Key{}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		table, ok := raw[name].(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: a binding goes in a section, [%s] or one of %s", name, Global, sectionList(known)))
			continue
		}
		if _, ok := known[name]; !ok {
			problems = append(problems, fmt.Sprintf("[%s] is not a section; they are [%s] and %s", name, Global, sectionList(known)))
			continue
		}
		sections[name] = map[string][]Key{}
		for _, id := range slices.Sorted(maps.Keys(table)) {
			where := fmt.Sprintf("[%s] %s", name, id)
			if !knownID(known, name, id) {
				problems = append(problems, fmt.Sprintf("%s: no action has that name there; unagit keys lists them", where))
				continue
			}
			spells, err := spellings(table[id])
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", where, err))
				continue
			}
			keys := []Key{}
			for _, s := range spells {
				k, err := Parse(s)
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s: %v", where, err))
					continue
				}
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
			if len(keys) == 0 && len(spells) > 0 {
				continue
			}
			sections[name][id] = keys
		}
	}
	m := &Map{scoped: map[string]map[string][]Key{}, global: sections[Global]}
	if m.global == nil {
		m.global = map[string][]Key{}
	}

	// The pickers' keys are answered first everywhere: what the file gives
	// an action of theirs, it cannot have.
	openers := map[Key]bool{}
	for _, id := range []string{SelectionActions, ScreenActions} {
		keys, ok := m.global[id]
		if !ok {
			keys = known[Global][id]
		}
		for _, k := range keys {
			openers[k] = true
		}
	}
	drop := func(where string, keys []Key) []Key {
		return slices.DeleteFunc(slices.Clone(keys), func(k Key) bool {
			if openers[k] {
				problems = append(problems, fmt.Sprintf("%s: %s opens the actions everywhere; give the pickers another key first", where, k.File()))
				return true
			}
			return false
		})
	}
	for _, id := range slices.Sorted(maps.Keys(m.global)) {
		if id != SelectionActions && id != ScreenActions {
			m.global[id] = drop(fmt.Sprintf("[%s] %s", Global, id), m.global[id])
		}
	}
	if keys := m.global[SelectionActions]; keys != nil {
		for _, k := range keys {
			if slices.Contains(m.global[ScreenActions], k) {
				problems = append(problems, fmt.Sprintf("[%s]: %s cannot open both pickers", Global, k.File()))
			}
		}
	}

	// A section's own line wins over global's. Two actions given one key
	// are not a slip here: most of a section's actions are never on screen
	// together (New Worktree…, New Grouped Worktree…), and where they are,
	// the first in the list has it.
	for _, name := range slices.Sorted(maps.Keys(known)) {
		if name == Global {
			continue
		}
		resolved := map[string][]Key{}
		for id, keys := range sections[name] {
			resolved[id] = drop(fmt.Sprintf("[%s] %s", name, id), keys)
		}
		m.scoped[name] = resolved
	}
	slices.Sort(problems)
	return m, slices.Compact(problems), nil
}

func hasID(ids map[string][]Key, id string) bool {
	_, ok := ids[id]
	return ok
}

// knownID reports whether an action can be bound in a section; global's
// are those of every section.
func knownID(known Registry, section, id string) bool {
	if section != Global {
		return hasID(known[section], id)
	}
	for _, ids := range known {
		if hasID(ids, id) {
			return true
		}
	}
	return false
}

// spellings is a binding's value: one key, a list of them, or none.
func spellings(v any) ([]string, error) {
	switch v := v.(type) {
	case string:
		if v == "" {
			return nil, nil
		}
		return []string{v}, nil
	case []any:
		var out []string
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("a key is written in quotes, as \"ctrl+e\"")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("a key is written in quotes, as \"ctrl+e\", or several in a list, as [\"ctrl+e\", \"f5\"]")
}

func sectionList(known Registry) string {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(known)) {
		if name != Global {
			names = append(names, "["+name+"]")
		}
	}
	return strings.Join(names, ", ")
}
