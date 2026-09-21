package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
)

// settingsView lets the user pick the groups whose projects and merge requests
// end up in the indexes.
type settingsView struct {
	app    *App
	root   *tview.Flex
	tree   *tview.TreeView
	info   *tview.TextView
	reload func()
}

func (a *App) newSettingsView() *settingsView {
	s := &settingsView{app: a}

	s.info = tview.NewTextView().SetDynamicColors(true)
	box(s.info.Box, "Configuration").SetBorderPadding(0, 0, 1, 1)

	s.tree = tview.NewTreeView()
	box(s.tree.Box, "Groups - space cycles: off → this group only → incl. subgroups · r reload · p refresh projects · m refresh merge requests").SetBorderPadding(0, 0, 1, 1)

	s.tree.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			a.switchTab(pageProjects)
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case ' ':
				s.toggleCurrent()
				return nil
			case 'r':
				a.refreshGroups()
				return nil
			case 'p':
				a.refreshProjects()
				return nil
			case 'm':
				a.refreshMRs()
				return nil
			case '?':
				a.showHelp()
				return nil
			case 'q':
				a.tv.Stop()
				return nil
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			}
			if a.tabKey(ev.Rune()) {
				return nil
			}
		}
		return ev
	})

	s.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.info, 8, 0, false).
		AddItem(s.tree, 0, 1, true)

	s.reload = s.build
	s.build()
	return s
}

// build renders the configuration summary and the group tree.
func (s *settingsView) build() {
	s.buildInfo()
	s.buildTree()
}

// buildInfo renders the configuration summary.
func (s *settingsView) buildInfo() {
	a := s.app
	selected := make([]string, 0, len(a.cfg.Groups))
	for _, g := range a.cfg.Groups {
		suffix := " (+sub)"
		if !g.IncludesSubgroups() {
			suffix = ""
		}
		selected = append(selected, g.FullPath+suffix)
	}
	sort.Strings(selected)
	sel := strings.Join(selected, ", ")
	if sel == "" {
		sel = tag(colBad) + "none - select at least one group" + tagEnd
	}
	dim := tag(colDim)
	s.info.SetText(fmt.Sprintf(
		" %sGitLab%s    %s\n %sRoot dir%s  %s\n %sEditor%s    %s %s\n %sConfig%s    %s\n %sIndexes%s   projects %s · merge requests %s\n %sGroups%s    %s",
		dim, tagEnd,
		a.cfg.GitLabURL,
		dim, tagEnd, a.cfg.Root(),
		dim, tagEnd, a.cfg.Editor, strings.Join(a.cfg.EditorArgs, " "),
		dim, tagEnd, config.Path(),
		dim, tagEnd, humanAge(a.projUpdated), humanAge(a.mrsUpdated),
		dim, tagEnd, sel,
	))
}

// buildTree renders the group tree with the current selection.
func (s *settingsView) buildTree() {
	a := s.app
	rootNode := tview.NewTreeNode("groups you can see").SetSelectable(false).SetColor(colDim)
	s.tree.SetRoot(rootNode).SetCurrentNode(rootNode)

	if len(a.groups) == 0 {
		rootNode.AddChild(tview.NewTreeNode("press r to load the group tree from GitLab").
			SetColor(colWarn).SetSelectable(false))
		return
	}

	byParent := map[int][]gitlab.Group{}
	known := map[int]bool{}
	for _, g := range a.groups {
		known[g.ID] = true
	}
	for _, g := range a.groups {
		parent := g.ParentID
		if !known[parent] {
			parent = 0 // a subgroup whose parent we cannot see becomes a root
		}
		byParent[parent] = append(byParent[parent], g)
	}

	var add func(parent *tview.TreeNode, id int, depth int)
	add = func(parent *tview.TreeNode, id int, depth int) {
		children := byParent[id]
		sort.Slice(children, func(i, j int) bool { return children[i].FullPath < children[j].FullPath })
		for _, g := range children {
			g := g
			node := tview.NewTreeNode("").SetReference(g).SetSelectable(true)
			s.label(node, g)
			node.SetSelectedFunc(func() { s.cycle(node, g) })
			if depth < 1 {
				node.SetExpanded(true)
			} else {
				node.SetExpanded(false)
			}
			parent.AddChild(node)
			add(node, g.ID, depth+1)
		}
	}
	add(rootNode, 0, 0)

	if len(rootNode.GetChildren()) > 0 {
		s.tree.SetCurrentNode(rootNode.GetChildren()[0])
	}
}

func (s *settingsView) label(node *tview.TreeNode, g gitlab.Group) {
	node.SetSelectedTextStyle(styleSelected)
	switch s.app.cfg.GroupScope(g.ID) {
	case config.ScopeGroup:
		node.SetText(tag(colOn) + "\u2713" + tagEnd + " " + g.FullPath +
			"  " + tag(colMuted) + "(this group only)" + tagEnd)
		node.SetColor(colText)
	case config.ScopeSubgroups:
		node.SetText(tag(colOn) + "\u2713" + tagEnd + " " + g.FullPath +
			"  " + tag(colAccent) + "(incl. subgroups)" + tagEnd)
		node.SetColor(colText)
	default:
		node.SetText(tag(colDim) + "\u00b7" + tagEnd + " " + g.FullPath)
		node.SetColor(colMuted)
	}
}

// cycle steps a group through the three selection states.
func (s *settingsView) cycle(node *tview.TreeNode, g gitlab.Group) {
	scope := s.app.cfg.CycleGroup(config.Group{ID: g.ID, FullPath: g.FullPath, Name: g.Name})
	s.label(node, g)
	if err := s.app.cfg.Save(); err != nil {
		s.app.errorf("cannot save the config: %v", err)
		return
	}
	s.buildInfo()
	switch scope {
	case config.ScopeGroup:
		s.app.note(g.FullPath + ": only the projects directly in this group - space again to add its subgroups")
	case config.ScopeSubgroups:
		s.app.note(g.FullPath + ": the whole tree below it - space again to unselect")
	default:
		s.app.note(g.FullPath + " unselected")
	}
}

func (s *settingsView) toggleCurrent() {
	node := s.tree.GetCurrentNode()
	if node == nil {
		return
	}
	g, ok := node.GetReference().(gitlab.Group)
	if !ok {
		return
	}
	s.cycle(node, g)
}
