package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/watch"
)

// Rewrite History: every change unagit made to the history of a
// repository's branches - squashes, messages edited, commits undone,
// rebases, bases set, branches deleted - in the order they were made, each
// one that can be undone with u. gitx keeps the record (rewrites.go); this
// is how it is read and undone, and how each change reaches the Activity
// screen's log, as the local history beside the news from the servers.

// rewriteScope is the repository whose rewrites are listed.
type rewriteScope struct {
	project forge.Project
	dir     string // a checkout of it
	// focus is the rewrite the cursor starts on, done what to say once
	// the list is drawn.
	focus, done string
}

// showRewrites reads the record off the loop and lists it, newest first.
func (a *App) showRewrites(s rewriteScope) {
	if s.dir == "" {
		a.flash(s.project.PathWithNamespace + " is not cloned - it has no history of its own here")
		return
	}
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		all := git.Rewrites(s.dir)
		a.tv.QueueUpdateDraw(func() { a.listRewrites(s, all) })
	}()
}

// listRewrites shows the record, the cursor on s.focus.
func (a *App) listRewrites(s rewriteScope, all []gitx.Rewrite) {
	if len(all) == 0 {
		// Asked from a list, the list stays (back.go).
		if back := a.peekBack(); back != nil {
			back()
		}
		a.note("nothing rewritten in " + s.project.PathWithNamespace + " yet - squashes, rebases and deleted branches come here")
		return
	}
	items := make([]pickItem, len(all))
	table := make([][]string, len(all))
	start := 0
	for i, r := range all {
		ink := tag(role("activity.what"))
		state := ""
		if gitx.Undone(all, r) {
			ink, state = tag(colDim), tag(colDim)+"undone"+tagEnd
		}
		table[i] = []string{
			tag(role("activity.when")) + esc(eventTime(r.At)) + tagEnd,
			tag(role("column.branch")) + esc(r.Branch) + tagEnd,
			ink + esc(r.What) + tagEnd,
			tag(colMuted) + esc(rewriteMove(r)) + tagEnd,
			state,
		}
		items[i] = pickItem{About: rewriteAbout(r), Data: r}
		if r.ID == s.focus {
			start = i
		}
	}
	header, labels := pickTable([]string{"WHEN", "BRANCH", "WHAT", "CHANGE", ""}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	at := func(it pickItem) gitx.Rewrite { return it.Data.(gitx.Rewrite) }
	again := func(it pickItem) func() {
		return a.backOr(func() {
			next := s
			next.focus, next.done = at(it).ID, ""
			a.showRewrites(next)
		})
	}
	a.showPickerWith("Rewrite History · "+s.project.PathWithNamespace, items, pickerOptions{
		start: start, wide: true, explain: true, header: header, enterHint: "details",
		enterName: "Show Details", enterAbout: "What the change was: the branch's commits before it and after it.",
		keys: []pickKey{
			{keys: "u", hint: "undo", name: "Undo…", about: "Put the branch back as it was before, the later changes of it with it; asks first. The undo can be undone here too.",
				when: func(it pickItem) bool { return !gitx.Undone(all, at(it)) },
				run:  func(it pickItem) { a.undoRewrite(s, at(it), again(it)) }},
			{keys: "D", hint: "diff", name: "Show Diff in Hunk", about: "What the change did to the files, from before to after; a squash or an edited message changes none.",
				when: func(it pickItem) bool { return at(it).Before != "" && at(it).After != "" },
				run:  func(it pickItem) { a.diffRewrite(s, at(it), again(it)) }},
			{keys: "y", hint: "copy", name: "Copy Commit Before", about: "Copy the id of the branch's tip before the change: what git reset would take to go back by hand.",
				when: func(it pickItem) bool { return at(it).Before != "" },
				run: func(it pickItem) {
					if err := copyToClipboard(at(it).Before); err != nil {
						a.errorf("copying: %v", err)
						return
					}
					again(it)()
					a.done("copied " + shortSHA(at(it).Before))
				}},
		},
	}, func(it pickItem) { a.showRewriteDetail(s, at(it), again(it)) })
	if s.done != "" {
		a.done(s.done)
	}
}

// rewriteMove is where a rewrite took its branch: 1234abcd → f00dbabe.
func rewriteMove(r gitx.Rewrite) string {
	switch {
	case r.Before == "" && r.After == "":
		return ""
	case r.After == "":
		return shortSHA(r.Before) + " → deleted"
	case r.Before == "":
		return "made → " + shortSHA(r.After)
	case r.Before == r.After:
		return "at " + shortSHA(r.After)
	}
	return shortSHA(r.Before) + " → " + shortSHA(r.After)
}

// rewriteAbout is the pane under the list: what kind of change, where.
func rewriteAbout(r gitx.Rewrite) string {
	about := r.Kind + " · " + r.At.Format("Mon 2006-01-02 15:04") + " · " + tildePath(r.Dir)
	if r.Files {
		about += " · the files changed too"
	}
	return about
}

// rewriteHeading is a rewrite's few words in the Activity log.
func rewriteHeading(r gitx.Rewrite) string {
	switch r.Kind {
	case gitx.RewriteSquash:
		return "Squashed"
	case gitx.RewriteReword:
		return "Message edited"
	case gitx.RewriteUndoCommit:
		return "Commit undone"
	case gitx.RewriteRebase:
		return "Rebased"
	case gitx.RewriteBase:
		return "Base set"
	case gitx.RewriteDelete:
		return "Branch deleted"
	case gitx.RewriteUndo:
		return "Undone"
	case gitx.RewriteRecover:
		return "Recovered"
	}
	return r.Kind
}

// showRewriteDetail is a rewrite whole: what, where, when, and the commits
// the branch had before that it no longer has, and the other way round.
func (a *App) showRewriteDetail(s rewriteScope, r gitx.Rewrite, back func()) {
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		gone := git.RewriteCommits(s.dir, r.Before, r.After)
		came := git.RewriteCommits(s.dir, r.After, r.Before)
		a.tv.QueueUpdateDraw(func() {
			d := &detailBuf{}
			d.title(r.What)
			d.blank()
			d.kv("Branch", esc(r.Branch))
			d.kv("When", esc(r.At.Format("Mon 2006-01-02 15:04")+" · "+humanAge(r.At)))
			d.kv("Where", esc(tildePath(r.Dir)))
			d.kv("Before", esc(r.Before))
			d.kv("After", esc(r.After))
			if r.Files {
				d.kv("Files", "changed too: an undo puts them back")
			}
			commits := func(heading string, lines []string) {
				d.blank()
				d.raw(tag(colWarn) + "[::b]" + heading + "[::-]" + tagEnd + "\n")
				if len(lines) == 0 {
					d.raw(tag(colDim) + "none" + tagEnd + "\n")
				}
				for _, line := range lines {
					id, subject, _ := strings.Cut(line, " ")
					d.raw(tag(colDim) + esc(id) + tagEnd + " " + tag(colText) + esc(subject) + tagEnd + "\n")
				}
			}
			commits("BEFORE, NOT AFTER", gone)
			commits("AFTER, NOT BEFORE", came)
			view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetScrollable(true)
			view.SetText(d.String())
			box(view.Box, "Rewrite · "+r.Kind)
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
		})
	}()
}

