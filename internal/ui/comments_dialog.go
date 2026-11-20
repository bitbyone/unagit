package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/incomm"
)

// commentsDialog is a merge request's conversation on screen, and what a
// view of a file opened from it shares: the comments, the code, reading
// them again, and what can be done with a comment.
type commentsDialog struct {
	a    *App
	mr   forge.MergeRequest
	conv *conversation
	view *tview.TextView
	fit  func(func(int) string)
	// cards are where each comment is drawn; local the Incomm threads.
	cards []commentCard
	local []incomm.Thread
	// files are the texts of files read at commits, by ref and path, kept
	// for the views of a file; read off the event loop, hence the lock.
	filesMu sync.Mutex
	files   map[string]fileText
	// loaded runs whenever what is drawn changed, for a view of a file
	// open over the conversation to draw it too.
	loaded func()
	// hideInline is whether a view of a file leaves its comments out,
	// which stays as the reader left it while the conversation is open.
	hideInline bool
	// generation tells the code read for one reading of the conversation
	// from that of a later one.
	generation int
	// rows are the conversation's rows as last drawn; vis the cursor in a
	// comment (v), footer and hint the keys named under it.
	rows   []string
	vis    visual
	footer *tview.TextView
	hint   func()
}

// conversationView is the conversation as the mouse finds it: its rows
// are the cards and the code of the threads, which a click chooses, a
// double click opens and the wheel goes through, as j and k do.
type conversationView struct {
	*tview.TextView
	d *commentsDialog
}

func (v *conversationView) mouseRows() *rowsUnder {
	d := v.d
	// With the cursor in a comment the mouse selects its text instead.
	if d.vis.on {
		return nil
	}
	return &rowsUnder{
		focus: v,
		at: func(x, y int) int {
			_, top, _, room := v.GetInnerRect()
			if y < top || y >= top+room {
				return -1
			}
			scroll, _ := v.GetScrollOffset()
			return cardAtRow(d.cards, y-top+scroll)
		},
		cursor: func() int {
			for i, c := range d.cards {
				if d.conv.isChosen(c) {
					return i
				}
			}
			return -1
		},
		move: d.choose,
		step: d.step,
		edge: func(int) int { return -1 },
	}
}

func (v *conversationView) Draw(screen tcell.Screen) {
	v.TextView.Draw(screen)
	// The chosen card, and the block it is in, are laid over the text as
	// drawn: the text is set when what it says changes, not at each step.
	if card, ok := v.d.chosen(); ok {
		if card.block != nil {
			paintBand(screen, v.TextView, *card.block)
		}
		paintBand(screen, v.TextView, card.band)
	}
	if v.d.vis.on {
		v.d.drawVisual(screen)
	}
}

func (v *conversationView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	if !v.d.vis.on {
		return v.TextView.MouseHandler()
	}
	return func(action tview.MouseAction, ev *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := ev.Position()
		if !v.InRect(x, y) && !v.d.vis.dragging {
			return false, nil
		}
		if !v.d.visualMouse(action, ev) {
			return false, nil
		}
		setFocus(v)
		if v.d.vis.dragging {
			return true, v
		}
		return true, nil
	}
}

// cardAtRow is the card a row of the conversation is in, -1 for none.
func cardAtRow(cards []commentCard, row int) int {
	for i, c := range cards {
		if row >= c.from && row <= c.to {
			return i
		}
	}
	return -1
}

// fileText is a file read at a commit, or that it is not there.
type fileText struct {
	text string
	ok   bool
}

