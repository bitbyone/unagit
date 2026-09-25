package incomm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// Poster is what publishing needs of a forge; forge.Provider is one.
type Poster interface {
	CreateDiscussion(ctx context.Context, mr forge.MergeRequest, path string, line int, body string) (*forge.Note, error)
	ReplyToDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, body string) (*forge.Note, error)
	CommentNote(ctx context.Context, mr forge.MergeRequest, body string) (*forge.Note, error)
	ResolveDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, resolved bool) error
}

// Step is one post to the forge: a comment that starts a conversation, or a
// reply to one.
type Step struct {
	Dir     string // the worktree it lives in
	File    string // where the conversation is anchored
	Line    int
	Root    Comment // the conversation's first comment, for context
	Comment Comment // what this step publishes
	IsRoot  bool    // this step publishes Root
	Parent  bool    // Root goes out only because a reply to it needs a place
	// Orphaned: the code Root was written against is gone. It is posted on the
	// conversation, with the file but not a line, rather than at a stale one.
	Orphaned bool
	// Resolve: this step resolves the conversation on the forge, once its posts
	// are out. Comment is not used.
	Resolve bool
}

// Plan lists what publishing would post, in the order it must happen: a
// conversation's first comment before its replies. Only what is pending goes,
// plus the first comment of a conversation that is not on the forge yet when a
// reply to it is pending, since a reply needs something to answer. Nothing else
// is ever published - a private comment is not in the threads at all.
func Plan(threads []Thread) []Step { return PlanResolving(threads, nil) }

// PlanResolving is Plan with the threads that should be resolved on the forge:
// one that is resolved here and is on the forge, or goes out in this very plan,
// and is not resolved there yet. forgeResolved says which threads the forge has
// and whether it holds them resolved, keyed by thread id (see ForgeResolved);
// nil means the forge's state is not known, and then nothing is resolved. A
// thread that is only in Incomm - never published, and not in this plan - is
// never resolved on the forge.
func PlanResolving(threads []Thread, forgeResolved map[string]bool) []Step {
	var steps []Step
	for _, t := range threads {
		base := Step{Dir: t.Dir, File: t.File, Line: t.Line, Root: t.Root, Orphaned: t.Orphaned}
		replies := 0
		for _, r := range t.Replies {
			if r.Pending() {
				replies++
			}
		}
		rootGoesOut := false
		switch {
		case t.Root.Pending():
			s := base
			s.Comment, s.IsRoot = t.Root, true
			steps = append(steps, s)
			rootGoesOut = true
		case replies > 0 && !t.Root.OnForge():
			s := base
			s.Comment, s.IsRoot, s.Parent = t.Root, true, true
			steps = append(steps, s)
			rootGoesOut = true
		}
		for _, r := range t.Replies {
			if r.Pending() {
				s := base
				s.Comment = r
				steps = append(steps, s)
			}
		}
		if forgeResolved != nil && t.Resolved && resolvesOnForge(t, rootGoesOut, forgeResolved) {
			s := base
			s.Resolve = true
			steps = append(steps, s)
		}
	}
	return steps
}

// resolvesOnForge says whether a thread that is resolved here should be resolved
// there: it has to exist there (a thread id it can be found by, or a root that
// is posted in this plan) and not be resolved already.
func resolvesOnForge(t Thread, rootGoesOut bool, forgeResolved map[string]bool) bool {
	if rootGoesOut {
		return true
	}
	if !t.Root.OnForge() || t.Root.Source.Thread == "" {
		return false
	}
	resolved, known := forgeResolved[t.Root.Source.Thread]
	return known && !resolved
}

// NeedsForgeState reports whether the forge has to be asked before resolving is
// planned: some thread that is resolved here is already on the forge, and only
// the forge can say whether it is resolved there. A thread that goes out in the
// plan is new there, and needs no asking.
func NeedsForgeState(threads []Thread) bool {
	for _, t := range threads {
		if t.Resolved && t.Root.OnForge() && t.Root.Source.Thread != "" {
			return true
		}
	}
	return false
}

