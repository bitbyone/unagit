package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/fuzzy"
)

// ------------------------------------------------------------------ help

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
	keys := buttonKeys([]string{"Cancel", accept})
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{markKey("Cancel", keys[0]), markKey(accept, keys[1])}).
		SetDoneFunc(func(i int, label string) {
			a.closeModal(pageConfirm)
			if i == 1 {
				onYes()
			}
		})
	modal.SetTextColor(colText)
	modal.SetButtonActivatedStyle(styleSelected)
	box(modal.Box, title)
	modal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc {
			a.closeModal(pageConfirm)
			return nil
		}
		if ev.Key() != tcell.KeyRune || ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			return ev
		}
		key := unicode.ToLower(ev.Rune())
		switch {
		case key == keys[0] || key == 'n' || key == 'q':
			a.closeModal(pageConfirm)
			return nil
		case key == keys[1] || key == 'y':
			a.closeModal(pageConfirm)
			onYes()
			return nil
		}
		return ev
	})

	hint := fmt.Sprintf("c cancel · %c %s · Esc back", keys[1], strings.ToLower(accept))
	a.pages.AddPage(pageConfirm, modalFull(&confirmationHint{Modal: modal, hint: hint}), true, true)
	a.tv.SetFocus(modal)
}

// ---------------------------------------------------------------- picker

type pickItem struct {
	Label string
	Sub   string
	// About is what the item is, in a sentence, for a picker that explains
	// its items; it is searched as well.
	About string
	Data  any
}

// showPicker opens a fuzzy-filtered single choice list.
//
// Like the main lists it has two modes, and it opens in the list one: j/k move,
// Enter picks, / starts the filter, Esc there goes back to the list, and Esc
// in the list closes the modal. Opening on the filter made every letter a
// search, so the keys the footer offers did nothing until Esc.
func (a *App) showPicker(title string, items []pickItem, onSelect func(pickItem)) {
	a.showPickerActions(title, items, onSelect, nil, nil)
}

// showPickerActions is showPicker with two extra, optional keys available
// while browsing (not filtering): 'n' calls onNew instead of picking
// anything, and 'd' calls onDelete with the highlighted item instead of
// onSelect. Both dismiss the picker first, the same order choose() uses, so
// whatever dialog they open lands cleanly on the page underneath. Either may
// be nil, in which case its key does nothing - the callers that only need a
// plain choice list are unaffected.
func (a *App) showPickerActions(title string, items []pickItem, onSelect func(pickItem), onNew func(), onDelete func(pickItem)) {
	a.showPickerWith(title, items, pickerOptions{onNew: onNew, onDelete: onDelete}, onSelect)
}

// showPickerAt is showPicker with the cursor on items[start] rather than the
// first one, for a list whose likely choice is somewhere in the middle.
func (a *App) showPickerAt(title string, items []pickItem, start int, onSelect func(pickItem)) {
	a.showPickerWith(title, items, pickerOptions{start: start}, onSelect)
}

// pickerOptions are the ways a picker can differ from the plain one.
type pickerOptions struct {
	start    int            // the item the cursor starts on
	onNew    func()         // n while browsing, when set
	onDelete func(pickItem) // d while browsing, when set
	again    rune           // while browsing, picks like Enter: the key that opened it
	// keys are more keys while browsing, each acting on the item under the
	// cursor, the picker closed first; written as an action's key is.
	keys []pickKey
	// footer replaces the hint of what Enter does, for a picker whose Enter
	// is not a choice (onSelect nil).
	enterHint string
	// pack sizes the picker to what it holds - as wide as its longest row,
	// as tall as its rows - rather than to most of the screen, so short rows
	// are not left at the edge of a wide empty box.
	pack bool
	// wide is a picker with many keys: most of the screen across, so their
	// hints stay on a line or two.
	wide bool
	// relabel, when set, writes the items' labels again for a row of width
	// cells, whenever the picker is drawn at a width it was not before - a
	// list whose columns follow the terminal.
	relabel func(items []pickItem, width int)
	// explain keeps a pane at the bottom with the About of the item under
	// the cursor, so the items themselves can be named in a word.
	explain bool
	// back, when set, is where Esc goes after closing the picker: a list
	// opened from another goes back to it.
	back func()
	// same tells whether two items are the same thing, for a picker whose
	// items are put again while it is open: the cursor stays on it. Unset,
	// the labels are compared.
	same func(a, b pickItem) bool
}

// livePicker is a picker that is open, whose items can be put again while
// it is - a list of something that changes as it is watched. Both run on
// the event loop.
type livePicker struct {
	// open reports whether the picker is still on screen.
	open func() bool
	// set puts new items and a new title in, the cursor kept on the item
	// it was on and the filter kept as typed.
	set func(title string, items []pickItem)
}

