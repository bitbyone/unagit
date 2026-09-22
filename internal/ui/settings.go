package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/forge"
)

// The sections of the Settings tab, in the order they are listed.
const (
	sectionGeneral = iota
	sectionGitLab
	sectionGitHub
	sectionGroups
	sectionSecurity
)

var sectionNames = []string{"General", "GitLab servers", "GitHub accounts", "Groups & roots", "Security"}

// settingsView is the whole configuration: a list of sections on the left and
// the section's editor on the right. Nothing here needs the file to be edited
// by hand.
type settingsView struct {
	app     *App
	root    *tview.Flex
	list    *tview.List
	content *tview.Pages
	footer  *tview.TextView

	general  *tview.Form
	gitlab   *tview.Table
	github   *tview.Table
	tree     *tview.TreeView
	security *tview.TextView

	current int
	// contentFocused remembers which side had the keyboard, so closing a
	// dialog puts it back where it was.
	contentFocused bool
}

func (a *App) newSettingsView() *settingsView {
	s := &settingsView{app: a}

	s.list = tview.NewList().ShowSecondaryText(false)
	s.list.SetHighlightFullLine(true).
		SetMainTextColor(colText).
		SetSelectedStyle(styleSelected)
	for _, name := range sectionNames {
		s.list.AddItem(" "+name, "", 0, nil)
	}
	s.list.SetChangedFunc(func(i int, _, _ string, _ rune) { s.show(i) })
	s.list.SetSelectedFunc(func(int, string, string, rune) { s.focusContent() })
	s.list.SetInputCapture(s.listKeys)
	box(s.list.Box, "Settings")

	s.general = s.newGeneralForm()
	s.gitlab = s.newServerTable(config.KindGitLab, "GitLab servers")
	s.github = s.newServerTable(config.KindGitHub, "GitHub accounts")
	s.tree = s.newGroupTree()
	s.security = s.newSecurityPane()

	s.content = tview.NewPages()
	s.content.AddPage("general", s.general, true, true)
	s.content.AddPage("gitlab", s.gitlab, true, false)
	s.content.AddPage("github", s.github, true, false)
	s.content.AddPage("groups", s.tree, true, false)
	s.content.AddPage("security", s.security, true, false)

	s.footer = tview.NewTextView().SetDynamicColors(true)

	s.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().
			AddItem(s.list, 24, 0, true).
			AddItem(s.content, 0, 1, false), 0, 1, true).
		AddItem(s.footer, 1, 0, false)

	s.show(sectionGeneral)
	return s
}

// reload rebuilds every section from the current configuration.
func (s *settingsView) reload() {
	s.fillGeneral()
	s.fillServerTables()
	s.fillTree()
	s.fillSecurity()
	s.updateFooter()
}

// selectSection jumps to a section, for the first-run nudges.
func (s *settingsView) selectSection(section int) {
	s.list.SetCurrentItem(section)
	s.show(section)
}

func (s *settingsView) show(section int) {
	s.current = section
	switch section {
	case sectionGeneral:
		s.content.SwitchToPage("general")
	case sectionGitLab:
		s.content.SwitchToPage("gitlab")
	case sectionGitHub:
		s.content.SwitchToPage("github")
	case sectionGroups:
		s.content.SwitchToPage("groups")
	case sectionSecurity:
		s.content.SwitchToPage("security")
	}
	s.updateFooter()
}

// focusTarget is where focus lands when the Settings tab is shown, or when a
// dialog closes.
func (s *settingsView) focusTarget() tview.Primitive {
	if !s.contentFocused {
		return s.list
	}
	switch s.current {
	case sectionGitLab:
		return s.gitlab
	case sectionGitHub:
		return s.github
	case sectionGroups:
		return s.tree
	case sectionSecurity:
		return s.security
	}
	return s.general
}

func (s *settingsView) focusContent() {
	s.contentFocused = true
	switch s.current {
	case sectionGeneral:
		s.app.tv.SetFocus(s.general)
	case sectionGitLab:
		s.app.tv.SetFocus(s.gitlab)
	case sectionGitHub:
		s.app.tv.SetFocus(s.github)
	case sectionGroups:
		s.app.tv.SetFocus(s.tree)
	case sectionSecurity:
		s.app.tv.SetFocus(s.security)
	}
	s.updateFooter()
}

func (s *settingsView) focusList() {
	s.contentFocused = false
	s.app.tv.SetFocus(s.list)
	s.updateFooter()
}

