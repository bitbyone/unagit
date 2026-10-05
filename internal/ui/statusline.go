package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// statusLine is the line under a screen: what the screen says on the left,
// the jobs under way at the right-hand end, just before "? help". On the
// right a job comes and goes without moving the summary. When the two do
// not fit side by side the jobs go on a line of their own above it, as many
// lines as they wrap to, so the summary stays the bottom line: they are never cut, since a running refresh that
// cannot be seen looks like one that does not run.
type statusLine struct {
	*tview.Flex
	text, help    *tview.TextView
	row           *tview.Flex
	inline, above *tview.TextView
	jobs          string
}

// helpWidth is the room "? help" takes at the end of the line.
const helpWidth = 8

// newStatusLine lays text and the help hint out as a status line.
func newStatusLine(text, help *tview.TextView) *statusLine {
	s := &statusLine{
		Flex:   tview.NewFlex().SetDirection(tview.FlexRow),
		text:   text,
		help:   help,
		inline: tview.NewTextView().SetDynamicColors(true),
		above:  tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetTextAlign(tview.AlignRight),
	}
	s.row = tview.NewFlex().
		AddItem(text, 0, 1, false).
		AddItem(s.inline, 0, 0, false).
		AddItem(help, helpWidth, 0, false)
	s.Flex.AddItem(s.above, 0, 0, false).AddItem(s.row, 1, 0, false)
	return s
}

// setRight says what the right-hand end holds - the word last said and the
// jobs under way - "" when nothing.
func (s *statusLine) setRight(jobs string) {
	s.jobs = jobs
	s.inline.SetText(jobs)
	s.above.SetText(jobs)
}

// fit lays the line out for a width and says how many rows it takes.
func (s *statusLine) fit(width int) int {
	if s.jobs == "" {
		s.row.ResizeItem(s.inline, 0, 0)
		s.Flex.ResizeItem(s.above, 0, 0)
		return 1
	}
	jobs := tview.TaggedStringWidth(s.jobs) + 2
	if tview.TaggedStringWidth(s.text.GetText(false))+jobs+helpWidth <= width {
		s.row.ResizeItem(s.inline, jobs, 0)
		s.Flex.ResizeItem(s.above, 0, 0)
		return 1
	}
	// The jobs' own lines end where the help ends, one cell in, like the
	// rest of the line.
	lines := len(tview.WordWrap(s.jobs, max(1, width-1)))
	s.row.ResizeItem(s.inline, 0, 0)
	s.Flex.ResizeItem(s.above, lines, 0)
	return 1 + lines
}

// holdIn gives the line, an item of holder, the rows it needs each time
// holder is drawn: the width is known only then.
func (s *statusLine) holdIn(holder *tview.Flex) {
	holder.SetDrawFunc(func(_ tcell.Screen, x, y, w, h int) (int, int, int, int) {
		holder.ResizeItem(s, s.fit(w), 0)
		return x, y, w, h
	})
}
