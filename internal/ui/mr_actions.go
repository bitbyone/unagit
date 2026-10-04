package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/tobola/unagit/internal/forge"
)

// The end of a review: merging, a draft made ready or back, closing, and who
// is asked to review. Each is one request to the server, after which the row
// is asked about again so the list shows what the server now has.

const labelWhenPipeline = "Wait for the pipeline"

// mergeFormWidth is the merge dialog's width, frame and padding included.
const mergeFormWidth = 72

// pipelineUnderway reports whether a pipeline status is one still going.
func pipelineUnderway(status string) bool {
	return ciStateOf(status) == ciRunning
}

// mergeMR asks how to merge, then merges. What stands in the way - a draft, a
// pipeline, approvals, threads - is listed first; the server has the last
// word, and its refusal is shown as it gives it.
func (a *App) mergeMR(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	underway := pipelineUnderway(mr.Pipeline)

	lines := []string{
		tview.Escape(mr.Title),
		tag(colMuted) + tview.Escape(mr.SourceBranch) + " → " + tview.Escape(mr.TargetBranch) + tagEnd,
	}
	for _, w := range mergeWarnings(mr, underway) {
		lines = append(lines, tag(colWarn)+"! "+tagEnd+tview.Escape(w))
	}

	// The text sits in the field column, after the widest label; it is given
	// the rows it wraps to, or tview shows its end and the title is lost.
	labelWidth := len(labelDeleteBranch)
	if underway {
		labelWidth = max(labelWidth, len(labelWhenPipeline))
	}
	rows := 0
	for _, line := range lines {
		rows += max(1, len(tview.WordWrap(line, mergeFormWidth-8-labelWidth)))
	}

	form := tview.NewForm()
	styleForm(form)
	form.SetItemPadding(1)
	form.AddTextView("", strings.Join(lines, "\n"), 0, rows, true, false)
	addCheckbox(form, labelSquash, false)
	addCheckbox(form, labelDeleteBranch, true)
	if underway {
		addCheckbox(form, labelWhenPipeline, true)
	}
	checked := func(label string) bool {
		box, ok := form.GetFormItemByLabel(label).(interface{ IsChecked() bool })
		return ok && box.IsChecked()
	}
	merge := func() {
		opts := forge.MergeOptions{
			Squash:               checked(labelSquash),
			RemoveSourceBranch:   checked(labelDeleteBranch),
			WhenPipelineSucceeds: underway && checked(labelWhenPipeline),
			SHA:                  mr.SHA,
		}
		a.closeModal(pageForm)
		a.note(fmt.Sprintf("Merging %s !%d …", path, mr.IID))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := client.Merge(ctx, mr, opts)
			a.tv.QueueUpdateDraw(func() {
				switch {
				case errors.Is(err, forge.ErrHeadMoved):
					a.flash(fmt.Sprintf("!%d has new commits since the list read it - look at them, then merge again", mr.IID))
					a.refetchMR(client, mr)
					return
				case err != nil:
					a.errorf("%v", err)
					return
				case opts.WhenPipelineSucceeds:
					a.done(fmt.Sprintf("%s !%d merges when its pipeline succeeds", path, mr.IID))
				default:
					a.done(fmt.Sprintf("Merged %s !%d into %s", path, mr.IID, mr.TargetBranch))
				}
				a.refetchMR(client, mr)
			})
		}()
	}
	form.AddButton("Merge", merge)
	form.AddButton("Cancel", func() { a.closeModal(pageForm) })
	height := 9 + rows
	if underway {
		height += 2
	}
	a.showFormModalSized(fmt.Sprintf("Merge %s !%d", path, mr.IID), form, mergeFormWidth, height)
}

// mergeWarnings are what the list knows that may stop a merge, or should
// make one think twice.
func mergeWarnings(mr forge.MergeRequest, underway bool) []string {
	var out []string
	if mr.Draft {
		out = append(out, "it is a draft - Ctrl-D marks it ready")
	}
	switch {
	case ciStateOf(mr.Pipeline) == ciFailed:
		out = append(out, "its pipeline failed")
	case underway:
		out = append(out, "its pipeline is still running")
	}
	if mr.ApprovalsRequired > 0 && len(mr.ApprovedBy) < mr.ApprovalsRequired {
		out = append(out, fmt.Sprintf("approved by %d of the %d needed", len(mr.ApprovedBy), mr.ApprovalsRequired))
	}
	if mr.UnresolvedKnown && mr.Unresolved > 0 {
		out = append(out, fmt.Sprintf("%d thread(s) not resolved", mr.Unresolved))
	}
	return out
}

