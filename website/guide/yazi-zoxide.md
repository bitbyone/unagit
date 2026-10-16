# Yazi and zoxide

## Yazi

With [`yazi`](https://yazi-rs.github.io) on `PATH`, **Browse Files** appears in
the selection action picker on repositories, merge requests, worktrees and the
lit block of the worktree view. Settings › Integrations › Yazi `e` turns it
off.

A repository is cloned if needed. A merge request opens its review worktree
first, its branch worktree otherwise, or prepares a review when neither exists.
Groups open their folder.

Yazi gets the terminal until you leave. Choosing a file opens it in the default
editor with the repository as its working directory. An existing Neovim server
opens the file in a new tab, keeping unsaved buffers. Quitting without choosing
a file returns to unagit.

### From Yazi into unagit

The `unagit.yazi` plugin goes the other way: jump from Yazi to any unagit
directory, or only to open sessions. It requires Yazi 26.9.1 or newer.

```sh
ya pkg add bitbyone/unagit:unagit
```

Then, in `keymap.toml`:

```toml
[mgr]
prepend_keymap = [
  { on = ["g", "u"], run = "plugin unagit" },
]
```

`plugin unagit -- sessions` chooses from open sessions only.

## zoxide

With [`zoxide`](https://github.com/ajeetdsouza/zoxide) on `PATH` the
integration is on automatically; `e` in its card turns it off.

**What counts as a visit:** opening an editor, returning to a running editor,
or entering a directory with `unagit go` or `unagit cd`, `--print` included.
Making a clone or worktree alone records nothing. Removing one forgets its
directory. zoxide's own `_ZO_EXCLUDE_DIRS` rules apply, and a missing or
failing zoxide never prevents an editor or shell from opening.

**Ordering by frecency:** choose **by frecency** in `o` in Repositories or
Worktrees for frequent and recent visits first. A repository takes the highest
score of its clone and worktrees. `unagit go` offers the most visited
directories first.

With both integrations on, Yazi's starting directory and a different final
directory count as visits. For the visits in between, put this in Yazi's
`init.lua`:

```lua
require("zoxide"):setup { update_db = true }
```
