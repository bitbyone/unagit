package ui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// listLayout is what a list tells layRows about its rows.
type listLayout struct {
	// favourite reports whether a row is starred.
	favourite func(idx int) bool
	// group, when the list is grouped, is the heading a row goes under.
	group func(idx int) (key, heading string)
	// draw fills in one row of the table.
	draw func(row, idx int)
	// width is how wide the separator is drawn.
	width int
}

// layRows draws the rows of a list from the table's second row on, the first
// being the column header. A grouped list goes under its headings, where the
// favourites are only marked: the headings are the order there. A flat one,
// when favourites come first and one is shown, leads with them, set apart
// from the rest by a line, each part in the list's order. It returns the
// first row the cursor may sit on, 0 for none.
func (a *App) layRows(t *tview.Table, filtered []int, l listLayout) int {
	parts := [][]int{filtered}
	if l.group == nil && a.cfg.Filters.FavouritesFirst() {
		var favourites, rest []int
		for _, idx := range filtered {
			if l.favourite(idx) {
				favourites = append(favourites, idx)
			} else {
				rest = append(rest, idx)
			}
		}
		if len(favourites) > 0 {
			parts = [][]int{favourites, rest}
		}
	}
	row, first := 0, 0
	put := func(idx int) {
		row++
		if first == 0 {
			first = row
		}
		l.draw(row, idx)
	}
	for i, part := range parts {
		if i > 0 && len(part) > 0 {
			row++
			t.SetCell(row, 0, tview.NewTableCell(tag(colDim)+strings.Repeat(string(tview.Borders.Horizontal), max(l.width, 1))+tagEnd).
				SetSelectable(false).SetExpansion(1))
		}
		if l.group == nil {
			for _, idx := range part {
				put(idx)
			}
			continue
		}
		for _, g := range gather(part, l.group) {
			row++
			setGroupHeading(t, row, g)
			for _, idx := range g.rows {
				put(idx)
			}
		}
	}
	return first
}

// starColumn is how wide the star column of a list is: one more than nothing
// when any shown row is a favourite, nothing otherwise, so a list without
// favourites keeps every column it had.
func starColumn(filtered []int, favourite func(idx int) bool) int {
	for _, idx := range filtered {
		if favourite(idx) {
			return 1
		}
	}
	return 0
}

// starred puts the star in front of a row's mark, which starts with a space;
// with no star column it leaves the mark alone.
func starred(column int, favourite bool, mark string) string {
	switch {
	case column == 0:
		return mark
	case favourite:
		return tag(colStar) + glyphFavourite + tagEnd + mark
	}
	return " " + mark
}

// rowGroup is the rows under one heading of a grouped list, in the order the
// filter put them.
type rowGroup struct {
	heading string
	rows    []int
}

// gather puts the rows under their headings, keeping the order the filter
// produced: the first row of a heading decides where the heading sits, and
// the rest follow inside it. key tells the groups apart; the heading is only
// what is drawn, and two groups may read the same.
func gather(filtered []int, of func(idx int) (key, heading string)) []rowGroup {
	var groups []rowGroup
	at := map[string]int{}
	for _, idx := range filtered {
		key, heading := of(idx)
		if i, ok := at[key]; ok {
			groups[i].rows = append(groups[i].rows, idx)
			continue
		}
		at[key] = len(groups)
		groups = append(groups, rowGroup{heading: heading, rows: []int{idx}})
	}
	return groups
}

// setGroupHeading draws the heading of a group: what it is, and how many rows
// follow. The cursor passes over it.
func setGroupHeading(t *tview.Table, row int, g rowGroup) {
	t.SetCell(row, 0, tview.NewTableCell(fmt.Sprintf("%s[::b]%s[::-]%s  %s(%d)%s",
		tag(colAccent), tview.Escape(g.heading), tagEnd, tag(colDim), len(g.rows), tagEnd)).
		SetSelectable(false).SetExpansion(1))
}

// headingKey is the group of a row: a server and a path on it.
func headingKey(a *App, instance, path string) (key, heading string) {
	heading = path
	if a.multiInstance() {
		heading = a.instanceLabel(instance) + " · " + path
	}
	return instance + "\x00" + path, heading
}
