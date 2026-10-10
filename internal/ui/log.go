package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/editors"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/workspace"
)

// The commit log, one list wherever it is opened: newest first, the pane
// under it giving who made the commit under the cursor, when, and the rest of
// its message. Enter is the same everywhere - the commit's detail, over the
// log, back to it on Esc - and the other keys are what the place it was
// opened from can do with a commit: look at it in Hunk, check it out, review
// a merge request from it, start a branch or a worktree there, open or copy
// it. A merge request's log marks what is new since the last review and
// starts on the oldest of it.

// logCommit is one commit of a log, from git or from the forge.
type logCommit struct {
	gitx.LogEntry
	WebURL   string
	New      bool // a merge request's, not yet given a review
	Unpushed bool // not on the branch's upstream yet
	Local    bool // on no branch of any remote: its message may be edited
	// Theirs is a commit of the upstream the branch does not have: what a
	// pull would bring, or, once the two have parted, a force push remove.
	Theirs bool
	// CI is the pipeline status, where the forge said it: a merge request's
	// head has one.
	CI string
}

// logPlace is where a log was opened, and so what can be done with its
// commits.
type logPlace struct {
	title   string
	project forge.Project
	// dir is the checkout the commits are looked at and checked out in, ""
	// when nothing is on disk; branch is what the log is of.
	dir    string
	branch string
	// checkout lets C put HEAD on a commit; mr makes it a merge request's
	// log, reviewed from a commit with Ctrl-R; branches lets n and Ctrl-W
	// start a branch or a worktree at one.
	checkout bool
	mr       *forge.MergeRequest
	branches bool
	// reload reads the log again, the cursor on focus, after something
	// changed what it shows, and says done over it.
	reload func(focus, done string)
	// upstream is the branch's upstream as git names it, "" without one:
	// the log says where it stands.
	upstream string
}

// historyLimit is how far back a log goes; it is for looking around, not
// for archaeology.
const historyLimit = 200

// showCommitLog lists commits, the cursor on start.
func (a *App) showCommitLog(place logPlace, commits []logCommit, start int) {
	a.showMarkedLog(place, commits, start, nil)
}

