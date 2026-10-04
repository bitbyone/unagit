package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/gitx"
)

// The commit log, one list for a repository, a merge request and a worktree:
// newest first, the pane under it giving who made the commit under the
// cursor, when, and the rest of its message. Enter shows the commit in Hunk
// when the checkout has it, w opens it on the server, y copies its id.

// logCommit is one commit of a log, from git or from the forge.
type logCommit struct {
	gitx.LogEntry
	WebURL string
}

// historyLimit is how far back a log goes; it is for looking around, not
// for archaeology.
const historyLimit = 200

// subjectWidth is as much of a subject as a log row shows; the pane under the
// list has the rest of the message.
const subjectWidth = 52

// showCommitLog lists commits. dir is the checkout Enter shows them from, ""
// when there is none on disk.
func (a *App) showCommitLog(title string, commits []logCommit, dir string) {
	if len(commits) == 0 {
		a.note("no commits to show")
		return
	}
	// The subjects make a column, so the ages after them line up.
	subjects := make([]string, len(commits))
	width := 0
	for i, c := range commits {
		subject := c.Subject
		if c.Merge {
			subject = "⑂ " + subject
		}
		subjects[i] = trim(subject, subjectWidth)
		width = max(width, len([]rune(subjects[i])))
	}
	items := make([]pickItem, len(commits))
	for i, c := range commits {
		label := fmt.Sprintf("%s  %-*s", shortSHA(c.SHA), width, subjects[i])
		items[i] = pickItem{Label: esc(label), Sub: humanAge(c.When), About: commitAbout(c), Data: c}
	}
	show := func(it pickItem) {
		c := it.Data.(logCommit)
		bin, ok := a.hunkBinary()
		switch {
		case !ok:
			return
		case dir == "":
			a.flash("not cloned - Enter shows a commit once the repository is on disk")
		case !gitx.New("", nil).HasCommit(dir, c.SHA):
			a.flash(shortSHA(c.SHA) + " is not in the clone yet - pull it, or open the review, first")
		default:
			go a.runView(bin, diffView{dir: dir, args: []string{"show", c.SHA}})
		}
	}
	opts := pickerOptions{pack: true, explain: true, enterHint: "show in Hunk", keys: []pickKey{
		{keys: "w", hint: "browser", run: func(it pickItem) { a.openWeb(it.Data.(logCommit).WebURL) }},
		{keys: "y", hint: "copy id", run: func(it pickItem) { a.yank("commit id", it.Data.(logCommit).SHA) }},
	}}
	a.showPickerWith(title, items, opts, show)
}

// commitAbout is the pane under the log: author and time, then the body.
func commitAbout(c logCommit) string {
	about := c.Author + " · " + c.When.Format("2006-01-02 15:04")
	if body := strings.Join(strings.Fields(c.Body), " "); body != "" {
		about += " · " + body
	}
	return about
}

func shortSHA(sha string) string { return sha[:min(8, len(sha))] }

// repositoryLog is the log of a repository: of the branch out in the clone,
// or, before it is cloned, of the default branch on the server.
func (a *App) repositoryLog(pr forge.Project) {
	if a.diskOf(pr.Instance, pr.PathWithNamespace).Cloned {
		mgr := a.pathManager(pr.Instance, pr.PathWithNamespace)
		dir := mgr.ProjectDir(pr.PathWithNamespace)
		a.localLog(dir, "Commit log · "+pr.PathWithNamespace)
		return
	}
	client := a.client(pr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(pr.Instance))
		return
	}
	var commits []logCommit
	a.runTaskThen("Reading the commits of "+pr.PathWithNamespace, func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		listed, err := client.ProjectCommits(ctx, pr, pr.DefaultBranch, 100)
		commits = forgeLog(listed)
		return "", err
	}, func(string) {
		a.showCommitLog(fmt.Sprintf("Commit log · %s (%s on the server)", pr.PathWithNamespace, pr.DefaultBranch), commits, "")
	})
}

// mergeRequestLog is the commits of a merge request as the server has them,
// shown from whichever checkout of it is on disk.
func (a *App) mergeRequestLog(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
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
	var commits []logCommit
	a.runTaskThen(fmt.Sprintf("Reading the commits of %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		listed, _, err := client.MergeRequestCommits(ctx, mr, 0)
		commits = forgeLog(listed)
		return "", err
	}, func(string) {
		a.showCommitLog(fmt.Sprintf("Commit log · %s !%d", path, mr.IID), commits, dir)
	})
}

// worktreeLog is the log of the branch a worktree has out. A grouped
// worktree has one per repository; its view gives each block its own.
func (a *App) worktreeLog(r worktreeRow) {
	if r.grouped() {
		a.flash("a group has a log per repository - open its view with Enter and light one")
		return
	}
	a.localLog(r.Dir, "Commit log · "+r.Path+" ("+r.Branch+")")
}

// localLog reads a checkout's log off the event loop and lists it.
func (a *App) localLog(dir, title string) {
	git := gitx.New("", nil)
	go func() {
		entries, err := git.History(dir, "HEAD", historyLimit)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("reading the log: %v", err)
				return
			}
			commits := make([]logCommit, len(entries))
			for i, e := range entries {
				commits[i] = logCommit{LogEntry: e}
			}
			a.showCommitLog(title, commits, dir)
		})
	}()
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
