package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

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

// matches reports whether a key event is an action's key.
func (act uiAction) matches(ev *tcell.EventKey) bool {
	spec := act.keys
	if spec == "" || keyWords[spec] {
		return false
	}
	if rest, ok := strings.CutPrefix(spec, "Ctrl-"); ok && len(rest) == 1 {
		letter := unicode.ToLower(rune(rest[0]))
		return letter >= 'a' && letter <= 'z' && ev.Key() == tcell.KeyCtrlA+tcell.Key(letter-'a')
	}
	if ev.Key() != tcell.KeyRune || ev.Modifiers()&tcell.ModCtrl != 0 {
		return false
	}
	alt := ev.Modifiers()&tcell.ModAlt != 0
	if rest, ok := strings.CutPrefix(spec, "Alt-"); ok && len([]rune(rest)) == 1 {
		return alt && unicode.ToLower(ev.Rune()) == unicode.ToLower([]rune(rest)[0])
	}
	runes := []rune(spec)
	return !alt && len(runes) == 1 && ev.Rune() == runes[0]
}

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

// opensSelectionActions and opensScreenActions are the keys of the two
// pickers. Alt-Enter is the natural one; Ctrl-A is there for terminals that
// keep Alt-Enter for themselves (full screen, mostly).
func opensSelectionActions(ev *tcell.EventKey) bool {
	return ev.Key() == tcell.KeyEnter && ev.Modifiers()&tcell.ModAlt != 0 || ev.Key() == tcell.KeyCtrlA
}

func opensScreenActions(ev *tcell.EventKey) bool {
	return ev.Key() == tcell.KeyRune && ev.Rune() == ':' && ev.Modifiers()&(tcell.ModAlt|tcell.ModCtrl) == 0
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
		keys := act.keys
		if keys == "" {
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
func (a *App) actionKeys(ev *tcell.EventKey, selection func() (string, []uiAction), screen func() (string, []uiAction)) bool {
	switch {
	case opensSelectionActions(ev) && selection != nil:
		title, acts := selection()
		if acts == nil {
			a.flash("nothing is selected")
			return true
		}
		a.showActions(title, acts)
		return true
	case opensScreenActions(ev) && screen != nil:
		title, acts := screen()
		a.showActions(title, joinActions(acts, a.globalActions()))
		return true
	}
	if selection != nil {
		if _, acts := selection(); runKey(acts, ev) {
			return true
		}
	}
	if screen != nil {
		if _, acts := screen(); runKey(joinActions(acts, a.globalActions()), ev) {
			return true
		}
	}
	return false
}

// globalActions can be done from any screen: the : picker lists them after
// the screen's own.
func (a *App) globalActions() []uiAction {
	return []uiAction{
		a.runningEditorsAction("E"),
		a.runningAgentsAction(""),
		{name: "Watches…", about: "Everything watched in one list, from any screen: go to one, or stop watching it.", keys: "W", rank: 510, run: a.showWatches},
		{name: "Switch Theme…", about: "Put another theme on, everywhere at once; the list opens on the one on now.",
			rank: 900, run: a.showThemePicker},
	}
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