// showComments opens the whole conversation of a merge request, oldest first,
// with the markdown rendered: j and k go from comment to comment, r answers
// the one chosen, Enter opens the file it is on with its comments.
func (a *App) showComments(mr forge.MergeRequest) {
	d := &commentsDialog{a: a, mr: mr, files: map[string]fileText{},
		conv: &conversation{diffs: map[string]forge.FileDiff{}, reading: map[string]bool{}, expanded: map[int]bool{}}}
	path := a.projectPathOfMR(mr)
	// Nothing is wrapped by the view: every row is laid out to its width
	// here, and what would not fit - a long line of code - is cut there.
	d.view = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(false)
	d.view.SetTextColor(colText)
	box(d.view.Box, fmt.Sprintf("Comments · %s !%d", path, mr.IID)).SetBorderPadding(0, 0, 1, 1)

	footer := tview.NewTextView().SetDynamicColors(true)
	hint := func() {
		footer.SetText(" " + litHint("r reply · c comment · e edit · d delete · x resolve · Enter expand file · v select · h toggle code · P publish · A approve · R reload"))
	}
	hint()
	d.footer, d.hint = footer, hint
	shown := &conversationView{TextView: d.view, d: d}
	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(shown, 0, 1, true).
		AddItem(footer, 1, 0, false)
	fitFooter(frame, footer, 0)
	// The conversation is drawn as boxes, which have to know how wide they are.
	d.fit = a.widthAware(d.view)
	d.view.SetInputCapture(d.keys)
	// Another theme put on with the conversation open: drawn in it.
	a.onRepaint(d.view, func() {
		d.conv.drawn = nil
		d.draw()
	})
	a.onRepaint(footer, func() {
		if d.vis.on {
			d.visualHint()
			return
		}
		hint()
	})
	a.pages.AddPage(pageComments, modalFit(frame, func(w, h int) (int, int) {
		return min(max(w*80/100, min(w, 60)), commentsWidth), h * 85 / 100
	}), true, true)
	a.tv.SetFocus(shown)
	d.load(false)
}

func (d *commentsDialog) draw() {
	d.fit(func(width int) string {
		var text string
		text, d.cards = d.conv.draw(width)
		if extra := renderLocalThreads(d.local, width); extra != "" {
			text += "\n\n" + tag(colDim) + strings.Repeat(glyphBar, 40) + tagEnd + "\n\n" + extra
		}
		d.rows = strings.Split(text, "\n")
		return text
	})
}

// changed draws again what changed under the reader - here and in a view
// of a file over it - keeping the comment chosen where it was on screen.
func (d *commentsDialog) changed() {
	card, ok := d.chosen()
	row, _ := d.view.GetScrollOffset()
	d.draw()
	if moved, still := d.chosen(); ok && still {
		d.view.ScrollTo(max(0, moved.from-(card.from-row)), 0)
	} else {
		d.view.ScrollTo(row, 0)
	}
	if d.loaded != nil {
		d.loaded()
	}
}

func (d *commentsDialog) chosen() (commentCard, bool) {
	for _, c := range d.cards {
		if d.conv.isChosen(c) {
			return c, true
		}
	}
	return commentCard{}, false
}

// rootOf is the comment a thread starts with, which says what code it is
// on.
func (d *commentsDialog) rootOf(n forge.Note) forge.Note {
	for _, c := range d.cards {
		if n.Thread != "" && c.note.Thread == n.Thread {
			return c.note
		}
	}
	return n
}

// choose moves to the card at i - a comment, or the code of a thread -
// scrolling so that it shows.
func (d *commentsDialog) choose(i int) {
	if len(d.cards) == 0 {
		return
	}
	i = max(0, min(i, len(d.cards)-1))
	d.conv.selected, d.conv.onCode = d.cards[i].note.ID, d.cards[i].code
	// What is drawn stays; the band follows the cursor.
	d.reveal()
}

// toggleCode shows the code of the threads, or draws them as their comments
// alone; the comment chosen stays where it was on screen.
func (d *commentsDialog) toggleCode() {
	d.conv.noCode = !d.conv.noCode
	if d.conv.noCode && d.conv.onCode {
		// With no code to be on, the cursor is on the comment under it.
		card, ok := d.chosen()
		row, _ := d.view.GetScrollOffset()
		d.conv.onCode = false
		d.draw()
		if moved, still := d.chosen(); ok && still {
			d.view.ScrollTo(max(0, moved.from-(card.from-row)), 0)
		}
		d.reveal()
		return
	}
	d.changed()
	d.reveal()
}

// reveal scrolls so that the chosen card shows, all of it when it fits,
// and its start when it does not: a comment is read from its beginning.
func (d *commentsDialog) reveal() {
	c, ok := d.chosen()
	if !ok {
		return
	}
	_, _, _, height := d.view.GetInnerRect()
	row, _ := d.view.GetScrollOffset()
	switch {
	case c.top < row, c.to-c.top+1 > height:
		d.view.ScrollTo(c.top, 0)
	case c.to >= row+height:
		d.view.ScrollTo(c.to-height+1, 0)
	}
}

// step moves to the next card, or the one before - a whole card at a
// time, its start shown: a comment taller than the view is read with v.
func (d *commentsDialog) step(by int) {
	for i, c := range d.cards {
		if d.conv.isChosen(c) {
			d.choose(i + by)
			return
		}
	}
	d.choose(0)
}

