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
	// loaded runs whenever the conversation was read again, for a view of
	// a file open over it to draw its comments anew.
	loaded func()
	// inline is whether a view of a file shows its comments, which stays
	// as the reader left it while the conversation is open.
	inline bool
}

// fileText is a file read at a commit, or that it is not there.
type fileText struct {
	text string
	ok   bool
}

// showComments opens the whole conversation of a merge request, oldest first,
// with the markdown rendered: j and k go from comment to comment, i answers
// the one chosen, Enter shows the code it is on, e the whole file.
func (a *App) showComments(mr forge.MergeRequest) {
	d := &commentsDialog{a: a, mr: mr, files: map[string]fileText{},
		conv: &conversation{diffs: map[string]forge.FileDiff{}, reading: map[string]bool{}, expanded: map[int]bool{}}}
	path := a.projectPathOfMR(mr)
	d.view = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)
	d.view.SetTextColor(colText)
	box(d.view.Box, fmt.Sprintf("Comments · %s !%d", path, mr.IID)).SetBorderPadding(0, 0, 1, 1)

	footer := tview.NewTextView().SetDynamicColors(true)
	footer.SetText(" " + litHint("i reply · c comment · x resolve · Enter code · e file · s code · P publish · A approve · r reload"))
	frame := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(d.view, 0, 1, true).
		AddItem(footer, 1, 0, false)
	fitFooter(frame, footer, 0)
	// The conversation is drawn as boxes, which have to know how wide they are.
	d.fit = a.widthAware(d.view)
	d.view.SetInputCapture(d.keys)
	a.pages.AddPage(pageComments, modalFit(frame, func(w, h int) (int, int) {
		return min(max(w*80/100, min(w, 60)), commentsWidth), h * 85 / 100
	}), true, true)
	a.tv.SetFocus(d.view)
	d.load()
}

func (d *commentsDialog) draw() {
	d.fit(func(width int) string {
		var text string
		text, d.cards = d.conv.draw(width)
		if extra := renderLocalThreads(d.local, width); extra != "" {
			text += "\n\n" + tag(colDim) + strings.Repeat(glyphBar, 40) + tagEnd + "\n\n" + extra
		}
		return text
	})
}

