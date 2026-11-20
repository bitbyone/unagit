package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
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

// showComments opens the whole conversation of a merge request, oldest first,
// with the markdown rendered: j and k go from comment to comment, i answers
// the one chosen, Enter shows the code it is on, e the whole file.
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
	footer.SetText(" " + litHint("i reply · c comment · Enter code · e file · w browser · A approve · r reload"))

	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(view, 0, 1, true).
		AddItem(footer, 1, 0, false)

	fitFooter(frame, footer, 0)
	// The conversation is drawn as boxes, which have to know how wide they are.
	fit := a.widthAware(view)
	conv := &conversation{changes: map[string][]forge.FileDiff{}, expanded: map[int]bool{}}
	var (
		local []incomm.Thread
		cards []commentCard
	)
	draw := func() {
		fit(func(width int) string {
			var text string
			text, cards = conv.draw(width)
			if extra := renderLocalThreads(local, width); extra != "" {
				text += "\n\n" + tag(colDim) + strings.Repeat(glyphBar, 40) + tagEnd + "\n\n" + extra
			}
			return text
		})
	}
	chosen := func() (commentCard, bool) {
		for _, c := range cards {
			if c.note.ID == conv.selected {
				return c, true
			}
		}
		return commentCard{}, false
	}
	// rootOf is the comment a thread starts with, which says what code it
	// is on.
	rootOf := func(n forge.Note) forge.Note {
		for _, c := range cards {
			if n.Thread != "" && c.note.Thread == n.Thread {
				return c.note
			}
		}
		return n
	}
	// choose moves to the comment at i, scrolling so that it shows - and the
	// code a thread is on, when it fits too.
	choose := func(i int) {
		if len(cards) == 0 {
			return
		}
		i = max(0, min(i, len(cards)-1))
		conv.selected = cards[i].note.ID
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
			if c.note.ID == conv.selected {
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
			var latest []forge.FileDiff
			done := make(chan struct{})
			go func() {
				defer close(done)
				latest, _ = client.MergeRequestDiffs(ctx, mr)
			}()
			read, err := client.MergeRequestNotes(ctx, mr, 200)
			var versions map[string][]forge.FileDiff
			if err == nil {
				versions = readVersions(ctx, client, mr, read)
			}
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
				conv.notes, conv.latest, conv.versions, local = a.namedNotes(mr.Instance, read), latest, versions, localThreads
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
	// compare opens or closes the change a later version made to a
	// commented line, reading it the first time.
	compare := func(n forge.Note) {
		if conv.expanded[n.ID] {
			delete(conv.expanded, n.ID)
			draw()
			return
		}
		conv.expanded[n.ID] = true
		key := changeKey(n.Changed)
		if _, read := conv.changes[key]; read {
			draw()
			return
		}
		conv.changes[key] = nil
		draw()
		client := a.client(mr.Instance)
		if client == nil {
			return
		}
		ch := *n.Changed
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			diffs, err := client.CompareDiffs(ctx, mr, ch.From, ch.To)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					delete(conv.changes, key)
					delete(conv.expanded, n.ID)
					draw()
					a.flash("cannot read the change: " + err.Error())
					return
				}
				if diffs == nil {
					diffs = []forge.FileDiff{}
				}
				conv.changes[key] = diffs
				draw()
			})
		}()
	}
	code := func() {
		c, ok := chosen()
		if !ok {
			a.flash("no comment chosen - j and k choose one")
			return
		}
		if c.note.Changed != nil {
			compare(c.note)
			return
		}
		root := rootOf(c.note)
		a.showCommentCode(mr, root, conv.diffsOf(root))
	}
	file := func() {
		c, ok := chosen()
		if !ok {
			a.flash("no comment chosen - j and k choose one")
			return
		}
		a.showCommentFile(mr, rootOf(c.note))
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
			case 'e':
				file()
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

// readVersions reads the diffs the comments on code were written on, each
// once, a few at a time. One that cannot be read is left out: its threads
// are read beside the latest diff instead.
func readVersions(ctx context.Context, client forge.Provider, mr forge.MergeRequest, notes []forge.Note) map[string][]forge.FileDiff {
	wanted := map[string]forge.Note{}
	for _, n := range notes {
		if n.Path != "" && n.Base != "" && n.Head != "" {
			wanted[versionKey(n)] = n
		}
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[string][]forge.FileDiff{}
		sem = make(chan struct{}, 4)
	)
	for key, n := range wanted {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			diffs, err := client.CompareDiffs(ctx, mr, n.Base, n.Head)
			if err != nil {
				return
			}
			mu.Lock()
			out[key] = diffs
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// showCommentCode shows the whole diff of the file a comment is on, at its
// lines - Enter on a comment in the conversation.
func (a *App) showCommentCode(mr forge.MergeRequest, n forge.Note, diffs []forge.FileDiff) {
	if n.Path == "" {
		a.flash("this comment is on the merge request, not on a line")
		return
	}
	lines, from, to := commentedRange(n, diffs)
	if from < 0 {
		a.flash(where(n) + " is not in the diff it was written on")
		return
	}
	view := tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
	view.SetTextColor(colText)
	box(view.Box, esc(fmt.Sprintf("%s · %s !%d", where(n), a.projectPathOfMR(mr), mr.IID))).SetBorderPadding(0, 0, 1, 1)
	fit := a.widthAware(view)
	fit(func(width int) string { return drawDiffLines(n.Path, lines, width, from, to, false) })
	// The first hunk's header is not drawn, so the lines are a row higher.
	row := from
	if len(lines) > 0 && lines[0].kind == '@' {
		row--
	}
	view.ScrollTo(max(0, row-codeBefore-2), 0)
	a.showReader(view)
}

// showCommentFile shows the whole file a comment is on, as it was in the
// diff the comment was written on, at its lines - e in the conversation:
// what the code round it does, without leaving the conversation.
func (a *App) showCommentFile(mr forge.MergeRequest, n forge.Note) {
	if n.Path == "" {
		a.flash("this comment is on the merge request, not on a file")
		return
	}
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	// A comment on deleted lines is on the file before the change.
	ref, line, last := n.Head, n.To.New, n.From.New
	if n.To.New == 0 && n.To.Old > 0 {
		ref, line, last = n.Base, n.To.Old, n.From.Old
	}
	if line == 0 {
		line = n.Line
	}
	if last == 0 || last > line {
		last = line
	}
	if ref == "" {
		ref = mr.SourceBranch
	}
	var text string
	a.loadThen("Reading "+n.Path, func(func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		text, err = client.FileAt(ctx, mr, n.Path, ref)
		return "", err
	}, func(string) {
		view := tview.NewTextView().SetDynamicColors(true).SetWrap(false).SetScrollable(true)
		view.SetTextColor(colText)
		box(view.Box, esc(fmt.Sprintf("%s · %s !%d", where(n), a.projectPathOfMR(mr), mr.IID))).SetBorderPadding(0, 0, 1, 1)
		fit := a.widthAware(view)
		fit(func(width int) string { return drawFile(n.Path, text, last, line) })
		view.ScrollTo(max(0, last-1-codeBefore-2), 0)
		a.showReader(view)
	})
}

// showReader puts a view of code to read in front of the conversation: j
// k g G and the pages move, Esc or q go back to it.
func (a *App) showReader(view *tview.TextView) {
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		if ev.Key() == tcell.KeyEsc || ev.Rune() == 'q' {
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
