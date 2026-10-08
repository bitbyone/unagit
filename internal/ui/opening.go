package ui

import (
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/agents"
	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/mux"
)

// Opening is three questions: what - the clone, a merge request's branch or
// its review, a worktree - with what - an editor, an agent - and where -
// this terminal, a tab, a split, a window. Every combination an action of
// its own would bury the pickers, so they come in three tiers: Open,
// Review, Open… and Open With… at the top (actions_lists.go); "Open with
// <agent>…" for every agent that is on, at the bottom, asking where; and,
// listed only once something is typed and run at once, every place for the
// favourite editor and every place for every agent. "with" names the tool,
// "in" names the place, and … ends a name exactly when another choice
// follows.

// openTarget is one thing a row opens, by the verb its actions use: a
// clone or a worktree is opened, a merge request's branch opened and its
// review reviewed. open takes the editor - nil for the favourite - and the
// place, which carries the agent when one is to start instead.
type openTarget struct {
	verb string
	open func(ed *editors.Editor, place editorPlace)
}

// placeName is a place as an action names it: "Zellij Vertical Split",
// "Ghostty Window", and herdr alone for the agents' workspace.
func placeName(place editorPlace) string {
	name := place.client.Name()
	switch place.where {
	case mux.Tab:
		return name + " Tab"
	case mux.Vertical:
		return name + " Vertical Split"
	case mux.Horizontal:
		return name + " Horizontal Split"
	case mux.Window:
		if place.client.Kind == mux.Herdr {
			return name
		}
		return name + " Window"
	}
	return name
}

// placeAbout says where a place is, after "in".
func placeAbout(place editorPlace) string {
	name := place.client.Name()
	switch place.where {
	case mux.Tab:
		return "a new tab of " + name
	case mux.Vertical:
		return "a split beside unagit, in " + name
	case mux.Horizontal:
		return "a split below unagit, in " + name
	case mux.Window:
		if place.client.Kind == mux.Herdr {
			return "a tab of herdr's " + mux.AgentsWorkspace + " workspace"
		}
		return "a new " + name + " window"
	}
	return name
}

// placeAliases are the short words a place is typed as; a vertical split
// is the usual one, so "split" alone means it.
func placeAliases(where mux.Placement) []string {
	switch where {
	case mux.Vertical:
		return []string{"vsplit", "vs", "split"}
	case mux.Horizontal:
		return []string{"hsplit", "hs"}
	case mux.Tab:
		return []string{"tab"}
	}
	return nil
}

// placePrefer puts a vertical split before a horizontal one among actions
// found equally well, being the more usual.
func placePrefer(where mux.Placement) int {
	if where == mux.Horizontal {
		return 1
	}
	return 0
}

// agentAliases are the short words an agent is typed as.
var agentAliases = map[string][]string{
	"claude":   {"cc", "claude"},
	"codex":    {"cx", "codex"},
	"copilot":  {"copilot"},
	"opencode": {"oc", "opencode"},
	"agy":      {"agy", "antigravity"},
}

func verbAliases(verb string) []string {
	if verb == "Review" {
		return []string{"rev"}
	}
	return nil
}

// placesOf are the places among picker items, this terminal left out.
func placesOf(items []pickItem) []editorPlace {
	var out []editorPlace
	for _, it := range items {
		if place := it.Data.(editorPlace); place.client != nil {
			out = append(out, place)
		}
	}
	return out
}

// openingActions are the second and third tiers for what a row opens.
func (a *App) openingActions(targets ...openTarget) []uiAction {
	var acts []uiAction
	editorPlaces := placesOf(a.editorPlaces(""))
	for _, t := range targets {
		object := "the directory"
		if t.verb == "Review" {
			object = "the review worktree, made as Ctrl-R makes it - the change unstaged"
		}
		for _, ag := range agents.All {
			if !a.agentOn(ag) {
				continue
			}
			aliases := append(append([]string{}, agentAliases[ag.ID]...), verbAliases(t.verb)...)
			acts = append(acts, uiAction{
				name:  t.verb + " with " + ag.Name + "…",
				about: "Start " + ag.Name + " in " + object + ": in this terminal, or in a tab, split or window of what is here, the usual place first.",
				rank:  600, icon: agentIcons[ag.ID], aliases: aliases,
				run: func() {
					a.pickPlace(placeOfAgent, t.verb+" with "+ag.Name+" · where", a.agentPlaces(ag), func(place editorPlace) {
						place.agent = &ag
						t.open(nil, place)
					})
				},
			})
			for _, place := range placesOf(a.agentPlaces(ag)) {
				acts = append(acts, uiAction{
					name:  t.verb + " with " + ag.Name + " in " + placeName(place),
					about: "Start " + ag.Name + " in " + object + ", in " + placeAbout(place) + ".",
					rank:  700, icon: agentIcons[ag.ID], filterOnly: true, prefer: placePrefer(place.where),
					aliases: append(append([]string{}, aliases...), placeAliases(place.where)...),
					run: func() {
						a.cfg.State.UsePlace(placeOfAgent, placeKeyOf(place))
						a.cfg.State.AgentPlace = placeKeyOf(place)
						a.saveState()
						place.agent = &ag
						t.open(nil, place)
					},
				})
			}
		}
		for _, place := range editorPlaces {
			acts = append(acts, uiAction{
				name:  t.verb + " in " + placeName(place),
				about: "Open " + object + " in the favourite terminal editor, in " + placeAbout(place) + ".",
				rank:  650, filterOnly: true, prefer: placePrefer(place.where),
				aliases: append(placeAliases(place.where), verbAliases(t.verb)...),
				run: func() {
					a.withEditorKind(false, true, func(ed *editors.Editor) { t.open(ed, place) })
				},
			})
		}
	}
	return acts
}