// showMarkedLog is showCommitLog with the commits of marked marked, by
// their place in commits: a log opened again after a dialog it led to was
// left keeps what was marked.
func (a *App) showMarkedLog(place logPlace, commits []logCommit, start int, marked map[int]bool) {
	if len(commits) == 0 {
		a.note("no commits to show")
		return
	}
	rules := logRules(place, commits)
	items := make([]pickItem, 0, len(commits)+len(rules))
	marks := &pickMarks{}
	first := 0
	for i, c := range commits {
		if words, ok := rules[i]; ok {
			items = append(items, pickItem{Rule: true, Data: logRule(words)})
		}
		if i == start {
			first = len(items)
		}
		if marked[i] {
			marks.toggle(len(items))
		}
		items = append(items, pickItem{About: commitAbout(c, place), Data: i})
	}
	start = first
	labelLog(items, commits, logRowWidth(a.screenWidth()))
	at := func(it pickItem) logCommit { return commits[it.Data.(int)] }
	again := func(it pickItem) func() { return func() { a.showCommitLog(place, commits, it.Data.(int)) } }
	// markedNow are the commits marked, by their place in commits, newest
	// first; kept opens the log again with them still marked.
	markedNow := func() []int {
		var at []int
		for _, it := range marks.marked() {
			at = append(at, it.Data.(int))
		}
		return at
	}
	kept := func(it pickItem) func() {
		return func() {
			set := map[int]bool{}
			for _, i := range markedNow() {
				set[i] = true
			}
			a.showMarkedLog(place, commits, it.Data.(int), set)
		}
	}

	// A commit no remote has is not on the server: it has no page there and
	// no pipeline ran for it.
	onServer := func(it pickItem) bool { return !at(it).Local }
	browser := a.browserKey("w", "browser", "Open in Browser", "The commit's page on the forge; the log stays open.",
		func(it pickItem) string { return a.commitURL(place, at(it)) })
	browser.when = onServer
	keys := []pickKey{
		{keys: "D", hint: "diff", name: "Show Diff in Hunk", about: "What the commit changed, in Hunk; with commits marked, what the run of them changed together.", marked: true,
			run: func(it pickItem) {
				if marked := markedNow(); len(marked) > 0 {
					a.diffMarkedCommits(place, commits, marked, kept(it))
					return
				}
				a.showCommitDiff(place, commits, it.Data.(int), false)
			}},
		{keys: "Alt-D", hint: "since", name: "Show Changes Since", about: "Everything from the commit to the working tree, in Hunk.", run: func(it pickItem) { a.showCommitDiff(place, commits, it.Data.(int), true) }},
	}
	if place.checkout {
		keys = append(keys, pickKey{keys: "C", hint: "checkout", name: "Check Out Commit", about: "Put the checkout at this commit, detached; B goes back to the branch.", run: func(it pickItem) { a.checkoutCommit(place, at(it)) },
			when: func(it pickItem) bool { return !isHead(at(it)) }},
			pickKey{keys: "e", name: "Edit Commit Message…", about: "Write the message of a commit again; the commits after it are replayed onto it. One on origin asks first: a force push follows.", run: func(it pickItem) { a.editCommitMessage(place, at(it), again(it)) },
				when: func(it pickItem) bool { return !at(it).Theirs }},
			pickKey{keys: "u", name: "Undo Commit", about: "Take the newest commit back: its changes stay on disk, to be committed again. One on origin asks first: a force push follows.", run: func(it pickItem) { a.undoCommit(place, at(it), isHead(at(it)), again(it)) },
				when: func(it pickItem) bool { return isHead(at(it)) }},
			pickKey{keys: "R", name: "Recover from Reflog…", about: "Every place the branch has been, whatever moved it - a rebase or reset in a terminal too: go back to one.", marked: true,
				run: func(it pickItem) {
					branch := place.branch
					if strings.HasPrefix(branch, "@") || branch == "(detached)" {
						branch = ""
					}
					a.showReflog(reflogScope{project: place.project, dir: place.dir, branch: branch})
				}},
			pickKey{keys: "H", name: "Rewrite History…", about: "Every squash, rebase, edited message, undone commit and deleted branch of the repository, in order: undo any of them.", marked: true,
				run: func(it pickItem) { a.showRewrites(rewriteScope{project: place.project, dir: place.dir}) }},
			pickKey{keys: "s", hint: "squash", name: "Squash Commits…", about: "Make one commit of the commits marked with space, next to each other; asks first when origin has any of them.", marked: true, markedOnly: true,
				run: func(it pickItem) {
					if why := squashable(commits, markedNow()); why != "" {
						kept(it)()
						a.flash(why)
						return
					}
					a.squashCommits(place, commits, markedNow(), kept(it))
				},
				when: func(pickItem) bool { return squashable(commits, markedNow()) == "" }})
	}
	if place.mr != nil {
		mr := *place.mr
		keys = append(keys,
			pickKey{keys: "Ctrl-R", hint: "review from here", name: "Review From Here", about: "Narrow the review to this commit and the ones after it.", run: func(it pickItem) { a.openMRReviewFrom(mr, at(it).SHA, nil) }},
			pickKey{keys: "Alt-R", hint: "…in an editor", name: "Review From Here In…", about: "The same, opened in an editor you choose.", run: func(it pickItem) {
				sha := at(it).SHA
				a.withEditor(true, func(ed *editors.Editor) { a.openMRReviewFrom(mr, sha, ed) })
			}})
	}
	if place.branches {
		keys = append(keys,
			pickKey{keys: "n", hint: "branch", name: "New Branch Here…", about: "Start a branch at this commit.", run: func(it pickItem) { a.branchAtCommit(place, at(it), again(it)) }},
			pickKey{keys: "Ctrl-W", hint: "worktree", name: "New Worktree Here…", about: "Start a branch at this commit in a worktree of its own.", run: func(it pickItem) { a.worktreeAtCommit(place, at(it), again(it)) }})
	}
	keys = append(keys,
		pickKey{keys: "J", hint: "pipelines", name: "Show Pipelines…", about: "The pipelines that ran for this commit, and their jobs.", run: func(it pickItem) {
			c := at(it)
			a.showCommitPipelines(commitCI(place.project.Instance, place.project, place.project.PathWithNamespace, c.SHA), again(it))
		}, when: onServer},
		browser,
		pickKey{keys: "y", hint: "copy", name: "Copy…", about: "Copy the commit's id, link or reference; with commits marked, their ids or subjects, one a line.", marked: true,
			run: func(it pickItem) {
				if marked := markedNow(); len(marked) > 0 {
					a.yankMarkedCommits(commits, marked)
					return
				}
				a.yankCommit(place, at(it))
			}})

	// Not packed: the rows are short, but the keys are many, and their hints
	// should fit on a line or two rather than wrap down the side.
	opts := pickerOptions{start: start, wide: true, explain: true, enterHint: "details", keys: keys,
		enterName: "Show Details", enterAbout: "The whole commit: its message, refs and the files it changed.",
		relabel: func(items []pickItem, width int) { labelLog(items, commits, width) }}
	// Commits are marked to be squashed, which only a checkout can do.
	if place.checkout {
		opts.marks = marks
	}
	a.showPickerWith(place.title, items, opts, func(it pickItem) { a.showCommitDetail(place, at(it), again(it)) })
}

// refWords is what points at a commit, as short as it reads: HEAD, the
// branches and tags, origin's copies.
func refWords(refs []string) string {
	words := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.Replace(ref, "HEAD -> ", "HEAD→", 1)
		ref = strings.TrimPrefix(ref, "tag: ")
		if ref == "origin/HEAD" {
			continue
		}
		words = append(words, ref)
	}
	return strings.Join(words, " ")
}

// commitAbout is the pane under the log: author and time, then the body, and
// what a review from a merge commit would bring in.
func commitAbout(c logCommit, place logPlace) string {
	about := c.Author + " · " + c.When.Format("2006-01-02 15:04")
	if place.mr != nil && c.Merge {
		about += " · merge commit: a review from here includes what it merged"
	}
	if body := strings.Join(strings.Fields(c.Body), " "); body != "" {
		about += " · " + body
	}
	return about
}

func shortSHA(sha string) string { return sha[:min(8, len(sha))] }

// isHead reports whether a commit is the one checked out.
func isHead(c logCommit) bool {
	for _, ref := range c.Refs {
		if ref == "HEAD" || strings.HasPrefix(ref, "HEAD -> ") {
			return true
		}
	}
	return false
}

