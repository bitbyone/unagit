package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/tobola/unagit/internal/draft"
	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/incomm"
)

// A comment written in unagit on a line can be published at once or saved:
// a saved one waits in unagit (draft), counted with Incomm's in PUB and
// published with them by P, until a worktree of its merge request is
// opened or synced with Incomm on - then it moves into that worktree's
// Incomm store, and from there on Incomm's copy is the comment.

func (a *App) draftStore() draft.Store { return draft.Store{Dir: a.cfg.DraftsDir()} }

func (a *App) draftKey(mr forge.MergeRequest) draft.Key {
	return draft.Key{Instance: mr.Instance, Project: a.projectPathOfMR(mr), IID: mr.IID}
}

// incommSync is what bringing comments into Incomm off the event loop
// needs to know from it: whether Incomm is on, and whose name a comment
// saved in unagit goes under there.
type incommSync struct {
	on     bool
	author string
}

// incommFor reads it on the event loop, for work on a server's merge
// request.
func (a *App) incommFor(instance string) incommSync {
	return incommSync{on: a.cfg.Integrations.Incomm, author: a.me[instance]}
}

// moveDrafts moves a merge request's saved comments into the Incomm store
// of a place, each that its line is in the checkout there. It runs off the
// event loop, where the worktree was just brought up to date; one that
// cannot move stays saved in unagit. author is who wrote them.
func (a *App) moveDrafts(ctx context.Context, mr forge.MergeRequest, place incomm.Place, author string, log func(string)) {
	store, key := a.draftStore(), a.draftKey(mr)
	drafts, err := store.List(key)
	if err != nil || len(drafts) == 0 {
		return
	}
	moved := 0
	for _, d := range drafts {
		err := incomm.AddPending(ctx, place, d.Path, d.Line, d.Body, author)
		switch {
		case errors.Is(err, incomm.ErrNoLine):
			if log != nil {
				log(fmt.Sprintf("Incomm: %s:%d is not in this checkout - the saved comment stays in unagit", d.Path, d.Line))
			}
			continue
		case err != nil:
			if log != nil {
				log("Incomm: a saved comment could not be moved: " + err.Error())
			}
			continue
		}
		if err := store.Remove(key, d.ID); err == nil {
			moved++
		}
	}
	if moved > 0 && log != nil {
		log(fmt.Sprintf("Incomm: moved %d comments saved in unagit", moved))
	}
}

// draftNotes are a merge request's saved comments as the conversation
// draws them, each a thread of its own, with ids no forge note has.
func draftNotes(drafts []draft.Draft) []forge.Note {
	notes := make([]forge.Note, len(drafts))
	for i, d := range drafts {
		notes[i] = forge.Note{ID: -1 - i, Thread: "draft:" + d.ID, Body: d.Body, CreatedAt: d.Created,
			Path: d.Path, Line: d.Line, From: forge.LineRef{New: d.Line}, To: forge.LineRef{New: d.Line},
			Author: forge.User{Name: "you"}}
	}
	return notes
}

// draftOf is the saved comment a note of the conversation stands for.
func draftOf(n forge.Note, drafts []draft.Draft) (draft.Draft, bool) {
	for _, d := range drafts {
		if n.Thread == "draft:"+d.ID {
			return d, true
		}
	}
	return draft.Draft{}, false
}
