# tmux and zellij

A plan, not yet built. A terminal editor takes unagit's terminal: the TUI is
suspended until the editor exits, one editor at a time. Inside tmux or
zellij there is a better place for it - a window (tmux) or tab (zellij) of
its own, named after what is open, with unagit still running in its own.

## Decisions

- **Only when unagit itself runs inside one.** `$TMUX` says tmux, `$ZELLIJ`
  zellij. Outside them nothing changes.
- **A setting, not a second set of keys.** Settings › Integrations › Terminal
  multiplexer: terminal editors open *here* (today's behaviour) or *in a new
  window*. Default: a new window, when inside a multiplexer - that is what
  someone running unagit in tmux wants nine times in ten. `Alt-O` and the
  other Alt keys keep choosing the editor; the editor picker lists each
  terminal editor twice while inside a multiplexer, "Neovim" and "Neovim ·
  new window", so the other way is one choice away.
- **One window per directory.** The window is named after the row -
  `api-gateway !42`, `api-gateway · feat/rate`, the group's folder - and
  `Ctrl-O` on something whose window is still there goes to it instead of
  starting a second editor on the same files. Two Neovims on one review
  worktree fight over swap files.
- **Window editors are untouched.** IDEA, VS Code and Zed open their own
  windows as now.

## How it goes

**tmux.**

```
tmux new-window -P -F '#{pane_id}' -c <dir> -n <name> -- <editor command>
```

prints the new pane's id. Going to an existing one is `tmux select-window -t
<pane id>`. Whether the pane still lives is `tmux list-panes -a -F
'#{pane_id}'`, which is cheap.

**zellij.** zellij's CLI has no "new tab running a command and tell me its
id". `zellij action new-tab --cwd <dir> --name <name> --layout <file>` with a
generated layout (`tab { pane command="nvim" cwd="<dir>" { args ... } }`) does
the first part; going back is `zellij action go-to-tab-name <name>`; whether
it still lives is `zellij action query-tab-names`. To be checked against the
installed zellij (the actions have changed between 0.40 and 0.43) before the
code is written.

**Sessions.** The record (`internal/session`) gains the multiplexer and the
pane id or tab name. A record with a pane is alive while the pane is, not
while the pid that wrote it is - the same change the
[background editors](background-editors.md) plan makes for a socket, and the
two should share it. So `unagit cd` and `unagit sessions` know an editor in
a tmux window even after the unagit that started it has quit.

**Zoxide.** A directory opened in a window is a visit like any other
([zoxide.md](zoxide.md)).

**Open Shell.** The same mechanism without an editor: "Open Shell" (`S`,
free on every list) opens a new window with the user's `$SHELL` in the row's
directory. Outside a multiplexer it suspends the TUI and runs the shell,
which is `unagit cd` without leaving unagit. Cheap, and it is the thing one
most often reaches for after `Ctrl-O`.

## How it relates to background editors

With a multiplexer, the background editors plan is mostly unnecessary - the
editors already run side by side, and the multiplexer is the list of them.
Build this first; the Neovim `:detach` work then matters only outside one.
The "a window per directory" rule and the session record with a liveness
check of its own are the parts both plans want.

## Where the code changes

| Place | Change |
| --- | --- |
| `internal/mux` (new) | detect tmux or zellij; open a window in a directory with a command and a name; find a window by name; tell whether a pane lives. One interface, two implementations. |
| `internal/session` | `Record.Mux`, `Record.Pane`; liveness by pane when set. |
| `internal/ui/editors.go` | a terminal editor goes to a window when the setting says so; an existing window is selected instead of opening another. |
| `internal/ui/editors.go`, `withEditor` | the "· new window" rows. |
| `internal/config` | `Integrations.Multiplexer` - `here` or `window`, unset meaning window inside one. |
| `internal/ui/integrations.go` | the card, its `found` line naming what unagit runs in. |
| `internal/ui/actions_lists.go`, `wtmodal.go` | "Open Shell". |
| README, `help.go` | the paragraph and the key. |

## Tests

- A fake `tmux` on `PATH` (serial, with `TMUX` set) that records its
  arguments and answers `list-panes` from a file the test writes:
  `Ctrl-O` opens a window named after the row in its directory; a second
  `Ctrl-O` selects it; once the pane is gone from `list-panes` it opens a
  new one; the setting `here` suspends as today.
- The same for zellij once its commands are settled.
- `internal/session`: a record with a living pane outlives its pid; a dead
  pane is swept.

## Open details

- WezTerm (`wezterm cli spawn --cwd`) and kitty (`kitty @ launch --type=tab
  --cwd`, which needs remote control on) fit the same interface. Not in the
  first change, but `internal/mux` should not assume a multiplexer is tmux
  or zellij.
- Whether a window closes with its editor (tmux's default) or drops to a
  shell. Closing is the default; a shell would need `<editor>; exec $SHELL`.
