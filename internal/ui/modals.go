package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
)

// center wraps a primitive in a box of the given proportions.
func center(p tview.Primitive, widthPct, heightPct int) tview.Primitive {
	rest := 100 - widthPct
	restV := 100 - heightPct
	return tview.NewFlex().
		AddItem(nil, 0, rest/2, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, restV/2, false).
			AddItem(p, 0, heightPct, true).
			AddItem(nil, 0, restV/2, false), 0, widthPct, true).
		AddItem(nil, 0, rest/2, false)
}

// ------------------------------------------------------------------ help

const helpText = `[::b]Navigation[::-]
  [yellow]Tab[-]          switch between Projects and Merge requests
  [yellow]j / k, ↑ / ↓[-] move            [yellow]g / G[-]  first / last
  [yellow]/[-]            filter mode (fuzzy, space separates terms)
  [yellow]Esc[-]          leave filter mode / clear the filter
  [yellow]?[-]            this help            [yellow]q[-]  quit

[::b]Projects[::-]
  [yellow]Enter[-]  clone if missing, fetch + fast-forward, then open the editor
  [yellow]b[-]      choose a branch (switches the branch in the main clone)
  [yellow]m[-]      show only the merge requests of this project
  [yellow]d[-]      delete the clone from disk (incl. its merge request worktrees)
  [yellow]w[-]      open the project in the browser
  [yellow]r[-]      refresh the project index from GitLab

[::b]Merge requests[::-]
  [yellow]Enter[-]  create/update a dedicated worktree for the MR and open the editor
  [yellow]p[-]      limit the list to one project      [yellow]P[-]  clear that limit
  [yellow]d[-]      delete the MR worktree from disk
  [yellow]w[-]      open the merge request in the browser
  [yellow]r[-]      refresh the merge request index from GitLab

[::b]Settings[::-] ([yellow]s[-] from any list)
  [yellow]Space[-]  select / unselect a group (saved immediately)
  [yellow]G[-]      reload the group tree from GitLab
  [yellow]p[-]      refresh projects      [yellow]m[-]  refresh merge requests
  [yellow]Esc[-]    back to the lists

[::b]On disk[::-]
  [green]●[-] present   [darkgray]○[-] not cloned yet
  <root>/<group>/<project>            main clone, branch switching happens here
  <root>/<group>/<project>.mrs/<iid>-<branch>   one worktree per merge request
  Worktrees share the main clone's objects, so uncommitted changes survive
  switching between merge requests.`

func (a *App) showHelp() {
	view := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	view.SetText(helpText)
	view.SetBorder(true).SetTitle(" unagit - keys ").SetTitleAlign(tview.AlignLeft)
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == '?' || ev.Rune() == 'q' {
			a.pages.RemovePage(pageHelp)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageHelp, center(view, 80, 90), true, true)
	a.tv.SetFocus(view)
}

// --------------------------------------------------------------- confirm

// confirm shows a yes/no dialog. warnings are rendered in red.
func (a *App) confirm(title, body string, warnings []string, onYes func()) {
	text := body
	if len(warnings) > 0 {
		text += "\n\n[red]Careful:[-]\n"
		for _, w := range warnings {
			text += "  [red]![-] " + tview.Escape(w) + "\n"
		}
	}
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Cancel", "Delete"}).
		SetDoneFunc(func(i int, label string) {
			a.pages.RemovePage(pageConfirm)
			if label == "Delete" {
				onYes()
			}
		})
	modal.SetBackgroundColor(tcell.ColorBlack)
	modal.SetBorder(true).SetTitle(" " + title + " ")
	modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Rune() {
		case 'y', 'Y':
			a.pages.RemovePage(pageConfirm)
			onYes()
			return nil
		case 'n', 'N', 'q':
			a.pages.RemovePage(pageConfirm)
			return nil
		}
		if ev.Key() == tcell.KeyEsc {
			a.pages.RemovePage(pageConfirm)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageConfirm, modal, true, true)
	a.tv.SetFocus(modal)
}

// ---------------------------------------------------------------- picker

type pickItem struct {
	Label string
	Sub   string
	Data  any
}

// showPicker opens a fuzzy-filtered single choice list.
func (a *App) showPicker(title string, items []pickItem, onSelect func(pickItem)) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkCyan).Foreground(tcell.ColorWhite).Bold(true))

	input := tview.NewInputField().SetLabel("/ ").SetFieldBackgroundColor(tcell.ColorDefault)

	shown := make([]pickItem, 0, len(items))
	rebuild := func(query string) {
		list.Clear()
		shown = shown[:0]
		type hit struct {
			it    pickItem
			score int
		}
		var hits []hit
		for _, it := range items {
			score, ok := fuzzy.Match(query, it.Label+" "+it.Sub)
			if !ok {
				continue
			}
			hits = append(hits, hit{it, score})
		}
		if strings.TrimSpace(query) != "" {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		}
		for _, h := range hits {
			label := h.it.Label
			if h.it.Sub != "" {
				label += "   [darkgray]" + h.it.Sub + "[-]"
			}
			shown = append(shown, h.it)
			list.AddItem(label, "", 0, nil)
		}
	}
	rebuild("")
	input.SetChangedFunc(rebuild)

	dismiss := func() { a.pages.RemovePage(pagePicker) }
	choose := func() {
		i := list.GetCurrentItem()
		if i < 0 || i >= len(shown) {
			return
		}
		it := shown[i]
		dismiss()
		onSelect(it)
	}

	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			dismiss()
			return nil
		case tcell.KeyEnter:
			choose()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn:
			if h := list.InputHandler(); h != nil {
				h(ev, func(tview.Primitive) {})
			}
			return nil
		case tcell.KeyCtrlN:
			list.SetCurrentItem(list.GetCurrentItem() + 1)
			return nil
		case tcell.KeyCtrlP:
			list.SetCurrentItem(list.GetCurrentItem() - 1)
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false)
	flex.SetBorder(true).SetTitle(" " + title + " ").SetTitleAlign(tview.AlignLeft)

	a.pages.AddPage(pagePicker, center(flex, 70, 70), true, true)
	a.tv.SetFocus(input)
}

// ------------------------------------------------------------- formatting

// humanAge renders a timestamp as a coarse relative age.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}
