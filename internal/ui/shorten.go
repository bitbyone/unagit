package ui

import (
	"math"
	"strings"
)

// columnCaps are how wide the repository and the directory columns of the
// main lists may grow, by the width of the window: a narrow one keeps them
// short so the other columns still fit, a very wide one does not hold them
// back at all. A list that has room left over may still give it back to
// them; a cap is what they are sure of, not a reason to cut a path while
// blank cells sit beside it.
func columnCaps(window int) (repo, path int) {
	switch {
	case window < 100:
		return 24, 24
	case window < 150:
		return 32, 36
	case window < 200:
		return 40, 44
	case window <= 250:
		return 56, 64
	}
	return math.MaxInt / 4, math.MaxInt / 4
}

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
// marks one) written as a single glyphElided - a folder in a Nerd Font, an
// ellipsis without one, never two dots, which would read as the folder
// above.
func joinSegments(segs []*string) string {
	var parts []string
	for i, s := range segs {
		switch {
		case s != nil:
			parts = append(parts, *s)
		case i == 0 || segs[i-1] != nil:
			parts = append(parts, elided())
		}
	}
	return strings.Join(parts, "/")
}

// elided is glyphElided, or an ellipsis before any theme is on.
func elided() string {
	if glyphElided == "" {
		return "…"
	}
	return glyphElided
}

func cells(s string) int { return len([]rune(s)) }

// shortenRepo fits a repository's path - group/subgroup/name - into n
// cells. The name is the last thing to give way: the groups in front of it
// go first, from the top down, each cut in the middle while that still
// leaves something to read and collapsed into an ellipsis after that, so
// my2n/ai-transformation/building-access-analysis becomes
// ▸/ai-transformation/…, then ▸/ai-tra…mation/…, then ▸/building-access-
// analysis (▸ being glyphElided), and only then is the name itself cut in
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
		over := cells(joinSegments(segs)) - n
		if keep := cells(parts[i]) - over; keep >= minCut && keep < cells(parts[i]) {
			cut := middleCut(parts[i], keep)
			segs[i] = &cut
			return joinSegments(segs)
		}
		segs[i] = nil
		if out := joinSegments(segs); cells(out) <= n {
			return out
		}
	}
	return lastResort(name, n)
}

// lastResort is a path down to its last segment: an ellipsis in front of it
// says there was more, and when even that does not fit, the segment is cut
// in the middle.
func lastResort(name string, n int) string {
	if cells(name)+2 <= n {
		return elided() + "/" + name
	}
	if cells(name)+1 <= n {
		return elided() + name
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
			return joinSegments(append(segs, &last))
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
	return lastResort(last, n)
}
