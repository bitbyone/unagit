# Developing unagit

```sh
make build            # the unagit binary
make test-fast        # go test ./...
make test             # the same with the race detector
make integration      # in Docker: the suite on Linux, then the real programs
```

The tests need **Go 1.26+ and git 2.38+**, and nothing else: no editor, no
multiplexer, no chezmoi, zoxide or incomm, no git identity or configuration of
yours. Every test package runs in an environment of its own: a temporary home,
a git configuration with an identity of its own, and a `PATH` holding git, go
and the shell's utilities alone. What is installed on the machine changes
nothing, and the tests change nothing on it.

`make integration` runs what needs the real programs, Neovim, incomm, Zellij
and herdr, in a Docker image that has them at pinned versions. It needs Docker
and nothing more.

The internals, and the mistakes already made, are in
[AGENTS.md](https://github.com/bitbyone/unagit/blob/main/AGENTS.md). The test
commands and their measured costs are in
[docs/testing.md](https://github.com/bitbyone/unagit/blob/main/docs/testing.md).

## The demo world

```sh
make demo         # the real unagit over made-up data; the passphrase is demo
make shots        # the website's screenshots, taken of the same world
```

`website/shots/` lays out a world of its own under `$TMPDIR/unagit-demo`: a
configuration with two servers, their repositories and merge requests as
the lists cache them, a vault sealed with `demo`, real clones with branches
and uncommitted changes, and a unibox stand-in with a few sandboxes. Its
`serve` answers on `127.0.0.1:8929` as both servers' GitLab would - a merge
request's detail, commits, threads and approvals - so nothing leaves the
machine, and `HOME` and unagit's configuration are the world's, not yours.
It needs Go and git.

`make shots` drives that unagit in a pseudo-terminal (`shoot.py`, Python's
standard library), replays what it wrote through xterm.js's emulator and
draws each screen as an SVG (`render.mjs`) into `website/public/shots/`:
the terminal's own cells, no browser and no screenshot tool. A new shot is
a few keys in `shoot.py`; the mock data is `mock.go`.

## This website

The site is [VitePress](https://vitepress.dev), in `website/` of the same
repository, and is published to GitHub Pages on every push to `main` that
changes it.

```sh
cd website
npm ci
npm run dev       # http://localhost:5173/unagit/
npm run build     # into website/.vitepress/dist
```

A page is a Markdown file under `guide/`, `tutorials/` or `reference/`, and the
sidebar is in `.vitepress/config.mts`.

## Releases

A release is a tag `vX.Y` on a commit of `main`. The release workflow waits for
that commit's tests to have passed, builds the tag, checks that
`unagit --version` prints `X.Y`, and only then points the Homebrew formula at
it. Homebrew builds from source; nothing compiled is published.
