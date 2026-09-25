package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

func TestBubbleIsABoxOfTheAskedWidth(t *testing.T) {
	out := renderBubble(bubble{Name: "jane", Colour: colAccent, Meta: tag(colDim) + "2h ago" + tagEnd,
		Body: "a comment that is a good deal longer than the box it has to fit into, so it must wrap"}, 40)
	rows := strings.Split(out, "\n")
	if len(rows) < 5 {
		t.Fatalf("expected a top, a header, wrapped text and a bottom, got:\n%s", out)
	}
	for _, r := range rows {
		if w := tview.TaggedStringWidth(r); w != 40 {
			t.Errorf("row is %d cells wide, want 40: %q", w, r)
		}
	}
	if !strings.Contains(rows[0], "╭") || !strings.Contains(rows[len(rows)-1], "╰") {
		t.Errorf("the box is not closed:\n%s", out)
	}
	if !strings.Contains(rows[1], "jane") || !strings.Contains(rows[1], "2h ago") {
		t.Errorf("the header row lacks the author or time: %q", rows[1])
	}
}

func TestAReplyIsNestedAndNarrower(t *testing.T) {
	out := renderBubbles([]bubble{
		{Name: "ann", Colour: colAccent, Body: "root"},
		{Name: "john", Colour: colAccent, Body: "answer", Indent: 2},
	}, 40)
	rows := strings.Split(out, "\n")
	first, last := rows[0], rows[len(rows)-1]
	if !strings.HasPrefix(last, "  ") || strings.HasPrefix(first, " ") {
		t.Errorf("the reply is not indented under its root:\n%s", out)
	}
	for _, r := range rows {
		if w := tview.TaggedStringWidth(r); w != 40 {
			t.Errorf("row is %d cells wide, want 40 (the indent counts): %q", w, r)
		}
	}
}

func TestStyleSurvivesAWrappedLine(t *testing.T) {
	// A code span that is cut across two rows must still be coloured on the
	// second one, although the border between them had to reset the style.
	out := renderBubble(bubble{Name: "x", Colour: colAccent,
		Body: "start `an inline code span that runs over the end of one row` end"}, 32)
	rows := strings.Split(out, "\n")
	code := colWarn.String()
	carried := 0
	for _, r := range rows[2 : len(rows)-1] {
		if strings.Contains(r, "["+code+":") {
			carried++
		}
	}
	if carried < 2 {
		t.Errorf("the code colour was not put back on the wrapped row:\n%s", out)
	}
}

func TestBubbleNeverGoesNarrowerThanItsFloor(t *testing.T) {
	out := renderBubble(bubble{Name: "x", Colour: colAccent, Body: "hi"}, 5)
	for _, r := range strings.Split(out, "\n") {
		if w := tview.TaggedStringWidth(r); w != minBubbleWidth {
			t.Errorf("row is %d cells wide, want the floor %d", w, minBubbleWidth)
		}
	}
}

func TestConversationIsBoxesInThreads(t *testing.T) {
	notes := []forge.Note{
		{ID: 1, Thread: "t", Body: "first", Author: forge.User{Username: "ann"}, Path: "a.go", Line: 3, Resolvable: true},
		{ID: 2, Thread: "t", Body: "answer", Author: forge.User{Username: "john"}},
	}
	got := renderConversation(notes, 60)
	if strings.Count(got, "╭") != 2 || strings.Count(got, "╰") != 2 {
		t.Errorf("want one box per comment:\n%s", got)
	}
	if !strings.Contains(got, "a.go:3") || !strings.Contains(got, "unresolved") {
		t.Errorf("the root should say where it is and whether it is resolved:\n%s", got)
	}
	if strings.Count(got, "a.go:3") != 1 {
		t.Errorf("only the first comment of a conversation names the place:\n%s", got)
	}
}

func TestAPlaceThatDoesNotFitGetsItsOwnRowAndKeepsTheTime(t *testing.T) {
	long := "src/pages/appPages/organizations/components/AddOrganizationModal/addOrganizationSchema.ts:10"
	out := renderBubble(bubble{Name: "tomas", Colour: colAccent,
		Meta:  tag(colDim) + "2d ago" + tagEnd + tag(colWarn) + " · unresolved" + tagEnd,
		Where: long, Body: "text"}, 60)
	rows := strings.Split(out, "\n")
	if !strings.Contains(rows[1], "2d ago") || !strings.Contains(rows[1], "unresolved") {
		t.Errorf("the time and the state must stay on the header: %q", rows[1])
	}
	if strings.Contains(rows[1], "src/pages") {
		t.Errorf("the place should not squeeze the header: %q", rows[1])
	}
	if !strings.Contains(rows[2], "…") || !strings.Contains(rows[2], "addOrganizationSchema.ts:10") {
		t.Errorf("the place should be cut from the left, keeping the file name: %q", rows[2])
	}
	for _, r := range rows {
		if w := tview.TaggedStringWidth(r); w != 60 {
			t.Errorf("row is %d cells wide, want 60: %q", w, r)
		}
	}

	short := renderBubble(bubble{Name: "tomas", Colour: colAccent, Meta: tag(colDim) + "2d ago" + tagEnd,
		Where: "a.go:3", Body: "text"}, 60)
	if got := strings.Split(short, "\n"); !strings.Contains(got[1], "a.go:3") {
		t.Errorf("a place that fits joins the header: %q", got[1])
	}
}

func TestOnlyTheFirstCommentSaysWhetherTheConversationIsResolved(t *testing.T) {
	root := forge.Note{Resolvable: true, Resolved: false}
	if got := noteMeta(root, false); !strings.Contains(got, "unresolved") {
		t.Errorf("the first comment should carry the state: %q", got)
	}
	if got := noteMeta(root, true); strings.Contains(got, "resolved") {
		t.Errorf("a reply must not carry the conversation's state: %q", got)
	}
	if got := noteMeta(forge.Note{Resolvable: true, Resolved: true}, false); !strings.Contains(got, " · resolved") {
		t.Errorf("a resolved conversation should say so: %q", got)
	}
}
