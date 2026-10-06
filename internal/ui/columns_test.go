package ui

import "testing"

// TestLayoutColumnsFits: whatever the room, the columns and the spaces
// between them fit, none is narrower than its heading, and those that may
// be left out go in their order.
func TestLayoutColumnsFits(t *testing.T) {
	t.Parallel()
	for room := 30; room <= 300; room += 7 {
		name := flexColumn("REPOSITORY", []int{12, 30, 47}, 20, 2)
		branch := flexColumn("BRANCH", []int{4, 26}, 10, 1)
		path := flexColumn("PATH", []int{43, 60}, minPath, 1)
		path.drop = 1
		cols := []*listColumn{fixedColumn(2), name, branch, path, fixedColumn(8)}
		spare := layoutColumns(room, cols...)
		used, n := 0, 0
		for _, c := range cols {
			if c.shown() {
				used += c.width
				n++
				if c.width < c.floor {
					t.Errorf("room %d: a column of %d under its heading's %d", room, c.width, c.floor)
				}
			}
		}
		if used+n-1+spare != room && used+n-1 <= room {
			t.Errorf("room %d: %d used and %d spare do not add up", room, used+n-1, spare)
		}
		if used+n-1 > room && path.shown() {
			t.Errorf("room %d: overflows with the path still shown", room)
		}
	}
}

// TestLayoutColumnsBalances: room is shared evenly by weight, so a short
// column is whole before a long one takes the rest; one long row does not take the room
// before every other column has what most of its rows need, and a column
// with a measure stops at it.
func TestLayoutColumnsBalances(t *testing.T) {
	t.Parallel()
	name := flexColumn("REPOSITORY", []int{40, 40, 40}, 20, 2)
	branch := flexColumn("BRANCH", []int{20, 20, 20}, 10, 1)
	layoutColumns(46, name, branch)
	// 45 cells, the name growing two for the branch's one from 20 and 10:
	// 30 and 15.
	if name.width != 30 || branch.width < 10 || name.width+branch.width != 45 {
		t.Errorf("name %d, branch %d", name.width, branch.width)
	}

	// Nine short paths and one very long: the branch gets its ideal before
	// the path grows past what nine in ten need.
	path := flexColumn("PATH", []int{20, 20, 20, 20, 20, 20, 20, 20, 20, 90}, minPath, 1)
	branch = flexColumn("BRANCH", []int{30}, 10, 1)
	layoutColumns(51, path, branch)
	if path.width != 20 || branch.width != 30 {
		t.Errorf("path %d, branch %d: the long path took the room", path.width, branch.width)
	}

	title := flexColumn("TITLE", []int{200}, 24, 2)
	title.max = titleMeasure
	if spare := layoutColumns(300, title); title.width != titleMeasure || spare != 300-titleMeasure {
		t.Errorf("title %d, spare %d", title.width, spare)
	}
}