// updateFooter shows the keys that work where the focus currently is.
func (s *settingsView) updateFooter() {
	if s.footer == nil {
		return
	}
	keys := "j k  move   ·   Enter  edit this section   ·   Esc  back to the lists"
	if !s.list.HasFocus() {
		switch s.current {
		case sectionGeneral:
			keys = "Tab  next field   ·   Enter on a button applies it   ·   Esc  back"
		case sectionGitLab, sectionGitHub:
			keys = "a  add   ·   e  edit   ·   t  token   ·   v  verify   ·   d  remove   ·   Esc  back"
		case sectionGroups:
			keys = "space  select   ·   d  root directory   ·   r  reload groups   ·   p m  refresh indexes   ·   Esc  back"
		case sectionSecurity:
			keys = "c  change the passphrase   ·   Esc  back"
		}
	}
	s.footer.SetText(" " + tag(colDim) + keys + tagEnd)
}

// listKeys drives the section list.
func (s *settingsView) listKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyEsc:
		s.app.switchTab(pageProjects)
		return nil
	case tcell.KeyTab, tcell.KeyRight, tcell.KeyEnter:
		s.focusContent()
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'j':
			s.list.SetCurrentItem(s.list.GetCurrentItem() + 1)
			return nil
		case 'k':
			s.list.SetCurrentItem(s.list.GetCurrentItem() - 1)
			return nil
		case 'l':
			s.focusContent()
			return nil
		case 'q':
			s.app.tv.Stop()
			return nil
		case '?':
			s.app.showHelp()
			return nil
		}
		if s.app.tabKey(ev.Rune()) {
			return nil
		}
		return nil
	}
	return ev
}

// contentKeys is the part every section's pane shares.
func (s *settingsView) contentKeys(ev *tcell.EventKey) (*tcell.EventKey, bool) {
	switch ev.Key() {
	case tcell.KeyEsc, tcell.KeyLeft:
		s.focusList()
		return nil, true
	case tcell.KeyTab:
		s.focusList()
		return nil, true
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			s.app.tv.Stop()
			return nil, true
		case '?':
			s.app.showHelp()
			return nil, true
		}
		if s.app.tabKey(ev.Rune()) {
			return nil, true
		}
	}
	return ev, false
}

// ------------------------------------------------------------------ general

func (s *settingsView) newGeneralForm() *tview.Form {
	form := tview.NewForm()
	styleForm(form)
	box(form.Box, "General").SetBorderPadding(1, 1, 2, 2)
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc {
			s.focusList()
			return nil
		}
		return ev
	})
	return form
}

func (s *settingsView) fillGeneral() {
	cfg := s.app.cfg
	form := s.general
	form.Clear(true)
	form.AddInputField("Default root", cfg.RootDir, 46, nil, nil)
	form.AddInputField("Editor", cfg.Editor, 46, nil, nil)
	form.AddInputField("Editor arguments", strings.Join(cfg.EditorArgs, " "), 46, nil, nil)
	form.AddTextView("", "Projects go under the default root unless a\n"+
		"server or a group overrides it.", 46, 2, true, false)
	form.AddButton("Save", func() {
		cfg.RootDir = strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText())
		cfg.Editor = strings.TrimSpace(form.GetFormItem(1).(*tview.InputField).GetText())
		cfg.EditorArgs = strings.Fields(form.GetFormItem(2).(*tview.InputField).GetText())
		s.app.saveConfig()
		s.reload()
		s.app.note("Saved. Projects already on disk keep their current location.")
	})
	form.AddButton("Revert", func() {
		s.fillGeneral()
		s.app.note("Reverted")
	})
}

// ------------------------------------------------------------------ servers

// kindOfTable says which servers a table shows.
func (s *settingsView) kindOfTable(t *tview.Table) string {
	if t == s.github {
		return config.KindGitHub
	}
	return config.KindGitLab
}

