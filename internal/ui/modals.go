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

const helpText = `[::b]Tabs[::-]
  {A}P{E} Projects    {A}M{E} Merge requests    {A}S{E} Settings

[::b]List[::-]
  {A}j k, ↑ ↓{E}   move              {A}g G{E}  first / last
  {A}/{E}          filter mode (fuzzy, space separates terms)
  {A}Esc{E}        leave filter mode · again clears it · again closes the detail
  {A}Enter{E}      load the detail column on the right and jump into it
  {A}Ctrl-O{E}     clone or update, then open the editor
  {A}l, →{E}       jump to the detail column
  {A}?{E}          this help            {A}q{E}  quit

[::b]Detail column[::-]
  {A}j k g G{E}    scroll             {A}Ctrl-F Ctrl-B{E}  page
  {A}h, ←, Esc{E}  back to the list
  Projects show statistics, languages, the latest pipeline, the last commits
  and the open merge requests. Merge requests are always fetched fresh:
  author, reviewers, approvals, pipeline, description and the newest comments.

[::b]Projects tab[::-]
  {A}Ctrl-O{E}  clone if missing, fetch + fast-forward, then open the editor
  {A}b{E}       choose a branch (switches the branch in the main clone)
  {A}m{E}       show only the merge requests of this project
  {A}d{E}       delete the clone from disk (incl. its merge request worktrees)
  {A}w{E}       open the project in the browser
  {A}r{E}       refresh the project index from GitLab

[::b]Merge requests tab[::-]
  {A}Ctrl-O{E}  create/update a dedicated worktree for the MR, open the editor
  {A}f{E}       limit the list to one project      {A}F{E}  clear that limit
  {A}d{E}       delete the MR worktree from disk
  {A}w{E}       open the merge request in the browser
  {A}r{E}       refresh the merge request index from GitLab

[::b]Modals[::-]
  {A}/{E}      type to filter        {A}j k g G{E}  move        {A}Enter{E}  pick
  {A}Esc{E}    leaves the filter so j/k work, again closes the modal

[::b]Settings tab[::-]
  {A}Space{E}   cycles a group: off → this group only → including subgroups
  {A}r{E}       reload the group tree from GitLab
  {A}p{E}       refresh projects      {A}m{E}  refresh merge requests

[::b]On disk[::-]
  {O}●{E} present   {D}○{E} not cloned yet
  <root>/<group>/<project>            main clone, branch switching happens here
  <root>/<group>/<project>.mrs/<iid>-<branch>   one worktree per merge request
  Worktrees share the main clone's objects, so uncommitted changes survive
  switching between merge requests.`

func (a *App) showHelp() {
	view := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	r := strings.NewReplacer(
		"{A}", tag(colAccent), "{E}", tagEnd,
		"{O}", tag(colOn), "{D}", tag(colDim))
	view.SetText(r.Replace(helpText))
	view.SetTextColor(colText)
	box(view.Box, "unagit - keys")
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == '?' || ev.Rune() == 'q' {
			a.pages.RemovePage(pageHelp)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageHelp, overlay(center(view, 80, 90)), true, true)
	a.tv.SetFocus(view)
}

// --------------------------------------------------------------- confirm

// confirm shows a yes/no dialog. warnings are rendered in red.
func (a *App) confirm(title, body string, warnings []string, onYes func()) {
	text := body
	if len(warnings) > 0 {
		text += "\n\n" + tag(colBad) + "Careful:" + tagEnd + "\n"
		for _, w := range warnings {
			text += "  " + tag(colBad) + "!" + tagEnd + " " + tview.Escape(w) + "\n"
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
	modal.SetTextColor(colText)
	modal.SetButtonBackgroundColor(tcell.ColorDefault)
	modal.SetButtonTextColor(colText)
	box(modal.Box, title)
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
	a.pages.AddPage(pageConfirm, overlay(modal), true, true)
	a.tv.SetFocus(modal)
}

// ---------------------------------------------------------------- picker

type pickItem struct {
	Label string
	Sub   string
	Data  any
}

// showPicker opens a fuzzy-filtered single choice list.
//
// Like the main lists it has two modes: typing filters, Esc leaves the input so
// j/k drive the selection, and a second Esc closes the modal.
func (a *App) showPicker(title string, items []pickItem, onSelect func(pickItem)) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetMainTextColor(colText)
	list.SetSelectedStyle(styleSelected)

	input := tview.NewInputField().
		SetLabel(" / ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(colText).
		SetLabelColor(colAccent)

	footer := tview.NewTextView().SetDynamicColors(true)

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
				label += "   " + tag(colDim) + h.it.Sub + tagEnd
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

	setMode := func(filtering bool) {
		if filtering {
			footer.SetText(" " + tag(colWarn) + "FILTER" + tagEnd + tag(colDim) +
				"   type to narrow · Esc to the list · Enter select" + tagEnd)
			a.tv.SetFocus(input)
			return
		}
		footer.SetText(" " + tag(colMuted) + "NORMAL" + tagEnd + tag(colDim) +
			"   j/k move · / filter · Enter select · Esc close" + tagEnd)
		a.tv.SetFocus(list)
	}

	move := func(delta int) {
		if n := list.GetItemCount(); n > 0 {
			next := list.GetCurrentItem() + delta
			list.SetCurrentItem(max(0, min(next, n-1)))
		}
	}

	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			setMode(false)
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
			move(1)
			return nil
		case tcell.KeyCtrlP:
			move(-1)
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
			choose()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
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
			// Runes are shortcuts in tview's List; nothing here uses them.
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false).
		AddItem(footer, 1, 0, false)
	box(flex.Box, title)

	a.pages.AddPage(pagePicker, overlay(center(flex, 70, 70)), true, true)
	setMode(true)
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
