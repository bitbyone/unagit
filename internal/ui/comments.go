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

// codeContext is how many lines of the diff a thread on code is read
// under, the commented line the last of them, as GitLab shows them.
const codeContext = 4

// showComments opens the whole conversation of a merge request, oldest first,
// with the markdown rendered: j and k go from comment to comment, i answers
// the one chosen, Enter shows the code it is on.
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
	footer.SetText(" " + litHint("i reply · c comment · Enter code · w browser · A approve · r reload"))

	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, true).
		AddItem(footer, 1, 0, false)

	fitFooter(frame, footer, 0)
	// The conversation is drawn as boxes, which have to know how wide they are.
	fit := a.widthAware(view)
	var (
		notes    []forge.Note
		diffs    []forge.FileDiff
		local    []incomm.Thread
		cards    []commentCard
		selected int // the chosen comment's ID, 0 before there is one
	)
	draw := func() {
		fit(func(width int) string {
			var text string
			text, cards = drawConversation(notes, diffs, width, selected)
			if extra := renderLocalThreads(local, width); extra != "" {
				text += "\n\n" + tag(colDim) + strings.Repeat(glyphBar, 40) + tagEnd + "\n\n" + extra
			}
			return text
		})
	}
	chosen := func() (commentCard, bool) {
		for _, c := range cards {
			if c.note.ID == selected {
				return c, true
			}
		}
		return commentCard{}, false
	}
	// choose moves to the comment at i, scrolling so that it shows - and the
	// code a thread is on, when it fits too.
	choose := func(i int) {
		if len(cards) == 0 {
			return
		}
		i = max(0, min(i, len(cards)-1))
		selected = cards[i].note.ID
		draw()
		c := cards[i]
		_, _, _, height := view.GetInnerRect()
		row, _ := view.GetScrollOffset()
		switch {
		case c.top < row:
			view.ScrollTo(c.top, 0)
		case c.to >= row+height:
			view.ScrollTo(max(c.top, c.to-height+1), 0)
		}
	}
	step := func(by int) {
		at := -1
		for i, c := range cards {
			if c.note.ID == selected {
				at = i
			}
		}
		if at < 0 {
			choose(0)
			return
		}
		choose(at + by)
	}
	var load func()
	load = func() {
		a.stopTextWait(view)
		fit(nil)
		client := a.client(mr.Instance)
		if client == nil {
			view.SetText(tag(colBad) + a.instanceLabel(mr.Instance) + " has no token" + tagEnd)
			return
		}
		finish := a.waitText(view, func() string { return a.loadingLine("Loading the conversation…") })
		// The worktrees are looked up here, where the configuration belongs to
		// the event loop; reading their files can wait for the goroutine.
		var places []incomm.Place
		if a.cfg.Integrations.Incomm {
			places = a.mrPlaces(mr)
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			// The code is read beside the comments; without it they are
			// still worth reading, so its failure only leaves it out.
			var fileDiffs []forge.FileDiff
			done := make(chan struct{})
			go func() {
				defer close(done)
				fileDiffs, _ = client.MergeRequestDiffs(ctx, mr)
			}()
			read, err := client.MergeRequestNotes(ctx, mr, 200)
			localThreads := incomm.ThreadsAt(places...)
			<-done
			a.tv.QueueUpdateDraw(func() {
				if !finish() {
					return
				}
				if err != nil {
					view.SetText(tag(colBad) + tview.Escape(err.Error()) + tagEnd)
					return
				}
				notes, diffs, local = a.namedNotes(mr.Instance, read), fileDiffs, localThreads
				draw()
				// The forge's conversation comes first and the local Incomm
				// threads after it, so it opens at the top - on what others
				// said - rather than on the last local note; a reload stays
				// on the comment chosen.
				if c, ok := chosen(); ok {
					view.ScrollTo(c.top, 0)
					return
				}
				view.ScrollToBeginning()
			})
		}()
	}
	reply := func() {
		c, ok := chosen()
		if !ok {
			a.showComposer(mr, nil, load)
			return
		}
		a.showComposer(mr, &c.note, load)
	}
	code := func() {
		c, ok := chosen()
		if !ok {
			a.flash("no comment chosen - j and k choose one")
			return
		}
		a.showCommentCode(mr, c.note, diffs)
	}

	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		switch ev.Key() {
		case tcell.KeyEsc:
			a.stopTextWait(view)
			a.closeModal(pageComments)
			return nil
		case tcell.KeyDown:
			step(1)
			return nil
		case tcell.KeyUp:
			step(-1)
			return nil
		case tcell.KeyEnter:
			code()
			return nil
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'q':
				a.stopTextWait(view)
				a.closeModal(pageComments)
				return nil
			case 'j':
				step(1)
				return nil
			case 'k':
				step(-1)
				return nil
			case 'g':
				choose(0)
				return nil
			case 'G':
				choose(len(cards) - 1)
				return nil
			case 'i':
				reply()
				return nil
			case 'c':
				a.showComposer(mr, nil, load)
				return nil
			case 'w':
				if c, ok := chosen(); ok && c.note.URL != "" {
					a.openWeb(c.note.URL)
					return nil
				}
				a.openWeb(mr.WebURL)
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

	a.pages.AddPage(pageComments, modalFit(frame, func(w, h int) (int, int) {
		return min(max(w*80/100, min(w, 60)), commentsWidth), h * 85 / 100
	}), true, true)
	a.tv.SetFocus(view)
	load()
}

