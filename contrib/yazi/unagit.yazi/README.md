# unagit.yazi

Jump from Yazi to a clone, worktree, review or grouped worktree unagit knows
on disk, or to a directory currently open in an editor. The picker uses the
terminal and hands it back to Yazi when you choose or cancel.

Requires Yazi 26.9.1 or newer and `unagit` on PATH. Install:

```sh
ya pkg add bitbyone/unagit:unagit
```

The repository's root `unagit.yazi` contains file links to this plugin for
Yazi's package manager. To install from a local checkout instead, copy this
folder to `~/.config/yazi/plugins/unagit.yazi/`.

Add to `keymap.toml` (under `$YAZI_CONFIG_HOME`, `$XDG_CONFIG_HOME/yazi`, or
`~/.config/yazi`):

```toml
[[mgr.prepend_keymap]]
on = ["g", "u"]
run = "plugin unagit"
desc = "Jump to an unagit directory"

[[mgr.prepend_keymap]]
on = ["g", "s"]
run = "plugin unagit -- sessions"
desc = "Jump to an open editor session"
```

The first calls `unagit go --print`, the second `unagit cd --print`.
Canceling keeps the current directory. Directory names retain their spaces
and are passed literally to Yazi.

To record intermediate visits in Yazi's own zoxide integration, add this to
`init.lua`:

```lua
require("zoxide"):setup { update_db = true }
```

Unagit records the initial directory and a different final directory when it
launches Yazi, if its zoxide integration is enabled. This option lets Yazi
also record the directories you visit between them.
