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

	hint := fmt.Sprintf("c cancel · %c %s", keys[1], strings.ToLower(accept))
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
	// Hidden keeps the item out of the list until something is typed: one
	// of many alike, looked for by name rather than walked past.
	Hidden bool
	// Aliases are short words it is found by besides its name - cc for
	// Claude Code - and Prefer orders it among items found equally well,
	// the lower first.
	Aliases []string
	Prefer  int
}

// wordsMatch says whether every word of the query is an alias of the item,
// or the start of one, or the start of a word of its name; and how many
// words of the name none of them starts - the fewer, the closer the name.
func wordsMatch(query string, it pickItem) (bool, int) {
	words := strings.FieldsFunc(strings.ToLower(plainText(it.Label)), func(r rune) bool {
		return r == ' ' || r == '/' || r == '-' || r == '·'
	})
	typed := strings.Fields(strings.ToLower(query))
	for _, q := range typed {
		found := false
		for _, w := range append(words, it.Aliases...) {
			if strings.HasPrefix(strings.ToLower(w), q) {
				found = true
				break
			}
		}
		if !found {
			return false, 0
		}
	}
	rest := 0
	for _, w := range words {
		said := false
		for _, q := range typed {
			said = said || strings.HasPrefix(w, q)
		}
		if !said {
			rest++
		}
	}
	return true, rest
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
	// page is the page the picker is put on; "" is the picker's own. An
	// action picker over a dialog takes another, so the dialog stays.
	page string
	// enterName and enterAbout name what Enter does to an item, for the
	// item's actions (Alt-Enter); unset, Enter is not offered there.
	enterName, enterAbout string
	// same tells whether two items are the same thing, for a picker whose
	// items are put again while it is open: the cursor stays on it. Unset,
	// the labels are compared.
	same func(a, b pickItem) bool
	// filter opens the picker typing into its filter rather than on the
	// list. Only the action pickers do: an action is looked for by name
	// more often than walked to.
	filter bool
	// query is the filter as it opens, typed already: a picker opened again
	// in place of itself keeps what was being looked for.
	query string
	// preview hears every item the cursor comes to, and cancel that the
	// picker was closed without a choice - so an item can be tried on the
	// screen behind and taken back.
	preview func(it pickItem, query string)
	cancel  func()
	// bright leaves the screen behind undimmed and sets the picker apart
	// on a background of its own instead: for a picker whose items are
	// tried on that screen.
	bright bool
	// header names the columns of a picker whose items are a table's rows
	// (pickTable), on a line of its own over them.
	header string
}

