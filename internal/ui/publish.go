package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/draft"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/incomm"
)

// mrPlaces are where a merge request's Incomm comments can live: its branch
// worktree, its review worktree, and every grouped worktree holding its
// branch, where they are the comments on that repository's folder.
func (a *App) mrPlaces(mr forge.MergeRequest) []incomm.Place {
	path := a.projectPathOfMR(mr)
	places := []incomm.Place{
		{Dir: a.mrDir(mr.Instance, path, mr.IID, mr.SourceBranch)},
		{Dir: a.reviewDir(mr.Instance, path, mr.IID, mr.SourceBranch)},
	}
	for _, r := range a.worktrees {
		for _, m := range r.Members {
			if m.Instance == mr.Instance && m.Path == path && m.Branch == mr.SourceBranch {
				places = append(places, incomm.Place{Dir: r.Dir, Prefix: filepath.Base(m.Dir) + "/"})
			}
		}
	}
	return places
}

// localThreads reads the Incomm comments of a merge request, or nothing when
// the integration is off.
func (a *App) localThreads(mr forge.MergeRequest) []incomm.Thread {
	if !a.cfg.Integrations.Incomm {
		return nil
	}
	return incomm.ThreadsAt(a.mrPlaces(mr)...)
}

// maxListed keeps the confirmation on one screen; the rest is counted.
const maxListed = 12

// publishSummary is the body of the confirmation: how many comments go out, how
// many threads are resolved, and what each of them is.
func publishSummary(path string, mr forge.MergeRequest, drafts []draft.Draft, steps []incomm.Step) string {
	posts, resolves := len(drafts), 0
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
	lines := make([]string, 0, len(drafts)+len(steps))
	for _, d := range drafts {
		lines = append(lines, fmt.Sprintf("%s:%d  comment saved in unagit: %s", d.Path, d.Line, trunc(strings.Join(strings.Fields(d.Body), " "), 60)))
	}
	for _, s := range steps {
		lines = append(lines, s.Summary())
	}
	for i, line := range lines {
		if i == maxListed {
			fmt.Fprintf(&b, "%s… and %d more%s\n", tag(colDim), len(lines)-maxListed, tagEnd)
			break
		}
		b.WriteString(tview.Escape(line) + "\n")
	}
	if posts > 0 {
		b.WriteString("\nThey are posted with your token. Comments the agent wrote are marked as the agent's.")
	}
	if resolves > 0 {
		b.WriteString("\nResolving closes the conversation on the forge for everyone.")
	}
	return b.String()
}

// publishMR posts the comments saved in unagit and the Incomm comments
// marked for the forge, after showing them. It only ever runs on request.
//
// The file and line that go out are the ones Incomm has stored, which are where
// the code is because worktrees are re-anchored when they are updated; a comment
// whose code is gone shows up as orphaned. The forge is asked which threads it already holds resolved when a thread that
// is resolved here is on it; if that fails, resolving is left out and the rest
// is offered as usual.
func (a *App) publishMR(mr forge.MergeRequest) {
	drafts, err := a.draftStore().List(a.draftKey(mr))
	if err != nil {
		a.errorf("%v", err)
		return
	}
	if !a.cfg.Integrations.Incomm && len(drafts) == 0 {
		a.flash("nothing saved to publish, and Incomm is off: turn it on in Settings > Integrations to publish its comments")
		return
	}
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in "+settingsTab, a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	var places []incomm.Place
	if a.cfg.Integrations.Incomm {
		places = a.mrPlaces(mr)
	}
	a.load(fmt.Sprintf("Preparing %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		threads := incomm.ThreadsAt(places...)
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
		if len(steps) == 0 && len(drafts) == 0 {
			return "", fmt.Errorf("nothing to publish on !%d: save a comment, or mark one external in the editor first", mr.IID)
		}
		// The task's own window closes right after this, so the confirmation is
		// put up on top of it and outlives it.
		a.tv.QueueUpdateDraw(func() {
			a.confirmWith("Publish comments", publishSummary(path, mr, drafts, steps), "Publish", nil, func() {
				a.runPublish(mr, path, client, places, drafts, steps, state)
			})
		})
		return "", nil
	})
}