// commentCard is where a comment stands in the drawn conversation: the
// rows of its card, the row to scroll to so that what it is read with -
// the code over a thread - shows with it, and the comment.
type commentCard struct {
	top, from, to int
	note          forge.Note
}

// renderConversation lays the comments out as the threads they belong to.
func renderConversation(notes []forge.Note, width int) string {
	text, _ := drawConversation(notes, nil, width, 0)
	return text
}

// drawConversation lays the comments out as the threads they belong to:
// each conversation as cards in the order it was written, replies nested
// under what they answer, a thread on code under the lines it is about,
// and the conversations themselves oldest first. selected is the ID of
// the comment chosen. It says where each comment's card is.
func drawConversation(notes []forge.Note, diffs []forge.FileDiff, width, selected int) (string, []commentCard) {
	var human []forge.Note
	for _, n := range notes {
		if !n.System && strings.TrimSpace(n.Body) != "" {
			human = append(human, n)
		}
	}
	if len(human) == 0 {
		return tag(colMuted) + "No comments yet. Press c to write the first one." + tagEnd, nil
	}

	var (
		b     strings.Builder
		row   int
		cards []commentCard
	)
	write := func(s string) {
		b.WriteString(s)
		row += strings.Count(s, "\n")
	}
	for i, thread := range groupIntoThreads(human) {
		if i > 0 {
			write("\n\n")
		}
		top := row
		bubbles := threadBubbles(thread, false)
		if snippet := codeSnippet(thread[0], diffs, width); snippet != "" {
			write(snippet + "\n")
			bubbles[0].Where = ""
		}
		for j, bubble := range bubbles {
			if j > 0 {
				write("\n")
			}
			bubble.Selected = thread[j].ID == selected
			from := row
			write(renderBubble(bubble, width))
			card := commentCard{top: from, from: from, to: row, note: thread[j]}
			if j == 0 {
				card.top = top
			}
			cards = append(cards, card)
		}
	}
	return b.String(), cards
}

// codeSnippet is the code a comment is on, as GitLab shows it over a
// thread: a few lines of the diff ending at the commented one, in a frame
// named by the file. Nothing when the comment is on no line, or its line
// is no longer in the diff.
func codeSnippet(n forge.Note, diffs []forge.FileDiff, width int) string {
	lines, at := commentedLines(n, diffs)
	if at < 0 {
		return ""
	}
	from := at
	for from > 0 && at-from < codeContext-1 && lines[from-1].kind != '@' {
		from--
	}
	return codeFrame(fmt.Sprintf("%s:%d", n.Path, n.Line), n.Path, lines[from:at+1], at-from, width)
}

