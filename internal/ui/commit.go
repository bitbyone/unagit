package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rivo/tview"
)

// c in Worktrees commits everything a worktree has not committed, or every
// repository of a grouped one: staged or not, new files and deletions alike.
// One message serves all of them; a repository can be given its own.

// commitTarget is one working tree about to be committed.
type commitTarget struct {
	row   worktreeRow
	name  string // its folder in a group, or its repository
	edits int
}

func (a *App) commitWorktree(r worktreeRow) {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	// Counted afresh: what the list last read may be a moment old.
	gits := make([]func() int, len(members))
	for i, m := range members {
		git := a.pathManager(m.Instance, m.Path).Git()
		gits[i] = func() int { return git.Edits(m.Dir) }
	}
	go func() {
		var targets []commitTarget
		for i, m := range members {
			if n := gits[i](); n > 0 {
				name := m.Path
				if r.grouped() {
					name = filepath.Base(m.Dir)
				}
				targets = append(targets, commitTarget{row: m, name: name, edits: n})
			}
		}
		a.tv.QueueUpdateDraw(func() {
			if len(targets) == 0 {
				a.flash("nothing to commit in " + r.Path)
				return
			}
			a.showCommitForm(r, targets)
		})
	}()
}

// showCommitForm asks for the message, and lets a repository have its own.
func (a *App) showCommitForm(r worktreeRow, targets []commitTarget) {
	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	// With several repositories the shared message says it is shared, and each
	// repository has a field of its own under it, named as one: a bare name
	// with a count beside it read as a number, not as somewhere to type.
	label := "Message"
	if len(targets) > 1 {
		label = "Message for all"
	}
	message := addTextArea(form, label, "", 4)
	own := make([]*tview.InputField, len(targets))
	if len(targets) > 1 {
		counts := make([]string, len(targets))
		for i, t := range targets {
			counts[i] = fmt.Sprintf("%s %d file%s", t.name, t.edits, plural(t.edits, "", "s"))
		}
		form.AddTextView("", strings.Join(counts, " · ")+"\nOr a message of its own for a repository:", 0, 2, true, false)
		for i, t := range targets {
			field := t.name + " message"
			form.AddInputField(field, "", 0, nil, nil)
			own[i] = form.GetFormItemByLabel(field).(*tview.InputField)
		}
	}
	commit := func() {
		shared := strings.TrimSpace(message.GetText())
		messages := make([]string, len(targets))
		for i, t := range targets {
			messages[i] = shared
			if own[i] != nil {
				if m := strings.TrimSpace(own[i].GetText()); m != "" {
					messages[i] = m
				}
			}
			if messages[i] == "" {
				a.flash("enter a message for " + t.name)
				return
			}
		}
		a.closeModal(pageForm)
		a.runTaskNoting("Committing "+r.Path, func(log func(string)) (string, error) {
			var done, failed []string
			for i, t := range targets {
				sha, err := a.newManager(t.row.Instance, t.row.Path, log).CommitAll(t.row.Dir, messages[i])
				switch {
				case err != nil:
					failed = append(failed, t.name+": "+firstLine(err.Error()))
				case sha != "":
					done = append(done, t.name+" "+sha)
					log(fmt.Sprintf("%s: committed %s", t.name, sha))
				}
			}
			if len(failed) > 0 {
				return "", fmt.Errorf("committed %d, not %d:\n%s", len(done), len(failed), strings.Join(failed, "\n"))
			}
			return "committed " + strings.Join(done, ", "), nil
		})
	}
	form.AddButton("Commit", commit)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	files := 0
	for _, t := range targets {
		files += t.edits
	}
	height := 10
	if len(targets) > 1 {
		height += 3 + 2*len(targets)
	}
	// A group with one repository changed says which one is committed.
	title := r.Path
	if r.grouped() && len(targets) == 1 {
		title += " · " + targets[0].name
	}
	a.showFormModalSized(fmt.Sprintf("Commit · %s · %d file(s)", title, files), form, 84, height)
}
