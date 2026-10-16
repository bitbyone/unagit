# Command Line

`unagit` with no command starts the interface. The commands below work from
any shell.

## unagit

```sh
unagit           # the interface
unagit --debug   # with Settings › Debug, to fire toasts and notifications by hand
unagit --version # the release it was built from
```

## unagit review, unagit open

```sh
unagit review <link>   # a merge request's review worktree, in your editor
unagit open <link>     # its branch worktree
```

A link pasted from chat or email: a GitLab merge request or a GitHub pull
request. It asks for the passphrase, opens the editor, and leaves you in the
lists when you close it. The merge request does not have to be in one of your
groups.

## unagit go

```sh
unagit go [search]
unagit go --print [search]
```

Opens a shell in anything unagit has on disk: a clone, a merge request's review
or branch worktree, another worktree, a grouped worktree. Every word of the
search has to match; one match needs no asking. It needs no passphrase.

`--print` (`-p`) writes the path alone instead of starting a shell, for a
function of your own:

```sh
ugo() { cd "$(unagit go --print "$@")"; }
```

## unagit cd

```sh
unagit cd [search]
unagit cd --print [search]
```

The same, for what is open in an editor right now, including editors left
aside. Leaving the shell puts you back, the way `chezmoi cd` does.

```sh
ug() { cd "$(unagit cd --print "$@")" || return; }
```

## unagit sessions

```sh
unagit sessions
```

Lists what unagit has open in an editor, including Zellij and herdr panes and
editors left aside.

## unagit attach

```sh
unagit attach [query]
```

Returns to a running Neovim, including one left by another unagit, without
opening the interface.

## unagit where

```sh
unagit where
```

Prints the configuration paths.

## unagit version

```sh
unagit version
```

Prints the version, the same line as `unagit --version`. A build that is not a
release prints `dev` and its commit.

## Environment

| Variable | Does |
| --- | --- |
| `UNAGIT_CONFIG_DIR` | where unagit keeps its files, instead of `~/.config/unagit` |

No token, passphrase or other secret is ever read from the environment, a flag
or a pipe.
