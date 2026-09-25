// Package incomm imports forge comments into Incomm review worktrees and
// publishes the ones marked for the forge back.
package incomm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tobola/unagit/internal/forge"
)

// importThread is one forge conversation as it lands in Incomm: the comment
// that started it, with the location it is anchored to, and the replies.
type importThread struct {
	Root    forge.Note
	Replies []forge.Note
}

func located(n forge.Note) bool { return n.Path != "" && n.Line > 0 }

// importThreads groups the comments into conversations, oldest first. A
// conversation that has no line to be anchored to (a general comment) is left
// out; system notes are too. A reply that carries no location of its own takes
// the one of the comment it answers.
func importThreads(notes []forge.Note) []importThread {
	var threads []importThread
	at := map[string]int{}
	for _, n := range notes {
		if n.System {
			continue
		}
		if n.Thread == "" {
			threads = append(threads, importThread{Root: n})
			continue
		}
		i, ok := at[n.Thread]
		if !ok {
			at[n.Thread] = len(threads)
			threads = append(threads, importThread{Root: n})
			continue
		}
		threads[i].Replies = append(threads[i].Replies, n)
	}
	kept := threads[:0]
	for _, t := range threads {
		if !located(t.Root) {
			var from *forge.Note
			for i := range t.Replies {
				if located(t.Replies[i]) {
					from = &t.Replies[i]
					break
				}
			}
			if from == nil {
				continue
			}
			t.Root.Path, t.Root.Line, t.Root.Orphaned = from.Path, from.Line, from.Orphaned
		}
		kept = append(kept, t)
	}
	return kept
}

// legacySource is how an earlier unagit remembered where a comment came from:
// a line at the end of its text.
var legacySource = regexp.MustCompile(`(?s)Source: (\S+) \(comment (\d+)\)\s*$`)

// existing is a comment already in the worktree, as the CLI lists it.
type existing struct {
	ID       string
	View     string // the view it is visible in, which its later commands must use
	SourceID int
	Orphaned bool
	Content  string
	Replies  []int // source ids of the replies that came from the forge
}

type listed struct {
	Notes []struct {
		ID       string `json:"id"`
		Content  string `json:"content"`
		Orphaned bool   `json:"orphaned"`
		Source   *struct {
			ID int `json:"id"`
		} `json:"source"`
		Replies []struct {
			Source *struct {
				ID int `json:"id"`
			} `json:"source"`
		} `json:"replies"`
	} `json:"notes"`
}

// existingNotes lists what is in the worktree through both views, so a comment
// the human has since made external-only is still recognised.
func (c *cli) existingNotes() ([]*existing, error) {
	var all []*existing
	seen := map[string]bool{}
	for _, view := range []string{viewAgent, viewExternal} {
		out, err := c.run(view, "list")
		if err != nil {
			return nil, err
		}
		var l listed
		if err := json.Unmarshal(out, &l); err != nil {
			return nil, fmt.Errorf("read incomm comments: %w", err)
		}
		for _, n := range l.Notes {
			if seen[n.ID] {
				continue
			}
			seen[n.ID] = true
			e := &existing{ID: n.ID, View: view, Orphaned: n.Orphaned, Content: n.Content}
			if n.Source != nil {
				e.SourceID = n.Source.ID
			}
			for _, r := range n.Replies {
				if r.Source != nil && r.Source.ID != 0 {
					e.Replies = append(e.Replies, r.Source.ID)
				}
			}
			all = append(all, e)
		}
	}
	return all, nil
}

// sourceLink is where a comment can be looked at: the forge's own link, or the
// merge request page when it did not send one.
func sourceLink(mr forge.MergeRequest, n forge.Note) string {
	if n.URL != "" {
		return n.URL
	}
	if mr.WebURL != "" {
		return fmt.Sprintf("%s#note_%d", mr.WebURL, n.ID)
	}
	return ""
}

func authorName(n forge.Note) string {
	if n.Author.Name != "" {
		return n.Author.Name
	}
	if n.Author.Username != "" {
		return n.Author.Username
	}
	return "Merge request reviewer"
}

