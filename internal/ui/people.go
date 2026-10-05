package ui

import (
	"context"
	"sync"
	"time"

	"github.com/tobola/unagit/internal/forge"
	"github.com/tobola/unagit/internal/index"
)

// People are shown by the name they go by, not by their handle. GitLab's
// lists carry the name; GitHub's carry the login alone, so the names are
// learnt into an index of their own (index-users.json) during a refresh of
// the merge requests and kept for index.NameAge - asked for once a month,
// not every time.

// personName is how a person is shown: their name, else their username.
func personName(u forge.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}

// named is u with its name filled in from what has been learnt, when it
// came without one.
func (a *App) named(instance string, u forge.User) forge.User {
	if u.Name == "" {
		u.Name = a.people.Name(instance, u.Username)
	}
	return u
}

// namedAll is named for a list of people.
func (a *App) namedAll(instance string, list []forge.User) []forge.User {
	out := make([]forge.User, len(list))
	for i, u := range list {
		out[i] = a.named(instance, u)
	}
	return out
}

// namedNotes is a conversation with its authors named.
func (a *App) namedNotes(instance string, notes []forge.Note) []forge.Note {
	out := make([]forge.Note, len(notes))
	for i, n := range notes {
		n.Author = a.named(instance, n.Author)
		out[i] = n
	}
	return out
}

// namesFanOut is how many names are asked for at once.
const namesFanOut = 4

// learnNames notes the names the merge requests came with and asks each
// server for those of the people it named without one, unless they were
// learnt lately. A name that cannot be had is left to the next refresh; the
// list does not wait on it.
func learnNames(ctx context.Context, people *index.Users, mrs []forge.MergeRequest, clients map[string]forge.Provider, progress func(done, of int)) {
	now := time.Now()
	nameless := map[string][]string{}
	note := func(instance string, u forge.User) {
		if u.Name != "" {
			people.Learn(instance, u.Username, u.Name, now)
			return
		}
		nameless[instance] = append(nameless[instance], u.Username)
	}
	for _, mr := range mrs {
		note(mr.Instance, mr.Author)
		for _, u := range mr.Reviewers {
			note(mr.Instance, u)
		}
	}
	type ask struct{ instance, username string }
	var asks []ask
	for instance, usernames := range nameless {
		if clients[instance] == nil {
			continue
		}
		for _, username := range people.Unknown(instance, usernames, now) {
			asks = append(asks, ask{instance, username})
		}
	}
	if len(asks) == 0 {
		return
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, namesFanOut)
	done := 0
	for _, q := range asks {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			name, err := clients[q.instance].UserName(ctx, q.username)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				people.Learn(q.instance, q.username, name, now)
			}
			done++
			if progress != nil {
				progress(done, len(asks))
			}
		}()
	}
	wg.Wait()
}