// labelLog writes the rows of a log for a row of width cells. The subjects
// make a column, so what comes after them lines up, and it takes what the row
// has left after the id, the age and the refs: a subject is cut only where
// the dialog ends. The pane under the list has the rest of the message. A
// rule between the sections runs across the row.
func labelLog(items []pickItem, commits []logCommit, width int) {
	refsW := 0
	for _, c := range commits {
		refsW = max(refsW, tview.TaggedStringWidth(logSub(c)))
	}
	// Who wrote each, in a column of its own after the subject.
	authorW := 0
	for _, c := range commits {
		authorW = max(authorW, len([]rune(c.Author)))
	}
	authorW = min(authorW, 18)
	// A row is a mark and the id (12), the subject, the author after a gap
	// (2), and after a gap (3) the age, the pipeline and the refs.
	room := width - 12 - 2 - authorW - 3 - min(refsW, 44)
	subjects := make([]string, len(commits))
	subjectW := 0
	for i, c := range commits {
		subject := c.Subject
		if c.Merge {
			subject = glyphMerge + " " + subject
		}
		subjects[i] = trim(subject, max(room, 24))
		subjectW = max(subjectW, len([]rune(subjects[i])))
	}
	for n, it := range items {
		if words, ok := it.Data.(logRule); ok {
			items[n].Label = ruleLabel(string(words), width)
			continue
		}
		i := it.Data.(int)
		c := commits[i]
		// The picker filters on the text as it is drawn, so the marks are
		// plain characters, not colour tags.
		mark := "  "
		switch {
		case c.New:
			mark = glyphDot + " "
		case c.Unpushed:
			mark = glyphAhead + " "
		}
		label := esc(fmt.Sprintf("%s%s  %-*s  %-*s", mark, shortSHA(c.SHA), subjectW, subjects[i], authorW, trim(c.Author, authorW)))
		if c.Theirs {
			label = tag(role("log.theirs")) + label + tagEnd
		}
		items[n].Label = label
		items[n].Sub = logSub(c)
	}
}

// logRule is the words of a line between the sections of a log.
type logRule string

// ruleLabel is a rule across a row of width cells, its words near the start.
func ruleLabel(words string, width int) string {
	line := "── " + words + " "
	return tag(role("log.boundary")) + esc(line+strings.Repeat("─", max(2, width-len([]rune(line))))) + tagEnd
}

// logRules are the lines a log is parted by, each before the commit it is
// keyed by. A branch ahead of its upstream has one where the upstream
// stands; one parted from it - after a squash, an amend, a rebase of what
// was pushed, or a push from elsewhere - has what is only here, what only
// on origin, and what both have. A branch with no upstream has one where
// the commits some remote has begin.
func logRules(place logPlace, commits []logCommit) map[int]string {
	rules := map[int]string{}
	firstOf := func(of func(c logCommit) bool) int {
		for i, c := range commits {
			if of(c) {
				return i
			}
		}
		return -1
	}
	here := firstOf(func(c logCommit) bool { return c.Unpushed })
	theirs := firstOf(func(c logCommit) bool { return c.Theirs })
	shared := firstOf(func(c logCommit) bool { return !c.Unpushed && !c.Theirs })
	switch {
	case theirs >= 0 && here >= 0:
		rules[here] = "only here · a force push puts these on origin"
		rules[theirs] = "only on origin · a force push removes these"
		if shared >= 0 {
			rules[shared] = "shared"
		}
	case theirs >= 0:
		rules[theirs] = "only on origin · a pull brings these"
		if shared >= 0 {
			rules[shared] = "shared"
		}
	case here >= 0 && shared > here:
		rules[shared] = place.upstream
	case place.upstream == "":
		local := firstOf(func(c logCommit) bool { return c.Local })
		pushed := firstOf(func(c logCommit) bool { return !c.Local })
		if local >= 0 && pushed > local {
			rules[pushed] = "on origin"
		}
	}
	return rules
}

// logSub is what follows a subject, as markup: the age in a column of its
// own, then the pipeline - its mark and its word in its colour, as every
// list draws a pipeline - and what points at the commit, where a varying
// length disturbs nothing.
func logSub(c logCommit) string {
	rest := esc(refWords(c.Refs))
	if ci, _ := ciMark(c.CI); ci != "" {
		mark, status := painted(ci, c.CI)
		rest = strings.TrimSpace(mark + " " + status + " " + rest)
	}
	return strings.TrimSpace(esc(fmt.Sprintf("%-8s", humanAge(c.When))) + "  " + rest)
}

// logRowWidth is how wide a row of the log is on a screen so wide: the wide
// picker's share of it, less its frame and padding.
func logRowWidth(screen int) int { return screen*widePct/100 - 4 }

// screenWidth is the terminal's width as last drawn.
func (a *App) screenWidth() int {
	if a.screen == nil {
		return 120
	}
	w, _ := a.screen.Size()
	return w
}

// onDisk says why a commit cannot be looked at in the checkout, or "".
func (a *App) onDisk(place logPlace, c logCommit) string {
	switch {
	case place.dir == "":
		return "not cloned - C on the repository clones it, then its commits can be looked at"
	case !gitx.New("", nil).HasCommit(place.dir, c.SHA):
		return shortSHA(c.SHA) + " is not on disk yet - pull, or open the review, first"
	}
	return ""
}

