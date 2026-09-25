package incomm

import (
	"context"
	"fmt"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// Poster is what publishing needs of a forge; forge.Provider is one.
type Poster interface {
	CreateDiscussion(ctx context.Context, mr forge.MergeRequest, path string, line int, body string) (*forge.Note, error)
	ReplyToDiscussion(ctx context.Context, mr forge.MergeRequest, thread string, body string) (*forge.Note, error)
	CommentNote(ctx context.Context, mr forge.MergeRequest, body string) (*forge.Note, error)
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
}

// Plan lists what publishing would post, in the order it must happen: a
// conversation's first comment before its replies. Only what is pending goes,
// plus the first comment of a conversation that is not on the forge yet when a
// reply to it is pending, since a reply needs something to answer. Nothing else
// is ever published - a private comment is not in the threads at all.
func Plan(threads []Thread) []Step {
	var steps []Step
	for _, t := range threads {
		base := Step{Dir: t.Dir, File: t.File, Line: t.Line, Root: t.Root}
		replies := 0
		for _, r := range t.Replies {
			if r.Pending() {
				replies++
			}
		}
		switch {
		case t.Root.Pending():
			s := base
			s.Comment, s.IsRoot = t.Root, true
			steps = append(steps, s)
		case replies > 0 && !t.Root.OnForge():
			s := base
			s.Comment, s.IsRoot, s.Parent = t.Root, true, true
			steps = append(steps, s)
		}
		for _, r := range t.Replies {
			if r.Pending() {
				s := base
				s.Comment = r
				steps = append(steps, s)
			}
		}
	}
	return steps
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
	return fmt.Sprintf("%s:%d  %s by %s: %s", s.File, s.Line, kind, who, text)
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
	for _, s := range steps {
		var note *forge.Note
		var err error
		audience := ""
		switch {
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