// diffRewrite shows in Hunk what a rewrite did to the files; one that
// changed none says so.
func (a *App) diffRewrite(s rewriteScope, r gitx.Rewrite, back func()) {
	bin, ok := a.hunkBinary()
	if !ok {
		back()
		return
	}
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		trees, _ := git.Run(s.dir, "rev-parse", r.Before+"^{tree}", r.After+"^{tree}")
		if f := strings.Fields(trees); len(f) == 2 && f[0] == f[1] {
			a.tv.QueueUpdateDraw(func() {
				back()
				a.note("the files are the same before and after - only the commits changed (Enter shows them)")
			})
			return
		}
		a.runView(bin, diffView{dir: s.dir, args: []string{"diff", r.Before, r.After}})
		a.tv.QueueUpdateDraw(back)
	}()
}

// undoRewrite says what undoing a rewrite would do and does it once asked.
func (a *App) undoRewrite(s rewriteScope, r gitx.Rewrite, back func()) {
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		plan, err := git.PlanUndo(s.dir, r.ID)
		a.tv.QueueUpdateDraw(func() {
			switch {
			case errors.Is(err, gitx.ErrUndone):
				back()
				a.flash("it was undone already - undo the undo to bring it back")
				return
			case err != nil:
				back()
				a.errorf("%v", err)
				return
			case plan.Blocked != "":
				back()
				a.flash(plan.Blocked)
				return
			}
			ask := func(warnings []string) {
				a.confirmChoicesBack("Undo", undoQuestion(r, plan), warnings, []choice{{"Undo", func() { a.runUndo(s, r) }}}, back)
			}
			if !plan.Force {
				ask(nil)
				return
			}
			a.withRewriteWarnings(s.project, r.Branch, ask)
		})
	}()
}

// undoQuestion says what an undo will do, before it is done.
func undoQuestion(r gitx.Rewrite, plan gitx.UndoPlan) string {
	var b strings.Builder
	switch {
	case r.Kind == gitx.RewriteDelete:
		fmt.Fprintf(&b, "Make [::b]%s[::-] again at %s?", esc(r.Branch), shortSHA(plan.To))
	default:
		fmt.Fprintf(&b, "Undo \"%s\" on [::b]%s[::-]?\n\nThe branch goes back to %s.", esc(r.What), esc(r.Branch), shortSHA(plan.To))
	}
	if n := len(plan.Chain) - 1; n > 0 {
		fmt.Fprintf(&b, "\n\n%s of it %s undone with it.", counted(n, "later change", "later changes"), plural(n, "is", "are"))
	}
	if plan.Since > 0 {
		fmt.Fprintf(&b, "\n\n%s made since %s the branch too.", counted(plan.Since, "commit", "commits"), plural(plan.Since, "leaves", "leave"))
	}
	if plan.Moved {
		b.WriteString("\n\nThe branch has moved elsewhere since; where it is now is kept here.")
	}
	if plan.Files {
		b.WriteString("\n\nThe files go back too; what is not committed stays.")
	}
	if plan.Force {
		b.WriteString("\n\nOrigin has what the branch leaves: a force push will be needed.")
	}
	b.WriteString("\n\nThe undo can be undone here as well.")
	return b.String()
}

