package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobola/unagit/internal/gitx"
	"github.com/tobola/unagit/internal/watch"
)

// A local watch follows a clone on disk - its branches, its tags, the
// commits that come to them - and says on Activity what came from outside
// unagit: an agent's commit, a tag made in a terminal. Nothing is asked of
// a server, and the news is quiet: the log and the marks, never a toast.

// localAskEvery is how often a watched clone's refs are read: a process
// apiece, and an agent's commit should show while it is still the news.
const localAskEvery = 3 * time.Second

// localWatch is the watch on a repository's clone.
func localWatch(instance, project, dir string) watch.Watch {
	return watch.Watch{Kind: watch.KindLocal, Instance: instance, Project: project, Dir: dir}
}

// watchLocalAction is Watch Local Changes on a clone, or Stop Watching
// Local Changes once it is watched. Like the other watches it has no key.
func (a *App) watchLocalAction(instance, project string, dir func() string) uiAction {
	w := localWatch(instance, project, "")
	if a.isWatched(w) {
		return uiAction{id: "toggle_local_watch", name: "Stop Watching Local Changes", about: "Stop following the clone's branches and tags; the Activity screen lets it go.", rank: 65,
			run: func() { a.setWatched(w, false) }}
	}
	return uiAction{id: "toggle_local_watch", name: "Watch Local Changes", about: "Follow the clone's branches and tags on the Activity screen: new commits, branches and tags made outside unagit - by an agent, in a terminal.", rank: 65,
		when: func() bool { return dir() != "" },
		run: func() {
			w.Dir = dir()
			a.setWatched(w, true)
		}}
}

// localWatchLog is the commit log of a watched clone.
func (a *App) localWatchLog(w watch.Watch) {
	a.repositoryLog(a.worktreeProject(worktreeRow{Instance: w.Instance, Path: w.Project}))
}

// localWords is how a watched clone stands: its branches and tags.
func localWords(st watch.State) string {
	if st.Read.IsZero() {
		return "reading…"
	}
	return fmt.Sprintf("%d %s · %d %s", st.Heads, plural(st.Heads, "branch", "branches"), st.Tags, plural(st.Tags, "tag", "tags"))
}

// pollLocal reads the watched clones whose turn it is and says what moved
// in them since they were last read, other than what unagit moved itself.
// It runs off the loop, in the poller.
func (f *watchFollower) pollLocal(snap *watch.Snapshot, due map[string]bool) []watch.Event {
	a := f.app
	every := a.localAskEvery
	if every == 0 {
		every = localAskEvery
	}
	var events []watch.Event
	for _, w := range f.watches {
		if !w.Local() {
			continue
		}
		key := w.Key()
		if _, read := snap.States[key]; read && !due[key] && time.Since(f.asked[key]) < every {
			continue
		}
		f.asked[key] = time.Now()
		before := snap.States[key]
		after := before
		after.Read, after.Error = time.Now(), ""
		refs, err := gitx.LocalRefs(w.Dir)
		if err != nil {
			after.Error = "the clone cannot be read at " + tildePath(w.Dir)
			snap.States[key] = after
			continue
		}
		// A watch carried over from before the note, or a note removed by
		// hand, is marked again.
		gitx.Follow(w.Dir, true)
		after.Heads, after.Tags = 0, 0
		for ref := range refs {
			if strings.HasPrefix(ref, "refs/tags/") {
				after.Tags++
			} else {
				after.Heads++
			}
		}
		// A command of unagit's moving refs this instant may have moved one
		// the note does not name yet: look again next time.
		if gitx.Busy(w.Dir) {
			delete(f.asked, key)
			continue
		}
		seen, known := f.store.LocalRefs(key)
		var evs []watch.Event
		if known {
			evs = localChanges(w, seen, refs, gitx.OwnRefs(w.Dir), func(from, to string) (int, string, string) {
				n, by, subject, err := gitx.NewCommits(w.Dir, from, to)
				if err != nil {
					return 0, "", ""
				}
				return n, by, subject
			})
		}
		if !known || len(evs) > 0 || !sameRefs(seen, refs) {
			f.store.WriteLocalRefs(key, refs)
		}
		if len(evs) > 0 {
			after.Changed, after.Seq = time.Now(), snap.Seq+1
		} else if before.Changed.IsZero() {
			after.Changed = time.Now()
		}
		snap.States[key] = after
		events = append(events, evs...)
	}
	return events
}

func sameRefs(x, y map[string]string) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if y[k] != v {
			return false
		}
	}
	return true
}

// localChanges are the events of a clone's refs moving from before to
// after: new commits on a branch, a branch or a tag made or deleted. A move
// unagit made itself - noted in own as where it left the ref - is not news.
// A branch moved back or sideways onto nothing new says nothing.
func localChanges(w watch.Watch, before, after, own map[string]string, commits func(from, to string) (n int, by, subject string)) []watch.Event {
	var refs []string
	for ref := range before {
		refs = append(refs, ref)
	}
	for ref := range after {
		if _, ok := before[ref]; !ok {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	var out []watch.Event
	add := func(heading, line string) {
		out = append(out, watch.Event{Key: w.Key(), What: w.Label(), Heading: heading, Line: line,
			Project: w.Project, Level: watch.Info, At: time.Now(), Dir: w.Dir, Quiet: true})
	}
	for _, ref := range refs {
		was, had := before[ref]
		now, has := after[ref]
		if was == now && had == has {
			continue
		}
		if mine, ok := own[ref]; ok && mine == now {
			continue
		}
		tag, isTag := strings.CutPrefix(ref, "refs/tags/")
		branch := strings.TrimPrefix(ref, "refs/heads/")
		switch {
		case isTag && !had:
			add("Tag created", "Tag "+tag+" made at "+shortSHA(now))
		case isTag && !has:
			add("Tag deleted", "Tag "+tag+" deleted")
		case isTag:
			add("Tag moved", "Tag "+tag+" moved to "+shortSHA(now))
		case !had:
			add("Branch created", "Branch "+branch+" made at "+shortSHA(now))
		case !has:
			add("Branch deleted", "Branch "+branch+" deleted")
		default:
			n, by, subject := commits(was, now)
			if n == 0 {
				continue
			}
			heading, line := "New commit", "New commit on "+branch
			if n > 1 {
				heading, line = "New commits", fmt.Sprintf("%d new commits on %s", n, branch)
			}
			if by != "" {
				line += " by " + by
			}
			if subject != "" {
				line += ": " + subject
			}
			add(heading, line)
		}
	}
	return out
}
