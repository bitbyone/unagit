package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/incomm"
)

// mrWorktrees are the two directories a merge request's Incomm comments can
// live in: the branch worktree and the review one.
func (a *App) mrWorktrees(mr forge.MergeRequest) []string {
	path := a.projectPathOfMR(mr)
	return []string{
		a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch),
		a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch),
	}
}

// localThreads reads the Incomm comments of a merge request's worktrees, or
// nothing when the integration is off.
func (a *App) localThreads(mr forge.MergeRequest) []incomm.Thread {
	if !a.cfg.Integrations.Incomm {
		return nil
	}
	return incomm.ThreadsOf(a.mrWorktrees(mr)...)
}

// maxListed keeps the confirmation on one screen; the rest is counted.
const maxListed = 12

// publishSummary is the body of the confirmation: how many comments go out and
// what each of them is.
func publishSummary(path string, mr forge.MergeRequest, steps []incomm.Step) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Publish %d comment%s to [::b]%s !%d[::-]?\n\n", len(steps), plural(len(steps), "", "s"), tview.Escape(path), mr.IID)
	for i, s := range steps {
		if i == maxListed {
			fmt.Fprintf(&b, "%s… and %d more%s\n", tag(colDim), len(steps)-maxListed, tagEnd)
			break
		}
		b.WriteString(tview.Escape(s.Summary()) + "\n")
	}
	b.WriteString("\nThey are posted with your token. Comments the agent wrote are marked as the agent's.")
	return b.String()
}

// publishMR posts the Incomm comments marked for the forge, after showing them.
// It only ever runs on request.
func (a *App) publishMR(mr forge.MergeRequest) {
	if !a.cfg.Integrations.Incomm {
		a.flash("Incomm is off: turn it on in Settings > Integrations to publish its comments")
		return
	}
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in Settings [S]", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	steps := incomm.Plan(a.localThreads(mr))
	if len(steps) == 0 {
		a.flash(fmt.Sprintf("nothing to publish on !%d: mark a comment external in the editor first", mr.IID))
		return
	}
	a.confirmWith("Publish comments", publishSummary(path, mr, steps), "Publish", nil, func() {
		a.runTask(fmt.Sprintf("Publishing %s !%d", path, mr.IID), func(log func(string)) (string, error) {
			// What went out stays recorded even when a later post fails, so the
			// lists are refreshed either way.
			defer a.tv.QueueUpdateDraw(func() {
				a.refreshDisk()
				a.mrsPane.reload()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if _, err := incomm.CheckCLI(ctx); err != nil {
				return "", err
			}
			done, err := incomm.Publisher{Poster: client, MR: mr, Log: log}.Publish(ctx, steps)
			log(fmt.Sprintf("Published %d of %d.", done, len(steps)))
			return "", err
		})
	})
}

// renderLocalThreads lays out the Incomm comments that are not on the forge, or
// that have replies waiting for it, marking what has not been published.
func renderLocalThreads(threads []incomm.Thread) string {
	var b strings.Builder
	for _, t := range threads {
		if t.Root.OnForge() && !t.NeedsPublishing() {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(tag(colAccent) + "Local comments" + tagEnd + tag(colDim) + " · from Incomm" + tagEnd + "\n\n")
		} else {
			b.WriteString("\n" + tag(colDim) + strings.Repeat("┈", 40) + tagEnd + "\n\n")
		}
		b.WriteString(tag(colDim) + tview.Escape(fmt.Sprintf("%s:%d", t.File, t.Line)) + tagEnd + "\n")
		writeLocalComment(&b, t.Root, "")
		for _, r := range t.Replies {
			b.WriteString("\n")
			writeLocalComment(&b, r, "  ")
		}
	}
	return b.String()
}

func writeLocalComment(b *strings.Builder, c incomm.Comment, indent string) {
	name := c.Title
	switch {
	case c.Author == "agent" && name != "":
		name = "Agent (" + name + ")"
	case c.Author == "agent":
		name = "Agent"
	case name == "":
		name = "you"
	}
	head := indent
	if indent != "" {
		head += tag(colDim) + "↳ " + tagEnd
	}
	head += tag(colAccent) + tview.Escape(name) + tagEnd
	switch {
	case c.Pending():
		head += tag(colWarn) + " · not published" + tagEnd
	case c.OnForge():
		head += tag(colDim) + " · published" + tagEnd
	default:
		head += tag(colDim) + " · local" + tagEnd
	}
	b.WriteString(head + "\n")
	if body := renderMarkdown(c.Content, indent+"  "); body != "" {
		b.WriteString(body + "\n")
	}
}