// pickTable lays rows out as columns under their names, for a picker whose
// items are the rows of a table: each cell is markup already, as wide as
// the widest of its column, two spaces between. A column with nothing in
// any row is left out - a forge that does not say who started a job has no
// column of nobody. It gives the header (pickerOptions.header) and each
// row's label.
func pickTable(heads []string, rows [][]string) (string, []string) {
	widths := make([]int, len(heads))
	used := make([]bool, len(heads))
	for i, h := range heads {
		widths[i] = len([]rune(h))
		used[i] = h == ""
	}
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], tview.TaggedStringWidth(c))
			used[i] = used[i] || strings.TrimSpace(c) != ""
		}
	}
	line := func(cells []string) string {
		var parts []string
		for i, c := range cells {
			if used[i] {
				parts = append(parts, c+strings.Repeat(" ", widths[i]-tview.TaggedStringWidth(c)))
			}
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	names := make([]string, len(heads))
	for i, h := range heads {
		names[i] = esc(h)
	}
	labels := make([]string, len(rows))
	for i, r := range rows {
		labels[i] = line(r)
	}
	return tag(role("column.header")) + line(names) + tagEnd, labels
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
	// setHeader puts new column names in, for rows whose columns widened.
	setHeader func(header string)
}

// widePct is how much of the screen across a wide picker takes.
const widePct = 90

// explainLines is the most an explanation may take; it is a sentence, not a
// paragraph.
const explainLines = 3

// browserKey is a picker's key that opens a page in the browser. The
// browser opens beside unagit, never instead of it, so the list stays open,
// the cursor where it was, and what was opened is said in its edge. Every
// key of a list that opens the browser is made here (TestBrowserKeysStay).
func (a *App) browserKey(keys, hint, name, about string, url func(pickItem) string) pickKey {
	return pickKey{keys: keys, hint: hint, name: name, about: about, stay: true,
		run: func(it pickItem) { a.openWeb(url(it)) }}
}

// pickKey is a key of a picker that acts on the item under the cursor.
type pickKey struct {
	keys string
	hint string
	// name and about are what the key is called and does, as an action is
	// (uiAction): the item's actions (Alt-Enter) list it by them.
	name, about string
	run         func(pickItem)
	// stay keeps the picker open, the cursor where it was: for a key whose
	// work happens elsewhere, like opening the browser.
	stay bool
	// named, when set, says the name and the about as they are now, for
	// an action that turns into its opposite once done (Watch Merge Request).
	named func() (string, string)
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
	// rebuilding is set while the list is filled again, whose every item
	// added moves the cursor on the way to where it ends.
	rebuilding := false
	rebuild := func(query string) {
		rebuilding = true
		defer func() { rebuilding = false }()
		list.Clear()
		shown = shown[:0]
		// What an item is called comes before what its explanation says:
		// searching for a worktree finds the action named so before one that
		// mentions a worktree in passing. Before both, an item every typed
		// word starts a word of, or an alias: "cc split" is Claude Code in a
		// split, whatever else the letters run through. Among those the
		// name with the fewest words left unsaid comes first, then the
		// preferred, then the list's own order.
		type hit struct {
			it    pickItem
			tier  int
			score int
			rest  int
		}
		typed := strings.TrimSpace(query) != ""
		var hits []hit
		for _, it := range items {
			if it.Hidden && !typed {
				continue
			}
			score, ok := fuzzy.Match(query, it.Label+" "+it.Sub+" "+strings.Join(it.Aliases, " "))
			tier := 1
			if !ok && it.About != "" {
				score, ok = fuzzy.Match(query, it.About)
				tier = 2
			}
			rest := 0
			if typed {
				if all, left := wordsMatch(query, it); all {
					ok, tier, rest = true, 0, left
				}
			}
			if !ok {
				continue
			}
			hits = append(hits, hit{it, tier, score, rest})
		}
		if typed {
			sort.SliceStable(hits, func(i, j int) bool {
				l, r := hits[i], hits[j]
				switch {
				case l.tier != r.tier:
					return l.tier < r.tier
				case l.tier == 0 && l.rest != r.rest:
					return l.rest < r.rest
				case l.tier == 0 && l.it.Prefer != r.it.Prefer:
					return l.it.Prefer < r.it.Prefer
				case l.tier == 0:
					return false
				case l.score != r.score:
					return l.score > r.score
				}
				return l.it.Prefer < r.it.Prefer
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
	rebuild(opts.query)
	if opts.query != "" && start >= 0 && start < len(items) {
		// start is an item's place among all of them; the filter has left
		// fewer.
		want, at := items[start].Label, 0
		for i, it := range shown {
			if it.Label == want {
				at = i
			}
		}
		start = at
	}
	if start > 0 && start < list.GetItemCount() {
		list.SetCurrentItem(start)
	}
	if opts.query != "" {
		input.SetText(opts.query)
	}
	// What Esc does in a picker that opened on its filter depends on
	// whether anything is typed, and the hint follows it (filterHint).
	var typed func()
	// moved hears where the cursor came to, once it has settled.
	moved := func(int) {}
	input.SetChangedFunc(func(query string) {
		rebuild(query)
		moved(list.GetCurrentItem())
		if typed != nil {
			typed()
		}
	})

	pageName := opts.page
	if pageName == "" {
		pageName = pagePicker
	}
	dismiss := func() { a.closeModal(pageName) }
	// giveUp closes the picker with nothing chosen.
	giveUp := func() {
		dismiss()
		if opts.cancel != nil {
			opts.cancel()
		}
	}
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
		// Only what can be done: moving, filtering and closing are what
		// every list does, and are not said.
		var hints []string
		switch {
		case onSelect == nil:
		case opts.enterHint != "":
			hints = append(hints, "Enter "+opts.enterHint)
		case opts.again != 0:
			hints = append(hints, fmt.Sprintf("Enter or %c select", opts.again))
		default:
			hints = append(hints, "Enter select")
		}
		for _, k := range opts.keys {
			// A key-less action is the actions picker's alone.
			if k.keys != "" {
				hints = append(hints, k.keys+" "+k.hint)
			}
		}
		if onNew != nil {
			hints = append(hints, "n new")
		}
		if onDelete != nil {
			hints = append(hints, "d delete")
		}
		hint := ""
		if len(hints) > 0 {
			hint = "   " + strings.Join(hints, " · ")
		}
		return " " + tag(colMuted) + "NORMAL" + tagEnd + tag(colDim) + hint + tagEnd
	}
	filterHint := func() {
		footer.SetText(" " + tag(colWarn) + "FILTER" + tagEnd + tag(colDim) + "   Enter select" + tagEnd)
	}
	if opts.filter {
		typed = func() {
			if filtering {
				filterHint()
			}
		}
	}
	setMode := func(filter bool) {
		filtering = filter
		if filtering {
			filterHint()
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
			// A picker that opened on its filter closes on Esc while nothing
			// is typed: there is no list it was taken from to go back to.
			if opts.filter && input.GetText() == "" {
				giveUp()
				if opts.back != nil {
					opts.back()
				}
				return nil
			}
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
		// What can be done with the item under the cursor, as a list of
		// actions, the way every list of the main screens has it.
		if opensSelectionActions(ev) {
			i := list.GetCurrentItem()
			if i < 0 || i >= len(shown) {
				a.flash("nothing is selected")
				return nil
			}
			it := shown[i]
			var acts []uiAction
			if onSelect != nil && opts.enterName != "" {
				acts = append(acts, uiAction{name: opts.enterName, about: opts.enterAbout, keys: "Enter", rank: 1, run: func() {
					dismiss()
					onSelect(it)
				}})
			}
			for n, k := range opts.keys {
				name, about := k.name, k.about
				if k.named != nil {
					name, about = k.named()
				}
				if name == "" {
					continue
				}
				acts = append(acts, uiAction{name: name, about: about, keys: k.keys, rank: 10 + n, run: func() {
					if !k.stay {
						dismiss()
					}
					k.run(it)
				}})
			}
			if len(acts) == 0 {
				a.flash("nothing can be done with it")
				return nil
			}
			a.showActions(tview.Escape(plainText(it.Label)), acts)
			return nil
		}
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
			giveUp()
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
				giveUp()
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
		AddItem(input, 1, 0, false)
	header := tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	if opts.header != "" {
		header.SetText(opts.header)
		flex.AddItem(header, 1, 0, false)
	}
	flex.AddItem(list, 0, 1, true)

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
	inner = max(inner, tview.TaggedStringWidth(title)+4, tview.TaggedStringWidth(opts.header)+1, 40)
	if opts.explain {
		for _, it := range items {
			for inner < 76 && len(tview.WordWrap(it.About, inner)) > explainLines {
				inner += 2
			}
		}
	}
	inner = min(inner, 76)

	extra := 0
	if opts.header != "" {
		extra = 1
	}
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
		input.SetChangedFunc(func(query string) {
			rebuild(query)
			explain(list.GetCurrentItem())
			moved(list.GetCurrentItem())
			if typed != nil {
				typed()
			}
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
		extra += 2 + lines
	}
	if opts.preview != nil {
		tried := ""
		if i := list.GetCurrentItem(); i >= 0 && i < len(shown) {
			tried = shown[i].Label
		}
		moved = func(i int) {
			if i < 0 || i >= len(shown) || shown[i].Label == tried {
				return
			}
			tried = shown[i].Label
			opts.preview(shown[i], input.GetText())
		}
	}
	list.SetChangedFunc(func(i int, _, _ string, _ rune) {
		explain(i)
		if !rebuilding {
			moved(i)
		}
	})
	flex.AddItem(footer, 1, 0, false)
	box(flex.Box, title)
	if opts.bright {
		for _, b := range []interface{ SetBackgroundColor(tcell.Color) *tview.Box }{flex, list, footer, input} {
			b.SetBackgroundColor(colPicker)
		}
		input.SetFieldBackgroundColor(colPicker)
		input.SetLabelStyle(tcell.StyleDefault.Foreground(colAccent).Background(colPicker))
		list.SetMainTextStyle(tcell.StyleDefault.Foreground(colText).Background(colPicker))
	}

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
		page = modalFixed(frame, inner+2+2*pad, 2+1+visibleItems(items)+extra+footerLines)
	} else if opts.wide {
		// As wide as its longest row and as tall as its rows, up to most of
		// the screen: a log of two commits is not a screenful of nothing
		// with the commits in a corner. A list whose rows follow the width
		// is laid out at the widest first, to see what it would take.
		page = modalFit(frame, func(w, h int) (int, int) {
			most := w * widePct / 100
			if opts.relabel != nil {
				opts.relabel(items, most-4)
			}
			rows := 0
			for _, it := range items {
				row := it.Label
				if it.Sub != "" {
					row += "   " + it.Sub
				}
				rows = max(rows, tview.TaggedStringWidth(row)+1)
			}
			// inner is the width the explanations were measured at, so the
			// pane under the list is as tall as they need.
			rows = max(rows, tview.TaggedStringWidth(title)+4, tview.TaggedStringWidth(header.GetText(false))+1, inner)
			width := min(most, rows+2+2*pad)
			footerLines := len(tview.WordWrap(normalHint(), width-2-2*pad))
			return width, min(h*85/100, 2+1+visibleItems(items)+extra+footerLines)
		})
	} else {
		page = modalPct(frame, 70, 70)
	}
	if opts.bright {
		if box, ok := page.(*modalBox); ok {
			box.bright = true
		}
	}
	a.pages.AddPage(pageName, page, true, true)
	setMode(opts.filter || opts.query != "")

	same := opts.same
	if same == nil {
		same = func(a, b pickItem) bool { return a.Label == b.Label }
	}
	return &livePicker{
		open: func() bool { return a.pages.GetPage(pageName) == page },
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
		setHeader: func(text string) { header.SetText(text) },
	}
}

// visibleItems counts the items listed before anything is typed; a picker
// is as tall as they need, and those found by typing scroll.
func visibleItems(items []pickItem) int {
	n := 0
	for _, it := range items {
		if !it.Hidden {
			n++
		}
	}
	return max(n, 1)
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
	return strings.Join(append(append(buttons, direct...), "i type"), " · ")
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
		// The padding is the block's own, in its background; what is under
		// the modal must not show through it.
		style := tcell.StyleDefault.Background(block.GetBackgroundColor())
		for row := y + inset; row < y+h-inset; row++ {
			for i := range pad {
				screen.SetContent(x+inset+i, row, ' ', nil, style)
				screen.SetContent(x+w-1-inset-i, row, ' ', nil, style)
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

// plainText is markup as it reads on screen, without its tags, for a title.
func plainText(markup string) string {
	return strings.Join(strings.Fields(tview.NewTextView().SetDynamicColors(true).SetText(markup).GetText(true)), " ")
}
