package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Every screen says what can be done in it as data: a list of actions, each
// with its name, the key that does it and how often it is wanted. The same
// list answers the keys and fills two pickers - Alt-Enter (or Ctrl-A) for
// what can be done with the selection, : for what the screen itself can do -
// so a key cannot do one thing while the picker says another, and an action
// too rare for a key of its own still has a place to be found.

// uiAction is one thing that can be done.
type uiAction struct {
	// name is what the action is called, in a word or two; about is the
	// sentence under the picker that says what it does.
	name  string
	about string
	// keys is the key that does it, as the picker shows it and as it is
	// matched: a letter, Ctrl-X, Alt-x, or a word for a key the list itself
	// handles (Enter, space, /). "" leaves the action to the picker alone.
	keys string
	// also is more keys that do it, when the user gave it several in
	// keybinds.toml.
	also []string
	// id is what keybinds.toml calls it, when its name will not do: a
	// name that changes with the state (Mark as Draft, Mark Ready), or one
	// action shown under several names.
	id string
	// rank is how often it is wanted; the pickers list the lower first, so
	// what one usually comes for is near the top.
	rank int
	// when says whether it can be done now; nil is always.
	when func() bool
	run  func()
	// icon goes before the name in the pickers in place of the theme's
	// icon for the name: an action about one thing of several, like an
	// agent, wears that thing's icon. Empty without a Nerd Font.
	icon string
	// filterOnly keeps the action out of the pickers until something is
	// typed: one of many alike - every agent in every place - found by
	// name. aliases are words it is found by besides its name, shown under
	// the picker so they can be learnt, and prefer orders it among those
	// found equally well, the lower first.
	filterOnly bool
	aliases    []string
	prefer     int
}