// ForgeResolved reads which threads the forge holds resolved: a thread counts
// as resolved when its first comment is resolvable and resolved, which is how
// the forge reports a thread that was resolved as a whole. Only threads it has a
// resolvable first comment for are in the map.
func ForgeResolved(notes []forge.Note) map[string]bool {
	first := map[string]forge.Note{}
	for _, n := range notes {
		if n.System || n.Thread == "" {
			continue
		}
		if cur, ok := first[n.Thread]; !ok || n.CreatedAt.Before(cur.CreatedAt) || (n.CreatedAt.Equal(cur.CreatedAt) && n.ID < cur.ID) {
			first[n.Thread] = n
		}
	}
	state := map[string]bool{}
	for thread, n := range first {
		if n.Resolvable {
			state[thread] = n.Resolved
		}
	}
	return state
}

// Confirmed keeps, of the steps planned now, the ones that were in the plan the
// person confirmed, in today's order and with today's file and line. Anything
// that has gone from the plan since (it was published elsewhere, or deleted) is
// dropped, and anything new is not added: what goes out is what was agreed to.
func Confirmed(planned, confirmed []Step) []Step {
	type key struct {
		dir, id string
		resolve bool
	}
	agreed := map[key]bool{}
	for _, s := range confirmed {
		agreed[key{s.Dir, s.Comment.ID, s.Resolve}] = true
	}
	// A resolve step carries no comment: it stands for its conversation.
	for _, s := range confirmed {
		if s.Resolve {
			agreed[key{s.Dir, s.Root.ID, true}] = true
		}
	}
	var kept []Step
	for _, s := range planned {
		id := s.Comment.ID
		if s.Resolve {
			id = s.Root.ID
		}
		if agreed[key{s.Dir, id, s.Resolve}] {
			kept = append(kept, s)
		}
	}
	return kept
}

// Body is the text that goes to the forge. The forge posts everything as the
// owner of the token, so a comment the agent wrote says so in its first line;
// what a person wrote goes as it is.
func Body(c Comment) string {
	if c.Author != "agent" {
		return c.Content
	}
	name := "Agent"
	if c.Title != "" {
		name = "Agent (" + c.Title + ")"
	}
	return "**" + name + ":**\n\n" + c.Content
}

// Summary is a line for the confirmation: where, what kind, and how it starts.
func (s Step) Summary() string {
	if s.Resolve {
		return fmt.Sprintf("%s:%d  resolve thread", s.File, s.Line)
	}
	kind := "reply"
	switch {
	case s.Parent:
		kind = "comment, only because a reply needs it"
	case s.IsRoot:
		kind = "comment"
	}
	text := strings.Join(strings.Fields(s.Comment.Content), " ")
	if r := []rune(text); len(r) > 60 {
		text = string(r[:59]) + "…"
	}
	who := "you"
	if s.Comment.Author == "agent" {
		who = "agent"
	}
	where := fmt.Sprintf("%s:%d", s.File, s.Line)
	if s.Orphaned && s.IsRoot {
		where = s.File + " (orphaned: its code has changed, posted on the conversation)"
	} else if s.Orphaned {
		where = s.File + " (orphaned)"
	}
	return fmt.Sprintf("%s  %s by %s: %s", where, kind, who, text)
}

// Publisher posts the steps of a plan and remembers, comment by comment, that
// they went out.
type Publisher struct {
	Poster Poster
	MR     forge.MergeRequest
	// Record writes where a comment went back into the worktree. It defaults to
	// SetSource, the CLI.
	Record func(ctx context.Context, dir, id, reply string, src Source, audience string) error
	Log    func(string)
}

func (p Publisher) log(format string, a ...any) {
	if p.Log != nil {
		p.Log(fmt.Sprintf(format, a...))
	}
}