func (s *settingsView) newServerTable(kind, title string) *tview.Table {
	t := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).SetSeparator(' ')
	t.SetSelectedStyle(styleSelected)
	box(t.Box, title).SetBorderPadding(0, 0, 1, 1)
	t.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		inst := s.selectedInstance(t)
		switch ev.Key() {
		case tcell.KeyEnter:
			if inst != nil {
				s.showServerForm(kind, inst)
			}
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'a':
				s.showServerForm(kind, nil)
				return nil
			case 'e':
				if inst != nil {
					s.showServerForm(kind, inst)
				}
				return nil
			case 't':
				if inst != nil {
					s.showTokenForm(inst)
				}
				return nil
			case 'v':
				if inst != nil {
					s.verifyInstance(*inst)
				}
				return nil
			case 'd':
				if inst != nil {
					s.confirmRemoveInstance(*inst)
				}
				return nil
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			}
		}
		return ev
	})
	return t
}

func (s *settingsView) fillServerTables() {
	s.fillServerTable(s.gitlab, config.KindGitLab)
	s.fillServerTable(s.github, config.KindGitHub)
}

func (s *settingsView) fillServerTable(t *tview.Table, kind string) {
	t.Clear()
	headers := []string{"", "NAME", "URL", "TOKEN", "GROUPS", "ROOT"}
	if kind == config.KindGitHub {
		headers = []string{"", "NAME", "ACCOUNT", "TOKEN", "ORGS", "ROOT"}
	}
	for c, h := range headers {
		t.SetCell(0, c, tview.NewTableCell(h).SetTextColor(colDim).SetSelectable(false))
	}
	t.SetCell(0, len(headers), tview.NewTableCell("").SetSelectable(false).SetExpansion(1))

	instances := s.app.cfg.InstancesOfKind(kind)
	if len(instances) == 0 {
		empty := "No servers yet - press a to add one"
		if kind == config.KindGitHub {
			empty = "No GitHub account yet - press a to add one"
		}
		t.SetCell(1, 1, tview.NewTableCell(empty).SetTextColor(colWarn).SetSelectable(false))
		return
	}
	for i, inst := range instances {
		token := tview.NewTableCell("missing").SetTextColor(colBad)
		if s.app.vault != nil && s.app.vault.Has(inst.ID) {
			token = tview.NewTableCell("stored").SetTextColor(colOn)
		}
		root := tildePath(config.Expand(inst.RootDir))
		if inst.RootDir == "" {
			root = tag(colDim) + "(default)" + tagEnd
		}
		who := inst.URL
		if kind == config.KindGitHub {
			who = s.app.githubLogin(inst.ID)
		}
		mark := tview.NewTableCell(" " + tag(colAccent) + "●" + tagEnd).SetReference(inst.ID)
		t.SetCell(i+1, 0, mark)
		t.SetCell(i+1, 1, tview.NewTableCell(inst.Label()).SetTextColor(colText))
		t.SetCell(i+1, 2, tview.NewTableCell(who).SetTextColor(colMuted))
		t.SetCell(i+1, 3, token)
		t.SetCell(i+1, 4, tview.NewTableCell(fmt.Sprintf("%d", len(inst.Groups))).SetTextColor(colMuted))
		t.SetCell(i+1, 5, tview.NewTableCell(root).SetTextColor(colMuted))
		t.SetCell(i+1, 6, tview.NewTableCell("").SetExpansion(1))
	}
	if row, _ := t.GetSelection(); row < 1 || row >= t.GetRowCount() {
		t.Select(1, 0)
	}
}

func (s *settingsView) selectedInstance(t *tview.Table) *config.Instance {
	row, _ := t.GetSelection()
	if row < 1 || row >= t.GetRowCount() {
		return nil
	}
	cell := t.GetCell(row, 0)
	if cell == nil {
		return nil
	}
	id, ok := cell.GetReference().(string)
	if !ok {
		return nil
	}
	return s.app.cfg.Instance(id)
}