// showCommitDiff shows a commit in Hunk, or, since, everything from it to
// the working tree; the log comes back when Hunk is closed. A commit not on
// disk is brought first - the repository cloned, the merge request or origin
// fetched - so a log can be paged through without stopping to clone.
func (a *App) showCommitDiff(place logPlace, commits []logCommit, i int, since bool) {
	bin, ok := a.hunkBinary()
	if !ok {
		return
	}
	c := commits[i]
	args := []string{"show", c.SHA}
	if since {
		args = []string{"diff", c.SHA}
	}
	show := func(place logPlace) {
		go func() {
			a.runView(bin, diffView{dir: place.dir, args: args})
			a.tv.QueueUpdateDraw(func() { a.showCommitLog(place, commits, i) })
		}()
	}
	if a.onDisk(place, c) == "" {
		show(place)
		return
	}
	// Since is measured against a working tree, and a fresh clone's is not
	// what the log was of; only a commit itself can be brought and shown.
	if since && place.dir == "" {
		a.flash("not cloned - D shows the commit itself; from it to now needs the repository on disk")
		return
	}
	pr := place.project
	var dir string
	a.runTaskThen("Bringing "+shortSHA(c.SHA)+" of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		var err error
		dir, err = a.newManager(pr.Instance, pr.PathWithNamespace, log).BringCommits(pr, place.mr, c.SHA)
		return "", err
	}, func(string) {
		a.refreshDisk()
		a.projectsPane.reload()
		if place.dir == "" {
			place.dir = dir
		}
		show(place)
	})
}

// showCommitDetail is a commit whole: who, when, what points at it, the
// message, and the files it changed. Esc goes back to the log.
func (a *App) showCommitDetail(place logPlace, c logCommit, back func()) {
	d := &detailBuf{}
	d.title(c.Subject)
	d.blank()
	d.kv("Commit", esc(c.SHA))
	d.kv("Author", esc(c.Author))
	d.kv("Date", esc(c.When.Format("Mon 2006-01-02 15:04")+" · "+humanAge(c.When)))
	d.kv("Refs", esc(strings.Join(c.Refs, ", ")))
	if c.Merge {
		d.kv("Merge", "yes")
	}
	if url := a.commitURL(place, c); url != "" {
		d.kv("Link", esc(url))
	}
	if c.Body != "" {
		d.blank()
		d.raw(tag(colText) + esc(c.Body) + tagEnd + "\n")
	}
	view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetScrollable(true)
	view.SetText(d.String())
	box(view.Box, "Commit "+shortSHA(c.SHA))
	view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if halfPage(view, ev) {
			return nil
		}
		if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyEnter || ev.Rune() == 'q' || ev.Rune() == 'h' {
			a.closeModal(pageCommit)
			back()
			return nil
		}
		return ev
	})
	a.pages.AddPage(pageCommit, modalPct(view, 70, 70), true, true)
	a.tv.SetFocus(view)
	// What it changed comes from git when the commit is on disk, else from
	// the forge, off the event loop either way.
	view.SetText(d.String() + "\n" + tag(colWarn) + "[::b]FILES[::-]" + tagEnd + "\n" + tag(colDim) + "reading…" + tagEnd + "\n")
	onDisk := a.onDisk(place, c) == ""
	client := a.client(place.project.Instance)
	dir, project := place.dir, place.project
	go func() {
		var files []forge.FileChange
		var err error
		switch {
		case onDisk:
			var stats []gitx.FileStat
			stats, err = gitx.New("", nil).CommitFiles(dir, c.SHA)
			for _, f := range stats {
				files = append(files, forge.FileChange{Path: f.Path, Added: f.Added, Deleted: f.Deleted, Binary: f.Binary})
			}
		case client != nil:
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			files, err = client.CommitFiles(ctx, project, c.SHA)
			cancel()
		default:
			err = fmt.Errorf("not on disk, and %s has no token", a.instanceLabel(project.Instance))
		}
		a.tv.QueueUpdateDraw(func() {
			text := filesSection(files)
			if err != nil {
				text = tag(colWarn) + "[::b]FILES[::-]" + tagEnd + "\n" + tag(colBad) + esc(err.Error()) + tagEnd + "\n"
			}
			row, col := view.GetScrollOffset()
			view.SetText(d.String() + "\n" + text)
			view.ScrollTo(row, col)
		})
	}()
}

// filesBar is the widest a file's bar of added and deleted lines gets.
const filesBar = 24

// filesSection is what a commit changed, as the detail shows it: each file
// with the lines it gained and lost, and a bar of them, + in the colour of
// good and - in that of bad, the way a diffstat reads.
func filesSection(files []forge.FileChange) string {
	added, deleted, most, pathW := 0, 0, 0, 0
	for _, f := range files {
		added += f.Added
		deleted += f.Deleted
		most = max(most, f.Added+f.Deleted)
		pathW = max(pathW, len([]rune(f.Path)))
	}
	pathW = min(pathW, 60)
	var b strings.Builder
	fmt.Fprintf(&b, "%s[::b]FILES[::-]%s %s%d · %s+%d%s %s-%d%s\n", tag(colWarn), tagEnd, tag(colDim), len(files), tag(colOn), added, tagEnd, tag(colBad), deleted, tagEnd)
	if len(files) == 0 {
		b.WriteString(tag(colDim) + "nothing changed" + tagEnd + "\n")
	}
	for _, f := range files {
		fmt.Fprintf(&b, "%s%-*s%s  ", tag(colText), pathW, esc(trim(f.Path, pathW)), tagEnd)
		if f.Binary {
			b.WriteString(tag(colDim) + "binary" + tagEnd + "\n")
			continue
		}
		plus, minus := f.Added, f.Deleted
		if most > filesBar {
			// Scaled to the widest, but never to nothing: a line is a line.
			plus = (f.Added*filesBar + most - 1) / most
			minus = (f.Deleted*filesBar + most - 1) / most
		}
		fmt.Fprintf(&b, "%s%5s%s %s%5s%s  %s%s%s%s%s%s\n",
			tag(colOn), fmt.Sprintf("+%d", f.Added), tagEnd, tag(colBad), fmt.Sprintf("-%d", f.Deleted), tagEnd,
			tag(colOn), strings.Repeat("+", plus), tagEnd, tag(colBad), strings.Repeat("-", minus), tagEnd)
	}
	return b.String()
}

