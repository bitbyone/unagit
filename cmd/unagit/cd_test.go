package main

import (
	"testing"

	"github.com/tobola/unagit/internal/session"
)

func TestMatchingNarrowsBySearch(t *testing.T) {
	open := []session.Record{
		{Project: "my2n/ng/calling", IID: 19, Mode: session.ModeReview, Dir: "/a"},
		{Project: "my2n/ng/email-dispatcher", IID: 7, Mode: session.ModeBranch, Dir: "/b", Title: "Wrap rendered content"},
		{Project: "acme/api", Mode: session.ModeRepository, Dir: "/c", Server: "Github"},
	}

	cases := []struct {
		query string
		want  int
	}{
		{"calling", 1},
		{"my2n", 2},
		{"wrap", 1},    // the title counts
		{"19", 1},      // so does the number
		{"github", 1},  // and the server
		{"review", 1},  // and the mode
		{"CALLING", 1}, // case does not matter
		{"nonsense", 0},
	}
	for _, c := range cases {
		if got := len(matching(open, c.query)); got != c.want {
			t.Errorf("matching(%q) = %d, want %d", c.query, got, c.want)
		}
	}
}
