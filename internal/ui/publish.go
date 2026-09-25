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

// publishSummary is the body of the confirmation: how many comments go out, how
// many threads are resolved, and what each of them is.
func publishSummary(path string, mr forge.MergeRequest, steps []incomm.Step) string {
	posts, resolves := 0, 0
	for _, s := range steps {
		if s.Resolve {
			resolves++
		} else {
			posts++
		}
	}
	what := ""
	switch {
	case posts > 0 && resolves > 0:
		what = fmt.Sprintf("Publish %d comment%s and resolve %d thread%s", posts, plural(posts, "", "s"), resolves, plural(resolves, "", "s"))
	case posts > 0:
		what = fmt.Sprintf("Publish %d comment%s", posts, plural(posts, "", "s"))
	default:
		what = fmt.Sprintf("Resolve %d thread%s", resolves, plural(resolves, "", "s"))
	}
	var b strings.Builder
	preposition := "to"
	if posts == 0 {
		preposition = "on"
	}
	fmt.Fprintf(&b, "%s %s [::b]%s !%d[::-]?\n\n", what, preposition, tview.Escape(path), mr.IID)
	for i, s := range steps {
		if i == maxListed {
			fmt.Fprintf(&b, "%s… and %d more%s\n", tag(colDim), len(steps)-maxListed, tagEnd)
			break
		}
		b.WriteString(tview.Escape(s.Summary()) + "\n")
	}
	if posts > 0 {
		b.WriteString("\nThey are posted with your token. Comments the agent wrote are marked as the agent's.")
	}
	if resolves > 0 {
		b.WriteString("\nResolving closes the conversation on the forge for everyone.")
	}
	return b.String()
}

// publishMR posts the Incomm comments marked for the forge, after showing them.
// It only ever runs on request.
//
// The file and line that go out are the ones Incomm has stored, which are where
// the code is because worktrees are re-anchored when they are updated; a comment
// whose code is gone shows up as orphaned. The forge is asked which threads it already holds resolved when a thread that
// is resolved here is on it; if that fails, resolving is left out and the rest
// is offered as usual.
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
	dirs := a.mrWorktrees(mr)
	a.runTask(fmt.Sprintf("Preparing %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		threads := incomm.ThreadsOf(dirs...)
		state := map[string]bool{}
		if incomm.NeedsForgeState(threads) {
			log("Asking the forge which threads are resolved ...")
			notes, err := client.MergeRequestNotes(ctx, mr, 0)
			if err != nil {
				log("! could not read the merge request from the forge: " + err.Error())
				log("  resolving threads is left out")
				state = nil
			} else {
				state = incomm.ForgeResolved(notes)
			}
		}
		steps := incomm.PlanResolving(threads, state)
		if len(steps) == 0 {
			return "", fmt.Errorf("nothing to publish on !%d: mark a comment external in the editor first", mr.IID)
		}
		// The task's own window closes right after this, so the confirmation is
		// put up on top of it and outlives it.
		a.tv.QueueUpdateDraw(func() {
			a.confirmWith("Publish comments", publishSummary(path, mr, steps), "Publish", nil, func() {
				a.runPublish(mr, path, client, dirs, steps, state)
			})
		})
		return "", nil
	})
}

// runPublish posts what was confirmed. The plan is drawn again from the comments
// as they are now, because time has passed since the confirmation; only what was
// agreed to goes out. The lines are the ones Incomm has stored: re-anchoring
// happens when a worktree is updated (Ctrl-O, Ctrl-R), and the editors keep them
// current while a file is open.
func (a *App) runPublish(mr forge.MergeRequest, path string, client forge.Provider, dirs []string, confirmed []incomm.Step, state map[string]bool) {
	a.runTask(fmt.Sprintf("Publishing %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		// What went out stays recorded even when a later post fails, so the
		// lists are refreshed either way.
		defer a.tv.QueueUpdateDraw(func() {
			a.refreshDisk()
			a.mrsPane.reload()
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		steps := incomm.Confirmed(incomm.PlanResolving(incomm.ThreadsOf(dirs...), state), confirmed)
		if gone := len(confirmed) - len(steps); gone > 0 {
			log(fmt.Sprintf("%d of the confirmed items are no longer pending and are left out.", gone))
		}
		done, err := incomm.Publisher{Poster: client, MR: mr, Log: log}.Publish(ctx, steps)
		log(fmt.Sprintf("Done %d of %d.", done, len(steps)))
		return "", err
	})
}

// renderLocalThreads lays out the Incomm comments that are not on the forge, or
// that have replies waiting for it, marking what has not been published.
func renderLocalThreads(threads []incomm.Thread, width int) string {
	var parts []string
	for _, t := range threads {
		if t.Root.OnForge() && !t.NeedsPublishing() {
			continue
		}
		bubbles := []bubble{localBubble(t.Root, 0)}
		for _, r := range t.Replies {
			bubbles = append(bubbles, localBubble(r, 2))
		}
		parts = append(parts, tag(colDim)+tview.Escape(fmt.Sprintf("%s:%d", t.File, t.Line))+tagEnd+"\n"+
			renderBubbles(bubbles, width))
	}
	if len(parts) == 0 {
		return ""
	}
	return tag(colAccent) + "Local comments" + tagEnd + tag(colDim) + " · from Incomm" + tagEnd + "\n\n" +
		strings.Join(parts, "\n\n")
}

// localBubble is one Incomm comment as a box: green for the agent and blue for a
// person, as Incomm draws them, with whether it has reached the forge.
func localBubble(c incomm.Comment, indent int) bubble {
	name, colour := c.Title, colAccent
	switch {
	case c.Author == "agent" && name != "":
		name, colour = "Agent ("+name+")", colOn
	case c.Author == "agent":
		name, colour = "Agent", colOn
	case name == "":
		name = "you"
	}
	var meta string
	switch {
	case c.Pending():
		meta = tag(colWarn) + "not published" + tagEnd
	case c.OnForge():
		meta = tag(colDim) + "published" + tagEnd
	default:
		meta = tag(colDim) + "local" + tagEnd
	}
	return bubble{Name: name, Colour: colour, Meta: meta, Body: c.Content, Indent: indent}
}
