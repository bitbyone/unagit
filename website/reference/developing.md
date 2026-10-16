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
