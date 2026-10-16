# chezmoi

[chezmoi](https://www.chezmoi.io) clones your dotfiles repository into a
directory of its own and applies it from there, so a second clone of it under
unagit's root would only drift apart from the first.

With `chezmoi` on `PATH` the integration is on. At every start unagit asks
chezmoi for its working tree and origin and pairs that with a repository of the
list.

## What changes

That repository wears a **↗ Managed by Chezmoi** badge, first in the tags
column. It is not a tag: you cannot put it on or take it off, it is never
written to the configuration, and hiding the tags keeps it.

- Opening the repository opens chezmoi's checkout, which counts as cloned.
- Its branch and review worktrees and its merge requests work as usual, under
  the root where unagit would otherwise have cloned it, not beside chezmoi's
  directory.

## What is left alone

chezmoi's checkout stays chezmoi's:

- deleting the repository deletes only its worktrees;
- switching a server's clone protocol leaves its remote alone;
- `e` cannot move it.

The pairing is found at every start and held in memory only, never in the
configuration, since that travels between machines and would carry one
machine's path.

Turn the integration off with `e` in its card to clone the repository like any
other.

## Keeping unagit's configuration in chezmoi

`config.yaml` holds your choices alone and changes only when you change a
setting, so it is safe to version with chezmoi. Everything else unagit keeps,
the indexes, `state.json`, sessions and watches, is this machine's. See
[Files on Disk](/reference/files).
