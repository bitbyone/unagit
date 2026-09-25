package incomm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Source is where a comment came from, or where it was published to.
type Source struct {
	URL    string `json:"url,omitempty"`
	ID     int    `json:"id,omitempty"`
	Thread string `json:"thread,omitempty"`
}

// Audience values as the notes file spells them. An absent one is the agent's.
const (
	audiencePrivate  = "private"
	audienceAgent    = "agent"
	audienceExternal = "external"
	audienceBoth     = "agent+external"
)

func effectiveAudience(a string) string {
	switch a {
	case "", audienceAgent:
		return audienceAgent
	case audiencePrivate, audienceExternal, audienceBoth:
		return a
	}
	return audiencePrivate // a value from the future is never shown
}

func includesExternal(a string) bool {
	e := effectiveAudience(a)
	return e == audienceExternal || e == audienceBoth
}

// Comment is a comment or a reply as it is needed to publish it.
type Comment struct {
	ID       string // Incomm's id, of the comment or of the reply
	Author   string // "user" or "agent"
	Title    string // the display name
	Audience string
	Content  string
	Source   Source
}

// OnForge reports whether the comment is already there: it was imported from
// the forge or has been published.
func (c Comment) OnForge() bool { return c.Source.ID != 0 }

// Pending is a comment that is meant for the forge and has not gone there yet.
func (c Comment) Pending() bool { return includesExternal(c.Audience) && !c.OnForge() }

// Thread is a comment with its replies, read from a worktree.
type Thread struct {
	Dir      string // the worktree it was read from
	File     string
	Line     int
	Orphaned bool
	Root     Comment
	Replies  []Comment // without the private ones
}

// PendingCount counts the comments and replies waiting to be published.
func (t Thread) PendingCount() int {
	n := 0
	if t.Root.Pending() {
		n++
	}
	for _, r := range t.Replies {
		if r.Pending() {
			n++
		}
	}
	return n
}

// NeedsPublishing says whether anything in the thread waits for the forge.
func (t Thread) NeedsPublishing() bool { return t.PendingCount() > 0 }

// notesFile is the part of .incomm/notes*.json unagit reads.
type notesFile struct {
	Version int `json:"version"`
	Notes   []struct {
		ID          string  `json:"id"`
		File        string  `json:"file"`
		StartLine   int     `json:"startLine"`
		Orphaned    bool    `json:"orphaned"`
		Content     string  `json:"content"`
		Author      string  `json:"author"`
		AuthorTitle string  `json:"authorTitle"`
		Audience    string  `json:"audience"`
		Source      *Source `json:"source"`
		Replies     []struct {
			ID          string  `json:"id"`
			Author      string  `json:"author"`
			AuthorTitle string  `json:"authorTitle"`
			Audience    string  `json:"audience"`
			Source      *Source `json:"source"`
			Content     string  `json:"content"`
		} `json:"replies"`
	} `json:"notes"`
}

func comment(id, author, title, audience, content string, src *Source) Comment {
	c := Comment{ID: id, Author: author, Title: title, Audience: audience, Content: content}
	if src != nil {
		c.Source = *src
	}
	return c
}

// ReadThreads reads every comment of a worktree straight from its notes files.
// unagit is a tool of the person the comments belong to, so it may read them
// all; what it must not do is act on a private one, so those are left out: a
// private comment hides its whole thread, and a reply under a private comment
// is private whatever it says. A file in a format this build does not know, or
// one that cannot be read, is skipped rather than guessed at.
func ReadThreads(dir string) []Thread {
	files, _ := filepath.Glob(filepath.Join(dir, ".incomm", "notes*.json"))
	sort.Strings(files)
	var threads []Thread
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var f notesFile
		if json.Unmarshal(data, &f) != nil || f.Version > formatVersion {
			continue
		}
		for _, n := range f.Notes {
			if effectiveAudience(n.Audience) == audiencePrivate {
				continue
			}
			t := Thread{
				Dir: dir, File: n.File, Line: n.StartLine, Orphaned: n.Orphaned,
				Root: comment(n.ID, n.Author, n.AuthorTitle, n.Audience, n.Content, n.Source),
			}
			for _, r := range n.Replies {
				if effectiveAudience(r.Audience) == audiencePrivate {
					continue
				}
				t.Replies = append(t.Replies, comment(r.ID, r.Author, r.AuthorTitle, r.Audience, r.Content, r.Source))
			}
			threads = append(threads, t)
		}
	}
	return threads
}

// PendingIn counts what is waiting to be published in a worktree.
func PendingIn(dir string) int {
	n := 0
	for _, t := range ReadThreads(dir) {
		n += t.PendingCount()
	}
	return n
}
