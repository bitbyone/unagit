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
	"github.com/tobola/unagit/internal/incomm"
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
		"i  write a comment   ·   A  approve   ·   r  reload   ·   j k  scroll   ·   Esc  close" + tagEnd)

	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, true).
		AddItem(footer, 1, 0, false)

	fitFooter(frame, footer, 0)
	// The conversation is drawn as boxes, which have to know how wide they are.
	fit := a.widthAware(view)
	var load func()
	load = func() {
		fit(nil)
		client := a.client(mr.Instance)
		if client == nil {
			view.SetText(tag(colBad) + a.instanceLabel(mr.Instance) + " has no token" + tagEnd)
			return
		}
		view.SetText(tag(colMuted) + "Loading the conversation…" + tagEnd)
		// The worktrees are looked up here, where the configuration belongs to
		// the event loop; reading their files can wait for the goroutine.
		dirs := []string(nil)
		if a.cfg.Integrations.Incomm {
			dirs = a.mrWorktrees(mr)
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			notes, err := client.MergeRequestNotes(ctx, mr, 200)
			localThreads := incomm.ThreadsOf(dirs...)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					view.SetText(tag(colBad) + tview.Escape(err.Error()) + tagEnd)
					return
				}
				fit(func(width int) string {
					text := renderConversation(notes, width)
					if local := renderLocalThreads(localThreads, width); local != "" {
						text += "\n\n" + tag(colDim) + strings.Repeat("━", 40) + tagEnd + "\n\n" + local
					}
					return text
				})
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
			case 'A':
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
// each conversation as boxes in the order it was written, replies nested under
// what they answer, and the conversations themselves oldest first.
func renderConversation(notes []forge.Note, width int) string {
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
	parts := make([]string, len(threads))
	for i, thread := range threads {
		parts[i] = renderBubbles(threadBubbles(thread, false), width)
	}
	return strings.Join(parts, "\n\n")
}

// threadBubbles is a conversation as boxes: the first comment, then its
// replies nested under it. short cuts a very long comment down.
func threadBubbles(thread []forge.Note, short bool) []bubble {
	bubbles := make([]bubble, len(thread))
	for j, n := range thread {
		body := n.Body
		if short {
			body = trimBody(body)
		}
		b := bubble{Name: n.Author.Username, Colour: colAccent, Meta: noteMeta(n, j > 0), Body: body}
		if j > 0 {
			b.Indent = 2
		} else if n.Path != "" {
			b.Where = fmt.Sprintf("%s:%d", n.Path, n.Line)
		}
		bubbles[j] = b
	}
	return bubbles
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