// widePct is how much of the screen across a wide picker takes.
const widePct = 90

// explainLines is the most an explanation may take; it is a sentence, not a
// paragraph.
const explainLines = 3

// pickKey is a key of a picker that acts on the item under the cursor.
type pickKey struct {
	keys string
	hint string
	run  func(pickItem)
	// stay keeps the picker open, the cursor where it was: for a key whose
	// work happens elsewhere, like opening the browser.
	stay bool
}

func (a *App) showPickerWith(title string, items []pickItem, opts pickerOptions, onSelect func(pickItem)) *livePicker {
	start, onNew, onDelete := opts.start, opts.onNew, opts.onDelete
	list := tview.NewList().ShowSecondaryText(false)
	list.SetHighlightFullLine(true)
	list.SetMainTextColor(colText)
	list.SetSelectedStyle(styleSelected)

	input := filterField(tview.NewInputField())

	footer := tview.NewTextView().SetDynamicColors(true)

	shown := make([]pickItem, 0, len(items))
	rebuild := func(query string) {
		list.Clear()
		shown = shown[:0]
		// What an item is called comes before what its explanation says:
		// searching for a worktree finds the action named so before one that
		// mentions a worktree in passing.
		type hit struct {
			it    pickItem
			named bool
			score int
		}
		var hits []hit
		for _, it := range items {
			score, ok := fuzzy.Match(query, it.Label+" "+it.Sub)
			named := ok
			if !ok && it.About != "" {
				score, ok = fuzzy.Match(query, it.About)
			}
			if !ok {
				continue
			}
			hits = append(hits, hit{it, named, score})
		}
		if strings.TrimSpace(query) != "" {
			sort.SliceStable(hits, func(i, j int) bool {
				if hits[i].named != hits[j].named {
					return hits[i].named
				}
				return hits[i].score > hits[j].score
			})
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
	if start > 0 && start < list.GetItemCount() {
		list.SetCurrentItem(start)
	}
	input.SetChangedFunc(rebuild)

	dismiss := func() { a.closeModal(pagePicker) }
	choose := func() {
		i := list.GetCurrentItem()
		if i < 0 || i >= len(shown) || onSelect == nil {
			return
		}
		it := shown[i]
		dismiss()
		onSelect(it)
	}

	filtering := false
	normalHint := func() string {
		hint := "   j/k move · / filter · Enter select"
		switch {
		case onSelect == nil:
			hint = "   j/k move · / filter"
		case opts.enterHint != "":
			hint = "   j/k move · / filter · Enter " + opts.enterHint
		case opts.again != 0:
			hint = fmt.Sprintf("   j/k move · / filter · Enter or %c select", opts.again)
		}
		for _, k := range opts.keys {
			hint += " · " + k.keys + " " + k.hint
		}
		if onNew != nil {
			hint += " · n new"
		}
		if onDelete != nil {
			hint += " · d delete"
		}
		return " " + tag(colMuted) + "NORMAL" + tagEnd + tag(colDim) + hint + " · Esc close" + tagEnd
	}
	setMode := func(filter bool) {
		filtering = filter
		if filtering {
			footer.SetText(" " + tag(colWarn) + "FILTER" + tagEnd + tag(colDim) +
				"   type to narrow · Esc to the list · Enter select" + tagEnd)
			a.tv.SetFocus(input)
			return
		}
		footer.SetText(normalHint())
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
		for _, k := range opts.keys {
			if (uiAction{keys: k.keys}).matches(ev) {
				if i := list.GetCurrentItem(); i >= 0 && i < len(shown) {
					it := shown[i]
					if !k.stay {
						dismiss()
					}
					k.run(it)
				}
				return nil
			}
		}
		switch ev.Key() {
		case tcell.KeyEsc:
			dismiss()
			if opts.back != nil {
				opts.back()
			}
			return nil
		case tcell.KeyEnter:
			choose()
			return nil
		case tcell.KeyRune:
			if opts.again != 0 && ev.Rune() == opts.again {
				choose()
				return nil
			}
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
			case 'n':
				if onNew != nil {
					dismiss()
					onNew()
				}
				return nil
			case 'd':
				if onDelete != nil {
					if i := list.GetCurrentItem(); i >= 0 && i < len(shown) {
						it := shown[i]
						dismiss()
						onDelete(it)
					}
				}
				return nil
			}
			// Runes are shortcuts in tview's List; nothing here uses them.
			return nil
		}
		return ev
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, false).
		AddItem(list, 0, 1, true)

	// The width a packed picker needs: its longest row, its title, and room
	// for every explanation to fit in explainLines.
	inner := 0
	for _, it := range items {
		row := it.Label
		if it.Sub != "" {
			row += "   " + it.Sub
		}
		inner = max(inner, tview.TaggedStringWidth(row)+1)
	}
	inner = max(inner, tview.TaggedStringWidth(title)+4, 40)
	if opts.explain {
		for _, it := range items {
			for inner < 76 && len(tview.WordWrap(it.About, inner)) > explainLines {
				inner += 2
			}
		}
	}
	inner = min(inner, 76)

	extra := 0
	explain := func(int) {}
	if opts.explain {
		about := tview.NewTextView().SetWrap(true).SetWordWrap(true).SetTextColor(colMuted)
		explain = func(i int) {
			if i >= 0 && i < len(shown) {
				about.SetText(shown[i].About)
				return
			}
			about.SetText("")
		}
		list.SetChangedFunc(func(i int, _, _ string, _ rune) { explain(i) })
		input.SetChangedFunc(func(query string) {
			rebuild(query)
			explain(list.GetCurrentItem())
		})
		explain(list.GetCurrentItem())
		lines := 1
		for _, it := range items {
			lines = max(lines, len(tview.WordWrap(it.About, inner)))
		}
		lines = min(lines, explainLines)
		// A rule above the explanation and one below it, so it reads as a
		// pane of its own and not as the start of the key hints.
		flex.AddItem(rule(), 1, 0, false).AddItem(about, lines, 0, false).AddItem(rule(), 1, 0, false)
		extra = 2 + lines
	}
	flex.AddItem(footer, 1, 0, false)
	box(flex.Box, title)

	pad := 0
	if opts.pack || opts.explain {
		pad = 1
	}
	fitFooterPadded(flex, footer, 1, pad)
	frame := &pickerFrame{Flex: flex, target: func() tview.Primitive {
		if filtering {
			return input
		}
		return list
	}}
	if opts.relabel != nil {
		frame.resized = func(width int) {
			opts.relabel(items, width)
			at := list.GetCurrentItem()
			rebuild(input.GetText())
			if at >= 0 && at < list.GetItemCount() {
				list.SetCurrentItem(at)
			}
		}
	}
	var page tview.Primitive
	if opts.pack {
		footerLines := len(tview.WordWrap(normalHint(), inner))
		page = modalFixed(frame, inner+2+2*pad, 2+1+len(items)+extra+footerLines)
	} else if opts.wide {
		page = modalPct(frame, widePct, 75)
	} else {
		page = modalPct(frame, 70, 70)
	}
	a.pages.AddPage(pagePicker, page, true, true)
	setMode(false)

	same := opts.same
	if same == nil {
		same = func(a, b pickItem) bool { return a.Label == b.Label }
	}
	return &livePicker{
		open: func() bool { return a.pages.GetPage(pagePicker) == page },
		set: func(title string, next []pickItem) {
			var current *pickItem
			if i := list.GetCurrentItem(); i >= 0 && i < len(shown) {
				it := shown[i]
				current = &it
			}
			at := list.GetCurrentItem()
			items = next
			rebuild(input.GetText())
			if current != nil {
				for i, it := range shown {
					if same(it, *current) {
						at = i
						break
					}
				}
			}
			if at >= 0 && at < list.GetItemCount() {
				list.SetCurrentItem(at)
			}
			explain(list.GetCurrentItem())
			box(flex.Box, title)
		},
	}
}

// rule is a line across a dialog, setting a pane apart from what is above.
func rule() tview.Primitive {
	line := tview.NewBox()
	line.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		for col := x; col < x+w; col++ {
			screen.SetContent(col, y, tview.Borders.Horizontal, nil, baseStyle().Foreground(colDim))
		}
		return x, y, w, h
	})
	return line
}