// showServerForm adds a server of one kind, or edits the one passed in.
func (s *settingsView) showServerForm(kind string, inst *config.Instance) {
	a := s.app
	adding := inst == nil
	isGitHub := kind == config.KindGitHub

	title := "Edit server"
	current := config.Instance{Kind: kind}
	switch {
	case adding && isGitHub:
		title = "Add a GitHub account"
	case adding:
		title = "Add a GitLab server"
		current.URL = "https://"
	default:
		current = *inst
	}

	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Name", current.Name, 44, nil, nil)
	if !isGitHub {
		form.AddInputField("URL", current.URL, 44, nil, nil)
	}
	form.AddInputField("Root directory", current.RootDir, 44, nil, nil)

	tokenLabel := "Token"
	if !adding && a.vault != nil && a.vault.Has(current.ID) {
		tokenLabel = "Token (stored)"
	}
	form.AddPasswordField(tokenLabel, "", 44, maskRune, nil)
	if isGitHub {
		form.AddTextView("", "Token: a personal access token with repo scope.\n"+
			"github.com only; Enterprise is not supported.\n"+
			"Root directory: blank means the default.", 44, 3, true, false)
	} else {
		form.AddTextView("", "Token: a personal access token (api scope).\n"+
			"Leave it empty to keep the stored one.\n"+
			"Root directory: blank means the default.", 44, 3, true, false)
	}

	// The fields shift by one when there is no URL to ask for.
	field := func(i int) *tview.InputField {
		if isGitHub && i > 0 {
			i--
		}
		return form.GetFormItem(i).(*tview.InputField)
	}

	form.AddButton("Save", func() {
		name := strings.TrimSpace(field(0).GetText())
		url := config.GitHubURL
		if !isGitHub {
			url = strings.TrimRight(strings.TrimSpace(field(1).GetText()), "/")
		}
		root := strings.TrimSpace(field(2).GetText())
		token := field(3).GetText()

		if url == "" || url == "https://" {
			a.errorf("a server needs a URL")
			return
		}
		if name == "" {
			name = config.Host(url)
		}
		target := inst
		if adding {
			target = a.cfg.AddInstance(config.Instance{Kind: kind, Name: name, URL: url, RootDir: root})
		} else {
			target.Name, target.URL, target.RootDir = name, url, root
		}
		if token != "" && a.vault != nil {
			a.vault.Set(target.ID, token)
			a.saveVault()
		}
		a.saveConfig()
		a.closeModal(pageForm)
		s.reload()
		a.note("Saved " + target.Label())
		if a.vault != nil && !a.vault.Has(target.ID) {
			a.flash(target.Label() + " has no token yet - press t to add one")
		}
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })

	a.showFormModal(title, form, 18)
}

// showTokenForm replaces the token of one server.
func (s *settingsView) showTokenForm(inst *config.Instance) {
	a := s.app
	scope := "the api scope"
	if inst.IsGitHub() {
		scope = "the repo scope"
	}
	form := tview.NewForm()
	styleForm(form)
	form.AddPasswordField("Token", "", 46, maskRune, nil)
	form.AddTextView("", "A personal access token with "+scope+".\n"+
		"It is stored encrypted in the vault.", 46, 2, true, false)
	form.AddButton("Save", func() {
		token := form.GetFormItem(0).(*tview.InputField).GetText()
		if token == "" {
			a.errorf("nothing entered")
			return
		}
		a.vault.Set(inst.ID, token)
		a.saveVault()
		a.closeModal(pageForm)
		s.fillServerTables()
		a.note("Token stored for " + inst.Label())
	})
	form.AddButton("Remove token", func() {
		a.vault.Remove(inst.ID)
		a.saveVault()
		a.closeModal(pageForm)
		s.fillServerTables()
		a.note("Token removed for " + inst.Label())
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("Token · "+inst.Label(), form, 11)
}

// verifyInstance checks the stored token against the server.
func (s *settingsView) verifyInstance(inst config.Instance) {
	a := s.app
	client := a.client(inst.ID)
	if client == nil {
		a.errorf("%s has no token yet - press t", inst.Label())
		return
	}
	a.runTask("Verifying "+inst.Label(), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		log("Asking " + inst.URL + " who the token belongs to …")
		user, err := client.CurrentUser(ctx)
		if err != nil {
			return "", err
		}
		log(fmt.Sprintf("The token works: %s (%s)", user.Name, user.Username))
		a.tv.QueueUpdateDraw(func() {
			a.rememberLogin(inst.ID, user.Username)
			s.fillServerTables()
			a.note(inst.Label() + ": signed in as " + user.Username)
		})
		return "", nil
	})
}

func (s *settingsView) confirmRemoveInstance(inst config.Instance) {
	a := s.app
	what := "server"
	if inst.IsGitHub() {
		what = "account"
	}
	body := fmt.Sprintf("Remove [::b]%s[::-] from unagit?\n\n%s\n\nIts token is deleted and its groups are forgotten.\nNothing already cloned to disk is touched.",
		inst.Label(), inst.URL)
	a.confirm("Remove "+what, body, nil, func() {
		a.cfg.RemoveInstance(inst.ID)
		if a.vault != nil {
			a.vault.Remove(inst.ID)
			a.saveVault()
		}
		a.saveConfig()
		s.reload()
		a.note("Removed " + inst.Label())
	})
}

// ------------------------------------------------------------------- groups

func (s *settingsView) newGroupTree() *tview.TreeView {
	tree := tview.NewTreeView()
	box(tree.Box, "Groups & roots").SetBorderPadding(0, 0, 1, 1)
	tree.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		switch ev.Key() {
		case tcell.KeyRune:
			switch ev.Rune() {
			case ' ':
				s.cycleCurrentGroup()
				return nil
			case 'd':
				s.showRootForm()
				return nil
			case 'r':
				s.app.refreshGroups()
				return nil
			case 'p':
				s.app.refreshProjects()
				return nil
			case 'm':
				s.app.refreshMRs()
				return nil
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			}
		}
		return ev
	})
	return tree
}

