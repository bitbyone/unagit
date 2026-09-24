package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/md"
)

// mdTheme paints rendered markdown in the interface's own palette.
var mdTheme = md.Theme{
	Text:    colText.String(),
	Heading: colAccent.String(),
	Code:    colWarn.String(),
	Quote:   colMuted.String(),
	Link:    colAccent.String(),
	Muted:   colDim.String(),
}

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

// showComments opens the whole conversation of a merge request, oldest first,
// with the markdown rendered.
func (a *App) showComments(mr forge.MergeRequest) {
	path := a.projectPathOfMR(mr)
	title := fmt.Sprintf("Comments · %s !%d", path, mr.IID)

	view := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)
	view.SetTextColor(colText)
	box(view.Box, title).SetBorderPadding(0, 0, 1, 1)

	footer := tview.NewTextView().SetDynamicColors(true)
	footer.SetText(" " + tag(colDim) +
		"i  write a comment   ·   a  approve   ·   r  reload   ·   j k  scroll   ·   Esc  close" + tagEnd)

	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, true).
		AddItem(footer, 1, 0, false)

	fitFooter(frame, footer, 0)
	var load func()
	load = func() {
		client := a.client(mr.Instance)
		if client == nil {
			view.SetText(tag(colBad) + a.instanceLabel(mr.Instance) + " has no token" + tagEnd)
			return
		}
		view.SetText(tag(colMuted) + "Loading the conversation…" + tagEnd)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			notes, err := client.MergeRequestNotes(ctx, mr, 200)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					view.SetText(tag(colBad) + tview.Escape(err.Error()) + tagEnd)
					return
				}
				view.SetText(renderConversation(notes))
				view.ScrollToEnd()
			})
		}()
	}

	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEsc:
			a.closeModal(pageComments)
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'q':
				a.closeModal(pageComments)
				return nil
			case 'i':
				a.showComposer(mr, load)
				return nil
			case 'a':
				a.approveMR(mr, load)
				return nil
			case 'r':
				load()
				return nil
			}
		}
		return ev
	})

	a.pages.AddPage(pageComments, modalPct(frame, 80, 85), true, true)
	a.tv.SetFocus(view)
	load()
}

// renderConversation lays the comments out as the threads they belong to:
// each conversation in the order it was written, replies indented under what
// they answer, and the conversations themselves oldest first.
func renderConversation(notes []forge.Note) string {
	var human []forge.Note
	for _, n := range notes {
		if !n.System && strings.TrimSpace(n.Body) != "" {
			human = append(human, n)
		}
	}
	if len(human) == 0 {
		return tag(colMuted) + "No comments yet. Press i to write the first one." + tagEnd
	}

	threads := groupIntoThreads(human)
	var b strings.Builder
	for i, thread := range threads {
		if i > 0 {
			b.WriteString("\n" + tag(colDim) + strings.Repeat("┈", 40) + tagEnd + "\n\n")
		}
		for j, n := range thread {
			indent := ""
			if j > 0 {
				// A reply sits under what it answers.
				indent = "  "
				b.WriteString("\n")
			}
			b.WriteString(indent + noteHeader(n, j > 0) + "\n")
			if body := renderMarkdown(n.Body, indent+"  "); body != "" {
				b.WriteString(body + "\n")
			}
		}
	}
	return b.String()
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

// noteHeader is the byline of one comment; a reply is marked as one.
func noteHeader(n forge.Note, reply bool) string {
	head := ""
	if reply {
		head = tag(colDim) + "↳ " + tagEnd
	}
	head += fmt.Sprintf("%s%s%s  %s%s", tag(colAccent), tview.Escape(n.Author.Username), tagEnd,
		tag(colDim), humanAge(n.CreatedAt))
	if n.Path != "" {
		head += fmt.Sprintf(" · %s:%d", tview.Escape(n.Path), n.Line)
	}
	head += tagEnd
	if n.Resolvable {
		if n.Resolved {
			head += tag(colDim) + " · resolved" + tagEnd
		} else {
			head += tag(colWarn) + " · unresolved" + tagEnd
		}
	}
	return head
}

// showComposer writes a new comment. onSent runs once it is posted.
func (a *App) showComposer(mr forge.MergeRequest, onSent func()) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)

	form := tview.NewForm()
	styleForm(form)
	form.AddTextArea("", "", 66, 10, 0, nil)
	form.GetFormItem(0).(*tview.TextArea).SetPlaceholder("Write a comment…")
	status := "Markdown is understood.\nCtrl-S sends it, Esc closes without sending."
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
			err := client.Comment(ctx, mr, body)
			a.tv.QueueUpdateDraw(func() {
				sending = false
				if err != nil {
					note().SetText(err.Error())
					return
				}
				a.closeModal(pageForm)
				a.note(fmt.Sprintf("Comment posted on %s !%d", path, mr.IID))
				if onSent != nil {
					onSent()
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

	a.showFormModal(fmt.Sprintf("Comment on %s !%d", path, mr.IID), form, 19)
}

// approveMR approves after asking, because it is seen by everyone else on the
// merge request.
func (a *App) approveMR(mr forge.MergeRequest, onDone func()) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(mr.Instance))
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
				a.note(fmt.Sprintf("Approved %s !%d", path, mr.IID))
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