func (d *commentsDialog) chosen() (commentCard, bool) {
	for _, c := range d.cards {
		if c.note.ID == d.conv.selected {
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

// choose moves to the comment at i, scrolling so that it shows - and the
// code a thread is on, when it fits too.
func (d *commentsDialog) choose(i int) {
	if len(d.cards) == 0 {
		return
	}
	i = max(0, min(i, len(d.cards)-1))
	d.conv.selected = d.cards[i].note.ID
	d.draw()
	d.reveal()
}

// toggleCode shows the code of the threads, or draws them as their comments
// alone; the comment chosen stays where it was on screen.
func (d *commentsDialog) toggleCode() {
	card, ok := d.chosen()
	row, _ := d.view.GetScrollOffset()
	d.conv.noCode = !d.conv.noCode
	d.draw()
	if !ok {
		return
	}
	if moved, ok := d.chosen(); ok {
		d.view.ScrollTo(max(0, moved.from-(card.from-row)), 0)
		d.reveal()
	}
}

// reveal scrolls so that the chosen comment shows, all of it when it fits.
func (d *commentsDialog) reveal() {
	c, ok := d.chosen()
	if !ok {
		return
	}
	_, _, _, height := d.view.GetInnerRect()
	row, _ := d.view.GetScrollOffset()
	switch {
	case c.top < row:
		d.view.ScrollTo(c.top, 0)
	case c.to >= row+height:
		d.view.ScrollTo(max(c.top, c.to-height+1), 0)
	}
}

func (d *commentsDialog) step(by int) {
	for i, c := range d.cards {
		if c.note.ID == d.conv.selected {
			d.choose(i + by)
			return
		}
	}
	d.choose(0)
}

// load reads the conversation again, and the code its threads are on.
func (d *commentsDialog) load() {
	a, mr := d.a, d.mr
	a.stopTextWait(d.view)
	d.fit(nil)
	client := a.client(mr.Instance)
	if client == nil {
		d.view.SetText(tag(colBad) + a.instanceLabel(mr.Instance) + " has no token" + tagEnd)
		return
	}
	finish := a.waitText(d.view, func() string { return a.loadingLine("Loading the conversation…") })
	// The worktrees are looked up here, where the configuration belongs to
	// the event loop; reading their files can wait for the goroutine.
	var places []incomm.Place
	if a.cfg.Integrations.Incomm {
		places = a.mrPlaces(mr)
	}
	known := d.conv.diffs
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
		wg.Wait()
		probe := &conversation{notes: read, drafts: saved}
		if detail != nil {
			probe.base, probe.head = detail.DiffRefs.BaseSHA, detail.DiffRefs.HeadSHA
		}
		var diffs map[string]forge.FileDiff
		if err == nil {
			diffs = d.readCode(ctx, client, probe, known)
		}
		localThreads := incomm.ThreadsAt(places...)
		a.tv.QueueUpdateDraw(func() {
			if !finish() {
				return
			}
			if err != nil {
				d.view.SetText(tag(colBad) + tview.Escape(err.Error()) + tagEnd)
				return
			}
			c := d.conv
			c.notes, c.drafts, c.latest, c.base, c.head = a.namedNotes(mr.Instance, read), saved, latest, probe.base, probe.head
			for k, v := range diffs {
				c.diffs[k] = v
			}
			d.local = localThreads
			d.draw()
			// The forge's conversation comes first and the local Incomm
			// threads after it, so it opens at the top - on what others
			// said - rather than on the last local note; a reload stays on
			// the comment chosen.
			if card, ok := d.chosen(); ok {
				d.view.ScrollTo(card.top, 0)
			} else {
				d.view.ScrollToBeginning()
			}
			if d.loaded != nil {
				d.loaded()
			}
		})
	}()
}

// readCode works out the diff of each file a thread is on, in the version
// it was written on, from the file at either end - however much of a large
// merge request's diff the forge would send. What is known already is not
// read again; a file that cannot be read is left to the latest diff.
func (d *commentsDialog) readCode(ctx context.Context, client forge.Provider, c *conversation, known map[string]forge.FileDiff) map[string]forge.FileDiff {
	type want struct{ base, head, oldPath, path string }
	wanted := map[string]want{}
	for _, n := range append(shown(c.notes), draftNotes(c.drafts)...) {
		base, head, ok := c.versionOf(n)
		if n.Path == "" || !ok || base == "" {
			continue
		}
		key := diffKey(base, head, n.Path)
		if _, done := known[key]; done {
			continue
		}
		old := n.OldPath
		if old == "" {
			old = n.Path
		}
		wanted[key] = want{base, head, old, n.Path}
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[string]forge.FileDiff{}
		sem = make(chan struct{}, 6)
	)
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
			out[key] = diff
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
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
func (d *commentsDialog) compare(n, root forge.Note, redraw func()) {
	c := d.conv
	if c.expanded[n.ID] {
		delete(c.expanded, n.ID)
		redraw()
		return
	}
	c.expanded[n.ID] = true
	ch := *n.Changed
	key := diffKey(ch.From, ch.To, root.Path)
	if _, read := c.diffs[key]; read || c.reading[key] {
		redraw()
		return
	}
	client := d.a.client(d.mr.Instance)
	if client == nil {
		return
	}
	c.reading[key] = true
	redraw()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		diff, ok := d.fileDiff(ctx, client, ch.From, ch.To, root.Path, root.Path)
		d.a.tv.QueueUpdateDraw(func() {
			delete(c.reading, key)
			if !ok {
				delete(c.expanded, n.ID)
				redraw()
				d.a.flash("cannot read the change of " + root.Path)
				return
			}
			c.diffs[key] = diff
			redraw()
		})
	}()
}

// reply answers a comment in its thread, or with none writes a new one.
func (d *commentsDialog) reply(n *forge.Note) {
	if n != nil && n.ID < 0 {
		d.a.flash("a saved comment is not on the forge yet - P publishes it")
		return
	}
	d.a.showComposer(d.mr, n, d.load)
}

// resolve resolves a comment's thread, or opens it again.
func (d *commentsDialog) resolve(n forge.Note) {
	root := d.rootOf(n)
	if !root.Resolvable || root.ID < 0 {
		d.a.flash("this thread cannot be resolved")
		return
	}
	d.a.resolveThread(d.mr, root, d.load)
}

// remove deletes a comment saved in unagit, after asking.
func (d *commentsDialog) remove(n forge.Note) {
	a := d.a
	saved, ok := draftOf(n, d.conv.drafts)
	if !ok {
		a.flash("only a comment saved in unagit can be deleted here")
		return
	}
	a.confirmWith("Delete", fmt.Sprintf("Delete the comment saved on [::b]%s:%d[::-]?\n\n%s", esc(saved.Path), saved.Line, esc(trunc(saved.Body, 200))), "Delete", nil, func() {
		if err := a.draftStore().Remove(a.draftKey(d.mr), saved.ID); err != nil {
			a.errorf("%v", err)
			return
		}
		a.refreshDisk()
		a.mrsPane.reload()
		d.load()
	})
}

func (d *commentsDialog) keys(ev *tcell.EventKey) *tcell.EventKey {
	a := d.a
	if halfPage(d.view, ev) {
		return nil
	}
	card, chosen := d.chosen()
	need := func(run func(n forge.Note)) {
		if !chosen {
			a.flash("no comment chosen - j and k choose one")
			return
		}
		run(card.note)
	}
	switch ev.Key() {
	case tcell.KeyEsc:
		a.stopTextWait(d.view)
		a.closeModal(pageComments)
	case tcell.KeyDown:
		d.step(1)
	case tcell.KeyUp:
		d.step(-1)
	case tcell.KeyEnter:
		need(func(n forge.Note) {
			if n.Changed != nil {
				d.compare(n, d.rootOf(n), func() { d.draw(); d.reveal() })
				return
			}
			root := d.rootOf(n)
			a.showCommentCode(d.mr, root, d.conv.diffsOf(root))
		})
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			a.stopTextWait(d.view)
			a.closeModal(pageComments)
		case 'j':
			d.step(1)
		case 'k':
			d.step(-1)
		case 'g':
			d.choose(0)
		case 'G':
			d.choose(len(d.cards) - 1)
		case 'i':
			if !chosen {
				d.reply(nil)
				return nil
			}
			d.reply(&card.note)
		case 'c':
			d.reply(nil)
		case 'e':
			need(func(n forge.Note) { d.openFile(d.rootOf(n)) })
		case 'x':
			need(d.resolve)
		case 'd':
			need(d.remove)
		case 's':
			d.toggleCode()
		case 'P':
			a.publishMR(d.mr)
		case 'w':
			if chosen && card.note.URL != "" {
				a.openWeb(card.note.URL)
				return nil
			}
			a.openWeb(d.mr.WebURL)
		case 'A':
			a.approveMR(d.mr, d.load)
		case 'r':
			d.load()
		default:
			return ev
		}
	default:
		return ev
	}
	return nil
}
