package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/draft"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/md"
)

// mdTheme paints rendered markdown in the interface's own palette.
// It is set from the theme.
var mdTheme md.Theme

// renderMarkdown turns a comment body into tview markup, indented so it sits
// under its author.
func renderMarkdown(body, indent string) string {
	out := md.RenderTheme(body, mdTheme)
	if out == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// commentsWidth is as wide as the comments dialog grows: a comment set
// across a wide monitor is read like a line of a newspaper.
const commentsWidth = 140

// showLineComposer writes a comment on a line of the latest version:
// Publish starts a thread on it on the forge, Save keeps it in unagit to
// publish later with P - or, with Incomm on, for the worktree to take in.
func (a *App) showLineComposer(mr forge.MergeRequest, path string, line int, changed func(posted *forge.Note)) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	form := tview.NewForm()
	styleForm(form)
	form.AddTextArea("", "", 66, 10, 0, nil)
	setPlaceholder(form.GetFormItem(0).(*tview.TextArea), "Write a comment…")
	later := "Save keeps it in unagit until P publishes it."
	if a.cfg.Integrations.Incomm {
		later = "Save keeps it in unagit; a worktree opened takes it into Incomm."
	}
	form.AddTextView("", "Publish puts it on the forge now.\n"+later, 66, 2, true, false)
	area := func() *tview.TextArea { return form.GetFormItem(0).(*tview.TextArea) }
	note := func() *tview.TextView { return form.GetFormItem(1).(*tview.TextView) }
	where := fmt.Sprintf("%s:%d", path, line)
	busy := false
	body := func() (string, bool) {
		text := strings.TrimSpace(area().GetText())
		if text == "" {
			note().SetText("Nothing written yet.")
			return "", false
		}
		return text, !busy
	}
	finish := func(said string, posted *forge.Note) {
		a.closeModal(pageForm)
		if changed != nil {
			changed(posted)
		}
		a.done(said)
		// The counts in the list follow, after what was written is drawn.
		a.refreshDisk()
		a.mrsPane.reload()
	}
	publish := func() {
		text, ok := body()
		if !ok {
			return
		}
		busy = true
		note().SetText("Publishing …")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			posted, err := client.CreateDiscussion(ctx, mr, path, line, text)
			a.tv.QueueUpdateDraw(func() {
				busy = false
				if err != nil {
					note().SetText(err.Error())
					return
				}
				finish("Comment on "+where+" published", posted)
			})
		}()
	}
	save := func() {
		text, ok := body()
		if !ok {
			return
		}
		if _, err := a.draftStore().Add(a.draftKey(mr), draft.Draft{Path: path, Line: line, Body: text}); err != nil {
			note().SetText(err.Error())
			return
		}
		finish("Comment on "+where+" saved · P publishes it", nil)
	}
	form.AddButton("Publish", publish)
	form.AddButton("Save", save)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS {
			save()
			return nil
		}
		return ev
	})
	a.showFormModal("Comment on "+where, form, 19)
}

// showNoteEditor edits the text of a comment: save gets the new text and
// says, by calling back, whether it was kept.
func (a *App) showNoteEditor(title, text string, save func(body string, done func(error))) {
	form := tview.NewForm()
	styleForm(form)
	form.AddTextArea("", text, 66, 10, 0, nil)
	form.AddTextView("", "Markdown is understood.\nCtrl-S saves it, Esc closes without saving.", 66, 2, true, false)
	area := func() *tview.TextArea { return form.GetFormItem(0).(*tview.TextArea) }
	note := func() *tview.TextView { return form.GetFormItem(1).(*tview.TextView) }
	busy := false
	keep := func() {
		body := strings.TrimSpace(area().GetText())
		switch {
		case busy:
			return
		case body == "":
			note().SetText("Nothing written - d deletes a comment.")
			return
		case body == strings.TrimSpace(text):
			a.closeModal(pageForm)
			return
		}
		busy = true
		note().SetText("Saving …")
		save(body, func(err error) {
			busy = false
			if err != nil {
				note().SetText(err.Error())
				return
			}
			a.closeModal(pageForm)
		})
	}
	form.AddButton("Save", keep)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS {
			keep()
			return nil
		}
		return ev
	})
	a.showFormModal(title, form, 19)
}

// resolveThread resolves a thread on the forge, or opens it again, and
// reads the conversation again.
func (a *App) resolveThread(mr forge.MergeRequest, root forge.Note, changed func()) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	resolve := !root.Resolved
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := client.ResolveDiscussion(ctx, mr, root.Thread, resolve)
		a.tv.QueueUpdateDraw(func() {
			switch {
			case errors.Is(err, forge.ErrNotSupported):
				a.flash("this forge resolves a thread only on its page - w opens it")
			case err != nil:
				a.errorf("cannot change the thread: %v", err)
			default:
				if resolve {
					a.done("Thread resolved")
				} else {
					a.done("Thread opened again")
				}
				changed()
			}
		})
	}()
}

// threadBubbles is a conversation as boxes: the first comment, then its
// replies nested under it. short cuts a very long comment down.
func threadBubbles(thread []forge.Note, short bool) []bubble {
	bubbles := make([]bubble, len(thread))
	frame := threadFrame(thread[0])
	for j, n := range thread {
		body := n.Body
		if short {
			body = trimBody(body)
		}
		b := bubble{Name: personName(n.Author), Colour: colAccent, Meta: noteMeta(n, j > 0), Body: body, Frame: frame}
		if j > 0 {
			b.Indent = 2
		} else if n.Path != "" {
			b.Where = fmt.Sprintf("%s:%d", n.Path, n.Line)
		}
		bubbles[j] = b
	}
	return bubbles
}

