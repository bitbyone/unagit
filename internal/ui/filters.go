package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/fuzzy"
)

// hiddenMark is what a project kept out of the lists is drawn with.
const hiddenMark = "⊘"

// passesFilters reports whether a project survives the shared filters. Both
// lists ask the same question, so a merge request disappears with the project
// it belongs to.
func (a *App) passesFilters(instance, path string) bool {
	f := &a.cfg.Filters
	if f.IsHidden(instance, path) {
		return false
	}
	if f.ClonedOnly && !a.diskOf(instance, path).Cloned {
		return false
	}
	return true
}

// filterSummary is the part of a list's header that says what is being left
// out, so a narrowed list never looks like an empty one.
func (a *App) filterSummary() string {
	f := &a.cfg.Filters
	parts := []string{sortLabel(f.Order())}
	if f.ClonedOnly {
		parts = append(parts, tag(colOn)+"cloned only"+tagEnd+tag(colMuted))
	}
	if n := len(f.Hidden); n > 0 {
		parts = append(parts, fmt.Sprintf("%s%s %d%s", tag(colWarn), hiddenMark, n, tagEnd)+tag(colMuted))
	}
	if f.GroupByProject {
		parts = append(parts, tag(colOn)+"grouped"+tagEnd+tag(colMuted))
	}
	return " · " + strings.Join(parts, " · ")
}

func sortLabel(order string) string {
	if order == config.SortName {
		return "by name"
	}
	return "by activity"
}

// applyFilters saves the shared settings and redraws both lists with them.
func (a *App) applyFilters() {
	if err := a.cfg.Save(); err != nil {
		a.errorf("cannot save the filters: %v", err)
		return
	}
	a.projectsPane.reload()
	a.mrsPane.reload()
}

// toggleClonedOnly narrows both lists to what is on disk, or widens them again.
func (a *App) toggleClonedOnly() {
	a.cfg.Filters.ClonedOnly = !a.cfg.Filters.ClonedOnly
	a.applyFilters()
	if a.cfg.Filters.ClonedOnly {
		a.note("Showing only the projects you have cloned")
		return
	}
	a.note("Showing every project again")
}

// hideProject takes the project under the cursor out of both lists, or puts
// it back.
func (a *App) hideProject(instance, path string) {
	if path == "" {
		return
	}
	hidden := a.cfg.Filters.ToggleHidden(instance, path)
	a.applyFilters()
	if hidden {
		a.note(fmt.Sprintf("%s hidden · X manages the hidden ones", path))
		return
	}
	a.note(path + " is back")
}

// toggleGrouping gathers the merge requests under their project, or lets
// them run flat again.
func (a *App) toggleGrouping() {
	a.cfg.Filters.GroupByProject = !a.cfg.Filters.GroupByProject
	a.applyFilters()
	if a.cfg.Filters.GroupByProject {
		a.note("Merge requests grouped by project, sorted " + sortLabel(a.cfg.Filters.Order()) + " inside each")
		return
	}
	a.note("Merge requests listed flat again")
}

// showSortPicker chooses the order both lists are drawn in.
func (a *App) showSortPicker() {
	items := []pickItem{
		{Label: sortLabel(config.SortActivity), Sub: "what moved most recently, first", Data: config.SortActivity},
		{Label: sortLabel(config.SortName), Sub: "by project path, merge requests by number", Data: config.SortName},
	}
	a.showPicker("Sort both lists", items, func(it pickItem) {
		a.cfg.Filters.Sort = it.Data.(string)
		a.applyFilters()
		a.note("Sorted " + sortLabel(a.cfg.Filters.Order()))
	})
}