// treeRef is what a tree node points at: an instance, or a group on it.
type treeRef struct {
	instance string
	group    *forge.Group
}

func (s *settingsView) fillTree() {
	a := s.app
	// Rebuilding the tree would otherwise throw the cursor back to the top.
	prev, hasPrev := s.currentRef()
	var restore *tview.TreeNode
	remember := func(node *tview.TreeNode, ref treeRef) {
		if !hasPrev || restore != nil {
			return
		}
		if ref.instance != prev.instance {
			return
		}
		switch {
		case ref.group == nil && prev.group == nil:
			restore = node
		case ref.group != nil && prev.group != nil && ref.group.ID == prev.group.ID:
			restore = node
		}
	}

	root := tview.NewTreeNode("").SetSelectable(false)
	s.tree.SetRoot(root)

	if len(a.cfg.Instances) == 0 {
		root.AddChild(tview.NewTreeNode("Add a GitLab server first (Settings → GitLab servers)").
			SetColor(colWarn).SetSelectable(false))
		return
	}

	var first *tview.TreeNode
	for _, inst := range a.cfg.Instances {
		instRef := treeRef{instance: inst.ID}
		instNode := tview.NewTreeNode(s.instanceNodeText(inst)).
			SetReference(instRef).
			SetSelectable(true)
		instNode.SetSelectedTextStyle(styleSelected)
		root.AddChild(instNode)
		remember(instNode, instRef)
		if first == nil {
			first = instNode
		}

		groups := s.groupsOf(inst.ID)
		if len(groups) == 0 {
			instNode.AddChild(tview.NewTreeNode("press r to load the groups of this server").
				SetColor(colWarn).SetSelectable(false))
			continue
		}
		byParent := map[int][]forge.Group{}
		known := map[int]bool{}
		for _, g := range groups {
			known[g.ID] = true
		}
		for _, g := range groups {
			parent := g.ParentID
			if !known[parent] {
				parent = 0
			}
			byParent[parent] = append(byParent[parent], g)
		}
		var add func(parent *tview.TreeNode, id, depth int)
		add = func(parent *tview.TreeNode, id, depth int) {
			children := byParent[id]
			sort.Slice(children, func(i, j int) bool { return children[i].FullPath < children[j].FullPath })
			for _, g := range children {
				g := g
				ref := treeRef{instance: inst.ID, group: &g}
				node := tview.NewTreeNode("").
					SetReference(ref).
					SetSelectable(true)
				node.SetSelectedTextStyle(styleSelected)
				remember(node, ref)
				s.labelGroup(node, inst.ID, g)
				node.SetSelectedFunc(func() { s.cycleGroup(node, inst.ID, g) })
				node.SetExpanded(depth < 1)
				parent.AddChild(node)
				add(node, g.ID, depth+1)
			}
		}
		add(instNode, 0, 0)
	}
	switch {
	case restore != nil:
		s.tree.SetCurrentNode(restore)
	case first != nil:
		s.tree.SetCurrentNode(first)
	}
}

func (s *settingsView) instanceNodeText(inst config.Instance) string {
	root := tildePath(s.app.cfg.RootFor(&inst, "x/y"))
	root = strings.TrimSuffix(root, "/x")
	return fmt.Sprintf("%s[::b]%s[::-]%s  %s→ %s%s",
		tag(colAccent), inst.Label(), tagEnd, tag(colDim), root, tagEnd)
}

func (s *settingsView) groupsOf(instanceID string) []forge.Group {
	var out []forge.Group
	for _, g := range s.app.groups {
		if g.Instance == instanceID {
			out = append(out, g)
		}
	}
	return out
}