// threadFrame is the colour a thread is framed in, by its state: open, or
// resolved, or a comment that is neither. Its replies and its code wear it
// too, so a thread reads as one.
func threadFrame(root forge.Note) tcell.Color {
	switch {
	case root.Resolvable && root.Resolved:
		return role("comments.resolved_border")
	case root.Resolvable:
		return role("comments.unresolved_border")
	}
	return role("comments.border")
}

// groupIntoThreads collects the notes of each conversation, oldest first
// inside a thread and by when the conversation started between them. Notes
// from a forge that does not thread carry no thread of their own and each
// stand alone.
func groupIntoThreads(notes []forge.Note) [][]forge.Note {
	var threads [][]forge.Note
	byThread := map[string]int{}
	for _, n := range notes {
		if n.Thread == "" {
			threads = append(threads, []forge.Note{n})
			continue
		}
		if at, ok := byThread[n.Thread]; ok {
			threads[at] = append(threads[at], n)
			continue
		}
		byThread[n.Thread] = len(threads)
		threads = append(threads, []forge.Note{n})
	}
	for _, thread := range threads {
		sort.SliceStable(thread, func(i, j int) bool {
			return thread[i].CreatedAt.Before(thread[j].CreatedAt)
		})
	}
	sort.SliceStable(threads, func(i, j int) bool {
		return threads[i][0].CreatedAt.Before(threads[j][0].CreatedAt)
	})
	return threads
}

// noteMeta is what a comment's box says after its author: when it was written
// and, on the first comment of a conversation, whether the conversation is
// resolved. Being resolved belongs to the conversation, not to each reply, and
// where it is anchored goes in the box's own place row.
func noteMeta(n forge.Note, reply bool) string {
	meta := tag(colDim) + humanAge(n.CreatedAt) + tagEnd
	if n.Resolvable && !reply {
		if n.Resolved {
			meta += tag(colDim) + " · resolved" + tagEnd
		} else {
			meta += tag(colWarn) + " · unresolved" + tagEnd
		}
	}
	return meta
}

// showComposer writes a new comment, or with to a reply to it in its
// thread. onSent runs once it is posted, with what the forge made of it
// when it says.
func (a *App) showComposer(mr forge.MergeRequest, to *forge.Note, onSent func(*forge.Note)) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)

	form := tview.NewForm()
	styleForm(form)
	form.AddTextArea("", "", 66, 10, 0, nil)
	setPlaceholder(form.GetFormItem(0).(*tview.TextArea), "Write a comment…")
	status := "Markdown is understood.\nCtrl-S sends it, Esc closes without sending."
	title := fmt.Sprintf("Comment on %s !%d", path, mr.IID)
	if to != nil {
		setPlaceholder(form.GetFormItem(0).(*tview.TextArea), "Write a reply…")
		status = "Answers " + personName(to.Author) + ": " + trunc(firstLine(strings.TrimSpace(to.Body)), 40) + "\nCtrl-S sends it, Esc closes without sending."
		title = fmt.Sprintf("Reply to %s · %s !%d", personName(to.Author), path, mr.IID)
	}
	form.AddTextView("", status, 66, 2, true, false)

	area := func() *tview.TextArea { return form.GetFormItem(0).(*tview.TextArea) }
	note := func() *tview.TextView { return form.GetFormItem(1).(*tview.TextView) }

	sending := false
	send := func() {
		if sending {
			return
		}
		body := strings.TrimSpace(area().GetText())
		if body == "" {
			note().SetText("Nothing written yet.")
			return
		}
		sending = true
		note().SetText("Sending …")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var (
				posted *forge.Note
				err    error
			)
			if to != nil && to.Thread != "" {
				posted, err = client.ReplyToDiscussion(ctx, mr, to.Thread, body)
			} else {
				posted, err = client.CommentNote(ctx, mr, body)
			}
			a.tv.QueueUpdateDraw(func() {
				sending = false
				if err != nil {
					note().SetText(err.Error())
					return
				}
				a.closeModal(pageForm)
				if to != nil {
					a.done(fmt.Sprintf("Reply to %s posted on %s !%d", personName(to.Author), path, mr.IID))
				} else {
					a.done(fmt.Sprintf("Comment posted on %s !%d", path, mr.IID))
				}
				if onSent != nil {
					onSent(posted)
				}
				a.reloadMRDetail(mr)
			})
		}()
	}

	form.AddButton("Send", send)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS {
			send()
			return nil
		}
		return ev
	})

	a.showFormModal(title, form, 19)
}

// approveMR approves after asking, because it is seen by everyone else on the
// merge request.
func (a *App) approveMR(mr forge.MergeRequest, onDone func()) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	body := fmt.Sprintf("Approve [::b]%s !%d[::-]?\n\n%s\n\nEveryone on the merge request will see it.",
		path, mr.IID, tview.Escape(mr.Title))

	a.confirmWith("Approve", body, "Approve", nil, func() {
		// One request; a task modal would be heavier than the action.
		a.note(fmt.Sprintf("Approving %s !%d …", path, mr.IID))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := client.Approve(ctx, mr)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("%v", err)
					return
				}
				a.done(fmt.Sprintf("Approved %s !%d", path, mr.IID))
				if onDone != nil {
					onDone()
				}
				a.reloadMRDetail(mr)
			})
		}()
	})
}

// reloadMRDetail refreshes the detail column when it is showing this merge
// request, so an approval or a comment appears straight away.
func (a *App) reloadMRDetail(mr forge.MergeRequest) {
	p := a.mrsPane
	if !p.detailShown || p.detailFor < 0 || p.detailFor >= len(a.mrs) {
		return
	}
	if current := a.mrs[p.detailFor]; current.IID == mr.IID && current.Instance == mr.Instance {
		a.showMRDetail(mr, false)
	}
}