// pickerFrame hands focus to whichever half of the picker its mode is in. A
// Flex would give it to the item it was built with, and focus comes back that
// way whenever a modal above closes - a task that opened the picker closes its
// own page after it - which left a list in NORMAL mode typing into its filter.
type pickerFrame struct {
	*tview.Flex
	target func() tview.Primitive
	// resized hears the width of a row whenever it changes.
	resized func(width int)
	width   int
}

func (f *pickerFrame) Draw(screen tcell.Screen) {
	if f.resized != nil {
		_, _, w, _ := f.GetRect()
		if row := w - 4; row != f.width {
			f.width = row
			f.resized(row)
		}
	}
	f.Flex.Draw(screen)
}

func (f *pickerFrame) Focus(delegate func(p tview.Primitive)) { delegate(f.target()) }

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

// Reserve c for Cancel even when an earlier action also starts with c.
func buttonKeys(labels []string) []rune {
	keys := make([]rune, len(labels))
	used := map[rune]bool{}
	for i, label := range labels {
		if label == "Cancel" {
			keys[i] = 'c'
			used['c'] = true
		}
	}
	// The keys a form moves and starts typing with are no button's.
	for _, key := range navigationKeys {
		used[key] = true
	}
	for i, label := range labels {
		if keys[i] != 0 {
			continue
		}
		for _, key := range strings.ToLower(label) + "1234567890" {
			if !used[key] && (unicode.IsLetter(key) || unicode.IsDigit(key)) {
				keys[i] = key
				used[key] = true
				break
			}
		}
	}
	return keys
}

