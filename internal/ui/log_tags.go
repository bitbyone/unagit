package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/watch"
)

// Tags are made, pushed and deleted from a commit of a log read from the
// disk: t puts one on the commit, T lists the ones it has. A tag is never
// moved - one of the same name is refused - since a tag others have
// fetched, or a release was built from, must keep meaning one commit.

const (
	labelTagName    = "Name"
	labelTagMessage = "Message"
	labelTagPush    = "Push to origin"
)

// hasTag reports whether a commit of a log has a tag on it.
func hasTag(c logCommit) bool {
	for _, ref := range c.Refs {
		if strings.HasPrefix(ref, "tag: ") {
			return true
		}
	}
	return false
}

// newTag asks for a tag on a commit: a name, a message that makes it
// annotated, and whether origin gets it at once. Cancel goes back.
func (a *App) newTag(place logPlace, c logCommit, back func()) {
	if why := a.onDisk(place, c); why != "" {
		a.flash(why)
		return
	}
	git := gitx.New("", nil)
	form := tview.NewForm()
	styleForm(form)
	form.AddInputField(labelTagName, "", 0, nil, nil)
	message := setPlaceholder(addTextArea(form, labelTagMessage, "", 3), "empty: a lightweight tag")
	push := addCheckbox(form, labelTagPush, true)
	form.AddButton("Create", func() {
		name := strings.TrimSpace(form.GetFormItemByLabel(labelTagName).(*tview.InputField).GetText())
		if why := git.CheckTagName(place.dir, name); why != "" {
			a.flash(why)
			return
		}
		a.closeModal(pageForm)
		a.createTag(place, c, name, message.GetText(), push.IsChecked())
	})
	form.AddButton("Cancel", func() {
		a.closeModal(pageForm)
		back()
	})
	a.showFormModalSized("New Tag at "+shortSHA(c.SHA), form, 72, 12)
}

// createTag makes the tag, pushes it when asked, and lists the log again
// with the tag on its commit.
func (a *App) createTag(place logPlace, c logCommit, name, message string, push bool) {
	pr := place.project
	a.runTaskThen("Tagging "+shortSHA(c.SHA)+" "+name, func(log func(string)) (string, error) {
		git := a.newManager(pr.Instance, pr.PathWithNamespace, log).Git()
		if err := git.CreateTag(place.dir, name, c.SHA, message); err != nil {
			return "", err
		}
		if push {
			if err := git.PushTag(place.dir, name); err != nil {
				return "", fmt.Errorf("%s is made here, but origin did not take it - push it again with T: %w", name, err)
			}
		}
		return "", nil
	}, func(string) {
		done := fmt.Sprintf("tagged %s %s", shortSHA(c.SHA), name)
		if push {
			done += ", pushed"
		}
		place.reload(c.SHA, done)
	})
}

// tagRow is a tag in the list of a commit's tags, and what origin has of
// it: onOrigin is unknown when origin could not be reached.
type tagRow struct {
	tag      gitx.Tag
	onOrigin bool
	known    bool
}

// showTags lists the tags on a commit, origin asked first which of them it
// has, so that only what can be done is offered.
func (a *App) showTags(place logPlace, c logCommit) {
	if !hasTag(c) {
		a.flash(shortSHA(c.SHA) + " has no tag - t puts one on it")
		return
	}
	pr := place.project
	var rows []tagRow
	a.loadThen("Reading the tags of "+shortSHA(c.SHA), func(step func(string)) (string, error) {
		git := a.newManager(pr.Instance, pr.PathWithNamespace, nil).Git()
		tags, err := git.TagsAt(place.dir, c.SHA)
		if err != nil {
			return "", err
		}
		step("asking origin")
		// Offline, the tags are still listed; every way is offered and git
		// says what it makes of it.
		remote, remoteErr := git.RemoteTags(place.dir)
		for _, t := range tags {
			sha, ok := remote[t.Name]
			rows = append(rows, tagRow{tag: t, onOrigin: ok && sha == c.SHA, known: remoteErr == nil})
		}
		return "", nil
	}, func(string) { a.listTags(place, c, rows) })
}