// load reads the conversation, and draws it as soon as it has it: the code
// of a thread on the latest version is in the merge request's diff, read
// with it; that of one on an older version comes after, a file at a time,
// each thread drawn once its code is there. quietly, what is on screen
// stays until the new reading replaces it - after something was done.
func (d *commentsDialog) load(quietly bool) {
	a, mr := d.a, d.mr
	client := a.client(mr.Instance)
	if client == nil {
		d.view.SetText(tag(colBad) + a.instanceLabel(mr.Instance) + " has no token" + tagEnd)
		return
	}
	finish := func() bool { return true }
	if !quietly {
		a.stopTextWait(d.view)
		d.fit(nil)
		finish = a.waitText(d.view, func() string { return a.loadingLine("Loading the conversation…") })
	}
	// The worktrees are looked up here, where the configuration belongs to
	// the event loop; reading their files can wait for the goroutine.
	var places []incomm.Place
	if a.cfg.Integrations.Incomm {
		places = a.mrPlaces(mr)
	}
	d.generation++
	generation := d.generation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		// The code is read beside the comments; without it they are still
		// worth reading, so its failure only leaves it out.
		var (
			latest []forge.FileDiff
			detail *forge.MergeRequestDetail
			wg     sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); latest, _ = client.MergeRequestDiffs(ctx, mr) }()
		go func() { defer wg.Done(); detail, _ = client.MergeRequestDetail(ctx, mr) }()
		read, err := client.MergeRequestNotes(ctx, mr, 200)
		saved, _ := a.draftStore().List(a.draftKey(mr))
		localThreads := incomm.ThreadsAt(places...)
		wg.Wait()
		a.tv.QueueUpdateDraw(func() {
			if !finish() || generation != d.generation {
				return
			}
			if err != nil {
				if !quietly {
					d.view.SetText(tag(colBad) + tview.Escape(err.Error()) + tagEnd)
				}
				return
			}
			c := d.conv
			c.notes, c.drafts, c.latest = a.namedNotes(mr.Instance, read), saved, latest
			if detail != nil {
				c.base, c.head = detail.DiffRefs.BaseSHA, detail.DiffRefs.HeadSHA
			}
			c.drawn = nil
			d.local = localThreads
			wanted := d.codeWanted()
			c.pending = len(wanted) > 0
			if quietly {
				d.changed()
			} else {
				d.draw()
				// The forge's conversation comes first and the local
				// Incomm threads after it, so it opens at the top, on the
				// first comment.
				if _, ok := d.chosen(); !ok {
					d.choose(0)
				}
				d.view.ScrollToBeginning()
				if d.loaded != nil {
					d.loaded()
				}
			}
			if len(wanted) > 0 {
				go d.readCode(client, wanted, generation)
			}
		})
	}()
}

// codeWant is a file whose diff in a version is to be read.
type codeWant struct{ base, head, oldPath, path string }

// codeWanted are the files whose diffs the threads need and the merge
// request's diff does not give: those of a version older than the latest,
// and those the forge left out of a large merge request's diff.
func (d *commentsDialog) codeWanted() map[string]codeWant {
	c := d.conv
	wanted := map[string]codeWant{}
	for _, n := range append(shown(c.notes), draftNotes(c.drafts)...) {
		base, head, ok := c.versionOf(n)
		if n.Path == "" || !ok || base == "" {
			continue
		}
		key := diffKey(base, head, n.Path)
		if _, done := c.diffs[key]; done {
			continue
		}
		if base == c.base && head == c.head {
			if _, from, _ := commentedRange(n, c.latest); from >= 0 {
				continue
			}
		}
		old := n.OldPath
		if old == "" {
			old = n.Path
		}
		wanted[key] = codeWant{base, head, old, n.Path}
	}
	return wanted
}

