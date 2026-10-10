package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// Recover from Reflog: every place the branch has been, whatever moved it
// there - a rebase or a reset in a terminal, lazygit, unagit - read from
// git's reflog, with the way back to any of them. Rewrite History knows
// only what unagit did; this is the net under everything else. Going back
// is written down like any rewrite, so Rewrite History undoes it.

// reflogScope is the checkout whose branch's places are listed.
type reflogScope struct {
	project forge.Project
	dir     string
	// branch is the branch out there, "" for a detached HEAD, whose own
	// reflog is listed then.
	branch string
	done   string
}

// reflogLimit is how far back the list goes.
const reflogLimit = 200

// showReflog reads the reflog off the loop and lists it, newest first.
func (a *App) showReflog(s reflogScope) {
	if s.dir == "" {
		a.flash(s.project.PathWithNamespace + " is not cloned - it has no reflog here")
		return
	}
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		entries, err := git.Reflog(s.dir, s.branch, reflogLimit)
		head, _ := git.Run(s.dir, "rev-parse", "HEAD")
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("reading the reflog: %v", err)
				return
			}
			a.listReflog(s, entries, strings.TrimSpace(head))
		})
	}()
}

// listReflog shows the places, the one the branch is at now marked.
func (a *App) listReflog(s reflogScope, entries []gitx.ReflogEntry, head string) {
	if len(entries) == 0 {
		if back := a.peekBack(); back != nil {
			back()
		}
		a.note("git keeps no reflog of " + s.label() + " here")
		return
	}
	items := make([]pickItem, len(entries))
	table := make([][]string, len(entries))
	for i, e := range entries {
		now := ""
		if e.SHA == head {
			now = tag(colOn) + "now" + tagEnd
		}
		table[i] = []string{
			tag(role("activity.when")) + esc(eventTime(e.At)) + tagEnd,
			tag(colMuted) + esc(shortSHA(e.SHA)) + tagEnd + " " + esc(trim(e.Subject, 48)),
			tag(role("activity.about")) + esc(trim(e.Action, 60)) + tagEnd,
			now,
		}
		items[i] = pickItem{About: e.At.Format("Mon 2006-01-02 15:04") + " · " + e.Action, Data: e}
	}
	header, labels := pickTable([]string{"WHEN", "COMMIT", "WHAT MOVED IT", ""}, table)
	for i := range items {
		items[i].Label = labels[i]
	}
	at := func(it pickItem) gitx.ReflogEntry { return it.Data.(gitx.ReflogEntry) }
	again := a.backOr(func() { a.showReflog(reflogScope{project: s.project, dir: s.dir, branch: s.branch}) })
	elsewhere := func(it pickItem) bool { return at(it).SHA != head }
	a.showPickerWith("Recover from Reflog · "+s.label(), items, pickerOptions{
		wide: true, explain: true, header: header, enterHint: "details",
		enterName: "Show Details", enterAbout: "The commits the branch had there and has not now, and the other way round.",
		keys: []pickKey{
			{keys: "u", hint: "go back here", name: "Go Back Here…", about: "Put the branch back where it was then, the files with it; asks first. Rewrite History undoes it.",
				when: elsewhere, run: func(it pickItem) {
					if !elsewhere(it) {
						again()
						a.flash("the branch is there now - pick a place it was before")
						return
					}
					a.recoverTo(s, at(it), head, again)
				}},
			{keys: "D", hint: "diff", name: "Show Diff in Hunk", about: "What the files are there against now.",
				when: elsewhere, run: func(it pickItem) { a.diffReflog(s, at(it), again) }},
			{keys: "y", hint: "copy", name: "Copy Commit", about: "Copy the commit's id.",
				run: func(it pickItem) {
					if err := copyToClipboard(at(it).SHA); err != nil {
						a.errorf("copying: %v", err)
						return
					}
					again()
					a.done("copied " + shortSHA(at(it).SHA))
				}},
		},
	}, func(it pickItem) { a.showReflogDetail(s, at(it), head, again) })
	if s.done != "" {
		a.done(s.done)
	}
}

// label names what the reflog is of.
func (s reflogScope) label() string {
	if s.branch == "" {
		return s.project.PathWithNamespace + " (HEAD)"
	}
	return s.project.PathWithNamespace + " (" + s.branch + ")"
}

