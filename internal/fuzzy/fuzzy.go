// Package fuzzy implements a small subsequence matcher in the spirit of fzf.
package fuzzy

import "strings"

// Match reports whether every rune of pattern appears in text in order, and
// scores the match: consecutive runs and matches at word boundaries rank
// higher, and a shorter haystack breaks ties.
//
// The pattern is split on spaces; every term must match somewhere in text.
func Match(pattern, text string) (int, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return 0, true
	}
	lowerText := strings.ToLower(text)
	total := 0
	for _, term := range strings.Fields(strings.ToLower(pattern)) {
		score, ok := matchTerm(term, lowerText)
		if !ok {
			return 0, false
		}
		total += score
	}
	return total - len(text)/16, true
}

func matchTerm(term, text string) (int, bool) {
	// An exact substring hit always beats a scattered subsequence.
	if idx := strings.Index(text, term); idx >= 0 {
		score := 100 + len(term)*8
		if idx == 0 || isBoundary(text[idx-1]) {
			score += 40
		}
		return score, true
	}
	score, ti := 0, 0
	lastFound := -2
	for i := 0; i < len(term); i++ {
		c := term[i]
		found := -1
		for ; ti < len(text); ti++ {
			if text[ti] == c {
				found = ti
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		score += 4
		if found == lastFound+1 {
			score += 8
		}
		if found == 0 || isBoundary(text[found-1]) {
			score += 10
		}
		lastFound = found
		ti++
	}
	return score, true
}

func isBoundary(c byte) bool {
	switch c {
	case '/', '-', '_', ' ', '.', ':', '[', '(':
		return true
	}
	return false
}