// checkoutCommit puts HEAD of the checkout on a commit. The list then shows
// the commit where the branch was, and how far behind that branch it is;
// Back to Branch returns.
func (a *App) checkoutCommit(place logPlace, c logCommit) {
	if why := a.onDisk(place, c); why != "" {
		a.flash(why)
		return
	}
	pr := place.project
	a.runTaskThen(fmt.Sprintf("Checking out %s in %s", shortSHA(c.SHA), tildePath(place.dir)),
		func(log func(string)) (string, error) {
			return "", a.newManager(pr.Instance, pr.PathWithNamespace, log).CheckoutCommit(place.dir, c.SHA)
		}, func(string) { a.afterHeadMoved() })
}

// afterHeadMoved shows what a checkout changed: the branch column, and where
// the clone or the worktree now stands.
func (a *App) afterHeadMoved() {
	a.refreshDisk()
	a.loadRepoSync(false)
	a.loadWorktreeRemotes()
	a.projectsPane.reload()
	a.worktreesPane.reload()
}

// branchAtCommit asks for a name and makes a branch at a commit, in the clone.
func (a *App) branchAtCommit(place logPlace, c logCommit, back func()) {
	if why := a.onDisk(place, c); why != "" {
		a.flash(why)
		return
	}
	pr := place.project
	a.askName("New Branch at "+shortSHA(c.SHA), "", func(name string) {
		a.runTaskThen("Creating "+name, func(log func(string)) (string, error) {
			return "", a.newManager(pr.Instance, pr.PathWithNamespace, log).NewBranch(pr, name, c.SHA)
		}, func(string) {
			a.refreshDisk()
			place.reload(c.SHA, fmt.Sprintf("created %s at %s", name, shortSHA(c.SHA)))
		})
	}, back)
}

// worktreeAtCommit asks for a name and makes a branch at a commit in a
// worktree of its own, for an old state without moving the clone.
func (a *App) worktreeAtCommit(place logPlace, c logCommit, back func()) {
	if why := a.onDisk(place, c); why != "" {
		a.flash(why)
		return
	}
	pr := place.project
	a.askName("New Worktree at "+shortSHA(c.SHA), "at-"+c.SHA[:min(7, len(c.SHA))], func(name string) {
		a.runTaskThen(fmt.Sprintf("Creating a worktree of %s (%s)", pr.PathWithNamespace, name),
			func(log func(string)) (string, error) {
				return a.newManager(pr.Instance, pr.PathWithNamespace, log).WorktreeAt(pr, name, c.SHA)
			}, a.showWorktreeAt)
	}, back)
}

// askName is a form with one name in it; Cancel goes back.
func (a *App) askName(title, value string, create func(name string), back func()) {
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(labelBranchName, value, 0, nil, nil)
	form.AddButton("Create", func() {
		name := strings.TrimSpace(form.GetFormItemByLabel(labelBranchName).(*tview.InputField).GetText())
		if name == "" {
			a.flash("enter a name for the branch")
			return
		}
		a.closeModal(pageForm)
		create(name)
	})
	form.AddButton("Cancel", func() {
		a.closeModal(pageForm)
		back()
	})
	a.showFormModalSized(title, form, 60, 7)
}

// editCommitMessage asks for a commit's message again and rewrites the
// commit with it. One origin has already asks first: the branch then needs
// a force push, and other people may have built on it.
func (a *App) editCommitMessage(place logPlace, c logCommit, back func()) {
	// A refusal keeps the log, the warning over it.
	if c.Theirs {
		back()
		a.flash(shortSHA(c.SHA) + " is only on origin - only the branch's own commits can have their message edited")
		return
	}
	git := gitx.New("", nil)
	message, err := git.Message(place.dir, c.SHA)
	if err != nil {
		back()
		a.errorf("reading the message of %s: %v", shortSHA(c.SHA), err)
		return
	}
	reword := func(text string) {
		var rewritten string
		since := time.Now()
		a.runTaskThen("Editing the message of "+shortSHA(c.SHA), func(log func(string)) (string, error) {
			git := a.newManager(place.project.Instance, place.project.PathWithNamespace, log).Git()
			_, err := git.Rewriting(place.dir, gitx.RewriteChange{Kind: gitx.RewriteReword, What: "edited the message of " + shortSHA(c.SHA)}, func() error {
				var err error
				rewritten, err = git.RewordCommit(place.dir, c.SHA, text)
				return err
			})
			return "", err
		}, func(string) {
			a.afterGitChange()
			a.logRewrites(place.project, place.dir, since)
			place.reload(rewritten, "the message of "+shortSHA(rewritten)+" is edited")
		})
	}
	form := tview.NewForm()
	styleForm(form)
	field := addTextArea(form, "Message", message, 8)
	form.AddButton("Save", func() {
		text := strings.TrimSpace(field.GetText())
		if text == "" {
			a.flash("enter a message")
			return
		}
		a.closeModal(pageForm)
		if text == strings.TrimSpace(message) {
			back()
			return
		}
		if c.Local {
			reword(text)
			return
		}
		a.withRewriteWarnings(place.project, place.branch, func(warnings []string) {
			a.confirmChoicesBack("Edit Commit Message", onOriginQuestion(shortSHA(c.SHA)+" is on origin. Editing its message"),
				warnings, []choice{{"Edit", func() { reword(text) }}}, back)
		})
	})
	form.AddButton("Cancel", func() {
		a.closeModal(pageForm)
		back()
	})
	a.showFormModalSized("Edit Commit Message · "+shortSHA(c.SHA), form, 84, 14)
}

