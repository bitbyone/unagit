package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// A commit is made as IntelliJ makes one: of the versioned files, as they
// are on disk, staged or not; a file git does not track yet stays out until
// it is chosen. One dialog makes it wherever it is asked for - a clone, a
// worktree, every repository of a grouped one - and pushes it on request.
// One message serves all the repositories; each can be given its own.

// commitTarget is one working tree about to be committed.
type commitTarget struct {
	instance, path, dir string
	name                string // its folder in a group, or its repository
	edits               int    // its versioned files with changes
	// changes are the files picked in the Changes dialog; nil commits
	// every versioned file.
	changes []gitx.Change
}

// commitWorktree commits a worktree, or every repository of a grouped one.
func (a *App) commitWorktree(r worktreeRow) {
	members := []worktreeRow{r}
	if r.grouped() {
		members = r.Members
	}
	targets := make([]commitTarget, len(members))
	for i, m := range members {
		name := m.Path
		if r.grouped() {
			name = filepath.Base(m.Dir)
		}
		targets[i] = commitTarget{instance: m.Instance, path: m.Path, dir: m.Dir, name: name}
	}
	a.startCommit(r.Path, r.grouped(), targets)
}

// commitProject commits the clone of a repository.
func (a *App) commitProject(pr forge.Project) {
	if !a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		a.flash(pr.PathWithNamespace + " is not cloned - there is nothing of yours to commit")
		return
	}
	a.startCommit(pr.PathWithNamespace, false, []commitTarget{{instance: pr.Instance, path: pr.PathWithNamespace,
		dir: a.projectDir(pr.Instance, pr.PathWithNamespace), name: pr.PathWithNamespace}})
}

// startCommit counts each working tree afresh - what a list last read may
// be a moment old - and asks for the message of those with something
// versioned to commit.
func (a *App) startCommit(title string, grouped bool, candidates []commitTarget) {
	gits := make([]*gitx.Git, len(candidates))
	for i, c := range candidates {
		gits[i] = a.pathManager(c.instance, c.path).Git()
	}
	go func() {
		var targets []commitTarget
		unversioned := 0
		for i, c := range candidates {
			v, u := editCounts(gits[i], c.dir)
			unversioned += u
			if v > 0 {
				c.edits = v
				targets = append(targets, c)
			}
		}
		a.tv.QueueUpdateDraw(func() {
			switch {
			case len(targets) == 0 && unversioned > 0:
				a.flash(fmt.Sprintf("nothing versioned to commit in %s: its %d unversioned file(s) are not committed until chosen", title, unversioned))
			case len(targets) == 0:
				a.flash("nothing to commit in " + title)
			default:
				a.showCommitForm(title, grouped, targets)
			}
		})
	}()
}

// commitPush is what follows a commit: nothing, a push, or a force push.
type commitPush int

const (
	commitOnly commitPush = iota
	commitAndPush
	commitAndForce
)

