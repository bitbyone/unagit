package ui

import "sort"

// listColumn is one column of a main list as its layout sees it: what its rows
// take and how much it minds being cut. layoutColumns turns the columns of a
// list into widths that fit the room, so no list keeps an order of its own
// for what shrinks first.
type listColumn struct {
	// floor is the narrowest it is ever drawn: its heading.
	floor int
	// min is the narrowest it still says something at; when even the
	// minimums do not fit, a column that may be left out goes first.
	min int
	// ideal is what most of its rows take whole, full what all of them
	// take: one very long row does not take the room of every other column
	// until each has its ideal.
	ideal, full int
	// max holds it back however much room is left; 0 is no limit.
	max int
	// weight is how much a cut costs it beside the others: while both are
	// cut, a column of weight 2 grows two cells for each one a column of
	// weight 1 does.
	weight float64
	// drop is when it is left out if the room is short: 1 first, 0 never.
	drop int

	// width is the result, 0 when the column was left out.
	width int
}

// headingWidth is what a column's heading takes: its words, and the
// theme's icon before them where there is one (withHeadingIcons).
func headingWidth(heading string) int {
	if icon := columnIcons[heading]; icon != "" {
		return cells(icon) + 1 + cells(heading)
	}
	return cells(heading)
}

// fixedColumn is a column that is always as wide as w: a mark, a count.
func fixedColumn(w int) *listColumn {
	return &listColumn{floor: w, min: w, ideal: w, full: w, weight: 1, width: w}
}

// flexColumn is a column that can be cut, sized from what its rows take.
// lengths are the cells of each row's text; least is the narrowest worth
// showing.
func flexColumn(heading string, lengths []int, least int, weight float64) *listColumn {
	floor := headingWidth(heading)
	full, ideal := spread(lengths)
	full, ideal = max(full, floor), max(ideal, floor)
	return &listColumn{floor: floor, min: max(floor, min(least, full)), ideal: ideal, full: full, weight: weight}
}

// gistColumn is a directory column: its ideal is what the gist of its
// rows takes (pathGist), and the rest of the paths only its full.
func gistColumn(heading string, paths []string, least int, weight float64) *listColumn {
	lengths, gists := make([]int, len(paths)), make([]int, len(paths))
	for i, p := range paths {
		lengths[i], gists[i] = cells(p), pathGist(p)
	}
	c := flexColumn(heading, lengths, least, weight)
	_, ideal := spread(gists)
	c.ideal = max(c.floor, min(ideal, c.full))
	return c
}

// spread is the longest of lengths, and the length that nine rows in ten
// fit in.
func spread(lengths []int) (full, ideal int) {
	if len(lengths) == 0 {
		return 0, 0
	}
	sorted := append([]int(nil), lengths...)
	sort.Ints(sorted)
	at := (len(sorted)*9 + 9) / 10 // the ninetieth percentile, rounded up
	return sorted[len(sorted)-1], sorted[at-1]
}

// shown says whether the layout kept the column.
func (c *listColumn) shown() bool { return c.width > 0 }

// layoutColumns gives each column a width so that all of them, with a space
// between each two, fit in room, and returns the cells left over for the
// list to place. Every column starts at its minimum; when that is already
// too much, the columns that may be left out go in their order, and after
// them the rest are cut evenly down to their headings, the widest for its
// weight first. What
// room remains goes a cell at a time to the narrowest column for its
// weight that still wants it, first up to the ideals, then up to full. So
// the columns grow evenly: a short one is soon whole, and what the long
// ones are still missing is cut out of them alone - not a branch name cut
// by one cell while a path, which is shortened by what tells it apart,
// takes forty.
func layoutColumns(room int, cols ...*listColumn) int {
	for _, c := range cols {
		c.width = c.min
	}
	used := func() int {
		total, n := 0, 0
		for _, c := range cols {
			if c.shown() {
				total += c.width
				n++
			}
		}
		return total + max(0, n-1)
	}
	for used() > room {
		next := (*listColumn)(nil)
		for _, c := range cols {
			if c.shown() && c.drop > 0 && (next == nil || c.drop < next.drop) {
				next = c
			}
		}
		if next == nil {
			break
		}
		next.width = 0
	}
	for over := used() - room; over > 0; over-- {
		next, widest := (*listColumn)(nil), 0.0
		for _, c := range cols {
			if !c.shown() || c.width <= c.floor {
				continue
			}
			if share := float64(c.width) / c.weight; next == nil || share > widest {
				next, widest = c, share
			}
		}
		if next == nil {
			break
		}
		next.width--
	}
	spare := room - used()
	for _, target := range []func(*listColumn) int{
		func(c *listColumn) int { return c.ideal },
		func(c *listColumn) int { return c.full },
	} {
		for spare > 0 {
			next, best := (*listColumn)(nil), 0.0
			for _, c := range cols {
				goal := target(c)
				if c.max > 0 {
					goal = min(goal, c.max)
				}
				if !c.shown() || c.width >= goal {
					continue
				}
				share := float64(c.width) / c.weight
				if next == nil || share < best {
					next, best = c, share
				}
			}
			if next == nil {
				break
			}
			next.width++
			spare--
		}
	}
	return spare
}