func (s *settingsView) labelGroup(node *tview.TreeNode, instanceID string, g forge.Group) {
	inst := s.app.cfg.Instance(instanceID)
	if inst == nil {
		return
	}
	suffix := ""
	if sel := inst.Group(g.ID); sel != nil && sel.RootDir != "" {
		suffix = fmt.Sprintf("  %s→ %s%s", tag(colWarn),
			tildePath(s.app.cfg.GroupRoot(inst, *sel)), tagEnd)
	}
	// GitHub has no subgroups, so an organisation is simply on or off.
	if inst.IsGitHub() {
		if inst.HasGroup(g.ID) {
			node.SetText(tag(colOn) + "\u2713" + tagEnd + " " + g.FullPath + suffix)
			node.SetColor(colText)
		} else {
			node.SetText(tag(colDim) + "\u00b7" + tagEnd + " " + g.FullPath + suffix)
			node.SetColor(colMuted)
		}
		return
	}
	switch inst.GroupScope(g.ID) {
	case config.ScopeGroup:
		node.SetText(tag(colOn) + "✓" + tagEnd + " " + g.FullPath +
			"  " + tag(colMuted) + "(this group only)" + tagEnd + suffix)
		node.SetColor(colText)
	case config.ScopeSubgroups:
		node.SetText(tag(colOn) + "✓" + tagEnd + " " + g.FullPath +
			"  " + tag(colAccent) + "(incl. subgroups)" + tagEnd + suffix)
		node.SetColor(colText)
	default:
		node.SetText(tag(colDim) + "·" + tagEnd + " " + g.FullPath + suffix)
		node.SetColor(colMuted)
	}
}

func (s *settingsView) currentRef() (treeRef, bool) {
	node := s.tree.GetCurrentNode()
	if node == nil {
		return treeRef{}, false
	}
	ref, ok := node.GetReference().(treeRef)
	return ref, ok
}

func (s *settingsView) cycleCurrentGroup() {
	ref, ok := s.currentRef()
	if !ok || ref.group == nil {
		s.app.flash("select a group, not a server")
		return
	}
	s.cycleGroup(s.tree.GetCurrentNode(), ref.instance, *ref.group)
}

// cycleGroup steps a group through the three selection states.
func (s *settingsView) cycleGroup(node *tview.TreeNode, instanceID string, g forge.Group) {
	inst := s.app.cfg.Instance(instanceID)
	if inst == nil {
		return
	}
	selection := config.Group{ID: g.ID, FullPath: g.FullPath, Name: g.Name}
	var scope string
	if inst.IsGitHub() {
		scope = inst.ToggleGroup(selection)
	} else {
		scope = inst.CycleGroup(selection)
	}
	s.labelGroup(node, instanceID, g)
	s.app.saveConfig()
	s.fillServerTables()
	if inst.IsGitHub() {
		if scope == "" {
			s.app.note(g.FullPath + " unselected")
		} else {
			s.app.note(g.FullPath + " selected")
		}
		return
	}
	switch scope {
	case config.ScopeGroup:
		s.app.note(g.FullPath + ": only the projects directly in this group - space again to add its subgroups")
	case config.ScopeSubgroups:
		s.app.note(g.FullPath + ": the whole tree below it - space again to unselect")
	default:
		s.app.note(g.FullPath + " unselected")
	}
}