// onOriginQuestion is the question before rewriting what origin has: what
// is done, then what follows from it.
func onOriginQuestion(doing string) string {
	return doing + " rewrites its history: a force push will be needed, and anyone who built on it has to rebase."
}

// undoCommit takes the newest commit back, its changes left on disk as
// they were: IntelliJ's Undo Commit. Only the newest, since one with
// commits after it would take them too; one origin has asks first.
func (a *App) undoCommit(place logPlace, c logCommit, newest bool, back func()) {
	if !newest {
		back()
		a.flash(shortSHA(c.SHA) + " has commits after it - only the newest commit can be undone")
		return
	}
	git := a.pathManager(place.project.Instance, place.project.PathWithNamespace).Git()
	undo := func() {
		since := time.Now()
		go func() {
			_, err := git.Rewriting(place.dir, gitx.RewriteChange{Kind: gitx.RewriteUndoCommit, What: "undid commit " + shortSHA(c.SHA)},
				func() error { return git.UndoCommit(place.dir) })
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					back()
					a.errorf("%v", err)
					return
				}
				a.afterGitChange()
				a.logRewrites(place.project, place.dir, since)
				place.reload("", "undid "+shortSHA(c.SHA)+": its changes wait to be committed again")
			})
		}()
	}
	if c.Local {
		undo()
		return
	}
	a.withRewriteWarnings(place.project, place.branch, func(warnings []string) {
		a.confirmChoicesBack("Undo Commit", onOriginQuestion(shortSHA(c.SHA)+" is on origin. Undoing it"), warnings, []choice{{"Undo", undo}}, back)
	})
}

// markedRun says why the commits marked are not one run of the branch's
// history - next to each other, none only on origin - or "".
func markedRun(commits []logCommit, marked []int) string {
	pos := map[int]int{}
	n := 0
	for i, c := range commits {
		if !c.Theirs {
			pos[i] = n
			n++
		}
	}
	for k, i := range marked {
		switch {
		case commits[i].Theirs:
			return shortSHA(commits[i].SHA) + " is only on origin - mark the branch's own commits"
		case k > 0 && pos[i] != pos[marked[k-1]]+1:
			return "the commits marked are not next to each other - mark a run of them"
		}
	}
	return ""
}

// diffMarkedCommits shows in Hunk what a run of marked commits changed
// together: from before the oldest to the newest.
func (a *App) diffMarkedCommits(place logPlace, commits []logCommit, marked []int, back func()) {
	if why := markedRun(commits, marked); why != "" {
		back()
		a.flash(why)
		return
	}
	bin, ok := a.hunkBinary()
	if !ok {
		back()
		return
	}
	newest, oldest := commits[marked[0]], commits[marked[len(marked)-1]]
	go func() {
		a.runView(bin, diffView{dir: place.dir, args: []string{"diff", oldest.SHA + "~1", newest.SHA}})
		a.tv.QueueUpdateDraw(back)
	}()
}

// yankMarkedCommits offers what can be copied of the marked commits, one
// a line, newest first as the log has them.
func (a *App) yankMarkedCommits(commits []logCommit, marked []int) {
	var ids, short, lines, subjects []string
	for _, i := range marked {
		c := commits[i]
		ids = append(ids, c.SHA)
		short = append(short, shortSHA(c.SHA))
		lines = append(lines, shortSHA(c.SHA)+" "+c.Subject)
		subjects = append(subjects, c.Subject)
	}
	a.showYank(fmt.Sprintf("Copy · %d commits", len(marked)), []yankItem{
		{"Short ids and subjects", strings.Join(lines, "\n")},
		{"Commit ids", strings.Join(ids, "\n")},
		{"Short ids", strings.Join(short, "\n")},
		{"Subjects", strings.Join(subjects, "\n")},
	})
}

// squashable says why the commits marked cannot be squashed, or "": two
// or more, next to each other in the branch's history, no merge among
// them, and none that only origin has.
func squashable(commits []logCommit, marked []int) string {
	if len(marked) < 2 {
		return "mark two commits or more with space, then s squashes them"
	}
	// The branch's own history, in order: the log less what only origin has.
	var place []int
	pos := map[int]int{}
	for i, c := range commits {
		if !c.Theirs {
			pos[i] = len(place)
			place = append(place, i)
		}
	}
	for n, i := range marked {
		c := commits[i]
		switch {
		case c.Theirs:
			return shortSHA(c.SHA) + " is only on origin - only the branch's own commits are squashed"
		case c.Merge:
			return shortSHA(c.SHA) + " is a merge - commits with a merge among them are not squashed"
		case n > 0 && pos[i] != pos[marked[n-1]]+1:
			return "the commits marked are not next to each other - mark a run of them"
		}
	}
	return ""
}

