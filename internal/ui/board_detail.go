package ui

import (
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/board"
	"github.com/tobola/unagit/internal/forge"
)

// A task's detail is the dialog of a merge request's conversation, fed from
// the task: its description and links at the top, then its comments as
// cards with the lines of its story between them. j and k go from comment
// to comment, v selects in one, h hides the story; c writes a comment,
// e and d change the chosen one.

// taskDetail is the task a commentsDialog shows.
type taskDetail struct {
	v         *boardView
	space, id string
	t         board.Task
}

// storyID numbers the story's lines apart from the comments, which are
// numbered from 1 in their order.
const storyID = 1_000_000

// openTask shows a task in full; write starts a new comment at once, as c
// does on a merge request.
func (v *boardView) openTask(t board.Task, write bool) {
	d := &commentsDialog{a: v.app, task: &taskDetail{v: v, space: t.Space, id: t.ID, t: t}}
	v.app.openCommentsDialog(d, "Task · "+esc(v.space.Name),
		"c comment · e edit · d delete · E edit task · i link · w browser · H L move · x done · h story")
	if write {
		d.writeTaskComment()
	}
}

// loadTask reads the task again and draws it.
func (d *commentsDialog) loadTask() {
	td := d.task
	tasks, err := td.v.store.Tasks(td.space)
	if err != nil {
		d.view.SetText(tag(colBad) + esc(err.Error()) + tagEnd)
		return
	}
	found := false
	for _, t := range tasks {
		if t.ID == td.id {
			td.t, found = t, true
		}
	}
	if !found {
		d.view.SetText(tag(colMuted) + "The task is gone." + tagEnd)
		return
	}
	t := td.t
	notes := make([]forge.Note, 0, len(t.Comments)+len(t.Story))
	for i, c := range t.Comments {
		notes = append(notes, forge.Note{ID: i + 1, Body: c.Body, CreatedAt: c.Written})
	}
	for i, e := range t.Story {
		notes = append(notes, forge.Note{ID: storyID + i, Body: e.What, CreatedAt: e.At, System: true})
	}
	_, had := d.chosen()
	d.conv.notes = notes
	d.conv.top = func(width int) string { return taskTop(t, width) }
	d.conv.laid = nil
	d.changed()
	if !had {
		d.choose(len(t.Comments) - 1)
	}
}

// taskTop is what stands above a task's comments: its state, space and
// colour, its description, its links.
func taskTop(t board.Task, width int) string {
	var rows []string
	facts := []string{tag(role("board."+t.State)) + "[::b]" + esc(board.StateName(t.State)) + "[::-]" + tagEnd}
	if t.Colour != "" {
		facts = append(facts, tag(role("board.card."+t.Colour))+"████ "+esc(t.Colour)+tagEnd)
	}
	facts = append(facts, tag(colMuted)+"created "+humanAge(t.Created)+tagEnd)
	if !t.Done.IsZero() {
		facts = append(facts, tag(colMuted)+"done "+humanAge(t.Done)+tagEnd)
	}
	rows = append(rows, strings.Join(facts, tag(colMuted)+" · "+tagEnd), "")
	rows = append(rows, tag(colText)+"[::b]"+esc(t.Title)+"[::-]"+tagEnd, "")
	if body := strings.TrimSpace(t.Body); body != "" {
		for _, l := range strings.Split(strings.TrimRight(renderMarkdown(body, ""), "\n"), "\n") {
			rows = append(rows, tview.WordWrap(l, max(1, width))...)
		}
	} else {
		rows = append(rows, tag(colMuted)+"No description - E writes one."+tagEnd)
	}
	if len(t.Links) > 0 {
		rows = append(rows, "")
		for _, l := range t.Links {
			line := tag(role("board.link")) + "↗ " + esc(l.Name()) + tagEnd
			if l.Label != "" && l.URL != "" {
				line += tag(colMuted) + "  " + esc(l.URL) + tagEnd
			}
			rows = append(rows, line)
		}
	}
	return strings.Join(rows, "\n")
}

// chosenComment is the comment the cursor is on, by its place in the task.
func (d *commentsDialog) chosenComment() (int, board.Comment, bool) {
	card, ok := d.chosen()
	i := card.note.ID - 1
	if !ok || card.note.System || i < 0 || i >= len(d.task.t.Comments) {
		return 0, board.Comment{}, false
	}
	return i, d.task.t.Comments[i], true
}