// Publish posts the steps one after another and stops at the first failure. What
// was written back before it is not posted again next time, which is why each
// post is recorded straight away rather than at the end. It returns how many
// steps went out.
func (p Publisher) Publish(ctx context.Context, steps []Step) (int, error) {
	record := p.Record
	if record == nil {
		record = SetSource
	}
	threads := map[string]string{} // conversation -> the forge's thread id
	links := map[string]string{}   // conversation -> where its first comment is
	key := func(s Step) string { return s.Dir + "\x00" + s.Root.ID }
	done := 0
	unsupported := false
	var notResolved []string
	for _, s := range steps {
		if s.Resolve {
			// After the conversation's posts, which come before it in the plan.
			// Failing here never undoes them.
			thread, known := threads[key(s)]
			if !known {
				thread = s.Root.Source.Thread
			}
			switch {
			case unsupported:
			case thread == "":
				p.log("%s:%d has no thread on the forge to resolve; left as it is", s.File, s.Line)
			default:
				err := p.Poster.ResolveDiscussion(ctx, p.MR, thread, true)
				switch {
				case errors.Is(err, forge.ErrNotSupported):
					unsupported = true
					p.log("This forge cannot resolve threads through its API; resolve them there.")
				case err != nil:
					p.log("! could not resolve %s:%d: %v", s.File, s.Line, err)
					notResolved = append(notResolved, fmt.Sprintf("%s:%d", s.File, s.Line))
					continue
				default:
					p.log("Resolved %s:%d", s.File, s.Line)
				}
			}
			done++
			continue
		}
		var note *forge.Note
		var err error
		audience := ""
		switch {
		case s.IsRoot && s.Orphaned:
			// The code this was written against is gone, so there is no line to
			// put it on; say where it was about and leave it on the conversation.
			body := fmt.Sprintf("`%s` (the code this comment was written against has changed)\n\n%s", s.File, Body(s.Comment))
			note, err = p.Poster.CommentNote(ctx, p.MR, body)
			if err == nil {
				p.log("%s is orphaned: posted on the conversation, not on a line", s.File)
				threads[key(s)], links[key(s)] = note.Thread, note.URL
				if s.Parent {
					audience = audienceBoth
				}
			}
		case s.IsRoot:
			note, err = p.Poster.CreateDiscussion(ctx, p.MR, s.File, s.Line, Body(s.Comment))
			if err == nil {
				if note.Path == "" && note.Line == 0 {
					p.log("%s:%d is not part of the diff; posted on the conversation instead", s.File, s.Line)
				}
				threads[key(s)], links[key(s)] = note.Thread, note.URL
				if s.Parent {
					audience = audienceBoth
				}
			}
		default:
			thread, known := threads[key(s)]
			if !known {
				thread = s.Root.Source.Thread
			}
			if thread != "" {
				note, err = p.Poster.ReplyToDiscussion(ctx, p.MR, thread, Body(s.Comment))
				break
			}
			body := Body(s.Comment)
			link := s.Root.Source.URL
			if l := links[key(s)]; l != "" {
				link = l
			}
			if link != "" {
				body = "In reply to " + link + "\n\n" + body
			}
			p.log("%s:%d cannot be replied to as a thread; posted as a comment on the conversation", s.File, s.Line)
			note, err = p.Poster.CommentNote(ctx, p.MR, body)
		}
		if err != nil {
			return done, fmt.Errorf("%s: %w", s.Summary(), err)
		}
		src := Source{URL: note.URL, ID: note.ID}
		reply := ""
		if s.IsRoot {
			src.Thread = note.Thread
		} else {
			reply = s.Comment.ID
		}
		id := s.Root.ID
		if err := record(ctx, s.Dir, id, reply, src, audience); err != nil {
			// It is on the forge and not written down: say so, because posting
			// again would duplicate it.
			return done, fmt.Errorf("%s was posted (comment %d) but could not be recorded in incomm: %w", s.Summary(), note.ID, err)
		}
		done++
		p.log("Published %s", s.Summary())
	}
	if len(notResolved) > 0 {
		return done, fmt.Errorf("%d thread(s) could not be resolved on the forge (%s); everything else went out",
			len(notResolved), strings.Join(notResolved, ", "))
	}
	return done, nil
}

// ThreadsOf reads the comments of every given worktree that exists.
func ThreadsOf(dirs ...string) []Thread {
	var threads []Thread
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		threads = append(threads, ReadThreads(dir)...)
	}
	return threads
}