// squashCommits asks for the message of the commit the marked ones become,
// their messages one after another to start with, and, when origin has any
// of them, whether to rewrite what it has. Cancel goes back to the log,
// the marks kept.
func (a *App) squashCommits(place logPlace, commits []logCommit, marked []int, back func()) {
	newest, oldest := commits[marked[0]], commits[marked[len(marked)-1]]
	git := a.pathManager(place.project.Instance, place.project.PathWithNamespace).Git()
	// Oldest first, as git's own squash puts them.
	var messages []string
	for n := len(marked) - 1; n >= 0; n-- {
		c := commits[marked[n]]
		message, err := git.Message(place.dir, c.SHA)
		if err != nil {
			back()
			a.errorf("reading the message of %s: %v", shortSHA(c.SHA), err)
			return
		}
		messages = append(messages, strings.TrimSpace(message))
	}
	pushed := 0
	for _, i := range marked {
		if !commits[i].Local {
			pushed++
		}
	}
	squash := func(text string) {
		var squashed string
		since := time.Now()
		a.runTaskThen(fmt.Sprintf("Squashing %d commits", len(marked)), func(log func(string)) (string, error) {
			git := a.newManager(place.project.Instance, place.project.PathWithNamespace, log).Git()
			_, err := git.Rewriting(place.dir, gitx.RewriteChange{Kind: gitx.RewriteSquash, What: fmt.Sprintf("squashed %d commits", len(marked))}, func() error {
				var err error
				squashed, err = git.SquashCommits(place.dir, oldest.SHA, newest.SHA, text)
				return err
			})
			return "", err
		}, func(string) {
			a.afterGitChange()
			a.logRewrites(place.project, place.dir, since)
			place.reload(squashed, fmt.Sprintf("squashed %d commits into %s", len(marked), shortSHA(squashed)))
		})
	}
	form := tview.NewForm()
	styleForm(form)
	field := addTextArea(form, "Message", strings.Join(messages, "\n\n"), 10)
	form.AddButton("Save", func() {
		text := strings.TrimSpace(field.GetText())
		if text == "" {
			a.flash("enter a message")
			return
		}
		a.closeModal(pageForm)
		if pushed == 0 {
			squash(text)
			return
		}
		body := fmt.Sprintf("%s on origin. Squashing them rewrites its history: a force push will be needed, "+
			"and anyone who built on them has to rebase.", counted(pushed, "of these commits is", "of these commits are"))
		a.withRewriteWarnings(place.project, place.branch, func(warnings []string) {
			a.confirmChoicesBack("Squash", body, warnings, []choice{{"Squash", func() { squash(text) }}}, back)
		})
	})
	form.AddButton("Cancel", func() {
		a.closeModal(pageForm)
		back()
	})
	a.showFormModalSized(fmt.Sprintf("Squash %d Commits", len(marked)), form, 84, 16)
}

// commitURL is a commit's page on the server, "" without one.
func (a *App) commitURL(place logPlace, c logCommit) string {
	if c.WebURL != "" {
		return c.WebURL
	}
	if client := a.client(place.project.Instance); client != nil {
		return client.CommitURL(place.project, c.SHA)
	}
	return ""
}

// yankCommit offers what can be copied of a commit, the link first. The
// reference is a line to paste into a chat: what and where in words, then
// the link, which the chat makes clickable.
func (a *App) yankCommit(place logPlace, c logCommit) {
	// A commit no remote has has no page to link to.
	url := ""
	if !c.Local {
		url = a.commitURL(place, c)
	}
	short := shortSHA(c.SHA)
	reference := linkWithText(url, place.project.PathWithNamespace, place.branch, short, trim(c.Subject, 60))
	markdown := ""
	if url != "" {
		markdown = fmt.Sprintf("[%s %s](%s)", short, c.Subject, url)
	}
	message := c.Subject
	if c.Body != "" {
		message += "\n\n" + c.Body
	}
	a.showYank("Copy · "+short, []yankItem{
		{"Link", url},
		{"Link with text", reference},
		{"Markdown", markdown},
		{"Commit id", c.SHA},
		{"Short id", short},
		{"Subject", c.Subject},
		{"Message", message},
	})
}

// ---------------------------------------------------------- the three logs

// repositoryLog is the log of a repository: of what is out in the clone, or,
// before it is cloned, of the default branch on the server.
func (a *App) repositoryLog(pr forge.Project) {
	info := a.diskOf(pr.Instance, pr.PathWithNamespace)
	if info.Cloned {
		var place logPlace
		place = logPlace{
			title: "Commit Log · " + pr.PathWithNamespace + " (" + info.Branch + ")", project: pr,
			dir: a.projectDir(pr.Instance, pr.PathWithNamespace), branch: info.Branch, checkout: true, branches: true,
			reload: func(focus, done string) { a.localLog(place, focus, done) },
		}
		a.localLog(place, "", "")
		return
	}
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(pr.Instance))
		return
	}
	place := logPlace{title: fmt.Sprintf("Commit Log · %s (%s on the server)", pr.PathWithNamespace, pr.DefaultBranch),
		project: pr, branch: pr.DefaultBranch}
	var commits []logCommit
	a.loadThen("Reading the commits of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		listed, err := client.ProjectCommits(ctx, pr, pr.DefaultBranch, 100)
		commits = forgeLog(listed)
		return "", err
	}, func(string) { a.showCommitLog(place, commits, 0) })
}

// worktreeLog is the log of the branch a worktree has out. A grouped
// worktree has one per repository; its view gives each block its own.
func (a *App) worktreeLog(r worktreeRow) {
	if r.grouped() {
		a.flash("a group has a log per repository - open its view with Enter and light one")
		return
	}
	var place logPlace
	place = logPlace{
		title: "Commit Log · " + r.Path + " (" + r.Branch + ")", project: a.worktreeProject(r),
		dir: r.Dir, branch: r.Branch, checkout: true, branches: true,
		reload: func(focus, done string) { a.localLog(place, focus, done) },
	}
	a.localLog(place, "", "")
}

