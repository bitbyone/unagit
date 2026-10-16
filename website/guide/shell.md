# Following into a Shell

Opening an editor does not end unagit: it suspends itself and waits, so for as
long as something is open it knows where. Another terminal can go there.

## unagit cd

```sh
unagit cd             # asks which, when more than one is open
unagit cd calling     # a search narrows it; one match needs no asking
unagit sessions       # what is open, including editors left aside
unagit attach calling # return to a Neovim left aside
```

`unagit cd` starts a shell in that directory, and leaving it puts you back, the
way `chezmoi cd` does. A process cannot change its parent's directory, so to
`cd` in the shell you are in, use `--print`:

```sh
ug() { cd "$(unagit cd --print "$@")" || return; }
```

## unagit go

`unagit go` does the same for anything on disk, open or not: a clone, a merge
request's review or branch worktree, another worktree, a grouped worktree. It
needs no passphrase: it reads unagit's index and the disk.

```sh
unagit go                  # everything on disk; / narrows the list
unagit go gateway review   # every word has to match; one match needs no asking
unagit go --print '!42'    # the path alone
```

```sh
ugo() { cd "$(unagit go --print "$@")"; }
```

With the [zoxide](/guide/yazi-zoxide) integration on, `unagit go` offers the
most visited directories first, and entering a directory records a visit.

## From a link

A link pasted from chat or email goes straight there:

```sh
unagit review https://gitlab.example.com/group/app/-/merge_requests/12
unagit open   https://github.com/owner/repo/pull/12     # the branch worktree
```

It asks for the passphrase as usual, opens the editor, and leaves you in the
lists when you close it. The merge request does not have to be in one of your
groups.
