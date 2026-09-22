package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// helpLine is one row of the help: a section heading, a key with what it
// does, or a paragraph of explanation.
type helpLine struct {
	section string
	keys    string
	text    string
	note    string
}

func section(name string) helpLine   { return helpLine{section: name} }
func key(keys, text string) helpLine { return helpLine{keys: keys, text: text} }
func note(text string) helpLine      { return helpLine{note: text} }
func blank() helpLine                { return helpLine{} }

// helpRows is the whole of the help, in the order it is read. Keeping it as
// data rather than as one long string is what lets the keys line up in their
// own column.
func helpRows() []helpLine {
	return []helpLine{
		section("Getting around"),
		key("R  M  S", "Repositories · Merge requests · Settings"),
		key("j  k", "move up and down"),
		key("g  G", "first · last"),
		key("/", "filter: fuzzy, spaces separate terms"),
		key("Esc", "leave the filter · again clears it · again closes the detail"),
		key("?", "this help"),
		key("q", "quit"),
		blank(),

		section("The detail column"),
		key("Enter", "load it and jump in; it then follows the cursor"),
		key("j k g G", "scroll"),
		key("Ctrl-F Ctrl-B", "page"),
		key("h  ←  Esc", "back to the list"),
		note("Repositories show statistics, languages, the latest pipeline, the " +
			"most recent commits and their open merge requests. Merge requests are " +
			"always fetched fresh."),
		blank(),

		section("Both lists"),
		key("Ctrl-O", "clone or update, then open the editor"),
		key("d", "delete from disk, warning about work that would be lost"),
		key("w", "open in the browser"),
		key("r", "refresh this list's index from the server"),
		blank(),

		section("Filters · shared by both lists"),
		key("C", "only the repositories you have cloned"),
		key("x", "hide the repository under the cursor, or bring it back"),
		key("X", "manage the hidden repositories"),
		key("o", "order: by activity, or by name"),
		key("Ctrl-G", "gather the merge requests under their repository"),
		note("Hiding a repository takes its merge requests with it. The header " +
			"under each list says what is being left out."),
		blank(),

		section("Repositories"),
		key("b", "pick a branch and switch the main clone to it"),
		key("m", "show only the merge requests of this repository"),
		note("The PATH column is where a repository is cloned, which is worth " +
			"seeing when a group or a server has a root of its own."),
		blank(),

		section("Merge requests"),
		key("Ctrl-R", "open for review: the whole change as pending edits"),
		key("c", "read the conversation, and write a comment"),
		key("a", "approve - it asks first"),
		key("f  F", "limit the list to one repository · clear that limit"),
		note("The COM column is how many comments a merge request has. GitLab " +
			"reports it on the listing; GitHub only on a single merge request, so " +
			"there it fills in once you have opened one."),
		blank(),

		section("Comments  (c)"),
		key("i", "write one, Ctrl-S sends it"),
		key("a", "approve"),
		key("r", "reload"),
		note("Oldest first, with the markdown rendered. The detail column keeps " +
			"the three newest."),
		blank(),

		section("Settings  (S)"),
		key("j  k", "move between the sections"),
		key("Enter", "edit the section"),
		key("a e t v d", "in a server list: add · edit · token · verify · remove"),
		key("space", "in the group tree: a GitLab group cycles off → this group "+
			"only → including subgroups; a GitHub organisation is on or off"),
		key("d", "in the group tree: the clone directory of a group or a server"),
		key("r  p  m", "reload the groups · refresh projects · refresh merge requests"),
		key("c", "under Security: change the passphrase"),
		blank(),

		section("Reviewing"),
		note("Ctrl-O gives you the branch: real commits, you can commit and push."),
		note("Ctrl-R gives you the review worktree: HEAD sits on the commit the " +
			"merge request branched from while the index and the working tree hold " +
			"the merge request, so the whole change is pending. Gutter signs, ]c and " +
			"diff views then work on it as one change."),
		note("Both record what they are: git config unagit.mr.base / .head / .iid " +
			"/ .target / .url / .mode."),
		blank(),

		section("From another terminal"),
		note("Opening an editor does not end unagit: it suspends itself and waits, so " +
			"it knows what you have open. Another window can follow it there with " +
			"cd \"$(unagit cd)\" - it asks which when more than one is open, and takes a " +
			"search to narrow it. unagit sessions lists them."),
		blank(),

		section("On disk"),
		key("○ ● ◐ ◉", "nothing · branch worktree · review worktree · both"),
		key("⊘", "hidden from the lists"),
		note("<root>/<group>/<repo> is the main clone, where branch switching " +
			"happens. <repo>.mrs/<iid>-<branch> is a branch worktree and " +
			"<repo>.reviews/<iid>-<branch> a review one. They share the main " +
			"clone's objects, so uncommitted changes survive switching between " +
			"merge requests. <root> comes from Settings, unless the server or the " +
			"group overrides it. The Repositories tab shows it in the PATH " +
			"column."),
	}
}