// localLog reads a checkout's log off the event loop and lists it, the
// cursor on focus, done said once it is drawn.
func (a *App) localLog(place logPlace, focus, done string) {
	git := gitx.New("", nil)
	go func() {
		entries, err := git.History(place.dir, "HEAD", historyLimit)
		unpushed := git.Unpushed(place.dir)
		local := git.LocalCommits(place.dir)
		upstream, theirs, theirsErr := git.UpstreamOnly(place.dir, historyLimit)
		a.tv.QueueUpdateDraw(func() {
			if err == nil {
				err = theirsErr
			}
			if err != nil {
				a.errorf("reading the log: %v", err)
				return
			}
			place.upstream = upstream
			commits := sectionLog(entries, theirs, unpushed, local)
			start := -1
			for i, c := range commits {
				if c.SHA == focus || (focus == "" && start < 0 && !c.Theirs) {
					start = i
				}
			}
			a.showCommitLog(place, commits, max(start, 0))
			if done != "" {
				a.done(done)
			}
		})
	}()
}

// sectionLog puts a checkout's commits in the order the log shows them:
// what is only here, then - once the branch and its upstream have parted -
// what only the upstream has, then what both have, each newest first.
func sectionLog(entries, theirs []gitx.LogEntry, unpushed, local map[string]bool) []logCommit {
	var here, shared []logCommit
	for _, e := range entries {
		c := logCommit{LogEntry: e, Unpushed: unpushed[e.SHA], Local: local[e.SHA]}
		if c.Unpushed {
			here = append(here, c)
		} else {
			shared = append(shared, c)
		}
	}
	commits := here
	for _, e := range theirs {
		commits = append(commits, logCommit{LogEntry: e, Theirs: true})
	}
	return append(commits, shared...)
}

// mergeRequestLog is the commits of a merge request as the server has them,
// those new since the last review marked, the cursor on the oldest of them:
// where a review of what is new starts. It is looked at from whichever
// checkout of it is on disk; nothing is cloned to list it.
func (a *App) mergeRequestLog(mr forge.MergeRequest) {
	seen := a.seen[seenKey(mr)]
	project := a.mrProject(mr)
	path := project.PathWithNamespace
	client := a.client(mr.Instance)
	dir := ""
	disk := a.diskOf(mr.Instance, path)
	switch {
	case disk.MRs[mr.IID].Review:
		dir = a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	case disk.MRs[mr.IID].Branch:
		dir = a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)
	case disk.Cloned:
		dir = a.projectDir(mr.Instance, path)
	}
	var commits []workspace.MRCommit
	a.loadThen(fmt.Sprintf("Reading the commits of %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		mr := a.refreshMR(client, mr, log)
		rev := reviewRefs(ctx, client, mr, log)
		rev.Seen = seen
		m := a.newManager(mr.Instance, path, log)
		var err error
		commits, err = forgeCommits(ctx, client, mr)
		if err != nil {
			log("! " + err.Error())
			log("  reading them from a clone instead")
			if commits, err = m.MRCommits(mr, project, rev); err != nil {
				return "", err
			}
		} else if err := m.MarkUnseen(mr, project, rev, commits); err != nil {
			log("! could not tell which commits are new: " + err.Error())
		}
		if len(commits) == 0 {
			return "", fmt.Errorf("!%d has no commits of its own", mr.IID)
		}
		return "", nil
	}, func(string) {
		// They come oldest first; the log is newest first.
		listed := make([]logCommit, len(commits))
		start, fresh := 0, 0
		for i, c := range commits {
			at := len(commits) - 1 - i
			listed[at] = logCommit{LogEntry: c.LogEntry, New: c.New}
			if client != nil {
				listed[at].WebURL = client.CommitURL(project, c.SHA)
			}
			if c.New {
				if fresh == 0 {
					start = at
				}
				fresh++
			}
		}
		if len(listed) > 0 {
			listed[0].CI = mr.Pipeline
		}
		title := fmt.Sprintf("Commit Log · %s !%d", path, mr.IID)
		if fresh > 0 {
			title += fmt.Sprintf(" · %s %d new since your last review", glyphDot, fresh)
		}
		place := logPlace{title: title, project: project, dir: dir, branch: mr.SourceBranch, mr: &mr}
		a.showCommitLog(place, listed, start)
	})
}

// forgeLog turns the forge's commits, newest first as it answers, into a log.
func forgeLog(listed []forge.Commit) []logCommit {
	commits := make([]logCommit, len(listed))
	for i, c := range listed {
		sha := c.ID
		if sha == "" {
			sha = c.ShortID
		}
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Message), c.Title))
		commits[i] = logCommit{LogEntry: gitx.LogEntry{
			SHA:     sha,
			Subject: c.Title,
			Author:  c.AuthorName,
			When:    c.CommittedDate,
			Merge:   len(c.ParentIDs) > 1,
			Body:    body,
		}, WebURL: c.WebURL}
	}
	return commits
}

// backToBranch checks out again the branch a detached HEAD came from.
func (a *App) backToBranch(pr forge.Project, dir string) {
	a.runTaskThen("Back to the branch in "+tildePath(dir), func(log func(string)) (string, error) {
		_, err := a.newManager(pr.Instance, pr.PathWithNamespace, log).BackToBranch(dir)
		return "", err
	}, func(string) { a.afterHeadMoved() })
}