// available is the actions that can be done now, the most wanted first.
func available(actions []uiAction) []uiAction {
	var out []uiAction
	for _, act := range actions {
		if act.when == nil || act.when() {
			out = append(out, act)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	return out
}

// keyWords are keys the lists handle themselves. An action names them only so
// that the picker shows them; they are never matched here.
var keyWords = map[string]bool{"Enter": true, "space": true, "Esc": true}

// matches reports whether a key event is one of an action's keys.
func (act uiAction) matches(ev *tcell.EventKey) bool { return keysMatch(act.keys, act.also, ev) }

// runKey does the action whose key this is. When several share a key the one
// that can be done now wins; when none can, the first is done anyway, so that
// it says why not - "is not on disk" - rather than the key doing nothing.
// when only keeps the pickers to what can be done.
func runKey(actions []uiAction, ev *tcell.EventKey) bool {
	var fallback *uiAction
	for i, act := range actions {
		if !act.matches(ev) {
			continue
		}
		if act.when == nil || act.when() {
			act.run()
			return true
		}
		if fallback == nil {
			fallback = &actions[i]
		}
	}
	if fallback != nil {
		fallback.run()
		return true
	}
	return false
}

// showActions opens a picker of actions, typing into its filter: an action
// is found by name, Enter does it, Esc goes to the list, where j/k move. The
// key of each is beside it, so the picker teaches the keys as it is used.
func (a *App) showActions(title string, actions []uiAction) {
	acts := available(actions)
	if len(acts) == 0 {
		a.flash("nothing can be done here")
		return
	}
	width := 0
	for _, act := range acts {
		width = max(width, len([]rune(act.name)))
	}
	items := make([]pickItem, len(acts))
	for i, act := range acts {
		keys := strings.Join(append([]string{act.keys}, act.also...), " ")
		if act.keys == "" {
			keys = "·"
		}
		label := fmt.Sprintf("%-*s", width, act.name)
		// With icons, every row has the column, empty where an action has
		// none, so the names stay in line.
		if len(actionIcons) > 0 {
			icon := " "
			if ic := actionIcons[act.name]; ic != "" {
				icon = ic
			}
			if act.icon != "" {
				icon = act.icon
			}
			label = tag(colMuted) + esc(icon) + tagEnd + " " + esc(label)
		}
		about := act.about
		if len(act.aliases) > 0 {
			about += " Typed as " + strings.Join(act.aliases, ", ") + "."
		}
		items[i] = pickItem{Label: label, Sub: keys, About: about, Data: act, Hidden: act.filterOnly, Aliases: act.aliases, Prefer: act.prefer}
	}
	opts := pickerOptions{pack: true, explain: true, filter: true}
	if a.modalOpen() {
		opts.page = pageActions
	}
	a.showPickerWith(title, items, opts, func(it pickItem) { it.Data.(uiAction).run() })
}

// actionKeys answers the keys of a screen: the two pickers, then the action
// whose key it is. It reports whether the key was used.
//
// scope is the section of keybinds.toml the screen's actions are bound in.
// The selection's actions, the screen's and the global ones are bound as
// one list, so a key the user gave one of them is taken from all others.
func (a *App) actionKeys(ev *tcell.EventKey, scope string, selection func() (string, []uiAction), screen func() (string, []uiAction)) bool {
	var selTitle, screenTitle string
	var sel, scr []uiAction
	if selection != nil {
		selTitle, sel = selection()
	}
	if screen != nil {
		screenTitle, scr = screen()
		scr = joinActions(scr, a.globalActions())
	}
	if a.keys != nil {
		all := a.bound(scope, append(append([]uiAction{}, sel...), scr...))
		if sel != nil {
			sel = all[:len(sel)]
		}
		if scr != nil {
			scr = all[len(sel):]
		}
	}
	switch {
	case a.opensSelectionActions(ev) && selection != nil:
		if sel == nil {
			a.flash("nothing is selected")
			return true
		}
		a.showActions(selTitle, sel)
		return true
	case a.opensScreenActions(ev) && screen != nil:
		a.showActions(screenTitle, scr)
		return true
	}
	return runKey(sel, ev) || runKey(scr, ev)
}

// globalActions can be done from any screen: the : picker lists them after
// the screen's own.
func (a *App) globalActions() []uiAction {
	if a.solo != nil {
		// Standalone has no lists to find a repository in, watches nothing
		// and runs no agents; the editors open and the theme are everyone's.
		return []uiAction{a.runningEditorsAction("E"), a.themeAction(), a.redrawAction()}
	}
	return []uiAction{
		a.runningEditorsAction("E"),
		a.runningAgentsAction(""),
		a.newTaskAction(),
		{name: "Find Repository…", about: "Every repository your accounts are members of, in groups you did not choose and hidden ones too: Enter brings it into Repositories.",
			keys: "+", rank: 500, run: a.showFindRepository},
		{name: "Watches…", about: "Everything watched in one list, from any screen: go to one, or stop watching it.", keys: "W", rank: 510, run: a.showWatches},
		a.themeAction(),
		a.redrawAction(),
	}
}

func (a *App) themeAction() uiAction {
	return uiAction{name: "Switch Theme…", about: "Put another theme on, everywhere at once; the list opens on the one on now.",
		rank: 900, run: a.showThemePicker}
}

func (a *App) redrawAction() uiAction {
	return uiAction{name: "Redraw Screen", about: "Send the whole screen to the terminal again, when it shows remains of what was there before.",
		rank: 910, run: func() { a.tv.Sync() }}
}

// showThemePicker lists the themes, each with its colours, the cursor on
// the one that is on; Enter puts another on, as Settings › Theme does.
// Over a main screen the cursor tries each theme on that screen as it comes
// to it, undimmed, and closing the list without Enter puts back the one
// that was on. Over a dialog it does not: drawing the screen again would
// take the dialog with it.
func (a *App) showThemePicker() {
	a.openThemePicker(theme.Name, "", "", !a.modalOpen())
}

// openThemePicker opens the theme list with on as the theme to come back
// to, the cursor on at and the filter as typed - a preview opens it again,
// in the colours tried.
func (a *App) openThemePicker(on, at, query string, preview bool) {
	width := 0
	for _, name := range a.themes.names {
		width = max(width, len([]rune(name)))
	}
	if at == "" {
		at = on
	}
	items := make([]pickItem, len(a.themes.names))
	start := 0
	for i, name := range a.themes.names {
		mark := " "
		if name == on {
			mark = glyphCheck
		}
		if name == at {
			start = i
		}
		// Names padded alike, so the colours stand in a column.
		items[i] = pickItem{Label: esc(fmt.Sprintf("%s %-*s", mark, width, name)), Sub: themeSwatch(a.themes.byName[name], name == at), Data: name}
	}
	opts := pickerOptions{start: start, pack: true, enterHint: "put on", query: query}
	if preview {
		opts.bright = true
		opts.preview = func(it pickItem, query string) {
			name := it.Data.(string)
			setTheme(a.themes.byName[name])
			a.repaintInterface()
			a.openThemePicker(on, name, query, true)
		}
		opts.cancel = func() {
			if theme.Name != on {
				setTheme(a.themes.byName[on])
				a.repaintInterface()
			}
		}
	}
	a.showPickerWith("Theme", items, opts, func(it pickItem) {
		if name := it.Data.(string); name != on {
			a.switchTheme(name)
		}
	})
}

// The main screens name global actions too when they give them a shortcut.
// Their entry wins, so the picker shows the action once with that shortcut.
func joinActions(local, global []uiAction) []uiAction {
	out := append([]uiAction(nil), local...)
	seen := map[string]bool{}
	for _, act := range local {
		seen[act.name] = true
	}
	for _, act := range global {
		if !seen[act.name] {
			out = append(out, act)
		}
	}
	return out
}
