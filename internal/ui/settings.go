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
	s.info.SetBorder(true).SetTitle(" Configuration ").SetTitleAlign(tview.AlignLeft)

	s.tree = tview.NewTreeView()
	s.tree.SetBorder(true).
		SetTitle(" Groups - Space select | G reload from GitLab | p refresh projects | m refresh merge requests ").
		SetTitleAlign(tview.AlignLeft)

	s.tree.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc, tcell.KeyTab:
			a.show(pageProjects)
			a.tv.SetFocus(a.projectsPane.table)
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case ' ':
				s.toggleCurrent()
				return nil
			case 'G':
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
		selected = append(selected, g.FullPath)
	}
	sort.Strings(selected)
	sel := strings.Join(selected, ", ")
	if sel == "" {
		sel = "[red]none - select at least one group[-]"
	}
	s.info.SetText(fmt.Sprintf(
		" [darkgray]GitLab:[-]   %s\n [darkgray]Root dir:[-] %s\n [darkgray]Editor:[-]   %s %s\n [darkgray]Config:[-]   %s\n [darkgray]Indexes:[-]  projects %s, merge requests %s\n [darkgray]Groups:[-]   %s",
		a.cfg.GitLabURL,
		a.cfg.Root(),
		a.cfg.Editor, strings.Join(a.cfg.EditorArgs, " "),
		config.Path(),
		humanAge(a.projUpdated), humanAge(a.mrsUpdated),
		sel,
	))
}

// buildTree renders the group tree with the current selection.
func (s *settingsView) buildTree() {
	a := s.app
	rootNode := tview.NewTreeNode("groups you can see").SetSelectable(false).SetColor(tcell.ColorGray)
	s.tree.SetRoot(rootNode).SetCurrentNode(rootNode)

	if len(a.groups) == 0 {
		rootNode.AddChild(tview.NewTreeNode("press G to load the group tree from GitLab").
			SetColor(tcell.ColorYellow).SetSelectable(false))
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
			node.SetSelectedFunc(func() { s.toggle(node, g) })
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
	name := g.FullPath
	if s.app.cfg.HasGroup(g.ID) {
		node.SetText("[green]✓[-] " + name + "  [darkgray](incl. subgroups)[-]")
		node.SetColor(tcell.ColorWhite)
		return
	}
	node.SetText("[darkgray]·[-] " + name)
	node.SetColor(tcell.ColorSilver)
}

func (s *settingsView) toggle(node *tview.TreeNode, g gitlab.Group) {
	on := s.app.cfg.ToggleGroup(config.Group{ID: g.ID, FullPath: g.FullPath, Name: g.Name})
	s.label(node, g)
	if err := s.app.cfg.Save(); err != nil {
		s.app.errorf("cannot save the config: %v", err)
		return
	}
	s.buildInfo()
	if on {
		s.app.flash(g.FullPath + " selected - press p / m to refresh the indexes")
	} else {
		s.app.flash(g.FullPath + " unselected")
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
	s.toggle(node, g)
}