// wrapText breaks a paragraph into lines that fit a width, on word
// boundaries. The table cannot wrap on its own, so the rows are made to fit.
func wrapText(text string, width int) []string {
	if width < 12 {
		width = 12
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// showHelp draws the help as a two column table: keys on the left, what they
// do on the right. A table rather than a block of text is what keeps the
// columns aligned however long a key combination is, and the rows are wrapped
// to the width the modal actually got.
func (a *App) showHelp() {
	table := tview.NewTable().SetSelectable(false, false)
	box(table.Box, "unagit · keys").SetBorderPadding(0, 0, 2, 2)

	rows := helpRows()
	keyWidth := 0
	for _, line := range rows {
		keyWidth = max(keyWidth, len([]rune(line.keys)))
	}

	fill := func(width int) {
		table.Clear()
		textWidth := width - keyWidth - 2
		row := 0
		put := func(keys, text string, keyColour, textColour tcell.Color, attrs tcell.AttrMask) {
			table.SetCell(row, 0, tview.NewTableCell(keys).
				SetTextColor(keyColour).
				SetAttributes(attrs).
				SetAlign(tview.AlignRight).
				SetSelectable(false))
			table.SetCell(row, 1, tview.NewTableCell(" "+text).
				SetTextColor(textColour).
				SetAttributes(attrs).
				SetExpansion(1).
				SetSelectable(false))
			row++
		}
		for _, line := range rows {
			switch {
			case line.section != "":
				put("", strings.ToUpper(line.section), colDim, colWarn, tcell.AttrBold)
			case line.keys != "":
				for i, text := range wrapText(line.text, textWidth) {
					if i == 0 {
						put(line.keys, text, colAccent, colText, tcell.AttrNone)
						continue
					}
					put("", text, colDim, colText, tcell.AttrNone)
				}
			case line.note != "":
				for _, text := range wrapText(line.note, textWidth) {
					put("", text, colDim, colMuted, tcell.AttrNone)
				}
			default:
				put("", "", colDim, colDim, tcell.AttrNone)
			}
		}
	}
	fill(76)

	// The modal is a share of the terminal, so the width is only known once
	// it has been laid out.
	width := 0
	table.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		if inner := w - 6; inner != width {
			width = inner
			go a.tv.QueueUpdateDraw(func() { fill(inner) })
		}
		return x + 3, y + 1, w - 6, h - 2
	})

	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc, tcell.KeyEnter:
			a.closeModal(pageHelp)
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case '?', 'q':
				a.closeModal(pageHelp)
				return nil
			case 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			case 'g':
				table.ScrollToBeginning()
				return nil
			case 'G':
				table.ScrollToEnd()
				return nil
			}
		}
		return ev
	})

	a.pages.AddPage(pageHelp, modalPct(table, 82, 90), true, true)
	a.tv.SetFocus(table)
}
