package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/tobola/unagit/internal/forge"
)

// TestTheCursorStartsAtTheTopWhateverLoadsAfter: the list read at start
// is put in order again as what loads behind it arrives - sizes, visits,
// where each clone stands. Until the person moves, the cursor stays on the
// first row and the view at the top, rather than following the repository
// it happened to start on down the list.
func TestTheCursorStartsAtTheTopWhateverLoadsAfter(t *testing.T) {
	t.Parallel()
	a, sc := newTestApp(t)
	waitFor(t, a, sc, "acme/gateway")
	changeOnLoop(a, func() {
		id := a.projects[0].Instance
		for i := range 30 {
			a.projects = append(a.projects, forge.Project{ID: 100 + i, Name: fmt.Sprintf("r%02d", i),
				PathWithNamespace: fmt.Sprintf("acme/r%02d", i), DefaultBranch: "main", Instance: id,
				LastActivityAt: time.Now().Add(-time.Duration(i+2) * time.Hour)})
		}
		a.projectsPane.reload()
	})
	waitFor(t, a, sc, "acme/r00")
	first := onLoop(a, func() string { return a.projects[a.projectsPane.selectedIndex()].PathWithNamespace })
	// What loaded behind it puts the first repository fourth.
	changeOnLoop(a, func() {
		for i := range a.projects {
			if a.projects[i].PathWithNamespace == first {
				a.projects[i].LastActivityAt = time.Now().Add(-150 * time.Minute)
			}
		}
		a.projectsPane.reload()
	})
	at := onLoop(a, func() [2]int {
		r, _ := a.projectsPane.table.GetSelection()
		o, _ := a.projectsPane.table.GetOffset()
		return [2]int{r, o}
	})
	row, offset := at[0], at[1]
	if row != 1 || offset != 0 {
		t.Errorf("the cursor is on row %d with the view %d rows down, want the first row at the top", row, offset)
	}
}
