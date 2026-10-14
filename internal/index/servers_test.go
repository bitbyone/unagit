package index

import (
	"testing"

	"github.com/tobola/unagit/internal/forge"
)

func TestDedupeKeepsTheSameIDOnDifferentServers(t *testing.T) {
	t.Parallel()
	projects := []forge.Project{{Instance: "first", ID: 1}, {Instance: "second", ID: 1}, {Instance: "first", ID: 1}}
	if got := DedupeProjects(projects); len(got) != 2 {
		t.Fatalf("projects on different servers were merged: %v", got)
	}
	mrs := []forge.MergeRequest{{Instance: "first", ID: 1}, {Instance: "second", ID: 1}, {Instance: "first", ID: 1}}
	if got := DedupeMergeRequests(mrs); len(got) != 2 {
		t.Fatalf("merge requests on different servers were merged: %v", got)
	}
}