// showReflogDetail is one place: what moved the branch there, and the
// commits it had there and has not now, and the other way round.
func (a *App) showReflogDetail(s reflogScope, e gitx.ReflogEntry, head string, back func()) {
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		gone := git.RewriteCommits(s.dir, e.SHA, head)
		came := git.RewriteCommits(s.dir, head, e.SHA)
		a.tv.QueueUpdateDraw(func() {
			d := &detailBuf{}
			d.title(e.Subject)
			d.blank()
			d.kv("Commit", esc(e.SHA))
			d.kv("When", esc(e.At.Format("Mon 2006-01-02 15:04")+" · "+humanAge(e.At)))
			d.kv("Moved by", esc(e.Action))
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
			commits("THERE, NOT NOW", gone)
			commits("NOW, NOT THERE", came)
			view := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetWordWrap(true).SetScrollable(true)
			view.SetText(d.String())
			box(view.Box, "Reflog · "+shortSHA(e.SHA))
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

// diffReflog shows in Hunk what the files were there against now.
func (a *App) diffReflog(s reflogScope, e gitx.ReflogEntry, back func()) {
	bin, ok := a.hunkBinary()
	if !ok {
		back()
		return
	}
	go func() {
		a.runView(bin, diffView{dir: s.dir, args: []string{"diff", e.SHA, "HEAD"}})
		a.tv.QueueUpdateDraw(back)
	}()
}

// recoverTo asks, then puts the branch back at a place of its reflog.
func (a *App) recoverTo(s reflogScope, e gitx.ReflogEntry, head string, back func()) {
	git := a.pathManager(s.project.Instance, s.project.PathWithNamespace).Git()
	go func() {
		leaving := git.Count(s.dir, e.SHA+"..HEAD")
		tip, tipErr := git.UpstreamTip(s.dir)
		force := tipErr == nil && tip != "" && !git.IsAncestor(s.dir, tip, e.SHA)
		a.tv.QueueUpdateDraw(func() {
			var b strings.Builder
			what := s.branch
			if what == "" {
				what = "HEAD"
			}
			fmt.Fprintf(&b, "Put [::b]%s[::-] back at %s, where it was %s?", esc(what), shortSHA(e.SHA), esc(eventTime(e.At)))
			fmt.Fprintf(&b, "\n\nIt got there by: %s.", esc(e.Action))
			if leaving > 0 {
				fmt.Fprintf(&b, "\n\n%s on it now %s it.", counted(leaving, "commit", "commits"), plural(leaving, "leaves", "leave"))
			}
			b.WriteString("\n\nThe files go back too; what is not committed stays.")
			if force {
				b.WriteString("\n\nOrigin has what the branch leaves: a force push will be needed.")
			}
			b.WriteString("\n\nRewrite History can undo it.")
			ask := func(warnings []string) {
				a.confirmChoicesBack("Go back", b.String(), warnings, []choice{{"Go Back", func() { a.runRecover(s, e) }}}, back)
			}
			if !force {
				ask(nil)
				return
			}
			a.withRewriteWarnings(s.project, s.branch, ask)
		})
	}()
}

// runRecover puts the branch back under a log, and lists the reflog again.
func (a *App) runRecover(s reflogScope, e gitx.ReflogEntry) {
	since := time.Now()
	what := "went back to " + shortSHA(e.SHA) + " from the reflog"
	a.runTaskThen("Going back to "+shortSHA(e.SHA), func(log func(string)) (string, error) {
		_, err := a.newManager(s.project.Instance, s.project.PathWithNamespace, log).Git().RecoverTo(s.dir, e.SHA, what)
		return "", err
	}, func(string) {
		a.afterGitChange()
		a.loadWorktreeRemotes()
		a.logRewrites(s.project, s.dir, since)
		next := s
		next.done = what
		a.showReflog(next)
	})
}

// worktreeReflog is the scope of Recover from Reflog… for a worktree.
func (a *App) worktreeReflog(r worktreeRow) reflogScope {
	branch := r.Branch
	if branch == "(detached)" {
		branch = ""
	}
	return reflogScope{project: a.worktreeProject(r), dir: r.Dir, branch: branch}
}

// cloneReflog is the scope of Recover from Reflog… for a clone.
func (a *App) cloneReflog(pr forge.Project) reflogScope {
	branch := a.diskOf(pr.Instance, pr.PathWithNamespace).Branch
	if strings.HasPrefix(branch, "@") {
		branch = ""
	}
	return reflogScope{project: pr, dir: a.projectDir(pr.Instance, pr.PathWithNamespace), branch: branch}
}
