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

// ------------------------------------------------------------------ help

const helpText = `[::b]Tabs[::-]
  {A}P{E} Projects    {A}M{E} Merge requests    {A}S{E} Settings

[::b]List[::-]
  {A}j k, ↑ ↓{E}   move              {A}g G{E}  first / last
  {A}/{E}          filter mode (fuzzy, space separates terms)
  {A}Esc{E}        leave filter mode · again clears it · again closes the detail
  {A}Enter{E}      load the detail column on the right and jump into it
               once it is open it follows the cursor, shortly after you stop
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
  {A}Ctrl-O{E}  check the MR branch out in its own worktree, open the editor
  {A}Ctrl-R{E}  open the MR for review: the whole change as pending edits
  {A}c{E}       read the whole conversation, and write a comment
  {A}a{E}       approve the merge request - it asks first
  {A}f{E}       limit the list to one project      {A}F{E}  clear that limit
  {A}d{E}       delete the MR worktrees from disk
  {A}w{E}       open the merge request in the browser
  {A}r{E}       refresh the merge request index

[::b]Comments[::-] ({A}c{E})
  The whole conversation, oldest first, with the markdown rendered: bold is
  bold, lists are lists, code is code. The detail column shows the three
  newest, the rest are in here.
  {A}i{E}  write a comment, {A}Ctrl-S{E} sends it   {A}a{E}  approve   {A}r{E}  reload

[::b]Modals[::-]
  {A}/{E}      type to filter        {A}j k g G{E}  move        {A}Enter{E}  pick
  {A}Esc{E}    leaves the filter so j/k work, again closes the modal

[::b]Settings tab[::-]  everything is configured here, no file to edit
  {A}j k{E}  move between the sections   {A}Enter{E}  edit one   {A}Esc{E}  back
  [::b]General[::-]         the default clone root, the editor and its arguments
  [::b]GitLab servers[::-]  {A}a{E} add   {A}e{E} edit   {A}t{E} token   {A}v{E} verify   {A}d{E} remove
  [::b]GitHub accounts[::-] the same, without a URL: github.com only
                  several servers can be used at once, each with its own
                  token; the lists then show which one a row came from,
                  and pull requests are merge requests here too
  [::b]Groups & roots[::-]  {A}space{E} cycles a GitLab group: off → this group only →
                  including subgroups; a GitHub organisation is on or off
                  {A}d{E} sets the clone directory of a group or of a whole
                  server; blank inherits the level above
                  {A}r{E} reload the groups   {A}p{E} {A}m{E} refresh the indexes
  [::b]Security[::-]        {A}c{E} change the passphrase

[::b]On disk[::-]
  {D}○{E} nothing   {O}●{E} branch worktree   {O}◐{E} review worktree   {O}◉{E} both
  <root>/<group>/<project>                       main clone, branch switching
  <root>/<group>/<project>.mrs/<iid>-<branch>    branch worktree per MR
  <root>/<group>/<project>.reviews/<iid>-<b>     review worktree per MR
  <root> is the default from Settings, unless the server or the group it
  belongs to overrides it.
  Worktrees share the main clone's objects, so uncommitted changes survive
  switching between merge requests.

[::b]Reviewing a merge request[::-]
  {A}Ctrl-O{E} gives you the branch: real commits, you can commit and push.
  {A}Ctrl-R{E} gives you the review worktree: HEAD sits on the commit the MR
  branched from, while the index and the working tree hold the MR. The whole
  change is therefore pending, so gutter signs, {A}]c{E} and diff views work on
  it as one change instead of a stack of commits.

  Both worktrees record what they are in their own git config:
    git config unagit.mr.base   the commit GitLab diffs against
    git config unagit.mr.head   the merge request head
    git config unagit.mr.iid / .target / .url / .mode
  So in a branch worktree you can open the same diff with, for example,
    :DiffviewOpen $(git config unagit.mr.base)...HEAD
  and in a review worktree plain :Gvdiffsplit or :DiffviewOpen is enough.`

func (a *App) showHelp() {
	view := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	r := strings.NewReplacer(
		"{A}", tag(colAccent), "{E}", tagEnd,
		"{O}", tag(colOn), "{D}", tag(colDim))
	view.SetText(r.Replace(helpText))
	view.SetTextColor(colText)
	box(view.Box, "unagit - keys").SetBorderPadding(0, 0, 1, 1)
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == '?' || ev.Rune() == 'q' {
			a.closeModal(pageHelp)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageHelp, modalPct(view, 80, 90), true, true)
	a.tv.SetFocus(view)
}

// --------------------------------------------------------------- confirm

// confirm shows a yes/no dialog for something destructive.
func (a *App) confirm(title, body string, warnings []string, onYes func()) {
	a.confirmWith(title, body, "Delete", warnings, onYes)
}

// confirmWith shows a yes/no dialog whose accepting button says what it does.
func (a *App) confirmWith(title, body, accept string, warnings []string, onYes func()) {
	text := body
	if len(warnings) > 0 {
		text += "\n\n" + tag(colBad) + "Careful:" + tagEnd + "\n"
		for _, w := range warnings {
			text += "  " + tag(colBad) + "!" + tagEnd + " " + tview.Escape(w) + "\n"
		}
	}
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Cancel", accept}).
		SetDoneFunc(func(i int, label string) {
			a.closeModal(pageConfirm)
			if label == accept {
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
			a.closeModal(pageConfirm)
			onYes()
			return nil
		case 'n', 'N', 'q':
			a.closeModal(pageConfirm)
			return nil
		}
		if ev.Key() == tcell.KeyEsc {
			a.closeModal(pageConfirm)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageConfirm, modalFull(modal), true, true)
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

	dismiss := func() { a.closeModal(pagePicker) }
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

	a.pages.AddPage(pagePicker, modalPct(flex, 70, 70), true, true)
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