// openTool is something to open with: an editor or an agent.
type openTool struct {
	key, label string
	editor     *editors.Editor
	agent      *agents.Agent
}

// openTools are the editors and the agents that are on, the favourite
// editor first.
func (a *App) openTools() []openTool {
	var tools []openTool
	all := a.editorsOn()
	if fav, ok := editors.Favourite(all, a.cfg.FavouriteEditor); ok {
		tools = append(tools, openTool{key: "editor:" + fav.ID, label: fav.Name + " (default)", editor: &fav})
	}
	for _, e := range all {
		if e.ID != a.cfg.FavouriteEditor {
			tools = append(tools, openTool{key: "editor:" + e.ID, label: e.Name, editor: &e})
		}
	}
	for _, ag := range agents.All {
		if a.agentOn(ag) {
			tools = append(tools, openTool{key: "agent:" + ag.ID, label: ag.Name, agent: &ag})
		}
	}
	return tools
}

// toolPlaces are where a tool can go: an agent anywhere it is offered, a
// terminal editor here or beside unagit, a window editor only into a
// window of its own, which is no choice.
func (a *App) toolPlaces(t openTool) []pickItem {
	switch {
	case t.agent != nil:
		return a.agentPlaces(*t.agent)
	case t.editor.Terminal:
		return a.editorPlaces("")
	}
	return []pickItem{{Label: "Its Own Window", Data: editorPlace{}}}
}

// placeLabel is a place as the Where select names it.
func placeLabel(it pickItem) string {
	if place := it.Data.(editorPlace); place.client != nil {
		return placeName(place)
	}
	return it.Label
}

// Labels of the Open… form.
const (
	labelOpenMode  = "Mode"
	labelOpenWith  = "With"
	labelOpenWhere = "Where"
)

// openAction is Open… for a kind of row, what it is called and what it
// opens.
func (a *App) openAction(kind, label string, targets ...openTarget) uiAction {
	return uiAction{name: "Open…", about: "Choose with what - an editor or an agent - and where it opens, the form filled in as you last used it here.",
		keys: "O", rank: 12, run: func() { a.showOpenForm(kind, label, targets) }}
}

// showOpenForm asks what to open with and where: a merge request's branch
// or its review, an editor or an agent that is on, and the places that tool
// can go - filled in as it was last used for the same kind of row.
func (a *App) showOpenForm(kind, label string, targets []openTarget) {
	tools := a.openTools()
	if len(tools) == 0 {
		a.flash("no editor or agent is installed and on - see Settings › Integrations")
		return
	}
	last := a.cfg.State.OpenForm[kind]
	form := tview.NewForm()
	styleForm(form)
	var mode *tview.DropDown
	if len(targets) > 1 {
		modes := make([]string, len(targets))
		at := 0
		for i, t := range targets {
			modes[i] = t.verb
			if t.verb == last.Mode {
				at = i
			}
		}
		modes[0] = "Branch"
		if last.Mode == "Branch" {
			at = 0
		}
		mode = addSelect(form, labelOpenMode, modes, at)
	}
	labels := make([]string, len(tools))
	toolAt := 0
	for i, t := range tools {
		labels[i] = t.label
		if t.key == last.With {
			toolAt = i
		}
	}
	with := addSelect(form, labelOpenWith, labels, toolAt)
	var places []pickItem
	where := addSelect(form, labelOpenWhere, []string{""}, 0)
	fillWhere := func(tool int, keep string) {
		places = a.toolPlaces(tools[tool])
		names := make([]string, len(places))
		at := 0
		for i, it := range places {
			names[i] = placeLabel(it)
			if placeKeyOf(it.Data.(editorPlace)) == keep {
				at = i
			}
		}
		where.SetOptions(names, nil)
		where.SetCurrentOption(at)
		fitSelects(form)
	}
	fillWhere(toolAt, last.Where)
	with.SetSelectedFunc(func(_ string, i int) {
		if i >= 0 && i < len(tools) {
			fillWhere(i, "")
		}
	})
	form.AddButton("Open", func() {
		target := targets[0]
		choice := openChoiceOf(tools, places, with, where)
		if mode != nil {
			i, verb := mode.GetCurrentOption()
			target, choice.Mode = targets[i], verb
		}
		ti, _ := with.GetCurrentOption()
		pi, _ := where.GetCurrentOption()
		if ti < 0 || pi < 0 || pi >= len(places) {
			return
		}
		tool, place := tools[ti], places[pi].Data.(editorPlace)
		if a.cfg.State.OpenForm == nil {
			a.cfg.State.OpenForm = map[string]config.OpenChoice{}
		}
		a.cfg.State.OpenForm[kind] = choice
		a.closeModal(pageForm)
		if tool.agent != nil {
			a.cfg.State.UsePlace(placeOfAgent, placeKeyOf(place))
			a.cfg.State.AgentPlace = placeKeyOf(place)
			a.saveState()
			place.agent = tool.agent
			target.open(nil, place)
			return
		}
		if place.client != nil {
			a.cfg.State.UsePlace(placeOfEditor, placeKeyOf(place))
		}
		a.saveState()
		target.open(tool.editor, place)
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	height := 9
	if mode != nil {
		height = 11
	}
	a.showFormModalSized("Open · "+label, form, 64, height)
}

// openChoiceOf is what the form holds, to be kept for the next time.
func openChoiceOf(tools []openTool, places []pickItem, with, where *tview.DropDown) config.OpenChoice {
	var c config.OpenChoice
	if i, _ := with.GetCurrentOption(); i >= 0 && i < len(tools) {
		c.With = tools[i].key
	}
	if i, _ := where.GetCurrentOption(); i >= 0 && i < len(places) {
		c.Where = placeKeyOf(places[i].Data.(editorPlace))
	}
	return c
}