// showRootForm edits the clone directory of the selected group or server.
func (s *settingsView) showRootForm() {
	ref, ok := s.currentRef()
	if !ok {
		return
	}
	a := s.app
	inst := a.cfg.Instance(ref.instance)
	if inst == nil {
		return
	}

	subject, current, inherited := inst.Label(), inst.RootDir, a.cfg.Root()
	if ref.group != nil {
		sel := inst.Group(ref.group.ID)
		if sel == nil {
			a.flash("select the group first (space), then give it a directory")
			return
		}
		subject = ref.group.FullPath
		current = sel.RootDir
		inherited = a.cfg.RootFor(inst, ref.group.FullPath+"/x")
		inherited = strings.TrimSuffix(inherited, "/x")
		if sel.RootDir != "" {
			// Show what it would fall back to, not the override itself.
			saved := sel.RootDir
			sel.RootDir = ""
			inherited = strings.TrimSuffix(a.cfg.RootFor(inst, ref.group.FullPath+"/x"), "/x")
			sel.RootDir = saved
		}
	}

	form := tview.NewForm()
	styleForm(form)
	form.AddInputField("Directory", current, 46, nil, nil)
	form.AddTextView("", "Blank inherits the root it would otherwise get:\n"+
		tildePath(inherited)+"\n"+
		"A relative path is taken from there.\n"+
		"An absolute one, or ~/…, replaces it.", 46, 5, true, false)

	apply := func(value string) {
		if ref.group != nil {
			if sel := inst.Group(ref.group.ID); sel != nil {
				sel.RootDir = value
			}
		} else {
			inst.RootDir = value
		}
		a.saveConfig()
		a.closeModal(pageForm)
		s.reload()
		a.note("Clone directory for " + subject + ": " + tildePath(a.cfg.RootFor(inst, subject+"/x")))
	}
	form.AddButton("Save", func() {
		apply(strings.TrimSpace(form.GetFormItem(0).(*tview.InputField).GetText()))
	})
	form.AddButton("Inherit", func() { apply("") })
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })

	a.showFormModal("Clone directory · "+subject, form, 14)
}

// ----------------------------------------------------------------- security

func (s *settingsView) newSecurityPane() *tview.TextView {
	v := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	v.SetTextColor(colText)
	box(v.Box, "Security").SetBorderPadding(1, 1, 2, 2)
	v.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev, handled := s.contentKeys(ev); handled {
			return ev
		}
		if ev.Key() == tcell.KeyRune && ev.Rune() == 'c' {
			s.showPassphraseForm()
			return nil
		}
		return ev
	})
	return v
}

func (s *settingsView) fillSecurity() {
	stored := 0
	if s.app.vault != nil {
		stored = len(s.app.vault.IDs())
	}
	s.security.SetText(fmt.Sprintf(
		"%sTokens%s   %d stored, encrypted with Argon2id + AES-256-GCM\n"+
			"%sVault%s    %s\n"+
			"%sConfig%s   %s\n\n"+
			"The passphrase is asked for once per start and the tokens only ever\n"+
			"exist in memory. Git gets them through a one-shot credential helper,\n"+
			"so they never reach .git/config or a remote URL.\n\n"+
			"%sc%s  change the passphrase",
		tag(colDim), tagEnd, stored,
		tag(colDim), tagEnd, tildePath(config.VaultPath()),
		tag(colDim), tagEnd, tildePath(config.Path()),
		tag(colAccent), tagEnd))
}

func (s *settingsView) showPassphraseForm() {
	a := s.app
	form := tview.NewForm()
	styleForm(form)
	form.AddPasswordField("New passphrase", "", 40, maskRune, nil)
	form.AddPasswordField("Repeat", "", 40, maskRune, nil)
	form.AddTextView("", "The vault is re-encrypted at once.\n"+
		"There is no recovery if you forget it.", 40, 2, true, false)
	form.AddButton("Change", func() {
		next := form.GetFormItem(0).(*tview.InputField).GetText()
		again := form.GetFormItem(1).(*tview.InputField).GetText()
		if next == "" {
			a.errorf("an empty passphrase is not allowed")
			return
		}
		if next != again {
			a.errorf("the two entries do not match")
			return
		}
		if err := a.vault.Rekey([]byte(next)); err != nil {
			a.errorf("%v", err)
			return
		}
		a.saveVault()
		a.closeModal(pageForm)
		s.fillSecurity()
		a.note("Passphrase changed")
	})
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	a.showFormModal("Change the passphrase", form, 12)
}

// -------------------------------------------------------------------- forms

// styleForm gives a tview form the muted look of the rest of the interface.
func styleForm(form *tview.Form) {
	form.SetLabelColor(colMuted).
		SetFieldBackgroundColor(tcell.Color236).
		SetFieldTextColor(colText).
		SetButtonBackgroundColor(tcell.Color238).
		SetButtonTextColor(colText).
		SetButtonActivatedStyle(styleSelected)
	form.SetBorderPadding(0, 0, 0, 0)
}

// showFormModal centres a form over the dimmed interface.
func (a *App) showFormModal(title string, form *tview.Form, height int) {
	box(form.Box, title).SetBorderPadding(1, 1, 2, 2)
	form.SetCancelFunc(func() { a.closeModal(pageForm) })
	a.pages.AddPage(pageForm, modalFixed(form, 72, height), true, true)
	a.tv.SetFocus(form)
}