// formButtonLabels are the buttons' names, without the marked letters.
func formButtonLabels(form *tview.Form) []string {
	labels := make([]string, form.GetButtonCount())
	for i := range labels {
		labels[i] = buttonName(form.GetButton(i).GetLabel())
	}
	return labels
}

// formButtonHint says which mode the form is in and what the keys do there.
// A button is pressed by the letter lit in its label and by nothing else, so
// the hint names exactly those letters; while typing, Esc comes first.
func (a *App) formButtonHint(form *tview.Form) string {
	labels := formButtonLabels(form)
	keys := buttonKeys(labels)
	insert := false
	if mode := a.formModes[form]; mode != nil {
		insert = mode.insert
	}
	buttons := make([]string, 0, len(labels))
	var direct []string
	for i, label := range labels {
		buttons = append(buttons, fmt.Sprintf("%c %s", keys[i], strings.ToLower(label)))
		if label == "Send" {
			direct = append(direct, "Ctrl-S send")
		}
	}
	if insert {
		hints := append(direct, "Esc stop typing, then "+strings.Join(buttons, " · "))
		return strings.Join(hints, " · ")
	}
	return strings.Join(append(append(buttons, direct...), "i type", "Esc back"), " · ")
}

// A modal leaves one empty line beneath its buttons. Draw there after its
// internal frame, which would otherwise erase the hint.
type confirmationHint struct {
	*tview.Modal
	hint string
}

func (m *confirmationHint) Draw(screen tcell.Screen) {
	m.Modal.Draw(screen)
	x, y, w, h := m.GetRect()
	tview.Print(screen, m.hint, x+2, y+h-2, max(0, w-4), tview.AlignCenter, colDim)
}

// Forms keep their hints outside the scrollable fields and button row.
func (a *App) hintForm(form *tview.Form) {
	hintPanel(form.Box, func() string { return a.formButtonHint(form) }, 1, 1, 2, 2)
}

// Reserve space inside the border so scrolling content cannot overwrite hints.
func hintPanel(panel *tview.Box, hint func() string, top, bottom, left, right int) {
	panel.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		width := max(0, w-2-left-right)
		lines := tview.WordWrap(hint(), max(1, width))
		for i, line := range lines {
			tview.Print(screen, line, x+1+left, y+h-1-len(lines)+i, width, tview.AlignLeft, colDim)
		}
		return x + 1 + left, y + 1 + top, width, max(0, h-2-top-bottom-len(lines))
	})
}

// Hints wrap with their block instead of disappearing off the terminal edge.
func fitFooter(block *tview.Flex, footer *tview.TextView, inset int) {
	fitFooterPadded(block, footer, inset, 0)
}

// fitFooterPadded is fitFooter with pad more columns on either side.
func fitFooterPadded(block *tview.Flex, footer *tview.TextView, inset, pad int) {
	block.SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		side := inset + pad
		// The padding is the block's own; what is under the modal must not
		// show through it.
		for row := y + inset; row < y+h-inset; row++ {
			for i := range pad {
				screen.SetContent(x+inset+i, row, ' ', nil, baseStyle())
				screen.SetContent(x+w-1-inset-i, row, ' ', nil, baseStyle())
			}
		}
		width := max(1, w-2*side)
		lines := len(tview.WordWrap(footer.GetText(false), width))
		block.ResizeItem(footer, max(1, lines), 0)
		return x + side, y + inset, max(0, w-2*side), max(0, h-2*inset)
	})
}

// halfPage moves a reader by half its height on Ctrl-D and Ctrl-U, as vim
// does, and reports whether the key was one of them. Whole pages are tview's
// own: Ctrl-F and Ctrl-B, PgDn and PgUp.
func halfPage(view *tview.TextView, ev *tcell.EventKey) bool {
	_, _, _, height := view.GetInnerRect()
	step := max(1, height/2)
	row, col := view.GetScrollOffset()
	switch ev.Key() {
	case tcell.KeyCtrlD:
		// tview keeps the last page in view if this goes past the end.
		view.ScrollTo(row+step, col)
	case tcell.KeyCtrlU:
		view.ScrollTo(max(0, row-step), col)
	default:
		return false
	}
	return true
}