// showCommitForm asks for the message, and lets a repository have its own.
func (a *App) showCommitForm(title string, grouped bool, targets []commitTarget) {
	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	// With several repositories the shared message says it is shared, and each
	// repository has a field of its own under it, named as one: a bare name
	// with a count beside it read as a number, not as somewhere to type.
	// A group with one repository changed names it: the message is that
	// repository's, not one for all.
	label := "Message"
	switch {
	case len(targets) > 1:
		label = "Message for all"
	case grouped:
		label = targets[0].name + " message"
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
	files := 0
	for _, t := range targets {
		files += t.edits
	}
	// A group with one repository changed says which one is committed.
	if grouped && len(targets) == 1 {
		title += " · " + targets[0].name
	}
	messages := func() ([]string, bool) {
		shared := strings.TrimSpace(message.GetText())
		out := make([]string, len(targets))
		for i, t := range targets {
			out[i] = shared
			if own[i] != nil {
				if m := strings.TrimSpace(own[i].GetText()); m != "" {
					out[i] = m
				}
			}
			if out[i] == "" {
				a.flash("enter a message for " + t.name)
				return nil, false
			}
		}
		return out, true
	}
	commit := func(push commitPush) func() {
		return func() {
			texts, ok := messages()
			if !ok {
				return
			}
			run := func() {
				a.closeModal(pageForm)
				a.runCommit(title, targets, texts, push)
			}
			// The push takes the branch whole: the commits made before and
			// not pushed yet go with the new one.
			with := ""
			if earlier := a.unpushedOf(targets); earlier > 0 {
				with = fmt.Sprintf(", with the %d earlier commit(s) not pushed yet", earlier)
			}
			switch push {
			case commitAndPush:
				a.confirmWith("Commit and push",
					fmt.Sprintf("Commit %d file(s) of [::b]%s[::-] and push to origin%s?", files, esc(title), with), "Push", nil, run)
			case commitAndForce:
				a.confirmWith("Commit and force push",
					fmt.Sprintf("Commit %d file(s) of [::b]%s[::-] and force push to origin%s?\n\n"+
						"Origin's copy of the branch is replaced by yours. Only what was last fetched of it is replaced: "+
						"if anyone pushed since, git refuses.", files, esc(title), with), "Force push", nil, run)
			default:
				run()
			}
		}
	}
	form.AddButton("Commit", commit(commitOnly))
	form.AddButton("Commit and Push", commit(commitAndPush))
	form.AddButton("Commit and Force Push", commit(commitAndForce))
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	height := 10
	if len(targets) > 1 {
		height += 3 + 2*len(targets)
	}
	a.showFormModalSized(fmt.Sprintf("Commit · %s · %d file(s)", title, files), form, 84, height)
}

// unpushedOf counts the commits the working trees have and origin lacks,
// as the lists last read them.
func (a *App) unpushedOf(targets []commitTarget) int {
	n := 0
	for _, t := range targets {
		if st, ok := a.wtRemote[t.dir]; ok {
			n += st.Upstream.Ahead
		} else if t.dir == a.projectDir(t.instance, t.path) {
			n += a.repoSync[projectKey{t.instance, t.path}].Upstream.Ahead
		}
	}
	return n
}

// runCommit commits each working tree under its message, then pushes those
// committed when asked, all under one log; the lists read what changed.
func (a *App) runCommit(title string, targets []commitTarget, messages []string, push commitPush) {
	a.runTaskThen("Committing "+title, func(log func(string)) (string, error) {
		var done, failed []string
		var committed []commitTarget
		for i, t := range targets {
			git := a.newManager(t.instance, t.path, log).Git()
			var sha string
			var err error
			if t.changes != nil {
				sha, err = git.CommitPaths(t.dir, messages[i], t.changes)
			} else {
				sha, err = git.CommitVersioned(t.dir, messages[i])
			}
			switch {
			case err != nil:
				failed = append(failed, t.name+": "+firstLine(err.Error()))
			case sha != "":
				done = append(done, t.name+" "+sha)
				committed = append(committed, t)
				log(fmt.Sprintf("%s: committed %s", t.name, sha))
			}
		}
		if len(failed) > 0 {
			return "", fmt.Errorf("committed %d, not %d:\n%s", len(done), len(failed), strings.Join(failed, "\n"))
		}
		said := "committed " + strings.Join(done, ", ")
		if push == commitOnly {
			return said, nil
		}
		for _, t := range committed {
			branch, err := a.newManager(t.instance, t.path, log).Git().PushHead(t.dir, push == commitAndForce)
			if err != nil {
				return "", fmt.Errorf("%s, but %s was not pushed: %w", said, t.name, err)
			}
			log(fmt.Sprintf("%s: pushed %s", t.name, branch))
		}
		return said + " and pushed", nil
	}, func(said string) {
		a.afterGitChange()
		a.done(said)
	})
}

// afterGitChange reads the working trees again after a commit or a push,
// so the lists show where they stand now.
func (a *App) afterGitChange() {
	a.reloadChanges()
	a.refreshDisk()
	if a.projectsPane != nil && a.projectsPane.reload != nil {
		a.projectsPane.reload()
	}
}

// editCounts reads a working tree's versioned files with changes and its
// unversioned ones, none when git cannot say.
func editCounts(git *gitx.Git, dir string) (versioned, unversioned int) {
	versioned, unversioned = git.EditCounts(dir)
	return max(versioned, 0), max(unversioned, 0)
}

// editsText is what an EDITS column says: the versioned files not
// committed, then after a slash the unversioned ones - "12/3", "12", "/3" -
// and nothing when there are neither.
func editsText(versioned, unversioned int) string {
	text := ""
	if versioned > 0 {
		text = fmt.Sprint(versioned)
	}
	if unversioned > 0 {
		text += fmt.Sprintf("/%d", unversioned)
	}
	return text
}

// editsMarkup is editsText with each count in its colour, the versioned in
// changed's.
func editsMarkup(versioned, unversioned int, changed ...tcell.Color) (plain, markup string) {
	ink := role("files.changed")
	if len(changed) > 0 {
		ink = changed[0]
	}
	if versioned > 0 {
		markup = tag(ink) + fmt.Sprint(versioned) + tagEnd
	}
	if unversioned > 0 {
		markup += tag(role("files.unversioned")) + fmt.Sprintf("/%d", unversioned) + tagEnd
	}
	return editsText(versioned, unversioned), markup
}

// editsField is a list's EDITS cell, right-aligned in width: what
// editsMarkup says, the versioned count in the list's own colour.
func editsField(width int, changed tcell.Color) func(versioned, unversioned int) field {
	return func(versioned, unversioned int) field {
		plain, markup := editsMarkup(versioned, unversioned, changed)
		if width <= 0 || plain == "" {
			return field{width: width}
		}
		return field{raw: strings.Repeat(" ", max(width-len(plain), 0)) + markup}
	}
}