// readCode works out the diff of each file wanted from the file at either
// end, a few at a time, and draws what has come in a few times a second.
func (d *commentsDialog) readCode(client forge.Provider, wanted map[string]codeWant, generation int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var (
		mu        sync.Mutex
		ready     = map[string]forge.FileDiff{}
		scheduled bool
		wg        sync.WaitGroup
		sem       = make(chan struct{}, 6)
	)
	flush := func(last bool) {
		mu.Lock()
		batch := ready
		ready, scheduled = map[string]forge.FileDiff{}, false
		mu.Unlock()
		d.a.tv.QueueUpdateDraw(func() {
			if generation != d.generation {
				return
			}
			for k, v := range batch {
				d.conv.diffs[k] = v
			}
			if last {
				d.conv.pending = false
			}
			d.conv.drawn = nil
			d.changed()
		})
	}
	for key, w := range wanted {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			diff, ok := d.fileDiff(ctx, client, w.base, w.head, w.oldPath, w.path)
			if !ok {
				return
			}
			mu.Lock()
			ready[key] = diff
			later := !scheduled
			scheduled = true
			mu.Unlock()
			if later {
				time.AfterFunc(150*time.Millisecond, func() { flush(false) })
			}
		}()
	}
	wg.Wait()
	flush(true)
}

// fileDiff is a file's diff from one commit to another, worked out from
// its text at each. A side where the file is not - added, deleted - is
// empty; when neither can be read, there is no diff.
func (d *commentsDialog) fileDiff(ctx context.Context, client forge.Provider, from, to, oldPath, path string) (forge.FileDiff, bool) {
	before := d.fileAt(ctx, client, from, oldPath)
	after := d.fileAt(ctx, client, to, path)
	if !before.ok && !after.ok {
		return forge.FileDiff{}, false
	}
	diff, err := gitx.DiffTexts(before.text, after.text)
	if err != nil {
		return forge.FileDiff{}, false
	}
	return forge.FileDiff{OldPath: oldPath, Path: path, Diff: diff}, true
}

// fileAt is a file at a commit, read once.
func (d *commentsDialog) fileAt(ctx context.Context, client forge.Provider, ref, path string) fileText {
	key := ref + "\x00" + path
	d.filesMu.Lock()
	f, ok := d.files[key]
	d.filesMu.Unlock()
	if ok {
		return f
	}
	text, err := client.FileAt(ctx, d.mr, path, ref)
	f = fileText{text: text, ok: err == nil}
	if err == nil || ctx.Err() == nil {
		d.filesMu.Lock()
		d.files[key] = f
		d.filesMu.Unlock()
	}
	return f
}

// compare opens or closes the change a later version made to a commented
// line, reading it the first time.
func (d *commentsDialog) compare(n, root forge.Note) {
	c := d.conv
	if c.expanded[n.ID] {
		delete(c.expanded, n.ID)
		d.changed()
		return
	}
	c.expanded[n.ID] = true
	ch := *n.Changed
	key := diffKey(ch.From, ch.To, root.Path)
	if _, read := c.diffs[key]; read || c.reading[key] {
		d.changed()
		d.reveal()
		return
	}
	client := d.a.client(d.mr.Instance)
	if client == nil {
		return
	}
	c.reading[key] = true
	d.changed()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		diff, ok := d.fileDiff(ctx, client, ch.From, ch.To, root.Path, root.Path)
		d.a.tv.QueueUpdateDraw(func() {
			delete(c.reading, key)
			if !ok {
				delete(c.expanded, n.ID)
				d.changed()
				d.a.flash("cannot read the change of " + root.Path)
				return
			}
			c.diffs[key] = diff
			d.changed()
			d.reveal()
		})
	}()
}

// posted puts a comment the forge has just made into the conversation at
// once, and reads the conversation again behind it.
func (d *commentsDialog) posted(n *forge.Note) {
	if n != nil && n.ID != 0 {
		named := d.a.namedNotes(d.mr.Instance, []forge.Note{*n})
		d.conv.notes = append(d.conv.notes, named...)
		d.changed()
	}
	d.load(true)
}

// saved draws the comments saved in unagit again, which are on disk here.
func (d *commentsDialog) saved() {
	d.conv.drafts, _ = d.a.draftStore().List(d.a.draftKey(d.mr))
	// A comment on a line the diff does not reach needs its file read.
	if wanted := d.codeWanted(); len(wanted) > 0 {
		if client := d.a.client(d.mr.Instance); client != nil {
			d.conv.pending = true
			go d.readCode(client, wanted, d.generation)
		}
	}
	d.changed()
}

// reply answers a comment in its thread, or with none writes a new one.
func (d *commentsDialog) reply(n *forge.Note) {
	if n != nil && n.ID < 0 {
		d.a.flash("a saved comment is not on the forge yet - P publishes it")
		return
	}
	d.a.showComposer(d.mr, n, d.posted)
}