// commentedLines is the diff of the file a comment is on and the index of
// its line there, -1 when it has none: a comment on a deleted line counts
// the old side's lines, any other the new side's.
func commentedLines(n forge.Note, diffs []forge.FileDiff) ([]diffLine, int) {
	if n.Path == "" || n.Line <= 0 {
		return nil, -1
	}
	for _, d := range diffs {
		if d.Path != n.Path && !(n.Orphaned && d.OldPath == n.Path) {
			continue
		}
		lines := parseDiff(d.Diff)
		for i, l := range lines {
			switch {
			case l.kind == '@' || l.kind == '\\':
			case n.Orphaned && l.kind != '+' && l.old == n.Line,
				!n.Orphaned && l.kind != '-' && l.new == n.Line:
				return lines, i
			}
		}
		return lines, -1
	}
	return nil, -1
}

// codeFrame draws lines of a diff in a frame titled with where they are,
// width cells wide.
func codeFrame(title, path string, lines []diffLine, mark, width int) string {
	total := max(width, minBubbleWidth)
	frame := tag(role("comments.code_border"))
	label := tag(role("comments.where")) + esc(truncLeft(title, total-6)) + tagEnd
	rule := func(n int) string { return strings.Repeat(string(tview.Borders.Horizontal), max(0, n)) }
	rows := []string{frame + string(tview.Borders.TopLeft) + string(tview.Borders.Horizontal) + " " + tagEnd + label +
		frame + " " + rule(total-6-tview.TaggedStringWidth(label)) + string(tview.Borders.Horizontal) + string(tview.Borders.TopRight) + inkReset}
	edge := frame + string(tview.Borders.Vertical) + inkReset
	body := strings.TrimRight(drawDiffLines(path, lines, total-2, mark, true), "\n")
	for _, line := range strings.Split(body, "\n") {
		rows = append(rows, edge+line+inkReset+edge)
	}
	rows = append(rows, frame+string(tview.Borders.BottomLeft)+rule(total-2)+string(tview.Borders.BottomRight)+inkReset)
	return strings.Join(rows, "\n")
}

// showCommentCode shows the whole diff of the file a comment is on, at its
// line - Enter on a comment in the conversation.
func (a *App) showCommentCode(mr forge.MergeRequest, n forge.Note, diffs []forge.FileDiff) {
	if n.Path == "" {
		a.flash("this comment is on the merge request, not on a line")
		return
	}
	lines, at := commentedLines(n, diffs)
	if at < 0 {
		a.flash(fmt.Sprintf("%s:%d is no longer in the merge request's diff", n.Path, n.Line))
		return
	}
	view := tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	view.SetTextColor(colText)
	box(view.Box, esc(fmt.Sprintf("%s:%d · %s !%d", n.Path, n.Line, a.projectPathOfMR(mr), mr.IID))).SetBorderPadding(0, 0, 1, 1)
	fit := a.widthAware(view)
	fit(func(width int) string { return drawDiffLines(n.Path, lines, width, at, false) })
	// The first hunk's header is not drawn, so the line is a row higher.
	row := at
	if len(lines) > 0 && lines[0].kind == '@' {
		row--
	}
	view.ScrollTo(max(0, row-5), 0)
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		if ev.Key() == tcell.KeyEsc || ev.Rune() == 'q' || ev.Rune() == 'h' {
			a.closeModal(pageCommit)
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageCommit, modalFit(view, func(w, h int) (int, int) {
		return min(w*90/100, commentsWidth+20), h * 85 / 100
	}), true, true)
	a.tv.SetFocus(view)
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
		b := bubble{Name: personName(n.Author), Colour: colAccent, Meta: noteMeta(n, j > 0), Body: body}
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

// showComposer writes a new comment, or with to a reply to it in its
// thread. onSent runs once it is posted.
func (a *App) showComposer(mr forge.MergeRequest, to *forge.Note, onSent func()) {
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
			var err error
			if to != nil && to.Thread != "" {
				_, err = client.ReplyToDiscussion(ctx, mr, to.Thread, body)
			} else {
				err = client.Comment(ctx, mr, body)
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