// runUndo undoes a rewrite under a log, and lists the record again.
func (a *App) runUndo(s rewriteScope, r gitx.Rewrite) {
	since := time.Now()
	var undo gitx.Rewrite
	a.runTaskThen("Undoing: "+r.What, func(log func(string)) (string, error) {
		var err error
		undo, err = a.newManager(s.project.Instance, s.project.PathWithNamespace, log).Git().UndoRewrite(s.dir, r.ID)
		return "", err
	}, func(string) {
		a.afterGitChange()
		a.loadWorktreeRemotes()
		a.logRewrites(s.project, s.dir, since)
		next := s
		next.focus, next.done = undo.ID, undo.What
		a.showRewrites(next)
	})
}

// logRewrites puts what was rewritten in a repository since a moment into
// the Activity log, as its local history.
func (a *App) logRewrites(pr forge.Project, dir string, since time.Time) {
	if a.watchStore == nil || dir == "" {
		return
	}
	git := a.pathManager(pr.Instance, pr.PathWithNamespace).Git()
	store := a.watchStore
	go func() {
		var events []watch.Event
		for _, r := range git.Rewrites(dir) {
			if r.At.Before(since) {
				break
			}
			events = append(events, rewriteEvent(pr, r))
		}
		// The record is newest first; the log is written oldest first.
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
		_ = store.AppendHistory(false, events...)
	}()
}

// localKey is the thing a repository's local history is in the Activity
// log, joined as a watch's key is.
func localKey(pr forge.Project) string {
	return strings.Join([]string{"local", pr.Instance, pr.PathWithNamespace}, "\x00")
}

// rewriteEvent is a rewrite as the Activity log has it.
func rewriteEvent(pr forge.Project, r gitx.Rewrite) watch.Event {
	line := r.What
	if move := rewriteMove(r); move != "" {
		line += " · " + move
	}
	return watch.Event{
		Key:     localKey(pr),
		What:    pr.PathWithNamespace + " " + r.Branch,
		Heading: rewriteHeading(r),
		Line:    line,
		Project: pr.PathWithNamespace,
		Level:   watch.Info,
		At:      r.At,
		Local:   true,
		Dir:     r.Dir,
		Ref:     r.ID,
	}
}

// openLocalEvent lists the rewrites of the repository of a local event,
// the cursor on its rewrite.
func (a *App) openLocalEvent(e watch.Event) {
	parts := strings.Split(e.Key, "\x00")
	if len(parts) != 3 {
		return
	}
	instance, path := parts[1], parts[2]
	pr, ok := a.projByKey[projectKey{instance, path}]
	if !ok {
		pr = forge.Project{Instance: instance, PathWithNamespace: path}
	}
	a.showRewrites(rewriteScope{project: pr, dir: e.Dir, focus: e.Ref})
}

// localHistoryAction narrows the Activity log to the local history, or
// widens it again.
func (a *App) localHistoryAction() uiAction {
	name, about := "Show Local History", "Show in the log only what was done to the branches here - squashes, rebases, deleted branches - each to be undone with Enter."
	if a.activityLocal {
		name, about = "Show Everything", "Show in the log everything again: the watches' news, the agents, and the local history."
	}
	return uiAction{name: name, about: about, keys: "H", rank: 22, run: a.toggleLocalHistory}
}

// withRewriteWarnings says what makes rewriting what origin has of a
// branch more serious - the default branch, one the server protects, a
// merge request open from it - and goes on with it on the loop. The
// server is asked whether it protects the branch; when it cannot answer,
// that warning is left out rather than the question held up.
func (a *App) withRewriteWarnings(pr forge.Project, branch string, then func(warnings []string)) {
	var warnings []string
	if branch != "" && branch == pr.DefaultBranch {
		warnings = append(warnings, branch+" is the default branch: rewriting what origin has of it rewrites the history everyone builds on")
	}
	if mr, ok := a.openMROn(pr, branch); ok {
		warnings = append(warnings, fmt.Sprintf("!%d is open from %s: after the force push it shows the new commits, and comments on the old ones may be outdated", mr.IID, branch))
	}
	client := a.client(pr.Instance)
	if client == nil || branch == "" {
		then(warnings)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		protected, _ := client.BranchProtected(ctx, pr, branch)
		a.tv.QueueUpdateDraw(func() {
			if protected {
				warnings = append(warnings, "origin protects "+branch+": the force push will most likely be refused")
			}
			then(warnings)
		})
	}()
}
