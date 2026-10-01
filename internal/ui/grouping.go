package ui

import (
	"fmt"

	"github.com/rivo/tview"
)

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
