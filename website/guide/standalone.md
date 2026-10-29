# One Repository Alone

`unagit -s` works on the repository of a directory and nothing else: no
server, no passphrase, no lists. That makes it a git interface for any
repository, one unagit never cloned too.

```sh
unagit -s            # the repository you are in
unagit -s ~/src/foo  # or the one of that directory
```

## The screen

![unagit -s on a repository: the changes not committed, one file's diff](/shots/standalone.svg)

The top line is the repository: its name, the branch checked out, where
that stands against origin, what is not committed, and where it is on disk.
Under it are four tabs, reached with `1` to `4`. Each is the dialog the
full unagit opens over its lists, made a screen: the same keys, the same
actions.

| Tab | What it is |
| --- | --- |
| `1` Changes | the files not committed and their diffs, as in [Changes and Commits](/guide/changes) |
| `2` Log | the history, as in [The Commit Log](/guide/commit-log) |
| `3` Branches | local branches and origin's as last fetched, as in [Branches](/guide/branches) |
| `4` Shelf | what was put aside, as in [The Shelf](/guide/shelf) |

A tab is never closed: Esc clears marks and filters, and `q` quits.

## What works from everywhere

Everything unagit does with a worktree works from every tab:

| Key | Does |
| --- | --- |
| `p` / `P` | pull · push |
| `C` | commit the changes to versioned files |
| `f` | fetch origin and read everything again |
| `R` | read the repository again |
| `Ctrl-O` / `Alt-O` | open in the editor · in one you choose |
| `D` / `Alt-D` | in Hunk: what is not committed · everything since the base |
| `Ctrl-R` | rebase onto the base |
| `:` | every action of the tab and of the repository |

A key the tab uses for itself is the tab's: on the Log, `P` pushes up to the
commit under the cursor, and the repository's Push is still in `:`, without
a key.

## What it leaves out

Whatever needs a server: merge requests, pipelines, watches, a commit's
page in the browser. Branches on origin are what the last fetch left, and
deleting one there is a `git push --delete`.

git runs with your own credentials: without a token of unagit's to hand it,
your credential helpers and ssh agent answer as they do in a terminal.
unagit's configuration is only read, for the theme and the editors, and
nothing of unagit's is written.