// resolve resolves a comment's thread, or opens it again.
func (d *commentsDialog) resolve(n forge.Note) {
	root := d.rootOf(n)
	if !root.Resolvable || root.ID < 0 {
		d.a.flash("this thread cannot be resolved")
		return
	}
	d.a.resolveThread(d.mr, root, func() {
		for i := range d.conv.notes {
			if d.conv.notes[i].Thread == root.Thread {
				d.conv.notes[i].Resolved = !root.Resolved
			}
		}
		d.changed()
		d.load(true)
	})
}

// own reports whether a comment is the reader's to change: one saved in
// unagit, or one the forge says they wrote.
func (d *commentsDialog) own(n forge.Note) bool {
	if n.ID < 0 {
		return true
	}
	me := d.a.me[d.mr.Instance]
	return me != "" && strings.EqualFold(n.Author.Username, me) && !n.System
}

// edit puts new text in a comment of the reader's own.
func (d *commentsDialog) edit(n forge.Note) {
	a := d.a
	if !d.own(n) {
		a.flash("only a comment of your own can be edited")
		return
	}
	if saved, ok := draftOf(n, d.conv.drafts); ok {
		a.showNoteEditor("Edit the comment saved on "+fmt.Sprintf("%s:%d", saved.Path, saved.Line), saved.Body, func(body string, done func(error)) {
			err := a.draftStore().Update(a.draftKey(d.mr), saved.ID, body)
			done(err)
			if err == nil {
				d.saved()
			}
		})
		return
	}
	client := a.client(d.mr.Instance)
	if client == nil {
		return
	}
	a.showNoteEditor("Edit your comment", n.Body, func(body string, done func(error)) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := client.EditNote(ctx, d.mr, n, body)
			a.tv.QueueUpdateDraw(func() {
				done(err)
				if err != nil {
					return
				}
				for i := range d.conv.notes {
					if d.conv.notes[i].ID == n.ID {
						d.conv.notes[i].Body = body
					}
				}
				d.changed()
				a.done("Comment edited")
			})
		}()
	})
}

// remove deletes a comment of the reader's own, after asking.
func (d *commentsDialog) remove(n forge.Note) {
	a := d.a
	if !d.own(n) {
		a.flash("only a comment of your own can be deleted")
		return
	}
	text := strings.TrimSpace(n.Body)
	saved, isDraft := draftOf(n, d.conv.drafts)
	question := "Delete your comment?\n\n" + esc(trunc(text, 200))
	if isDraft {
		question = fmt.Sprintf("Delete the comment saved on [::b]%s:%d[::-]?\n\n%s", esc(saved.Path), saved.Line, esc(trunc(text, 200)))
	} else {
		question += "\n\nEveryone on the merge request loses it."
	}
	a.confirmWith("Delete", question, "Delete", nil, func() {
		if isDraft {
			if err := a.draftStore().Remove(a.draftKey(d.mr), saved.ID); err != nil {
				a.errorf("%v", err)
				return
			}
			d.saved()
			a.refreshDisk()
			a.mrsPane.reload()
			return
		}
		client := a.client(d.mr.Instance)
		if client == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := client.DeleteNote(ctx, d.mr, n)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("cannot delete the comment: %v", err)
					return
				}
				kept := d.conv.notes[:0]
				for _, m := range d.conv.notes {
					if m.ID != n.ID {
						kept = append(kept, m)
					}
				}
				d.conv.notes = kept
				d.changed()
				a.done("Comment deleted")
			})
		}()
	})
}