// changeTask changes the task on disk and shows it, and the board, again.
func (d *commentsDialog) changeTask(what func(*board.Task)) {
	td := d.task
	if _, err := td.v.store.Change(td.space, td.id, func(m *board.Task) error { what(m); return nil }); err != nil {
		d.a.errorf("%v", err)
	}
	d.loadTask()
	td.v.reload()
}

func (d *commentsDialog) writeTaskComment() {
	d.a.showNoteEditor("Comment · "+d.task.t.Title, "", func(body string, done func(error)) {
		done(nil)
		d.changeTask(func(m *board.Task) {
			m.Comments = append(m.Comments, board.Comment{ID: board.NewID(), Body: body, Written: time.Now()})
		})
		// The story's lines are not cards: the last card is the comment.
		d.choose(len(d.task.t.Comments) - 1)
	})
}

// taskActions are what can be done in a task's detail.
func (d *commentsDialog) taskActions() []uiAction {
	a, td := d.a, d.task
	t := td.t
	i, c, chosen := d.chosenComment()
	state := 0
	for n, s := range board.States {
		if s == t.State {
			state = n
		}
	}
	return []uiAction{
		{name: "New Comment…", about: "Write a comment on the task.", keys: "c", rank: 10, run: d.writeTaskComment},
		{name: "Edit Comment…", about: "Put new text in the chosen comment.", keys: "e", rank: 20, when: func() bool { return chosen },
			run: func() {
				a.showNoteEditor("Edit the comment", c.Body, func(body string, done func(error)) {
					done(nil)
					d.changeTask(func(m *board.Task) {
						if i < len(m.Comments) && m.Comments[i].ID == c.ID {
							m.Comments[i].Body, m.Comments[i].Edited = body, time.Now()
						}
					})
				})
			}},
		{name: "Delete Comment…", about: "Take the chosen comment away, after asking.", keys: "d", rank: 30, when: func() bool { return chosen },
			run: func() {
				a.confirmWith("Delete", "Delete the comment?\n\n"+esc(trunc(strings.TrimSpace(c.Body), 200)), "Delete", nil, func() {
					d.changeTask(func(m *board.Task) {
						if i < len(m.Comments) && m.Comments[i].ID == c.ID {
							m.Comments = append(m.Comments[:i], m.Comments[i+1:]...)
						}
					})
				})
			}},
		{name: "Edit Task…", about: "Change the task's title and description.", keys: "E", rank: 40,
			run: func() { td.v.editTask(&t, t.State, d.loadTask) }},
		{name: "Link URL…", about: "Add a link to the task.", keys: "i", rank: 45, run: func() { td.v.linkURL(t, d.loadTask) }},
		{name: "Open in Browser", about: "The task's link; a list when it has several.", keys: "w", rank: 50,
			when: func() bool { return len(t.Links) > 0 }, run: func() { td.v.openLink(t) }},
		{name: "Move Right", about: "To the next column: to do, in progress, done.", keys: "L", rank: 60,
			when: func() bool { return state < 2 }, run: func() { td.v.setState(t, board.States[state+1]); d.loadTask() }},
		{name: "Move Left", about: "Back to the column before.", keys: "H", rank: 61,
			when: func() bool { return state > 0 }, run: func() { td.v.setState(t, board.States[state-1]); d.loadTask() }},
		{name: "Mark Done", about: "Move it to Done.", keys: "x", rank: 62,
			when: func() bool { return t.State != board.Done }, run: func() { td.v.setState(t, board.Done); d.loadTask() }},
		{name: "Open Again", about: "Back to the column of things to do.", keys: "x", rank: 63,
			when: func() bool { return t.State == board.Done }, run: func() { td.v.setState(t, board.Todo); d.loadTask() }},
		{name: "Select Text", about: "Put a cursor in the chosen comment, to select some of it and copy it with y.", keys: "v", rank: 70,
			when: func() bool { return chosen }, run: d.startVisual},
		{name: "Toggle Story", about: "Hide the lines of what happened to the task, to read the comments through, or show them again.", keys: "h", rank: 80,
			run: d.toggleCode},
	}
}
