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
}

// historyLimit is how far back a log goes; it is for looking around, not
// for archaeology.
const historyLimit = 200

// showCommitLog lists commits, the cursor on start.
func (a *App) showCommitLog(place logPlace, commits []logCommit, start int) {
	if len(commits) == 0 {
		a.note("no commits to show")
		return
	}
	items := make([]pickItem, len(commits))
	for i, c := range commits {
		items[i] = pickItem{About: commitAbout(c, place), Data: i}
	}
	labelLog(items, commits, logRowWidth(a.screenWidth()))
	at := func(it pickItem) logCommit { return commits[it.Data.(int)] }
	again := func(it pickItem) func() { return func() { a.showCommitLog(place, commits, it.Data.(int)) } }

	keys := []pickKey{
		{keys: "D", hint: "diff", name: "Show Diff in Hunk", about: "What the commit changed, in Hunk.", run: func(it pickItem) { a.showCommitDiff(place, commits, it.Data.(int), false) }},
		{keys: "Alt-D", hint: "since", name: "Show Changes Since", about: "Everything from the commit to the working tree, in Hunk.", run: func(it pickItem) { a.showCommitDiff(place, commits, it.Data.(int), true) }},
	}
	if place.checkout {
		keys = append(keys, pickKey{keys: "C", hint: "checkout", name: "Check Out Commit", about: "Put the checkout at this commit, detached; B goes back to the branch.", run: func(it pickItem) { a.checkoutCommit(place, at(it)) }})
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
		pickKey{keys: "w", hint: "browser", name: "Open in Browser", about: "The commit's page on the forge.", run: func(it pickItem) { a.openWeb(a.commitURL(place, at(it))) }},
		pickKey{keys: "y", hint: "copy", name: "Copy…", about: "Copy the commit's id, link or reference.", run: func(it pickItem) { a.yankCommit(place, at(it)) }})

	// Not packed: the rows are short, but the keys are many, and their hints
	// should fit on a line or two rather than wrap down the side.
	opts := pickerOptions{start: start, wide: true, explain: true, enterHint: "details", keys: keys,
		enterName: "Show Details", enterAbout: "The whole commit: its message, refs and the files it changed.",
		relabel: func(items []pickItem, width int) { labelLog(items, commits, width) }}
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

// labelLog writes the rows of a log for a row of width cells. The subjects
// make a column, so what comes after them lines up, and it takes what the row
// has left after the id, the age and the refs: a subject is cut only where
// the dialog ends. The pane under the list has the rest of the message.
func labelLog(items []pickItem, commits []logCommit, width int) {
	refsW := 0
	for _, c := range commits {
		refsW = max(refsW, len([]rune(logSub(c))))
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
	for i, c := range commits {
		// The picker filters on the text as it is drawn, so the marks are
		// plain characters, not colour tags.
		mark := "  "
		switch {
		case c.New:
			mark = glyphDot + " "
		case c.Unpushed:
			mark = glyphAhead + " "
		}
		items[i].Label = esc(fmt.Sprintf("%s%s  %-*s  %-*s", mark, shortSHA(c.SHA), subjectW, subjects[i], authorW, trim(c.Author, authorW)))
		items[i].Sub = esc(logSub(c))
	}
}

// logSub is what follows a subject: the age in a column of its own, then the
// pipeline and what points at the commit, where a varying length disturbs
// nothing.
func logSub(c logCommit) string {
	rest := refWords(c.Refs)
	if ci, _ := ciMark(c.CI); ci != "" {
		rest = strings.TrimSpace(ci + " " + c.CI + " " + rest)
	}
	return strings.TrimSpace(fmt.Sprintf("%-8s  %s", humanAge(c.When), rest))
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
	hintPanel(view.Box, func() string { return "j/k scroll · Ctrl-D/U half a page · Esc back to the log" }, 0, 0, 1, 1)
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
	// What it changed comes from git, off the event loop.
	if a.onDisk(place, c) == "" {
		dir := place.dir
		go func() {
			stat, err := gitx.New("", nil).ShowStat(dir, c.SHA)
			if err != nil || stat == "" {
				return
			}
			a.tv.QueueUpdateDraw(func() {
				view.SetText(d.String() + "\n" + tag(colWarn) + "[::b]FILES[::-]" + tagEnd + "\n" + tag(colMuted) + esc(stat) + tagEnd + "\n")
			})
		}()
	}
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
	url := a.commitURL(place, c)
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
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(pr.Instance))
		return
	}
	place := logPlace{title: fmt.Sprintf("Commit Log · %s (%s on the server)", pr.PathWithNamespace, pr.DefaultBranch),
		project: pr, branch: pr.DefaultBranch}
	var commits []logCommit
	a.runTaskThen("Reading the commits of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
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
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("reading the log: %v", err)
				return
			}
			commits := make([]logCommit, len(entries))
			start := 0
			for i, e := range entries {
				commits[i] = logCommit{LogEntry: e, Unpushed: unpushed[e.SHA]}
				if e.SHA == focus {
					start = i
				}
			}
			a.showCommitLog(place, commits, start)
			if done != "" {
				a.done(done)
			}
		})
	}()
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
	a.runTaskThen(fmt.Sprintf("Reading the commits of %s !%d", path, mr.IID), func(log func(string)) (string, error) {
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
