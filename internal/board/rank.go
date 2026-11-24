package board

import "strings"

// A task's place in its column is a string, compared as text, so a task
// put between two others takes a rank between theirs and nothing else is
// written - one file a task, however the column is ordered.

const rankDigits = "0123456789abcdefghijklmnopqrstuvwxyz"

// Between is a rank after a and before b; "" for a is the start of the
// column, for b its end. a must sort before b when both are given.
func Between(a, b string) string {
	base := len(rankDigits)
	var out strings.Builder
	open := b == "" // past b's digits nothing bounds it from above
	for i := 0; ; i++ {
		lo := 0
		if i < len(a) {
			lo = strings.IndexByte(rankDigits, a[i])
		}
		hi := base
		if !open && i < len(b) {
			hi = strings.IndexByte(rankDigits, b[i])
		}
		switch {
		case hi-lo > 1:
			out.WriteByte(rankDigits[(lo+hi)/2])
			return out.String()
		case hi-lo == 1:
			// lo here is below b from now on: the rest only has to come
			// after a.
			out.WriteByte(rankDigits[lo])
			open = true
		default:
			out.WriteByte(rankDigits[lo])
		}
	}
}
