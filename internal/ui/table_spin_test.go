package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestAMoveInATableWithNothingToSelectEnds: a table whose rows cannot be
// selected - a heading and a message, as Activity is with every watch
// stopped - with its cursor left past its rows, at none, or in a column
// that is gone. tview spins for ever moving it; put back inside first, the
// move ends.
func TestAMoveInATableWithNothingToSelectEnds(t *testing.T) {
	t.Parallel()
	keys := []tcell.Key{tcell.KeyDown, tcell.KeyUp, tcell.KeyPgDn, tcell.KeyPgUp, tcell.KeyHome, tcell.KeyEnd}
	for _, columns := range []int{1, 4} {
		for _, at := range [][2]int{{5, 0}, {5, 3}, {1, 3}, {-1, -1}} {
			for _, key := range keys {
				table := tview.NewTable().SetSelectable(true, false)
				for c := 0; c < columns; c++ {
					table.SetCell(0, c, tview.NewTableCell("WHAT").SetSelectable(false))
				}
				table.SetCell(1, 0, tview.NewTableCell("Nothing under way.").SetSelectable(false))
				table.Select(at[0], at[1])
				done := make(chan struct{})
				go func() {
					selectionInTable(table)
					table.InputHandler()(tcell.NewEventKey(key, 0, tcell.ModNone), func(tview.Primitive) {})
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatalf("%s", fmt.Sprintf("%d columns, cursor at %v, %s: the move never ended", columns, at, tcell.KeyNames[key]))
				}
			}
		}
	}
}
