package ui

import (
	"path/filepath"
	"time"

	"github.com/tobola/unagit/internal/unibox"
)

// A task of a sandbox can mount a clone of the host's: its agent works on
// those very files. Deleting such a clone takes the agent's work with it,
// and unibox cannot warn, since it is not asked - so the question that
// asks before deleting says so, read from unibox at that moment.

// mountWarnings hands then the warnings for dir: a sentence for each task
// that mounts it or something under it. With unibox off, or no dir, there
// is nothing to say; a list read moments ago is used as it is, otherwise
// unibox is asked first.
func (a *App) mountWarnings(dir string, then func([]string)) {
	if dir == "" || !a.uniboxOn() {
		then(nil)
		return
	}
	if a.ubx.read && a.ubx.err == nil && time.Since(a.ubx.at) <= sandboxesStale {
		then(mountedWarnings(a.ubx.overview.Sandboxes, dir))
		return
	}
	c, err := a.uniboxClient()
	if err != nil {
		then(nil)
		return
	}
	var boxes []unibox.Sandbox
	var readErr error
	a.loadThen("Asking unibox what its sandboxes mount", func(func(string)) (string, error) {
		// A failure is said in the question, not instead of it: the
		// person still decides.
		boxes, readErr = c.Sandboxes()
		return "", nil
	}, func(string) {
		if readErr != nil {
			then([]string{"unibox could not say whether a sandbox mounts it: " + readErr.Error()})
			return
		}
		then(mountedWarnings(boxes, dir))
	})
}

// mountedWarnings are the sentences for the tasks that mount dir or a
// directory under it, those with a session of their agent open first.
func mountedWarnings(boxes []unibox.Sandbox, dir string) []string {
	var active, idle []string
	for _, b := range boxes {
		for _, t := range b.Tasks {
			if !mountsUnder(t.HostPaths(), dir) {
				continue
			}
			if _, at := b.RunOf(t.ID); at {
				active = append(active, "task "+t.Name()+" in sandbox "+b.Name+" mounts it, and a session of its agent is open now")
				continue
			}
			idle = append(idle, "task "+t.Name()+" in sandbox "+b.Name+" mounts it; unibox task rm "+t.Name()+" unmounts it first")
		}
	}
	return append(active, idle...)
}

// mountsUnder says whether any of paths is dir or lies under it, through
// links: unibox names a mount as it was given, unagit a clone as it is
// configured.
func mountsUnder(paths []string, dir string) bool {
	for _, p := range paths {
		if underDirectory(resolved(p), resolved(dir)) {
			return true
		}
	}
	return false
}

// resolved is path with its links followed as far as it exists: a mount
// under a clone may be gone while the clone, under a linked temporary
// directory, still has to compare equal to it.
func resolved(path string) string {
	rest := ""
	for dir := filepath.Clean(path); ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if parent := filepath.Dir(dir); parent == dir {
			return filepath.Clean(path)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}
