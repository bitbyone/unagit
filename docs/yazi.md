# Yazi

Implemented in unagit and `contrib/yazi/unagit.yazi`. The first half opens
Yazi on a directory from unagit; the second lets Yazi jump to a directory
unagit knows. Verified with Yazi 26.9.1.

## Yazi from unagit

**Browse Files** has no key and lives in the selection action picker
(`Alt-Enter` or `Ctrl-A`). Repositories use their clone, cloning first if
needed. Merge requests prefer an existing review, then a branch worktree,
and prepare a review if neither exists. Worktrees and the lit block of the
worktree view use their directory; a group uses its folder.

Yazi is an integration, not a favourite editor. Its Settings › Integrations
card is on when the binary is found, with `e` to toggle and `c` to check.

`runInTerminal` shares the terminal lifecycle with terminal editors: record
the original directory in sessions, add a zoxide visit, suspend the UI,
run the child, remove its temporary session and refresh the disk. Neovim's
persistent server records survive detachment as before.

The command is:

```
yazi --cwd-file <tmp>/cwd --chooser-file <tmp>/chosen <dir>
```

Both result files live in a fresh 0700 temporary directory that is removed
on return, including on failure. A different final cwd records another
zoxide visit when that integration is enabled. Choosing a file opens the
favourite editor with the original directory as its working directory and
review context. Without a usable favourite, the shared editor picker asks.
Quitting without a file returns to unagit. Multiple selections open the
first file.

`Editor.CommandAt` supplies the file to Neovim and custom editors, the
folder and file to VS Code and Zed, and the folder plus `--line 1` and file
to IDEA. A macOS application without a launcher gets folder and file through
`open -a`. An already running Neovim opens the file through RPC in a new tab
before attaching, keeping unsaved buffers.

Yazi's built-in zoxide plugin only records intermediate visits when
`require("zoxide"):setup { update_db = true }` is in `init.lua`; this remains
opt-in in Yazi 26.9.1. The card searches that file for a hint, without running
Lua. It follows `YAZI_CONFIG_HOME`, then `XDG_CONFIG_HOME/yazi`, then
`~/.config/yazi`. It does not edit the configuration.

## unagit from Yazi

The plugin hides Yazi, runs `unagit go --print` with inherited terminal
input and stderr and captured stdout, restores Yazi, then emits a literal
`cd` to the chosen path. `plugin unagit -- sessions` uses `unagit cd --print`.
Canceling leaves the directory alone. Only the final newline is stripped,
so spaces remain part of the path.

Install with `ya pkg add bitbyone/unagit:unagit`; file links in the root
`unagit.yazi` expose the sources to Yazi's package manager. The plugin's
[README](../contrib/yazi/unagit.yazi/README.md) gives installation and keymaps.

## Validation

Fake-tool tests cover file handoff, zoxide visits, private temporary files,
session lifetime, cancellation, disabling, cloning and review preparation,
review/branch preference and reuse of a running Neovim. Editor tests cover
every launcher and macOS application command. The Yazi card is rendered at
several terminal sizes and checked for legibility. Real Neovim verifies that
opening a file with spaces and quotes keeps previous unsaved edits.

The plugin and Yazi's chooser are exercised manually in a real Yazi with
isolated configuration, including both pickers, cancellation and a directory
whose name ends in a space. Yazi is not needed by the automated test suite.

## Later

A linemode or fetcher could mark a directory unagit made with its review
metadata, read from `git config --worktree unagit.mr.iid` and
`unagit.mr.mode`. It costs a git process per directory shown and would need
Yazi's fetcher caching; it is outside this integration.
