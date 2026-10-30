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
make demo-fresh   # the same world, unagit not set up yet: add its servers yourself
make shots        # the website's screenshots and the walk its home page plays
make video        # that walk as unagit.mp4, for where the page's player cannot go
```

`website/shots/` lays out a world of its own under `$TMPDIR/unagit-demo`:
two GitLab servers, `gitlab.northwind.dev` and `gitlab.com`, with their
groups, fifteen repositories, pipelines and merge requests; real clones
with branches and uncommitted changes; a vault sealed with `demo`; a unibox
stand-in with a few sandboxes; and, but for `demo-fresh`, a configuration
of unagit's with the servers added and the lists filled in. Its `serve`
answers as both servers would: it is the HTTPS proxy unagit is pointed at,
with a certificate of the world's own authority, and it keeps what is done
to it - a job run again runs for a few seconds and passes, a push to a
clone's origin starts a pipeline, a comment, an approval or a new merge
request stays. Nothing leaves the machine, and `HOME` and unagit's
configuration are the world's, not yours. It needs Go and git.

`make shots` drives that unagit in a pseudo-terminal (`shoot.py`, Python's
standard library), replays what it wrote through xterm.js's emulator and
draws each screen as an SVG (`render.mjs`, `term-svg.mjs`) into
`website/public/shots/`: the terminal's own cells, no browser and no
screenshot tool. It then records the home page's walk (`record.py`) in the
fresh world: the servers and groups added, a repository starred, a failed
job's log read and the job run again, its branch watched, a merge request
answered and approved, changes committed and pushed and a merge request
opened, the watch's news as it comes - kept as an asciicast whose markers
are the captions, squeezed to the cells that changed (`squeeze.mjs`);
asciinema-player plays it on the page in a loop. `make video` renders the
same walk frame by frame into an MP4 (`video.mjs`, ffmpeg). A new shot or
scene is a few keys in `shoot.py` or `record.py`; the mock data is
`mock.go`, the servers `serve.go`. The tools are
`website/shots/package.json`'s, apart from the site's own. The terminal's
font on the website - the player's, the screenshots' (each carries what it
shows of it) and the video's - is unagit mono: JetBrains Mono with Nerd
Font's icons, cut down to what unagit draws, its box lines drawn edge to
edge of the cell as a terminal draws them. `fonts.py` makes it (`pip
install fonttools brotli`); run it again when unagit draws a new icon.

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