// listTags is the list of showTags.
func (a *App) listTags(place logPlace, c logCommit, rows []tagRow) {
	items := make([]pickItem, len(rows))
	width := 0
	for _, r := range rows {
		width = max(width, len([]rune(r.tag.Name)))
	}
	for i, r := range rows {
		kind := "lightweight"
		about := r.tag.Name + " is only a name for " + shortSHA(c.SHA) + "."
		if r.tag.Annotated {
			kind = "annotated"
			about = r.tag.Subject + "\n" + r.tag.Tagger + " · " + r.tag.When.Format("2006-01-02 15:04")
		}
		where := "origin not reached"
		switch {
		case r.known && r.onOrigin:
			where = "local · origin"
		case r.known:
			where = "local only"
		}
		items[i] = pickItem{Label: fmt.Sprintf("%-*s", width, r.tag.Name), Sub: fmt.Sprintf("%-11s  %s", kind, where),
			About: about, Data: r}
	}
	at := func(it pickItem) tagRow { return it.Data.(tagRow) }
	opts := pickerOptions{pack: true, explain: true, keys: []pickKey{
		{keys: "p", hint: "push", name: "Push to Origin", about: "Send the tag to origin; one origin has on another commit is refused, never replaced.",
			when: func(it pickItem) bool { return !at(it).onOrigin },
			run:  func(it pickItem) { a.pushTag(place, c, at(it)) }},
		{keys: "d", hint: "delete here", name: "Delete Locally…", about: "Delete the tag here; origin keeps it, and a fetch brings it back.",
			run: func(it pickItem) { a.deleteTag(place, c, at(it), false) }},
		{keys: "D", hint: "everywhere", name: "Delete Everywhere…", about: "Delete the tag on origin and here; whoever fetched it keeps their copy.",
			when: func(it pickItem) bool { return at(it).onOrigin || !at(it).known },
			run:  func(it pickItem) { a.deleteTag(place, c, at(it), true) }},
	}}
	a.showPickerWith("Tags of "+shortSHA(c.SHA), items, opts, nil)
}

// pushTag sends a tag to origin and lists the log again.
func (a *App) pushTag(place logPlace, c logCommit, r tagRow) {
	if r.known && r.onOrigin {
		a.note(r.tag.Name + " is on origin already")
		return
	}
	pr := place.project
	a.runTaskThen("Pushing "+r.tag.Name, func(log func(string)) (string, error) {
		return "", a.newManager(pr.Instance, pr.PathWithNamespace, log).Git().PushTag(place.dir, r.tag.Name)
	}, func(string) { place.reload(c.SHA, "pushed "+r.tag.Name) })
}

// deleteTag deletes a tag here, or everywhere - origin first, so that a
// refusal there leaves both as they were - after asking. It is written in
// the Activity log, with the commit it was on, to be made again from.
func (a *App) deleteTag(place logPlace, c logCommit, r tagRow, everywhere bool) {
	name := r.tag.Name
	body := fmt.Sprintf("Delete the tag [::b]%s[::-] here?\n\nIt is on %s. Origin keeps its own, and a fetch brings it back.", esc(name), shortSHA(c.SHA))
	var warnings []string
	if everywhere {
		body = fmt.Sprintf("Delete the tag [::b]%s[::-] on origin and here?\n\nIt is on %s.", esc(name), shortSHA(c.SHA))
		warnings = []string{"whoever fetched it keeps their copy", "a release or a pipeline may have been made from it"}
	} else if r.known && !r.onOrigin {
		body = fmt.Sprintf("Delete the tag [::b]%s[::-]?\n\nIt is on %s, and origin does not have it.", esc(name), shortSHA(c.SHA))
	}
	pr := place.project
	a.confirm("Delete Tag", body, warnings, func() {
		a.runTaskThen("Deleting "+name, func(log func(string)) (string, error) {
			git := a.newManager(pr.Instance, pr.PathWithNamespace, log).Git()
			if everywhere {
				if err := git.DeleteRemoteTag(place.dir, name); err != nil {
					return "", err
				}
				log("deleted on origin")
			}
			if err := git.DeleteTag(place.dir, name); err != nil {
				return "", err
			}
			log("deleted here")
			return "", nil
		}, func(string) {
			done := "deleted " + name
			if everywhere {
				done += " on origin and here"
			}
			a.logTagDeleted(pr, place.dir, name, c.SHA, everywhere)
			place.reload(c.SHA, done)
		})
	})
}

// logTagDeleted puts a deleted tag into the Activity log as local history,
// with the commit it was on.
func (a *App) logTagDeleted(pr forge.Project, dir, name, sha string, everywhere bool) {
	if a.watchStore == nil {
		return
	}
	line := fmt.Sprintf("deleted %s, which was on %s", name, shortSHA(sha))
	if everywhere {
		line += ", on origin too"
	}
	e := watch.Event{
		Key:     localKey(pr),
		What:    pr.PathWithNamespace + " " + name,
		Heading: "Tag deleted",
		Line:    line,
		Project: pr.PathWithNamespace,
		Level:   watch.Info,
		At:      time.Now(),
		Local:   true,
		Dir:     dir,
	}
	store := a.watchStore
	go func() { _ = store.AppendHistory(false, e) }()
}