// runPublish posts what was confirmed. The plan is drawn again from the comments
// as they are now, because time has passed since the confirmation; only what was
// agreed to goes out. The lines are the ones Incomm has stored: re-anchoring
// happens when a worktree is updated (o, Ctrl-R), and the editors keep them
// current while a file is open.
func (a *App) runPublish(mr forge.MergeRequest, path string, client forge.Provider, places []incomm.Place, drafts []draft.Draft, confirmed []incomm.Step, state map[string]bool) {
	a.runTask(fmt.Sprintf("Publishing %s !%d", path, mr.IID), func(log func(string)) (string, error) {
		// What went out stays recorded even when a later post fails, so the
		// lists are refreshed either way.
		defer a.tv.QueueUpdateDraw(func() {
			a.refreshDisk()
			a.mrsPane.reload()
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// The saved comments go first, each taken out of unagit once it is
		// on the forge, so a failure halfway leaves only the rest saved.
		store, key := a.draftStore(), a.draftKey(mr)
		for i, d := range drafts {
			if _, err := client.CreateDiscussion(ctx, mr, d.Path, d.Line, d.Body); err != nil {
				log(fmt.Sprintf("Done %d of %d.", i, len(drafts)+len(confirmed)))
				return "", err
			}
			if err := store.Remove(key, d.ID); err != nil {
				return "", err
			}
			log(fmt.Sprintf("Published %s:%d", d.Path, d.Line))
		}
		if len(confirmed) == 0 {
			return "", nil
		}
		steps := incomm.Confirmed(incomm.PlanResolving(incomm.ThreadsAt(places...), state), confirmed)
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

// localBubble is one Incomm comment as a card: green for the agent and blue for a
// person, as Incomm draws them, with whether it has reached the forge.
func localBubble(c incomm.Comment, indent int) bubble {
	name, colour, icon := c.Title, colAccent, ""
	switch {
	case c.Author == "agent" && name != "":
		name, colour, icon = "Agent ("+name+")", colOn, glyphAgent
	case c.Author == "agent":
		name, colour, icon = "Agent", colOn, glyphAgent
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
	return bubble{Name: name, Icon: icon, Colour: colour, Meta: meta, Body: c.Content, Indent: indent}
}

// syncWorktreeComments brings the comments of the merge requests open from the
// worktrees into their Incomm stores - from every repository of a grouped one
// into the group's store, each on its repository's folder - so that whoever
// works there reads them beside the code. It runs in the background on r;
// opening a worktree never waits for it, since these comments are on one's own
// work and come second to it. Running it again adds only what is new.
func (a *App) syncWorktreeComments(rows []worktreeRow, then func()) {
	type source struct {
		mr     forge.MergeRequest
		client forge.Provider
		place  incomm.Place
		author string
	}
	var sources []source
	var prepare []string
	for _, r := range rows {
		members := []worktreeRow{r}
		if r.grouped() {
			members = r.Members
		}
		for _, m := range members {
			mr, ok := a.openMRFor(m)
			client := a.client(m.Instance)
			if !ok || client == nil {
				continue
			}
			place := incomm.Place{Dir: r.Dir}
			if r.grouped() {
				place.Prefix = filepath.Base(m.Dir) + "/"
			}
			sources = append(sources, source{mr: mr, client: client, place: place, author: a.me[m.Instance]})
			prepare = append(prepare, r.Dir)
		}
	}
	if len(sources) == 0 {
		then()
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var failed []string
		for _, dir := range prepare {
			_ = incomm.Prepare(dir)
		}
		for _, s := range sources {
			notes, err := s.client.MergeRequestNotes(ctx, s.mr, 0)
			if err == nil {
				err = incomm.ImportAt(ctx, s.place, s.mr, notes, nil)
			}
			if err == nil {
				a.moveDrafts(ctx, s.mr, s.place, s.author, nil)
			}
			if err != nil {
				failed = append(failed, fmt.Sprintf("!%d: %s", s.mr.IID, firstLine(err.Error())))
			}
		}
		a.tv.QueueUpdateDraw(func() {
			if len(failed) > 0 {
				a.flash("comments not synced from " + strings.Join(failed, "; "))
			} else {
				a.done(fmt.Sprintf("synced the comments of %d merge request(s)", len(sources)))
			}
			then()
		})
	}()
}