// actions are what can be done in the conversation, with the comment
// chosen - or with the thread whose code is chosen, through its first
// comment. The keys answer from them, and so does the picker of actions,
// so a key cannot do one thing while the picker says another.
func (d *commentsDialog) actions() []uiAction {
	a := d.a
	card, chosen := d.chosen()
	n := card.note
	need := func(run func(n forge.Note)) func() {
		return func() {
			if !chosen {
				a.flash("no comment chosen - j and k choose one")
				return
			}
			run(n)
		}
	}
	change := func() bool { return chosen && n.Changed != nil && !card.code }
	return []uiAction{
		{name: "Expand File", about: "The whole file the comment is on, at the version it was written on, with its comments under their lines and the cursor on it.",
			keys: "Enter", rank: 5, when: func() bool { return chosen && !change() && d.rootOf(n).Path != "" },
			run: need(func(n forge.Note) {
				focus := n.ID
				if card.code {
					focus = 0
				}
				d.openFile(d.rootOf(n), focus)
			})},
		{name: "Compare Change", about: "Show, or hide again, what a later version of the merge request changed on the commented line.",
			keys: "Enter", rank: 6, when: change, run: need(func(n forge.Note) { d.compare(n, d.rootOf(n)) })},
		{name: "Reply…", about: "Answer the chosen comment in its thread.", keys: "r", rank: 10,
			when: func() bool { return chosen && n.ID >= 0 }, run: need(func(n forge.Note) { d.reply(&n) })},
		{name: "New Comment…", about: "Write a comment on the merge request as a whole.", keys: "c", rank: 20,
			run: func() { d.reply(nil) }},
		{name: "Edit Comment…", about: "Put new text in a comment of your own, published or saved.", keys: "e", rank: 30,
			when: func() bool { return chosen && d.own(n) }, run: need(d.edit)},
		{name: "Delete Comment…", about: "Take a comment of your own away, after asking.", keys: "d", rank: 40,
			when: func() bool { return chosen && d.own(n) }, run: need(d.remove)},
		{name: "Resolve or Reopen Thread", about: "Resolve the chosen comment's thread on the forge, or open it again.", keys: "x", rank: 50,
			when: func() bool { root := d.rootOf(n); return chosen && root.Resolvable && root.ID >= 0 }, run: need(d.resolve)},
		{name: "Select Text", about: "Put a cursor in the chosen comment, to select some of it - v by characters, V by lines - and copy it with y.", keys: "v", rank: 58,
			when: func() bool { return chosen && !card.code && n.Changed == nil }, run: d.startVisual},
		{name: "Toggle Code", about: "Hide the code of the threads, to read the comments through, or show it again.", keys: "h", rank: 60,
			run: d.toggleCode},
		{name: "Publish Comments…", about: "Publish the comments saved in unagit, and Incomm's, after showing them.", keys: "P", rank: 70,
			run: func() { a.publishMR(d.mr) }},
		{name: "Open in Browser", about: "The chosen comment on the forge's page, or the merge request.", keys: "w", rank: 80,
			run: func() {
				if chosen && n.URL != "" {
					a.openWeb(n.URL)
					return
				}
				a.openWeb(d.mr.WebURL)
			}},
		{name: "Approve…", about: "Approve the merge request, after asking - everyone on it sees it.", keys: "A", rank: 90,
			run: func() { a.approveMR(d.mr, func() { d.load(true) }) }},
		{name: "Reload", about: "Read the conversation again.", keys: "R", rank: 100, run: func() { d.load(false) }},
	}
}

// runEnter does what Enter does now: the first action on Enter that can be
// done.
func runEnter(actions []uiAction) bool {
	for _, act := range available(actions) {
		if act.keys == "Enter" {
			act.run()
			return true
		}
	}
	return false
}

func (d *commentsDialog) keys(ev *tcell.EventKey) *tcell.EventKey {
	a := d.a
	if d.vis.on {
		return d.visualKeys(ev)
	}
	if halfPage(d.view, ev) {
		return nil
	}
	switch {
	case ev.Key() == tcell.KeyEsc, ev.Key() == tcell.KeyRune && ev.Rune() == 'q':
		a.stopTextWait(d.view)
		a.closeModal(pageComments)
	case ev.Key() == tcell.KeyDown, ev.Key() == tcell.KeyRune && ev.Rune() == 'j':
		d.step(1)
	case ev.Key() == tcell.KeyUp, ev.Key() == tcell.KeyRune && ev.Rune() == 'k':
		d.step(-1)
	case ev.Key() == tcell.KeyRune && ev.Rune() == 'g':
		d.choose(0)
	case ev.Key() == tcell.KeyRune && ev.Rune() == 'G':
		d.choose(len(d.cards) - 1)
	case a.opensSelectionActions(ev):
		a.showActions("Actions · comments", d.actions())
	case ev.Key() == tcell.KeyEnter:
		if !runEnter(d.actions()) {
			if _, chosen := d.chosen(); !chosen {
				a.flash("no comment chosen - j and k choose one")
			} else {
				a.flash("this comment is on the merge request, not on a file")
			}
		}
	case runKey(d.actions(), ev):
	default:
		return ev
	}
	return nil
}