// Import waits for every write so the editor sees a complete conversation.
//
// Each forge comment becomes an Incomm comment addressed to the agent and to
// the forge, remembering where it came from; replies become replies. Running it
// again adds nothing: a comment is recognised by the id the forge gave it.
func Import(ctx context.Context, dir string, mr forge.MergeRequest, notes []forge.Note, log func(string)) error {
	threads := importThreads(notes)
	if len(threads) == 0 {
		return nil
	}
	c, err := newCLI(ctx, dir)
	if err != nil {
		return err
	}
	// Incomm searches parents even with --root. Give this worktree its own
	// store before invoking it, so another checkout's notes stay separate.
	if err := os.MkdirAll(filepath.Join(dir, ".incomm"), 0o755); err != nil {
		return err
	}
	current, err := c.existingNotes()
	if err != nil {
		return err
	}

	byID := map[int]*existing{}
	replied := map[int]bool{}
	for _, e := range current {
		if e.SourceID != 0 {
			byID[e.SourceID] = e
		}
		for _, id := range e.Replies {
			replied[id] = true
		}
	}

	forgeByID := map[int]forge.Note{}
	root := map[int]bool{}
	for _, t := range threads {
		forgeByID[t.Root.ID] = t.Root
		root[t.Root.ID] = true
		for _, r := range t.Replies {
			forgeByID[r.ID] = r
		}
	}

	// A comment imported by an earlier unagit carries its origin at the end of
	// its text and no source. Give it the metadata rather than importing it
	// again; its text stays as it is.
	migrated := 0
	for _, e := range current {
		if e.SourceID != 0 {
			continue
		}
		m := legacySource.FindStringSubmatch(e.Content)
		if m == nil {
			continue
		}
		id, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		fn, ok := forgeByID[id]
		if !ok {
			continue
		}
		link := sourceLink(mr, fn)
		if link == "" {
			link = m[1]
		}
		src := Source{URL: link, ID: id}
		if root[id] {
			src.Thread = fn.Thread
		}
		if err := c.setSource(viewAgent, e.ID, "", src, "agent+external"); err != nil {
			return err
		}
		e.SourceID = id
		byID[id] = e
		migrated++
	}

	added, orphaned, replies := 0, 0, 0
	for _, t := range threads {
		n := t.Root
		// A forge path must stay inside the worktree, including through symlinks.
		if !filepath.IsLocal(n.Path) {
			return fmt.Errorf("incomm comment has an invalid file path: %q", n.Path)
		}
		if !n.Orphaned {
			ok, err := hasLine(dir, n.Path, n.Line)
			if err != nil {
				return err
			}
			n.Orphaned = !ok
		}
		rec := byID[n.ID]
		switch {
		case rec != nil:
			// A comment imported on an earlier head may now belong to a
			// deleted line. Keep its replies and identity while orphaning it.
			if n.Orphaned {
				if _, err := c.run(rec.View, "anchor", "set", rec.ID, "--orphaned",
					"--line", strconv.Itoa(n.Line), "--no-recompute",
					"--start-prefix", "\n", "--end-prefix", "",
					"--context-before", "", "--context-after", "", "--checksum", ""); err != nil {
					return err
				}
			}
		default:
			link := sourceLink(mr, n)
			var id string
			if n.Orphaned {
				id, err = appendOrphan(ctx, dir, n, n.Body, authorName(n), link)
				if err != nil {
					return err
				}
				orphaned++
			} else {
				args := []string{"add", "--file", n.Path, "--line", strconv.Itoa(n.Line),
					"--content", n.Body, "--author", "user", "--author-title", authorName(n),
					"--audience", "agent+external", "--source-id", strconv.Itoa(n.ID)}
				if link != "" {
					args = append(args, "--source-url", link)
				}
				if n.Thread != "" {
					args = append(args, "--source-thread", n.Thread)
				}
				out, err := c.run(viewAgent, args...)
				if err != nil {
					return err
				}
				var created struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(out, &created); err != nil || created.ID == "" {
					return fmt.Errorf("read the comment incomm created: %v", err)
				}
				id = created.ID
			}
			rec = &existing{ID: id, View: viewAgent, SourceID: n.ID}
			byID[n.ID] = rec
			added++
		}
		for _, r := range t.Replies {
			if replied[r.ID] || byID[r.ID] != nil {
				continue
			}
			args := []string{"reply", rec.ID, "--content", r.Body, "--author", "user",
				"--author-title", authorName(r), "--audience", "agent+external",
				"--source-id", strconv.Itoa(r.ID)}
			if link := sourceLink(mr, r); link != "" {
				args = append(args, "--source-url", link)
			}
			if _, err := c.run(rec.View, args...); err != nil {
				return err
			}
			replied[r.ID] = true
			replies++
		}
	}
	if log != nil {
		log(fmt.Sprintf("Incomm: imported %d comments", added))
		if replies > 0 {
			log(fmt.Sprintf("Incomm: imported %d replies", replies))
		}
		if orphaned > 0 {
			log(fmt.Sprintf("Incomm: imported %d orphaned comments", orphaned))
		}
		if migrated > 0 {
			log(fmt.Sprintf("Incomm: recorded the origin of %d comments imported earlier", migrated))
		}
	}
	return nil
}

// hasLine checks the current checkout without following a path outside it.
func hasLine(dir, path string, line int) (bool, error) {
	target, err := filepath.EvalSymlinks(filepath.Join(dir, path))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(rel) {
		return false, fmt.Errorf("incomm comment file is outside the review: %q", path)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return line <= len(lines), nil
}