// showHiddenPicker manages which projects stay out of the lists. It is a
// multiple choice, so space toggles and the modal stays open.
func (a *App) showHiddenPicker() {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true).
		SetMainTextColor(colText).
		SetSelectedStyle(styleSelected)

	input := tview.NewInputField().
		SetLabel(" / ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(colText).
		SetLabelColor(colAccent)

	footer := tview.NewTextView().SetDynamicColors(true)

	// Every project of every server, hidden ones included: this is the one
	// place they can be found again.
	type row struct{ instance, path string }
	var shown []row

	rebuild := func(query string) {
		current := list.GetCurrentItem()
		list.Clear()
		shown = shown[:0]
		type hit struct {
			row   row
			score int
		}
		var hits []hit
		for _, p := range a.projects {
			hay := p.PathWithNamespace + " " + a.instanceLabel(p.Instance)
			score, ok := fuzzy.Match(query, hay)
			if !ok {
				continue
			}
			hits = append(hits, hit{row{p.Instance, p.PathWithNamespace}, score})
		}
		if strings.TrimSpace(query) != "" {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		} else {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].row.path < hits[j].row.path })
		}
		for _, h := range hits {
			marker := tag(colDim) + "·" + tagEnd
			text := h.row.path
			if a.cfg.Filters.IsHidden(h.row.instance, h.row.path) {
				marker = tag(colWarn) + hiddenMark + tagEnd
				text = tag(colMuted) + text + tagEnd
			}
			label := marker + " " + text
			if a.multiInstance() {
				label += "   " + tag(colDim) + a.instanceLabel(h.row.instance) + tagEnd
			}
			shown = append(shown, h.row)
			list.AddItem(label, "", 0, nil)
		}
		if current > 0 && current < list.GetItemCount() {
			list.SetCurrentItem(current)
		}
		footer.SetText(fmt.Sprintf(" %sspace  hide / show   ·   a  show all   ·   /  search   ·   Esc  close%s   %s%d hidden%s",
			tag(colDim), tagEnd, tag(colWarn), len(a.cfg.Filters.Hidden), tagEnd))
	}
	rebuild("")
	input.SetChangedFunc(rebuild)

	toggle := func() {
		i := list.GetCurrentItem()
		if i < 0 || i >= len(shown) {
			return
		}
		a.cfg.Filters.ToggleHidden(shown[i].instance, shown[i].path)
		a.applyFilters()
		rebuild(input.GetText())
	}
	dismiss := func() { a.closeModal(pageHidden) }

	move := func(delta int) {
		if n := list.GetItemCount(); n > 0 {
			list.SetCurrentItem(max(0, min(list.GetCurrentItem()+delta, n-1)))
		}
	}
	setMode := func(filtering bool) {
		if filtering {
			a.tv.SetFocus(input)
			return
		}
		a.tv.SetFocus(list)
	}

	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			setMode(false)
			return nil
		case tcell.KeyEnter:
			toggle()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn:
			if h := list.InputHandler(); h != nil {
				h(ev, func(tview.Primitive) {})
			}
			return nil
		}
		return ev
	})

	list.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			dismiss()
			return nil
		case tcell.KeyEnter:
			toggle()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case ' ':
				toggle()
				return nil
			case 'a':
				if n := a.cfg.Filters.ShowAll(); n > 0 {
					a.applyFilters()
					rebuild(input.GetText())
					a.note(fmt.Sprintf("%d project(s) are back", n))
				}
				return nil
			case '/':
				setMode(true)
				return nil
			case 'j':
				move(1)
				return nil
			case 'k':
				move(-1)
				return nil
			case 'g':
				list.SetCurrentItem(0)
				return nil
			case 'G':
				list.SetCurrentItem(list.GetItemCount() - 1)
				return nil
			case 'q':
				dismiss()
				return nil
			}
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)
	box(flex.Box, "Hidden projects")

	a.pages.AddPage(pageHidden, modalPct(flex, 70, 75), true, true)
	a.tv.SetFocus(list)
}

// selectedProjectOf reports which project the cursor is on, whichever list it
// is in.
func (a *App) selectedProjectOf(p *pane) (instance, path string) {
	i := p.selectedIndex()
	if i < 0 {
		return "", ""
	}
	if p == a.projectsPane && i < len(a.projects) {
		pr := a.projects[i]
		return pr.Instance, pr.PathWithNamespace
	}
	if p == a.mrsPane && i < len(a.mrs) {
		mr := a.mrs[i]
		return mr.Instance, a.projectPathOfMR(mr)
	}
	return "", ""
}

// filterKeysFor wires the shared filter keys into a list.
func (a *App) filterKeysFor(p *pane) func(*tcell.EventKey) bool {
	return func(ev *tcell.EventKey) bool {
		if ev.Key() != tcell.KeyRune {
			return false
		}
		switch ev.Rune() {
		case 'C':
			a.toggleClonedOnly()
			return true
		case 'x':
			instance, path := a.selectedProjectOf(p)
			a.hideProject(instance, path)
			return true
		case 'X':
			a.showHiddenPicker()
			return true
		case 'o':
			a.showSortPicker()
			return true
		}
		return false
	}
}
