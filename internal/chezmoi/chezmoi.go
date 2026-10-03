// Package chezmoi finds the repository chezmoi keeps the user's dotfiles in.
// chezmoi clones it into a directory of its own and applies it from there, so
// unagit opens that checkout instead of cloning the repository a second time.
package chezmoi

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Checkout is chezmoi's clone: the top of its working tree and where its
// origin points.
type Checkout struct {
	Dir    string
	Origin string
}

// timeout keeps a chezmoi that waits for something - a template prompting,
// a locked state file - from holding the start of unagit.
const timeout = 5 * time.Second

// Find asks chezmoi for its working tree and the origin of it. It goes
// through `chezmoi git`, which runs in the working tree rather than the
// source directory - with a .chezmoiroot the two differ, and the repository
// is the former. A checkout without an origin is not found: there is nothing
// on a server to pair it with.
func Find(bin string) (Checkout, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	run := func(args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, bin, append([]string{"--no-tty", "git", "--"}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	dir, err := run("rev-parse", "--show-toplevel")
	if err != nil || dir == "" {
		return Checkout{}, errors.New("chezmoi has no repository yet - run chezmoi init")
	}
	origin, err := run("remote", "get-url", "origin")
	if err != nil || origin == "" {
		return Checkout{}, errors.New("chezmoi's repository has no origin to pair with a server")
	}
	return Checkout{Dir: dir, Origin: origin}, nil
}

// Same says whether two remote addresses name one repository, whatever the
// protocol: https://host/a/b.git, git@host:a/b and ssh://git@host:22/a/b are
// all host/a/b. Hosts and paths are compared without case, as the forges do.
func Same(a, b string) bool {
	ka, kb := Key(a), Key(b)
	return ka != "" && ka == kb
}

// Key is the host and path an address points at, lower case, without a
// user, a port or a .git ending.
func Key(raw string) string {
	raw = strings.TrimSpace(raw)
	var host, path string
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		host, path = u.Hostname(), u.Path
	} else if at, rest, ok := strings.Cut(raw, ":"); ok && !strings.Contains(at, "/") {
		// scp-like: [user@]host:path
		if i := strings.LastIndex(at, "@"); i >= 0 {
			at = at[i+1:]
		}
		host, path = at, rest
	} else {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host + "/" + path)
}