// toggleDraft marks a draft ready for review, or a merge request back as a
// draft. It asks nothing: it is undone the same way.
func (a *App) toggleDraft(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	draft := !mr.Draft
	a.note(fmt.Sprintf("Marking %s !%d %s …", path, mr.IID, map[bool]string{true: "as a draft", false: "ready"}[draft]))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := client.SetDraft(ctx, mr, draft)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			if draft {
				a.done(fmt.Sprintf("%s !%d is a draft", path, mr.IID))
			} else {
				a.done(fmt.Sprintf("%s !%d is ready for review", path, mr.IID))
			}
			a.refetchMR(client, mr)
		})
	}()
}

// closeMR closes a merge request without merging it, after asking. Its
// branch stays, and so do its worktrees while they hold work of the user's.
func (a *App) closeMR(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	body := fmt.Sprintf("Close [::b]%s !%d[::-] without merging it?\n\n%s\n\nIts branch stays; everyone on it will see it closed.",
		path, mr.IID, tview.Escape(mr.Title))
	a.confirmWith("Close merge request", body, "Close", nil, func() {
		a.note(fmt.Sprintf("Closing %s !%d …", path, mr.IID))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			err := client.CloseMergeRequest(ctx, mr)
			a.tv.QueueUpdateDraw(func() {
				if err != nil {
					a.errorf("%v", err)
					return
				}
				a.done(fmt.Sprintf("Closed %s !%d", path, mr.IID))
				a.refetchMR(client, mr)
			})
		}()
	})
}

// editReviewers lists who can review, those asked marked; space asks or
// withdraws, and the choice goes to the server when the list closes.
func (a *App) editReviewers(mr forge.MergeRequest) {
	client := a.client(mr.Instance)
	if client == nil {
		a.errorf("%s has no token - set one in [4] Settings", a.instanceLabel(mr.Instance))
		return
	}
	path := a.projectPathOfMR(mr)
	a.note(fmt.Sprintf("Reading who can review %s !%d …", path, mr.IID))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		users, err := client.ReviewerCandidates(ctx, mr)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			a.showReviewerToggles(client, mr, path, users)
		})
	}()
}

func (a *App) showReviewerToggles(client forge.Provider, mr forge.MergeRequest, path string, users []forge.User) {
	var before []string
	asked := map[string]bool{}
	for _, r := range mr.Reviewers {
		before = append(before, r.Username)
		asked[r.Username] = true
	}
	// Someone asked already stays on the list even when the candidates do
	// not name them, or they could not be taken off.
	known := map[string]bool{}
	for _, u := range users {
		known[u.Username] = true
	}
	for _, r := range mr.Reviewers {
		if !known[r.Username] {
			users = append(users, r)
		}
	}
	sort.SliceStable(users, func(i, j int) bool { return users[i].Username < users[j].Username })

	a.showToggles(toggles{
		title: fmt.Sprintf("Reviewers · %s !%d", path, mr.IID),
		verb:  "ask/withdraw",
		items: func() []toggleItem {
			items := make([]toggleItem, 0, len(users))
			for _, u := range users {
				label := tagMark(asked[u.Username]) + " " + esc(u.Username)
				if u.Name != "" && u.Name != u.Username {
					label += "  " + tag(colDim) + esc(u.Name) + tagEnd
				}
				items = append(items, toggleItem{Label: label, Search: u.Username + " " + u.Name, Data: u.Username})
			}
			return items
		},
		toggle: func(it toggleItem) {
			name := it.Data.(string)
			asked[name] = !asked[name]
		},
		status: func() string {
			n := 0
			for _, on := range asked {
				if on {
					n++
				}
			}
			return fmt.Sprintf("%d asked", n)
		},
		escSays: "save",
		closed: func() {
			var after []string
			for _, u := range users {
				if asked[u.Username] {
					after = append(after, u.Username)
				}
			}
			sort.Strings(before)
			if slices.Equal(before, after) {
				return
			}
			a.saveReviewers(client, mr, path, after)
		},
	})
}

func (a *App) saveReviewers(client forge.Provider, mr forge.MergeRequest, path string, usernames []string) {
	a.note(fmt.Sprintf("Asking for reviews of %s !%d …", path, mr.IID))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := client.SetReviewers(ctx, mr, usernames)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.errorf("%v", err)
				return
			}
			who := "nobody"
			if len(usernames) > 0 {
				who = strings.Join(usernames, ", ")
			}
			a.done(fmt.Sprintf("Reviewers of %s !%d: %s", path, mr.IID, who))
			a.refetchMR(client, mr)
		})
	}()
}
