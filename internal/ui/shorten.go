package ui

import "strings"

// minPath is the narrowest a directory column is let shrink to before it
// is left out: enough for the start and most of the directory's own name.
const minPath = 16

// minCut is the fewest cells a segment cut in the middle keeps: two of its
// start, the ellipsis and two of its end. Fewer than that says nothing a
// collapsed segment would not.
const minCut = 5

// middleCut shortens s to n cells by taking out its middle: the start and
// the end of a name are what tell it from its neighbours.
func middleCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return "…"
	}
	head := n / 2
	tail := n - 1 - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// joinSegments puts segments back together, a run of collapsed ones (nil
// marks one) written as a single mark: never two dots, which would read as
// the folder above.
func joinSegments(segs []*string, mark string) string {
	var parts []string
	for i, s := range segs {
		switch {
		case s != nil:
			parts = append(parts, *s)
		case i == 0 || segs[i-1] != nil:
			parts = append(parts, mark)
		}
	}
	return strings.Join(parts, "/")
}

// elided is what stands for the folders a directory leaves out, and
// elidedGroups for the groups in front of a repository's name: a folder
// and a tree in a Nerd Font. Each is an ellipsis before any theme is on.
func elided() string       { return orEllipsis(glyphElided) }
func elidedGroups() string { return orEllipsis(glyphElidedGroup) }

func orEllipsis(glyph string) string {
	if glyph == "" {
		return "…"
	}
	return glyph
}

func cells(s string) int { return len([]rune(s)) }

// shortenRepo fits a repository's path - group/subgroup/name - into n
// cells. The name is the last thing to give way: the groups in front of it
// go first, from the top down, each cut in the middle while that still
// leaves something to read and collapsed into an ellipsis after that, so
// my2n/ai-transformation/building-access-analysis becomes
// ▸/ai-transformation/…, then ▸/ai-tra…mation/…, then ▸/building-access-
// analysis (▸ being elidedGroups), and only then is the name itself cut in
// the middle.
func shortenRepo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if cells(s) <= n {
		return s
	}
	parts := strings.Split(s, "/")
	name := parts[len(parts)-1]
	segs := make([]*string, len(parts))
	for i := range parts {
		segs[i] = &parts[i]
	}
	for i := 0; i < len(parts)-1; i++ {
		over := cells(joinSegments(segs, elidedGroups())) - n
		if keep := cells(parts[i]) - over; keep >= minCut && keep < cells(parts[i]) {
			cut := middleCut(parts[i], keep)
			segs[i] = &cut
			return joinSegments(segs, elidedGroups())
		}
		segs[i] = nil
		if out := joinSegments(segs, elidedGroups()); cells(out) <= n {
			return out
		}
	}
	return lastResort(name, n, elidedGroups())
}

// lastResort is a path down to its last segment: an ellipsis in front of it
// says there was more, and when even that does not fit, the segment is cut
// in the middle.
func lastResort(name string, n int, mark string) string {
	if cells(name)+2 <= n {
		return mark + "/" + name
	}
	if cells(name)+1 <= n {
		return mark + name
	}
	return middleCut(name, n)
}

// shortenPath fits a directory into n cells, keeping where it starts (~ or
// /) and the directory itself whole as long as it can. The folders between
// are cut in the middle first, the longest first and all to the same
// length, so ~/workspace/jantobola/websites/tobolovi-com becomes
// ~/wor…ce/ja…la/we…es/tobolovi-com; when that is not enough, they are
// collapsed from the directory's end upwards - ~/wor…ce/▸/tobolovi-com -
// and the folders still shown get back what the collapse freed.
func shortenPath(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if cells(s) <= n {
		return s
	}
	parts := strings.Split(s, "/")
	if len(parts) < 3 {
		return middleCut(s, n)
	}
	first, last, between := parts[0], parts[len(parts)-1], parts[1:len(parts)-1]
	// fit is the path with the folders before shown cut to at most width
	// cells each, and those from shown on collapsed. What the cut leaves
	// unused of n goes back to the folders, a cell at a time from the start.
	fit := func(shown, width int) string {
		widths := make([]int, shown)
		for i := range widths {
			widths[i] = min(cells(between[i]), width)
		}
		render := func() string {
			segs := []*string{&first}
			for i := range between {
				if i >= shown {
					segs = append(segs, nil)
					continue
				}
				cut := middleCut(between[i], widths[i])
				segs = append(segs, &cut)
			}
			return joinSegments(append(segs, &last), elided())
		}
		out := render()
		for spare := n - cells(out); spare > 0; {
			grown := false
			for i := range widths {
				if spare > 0 && widths[i] < cells(between[i]) {
					widths[i]++
					spare--
					grown = true
				}
			}
			if !grown {
				break
			}
		}
		return render()
	}
	for shown := len(between); shown >= 1; shown-- {
		longest := 0
		for _, b := range between[:shown] {
			longest = max(longest, cells(b))
		}
		for width := longest; width >= min(minCut, longest); width-- {
			if out := fit(shown, width); cells(out) <= n {
				return out
			}
		}
	}
	if out := fit(0, 0); cells(out) <= n {
		return out
	}
	return lastResort(last, n, elided())
}

// pathGist is how wide a directory is with only what says the most: where it
// starts and its last two folders, ~/…/websites/tobolovi-com. A list gives a
// directory column this much before the other columns' extras, and the rest
// of the path only after them.
func pathGist(s string) int {
	parts := strings.Split(s, "/")
	if len(parts) <= 4 {
		return cells(s)
	}
	return cells(parts[0] + "/" + elided() + "/" + strings.Join(parts[len(parts)-2:], "/"))
}

// minBranchRest is the fewest cells the part of a branch after its kind is
// cut to before the kind gives way further: enough to find it by.
const minBranchRest = 8

// shortenBranch fits a branch into n cells. A branch is mostly a kind and a
// name, feature/this-is-super-feature-long, and the name is what it is
// found by: the kind is cut in the middle first, then the name keeps its
// start and a little of its end - fe…re/this-is-supe…long - and only then
// does the kind shrink to its first letter.
func shortenBranch(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if cells(s) <= n {
		return s
	}
	kind, name, ok := strings.Cut(s, "/")
	if !ok || kind == "" || name == "" {
		return headCut(s, n)
	}
	least := min(minCut, cells(kind))
	for k := cells(kind); k >= least; k-- {
		if out := middleCut(kind, k) + "/" + name; cells(out) <= n {
			return out
		}
	}
	short := middleCut(kind, least)
	if room := n - cells(short) - 1; room >= minBranchRest {
		return short + "/" + headCut(name, room)
	}
	if n >= 3 {
		return string([]rune(kind)[:1]) + "/" + headCut(name, n-2)
	}
	return headCut(name, n)
}

// headCut shortens s to n cells keeping most of its start, where a name is
// read from, and a few cells of its end, which tell its neighbours apart -
// when there are ten cells or more: in fewer, the start is worth more.
func headCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return middleCut(s, n)
	}
	tail := 0
	if n >= 10 {
		tail = min(4, (n-1)/3)
	}
	return string(r[:n-1-tail]) + "…" + string(r[len(r)-tail:])
}
